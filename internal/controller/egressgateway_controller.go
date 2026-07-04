/*
Copyright 2026.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	egressv1alpha1 "github/stefanlievers/cilium-egress-operator/api/v1alpha1"
)

const (
	// labelEgressNode markeert de node die als egress gateway fungeert
	labelEgressNode = "egress-node"
	// labelControlPlane is het standaard Kubernetes control plane label
	labelControlPlane = "node-role.kubernetes.io/control-plane"
	labelValueTrue    = "true"
)

// pinnerLabels zijn de selector labels van de pinner DaemonSet voor een gateway
func pinnerLabels(eg *egressv1alpha1.EgressGateway) map[string]string {
	return map[string]string{
		"app":            "egress-ip-pinner",
		"egress-gateway": eg.Name,
	}
}

// EgressGatewayReconciler reconciles a EgressGateway object
type EgressGatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=egress.cilium-egress-operator.io,resources=egressgateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=egress.cilium-egress-operator.io,resources=egressgateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=egress.cilium-egress-operator.io,resources=egressgateways/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete

func (r *EgressGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	eg := &egressv1alpha1.EgressGateway{}
	if err := r.Get(ctx, req.NamespacedName, eg); err != nil {
		if errors.IsNotFound(err) {
			log.Info("EgressGateway niet gevonden, waarschijnlijk verwijderd")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Fout bij ophalen EgressGateway")
		return ctrl.Result{}, err
	}

	log.Info("Reconciling EgressGateway",
		"name", eg.Name,
		"egressIP", eg.Spec.EgressIP,
	)

	// Stap 1: zorg dat er een egress node is
	egressNode, err := r.reconcileEgressNode(ctx)
	if err != nil {
		log.Error(err, "Fout bij reconcilen egress node")
		return ctrl.Result{}, err
	}

	// Stap 2: zorg dat de IP pinner DaemonSet bestaat
	ds, err := r.reconcileDaemonSet(ctx, eg)
	if err != nil {
		log.Error(err, "Fout bij reconcilen DaemonSet")
		return ctrl.Result{}, err
	}

	// Stap 3: status terugschrijven
	if err := r.updateStatus(ctx, eg, egressNode, ds); err != nil {
		log.Error(err, "Fout bij updaten status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *EgressGatewayReconciler) reconcileEgressNode(ctx context.Context) (string, error) {
	log := logf.FromContext(ctx)

	egressNodes := &corev1.NodeList{}
	if err := r.List(ctx, egressNodes, client.MatchingLabels{
		labelEgressNode: labelValueTrue,
	}); err != nil {
		return "", err
	}

	if len(egressNodes.Items) > 0 {
		nodeName := egressNodes.Items[0].Name
		log.Info("Egress node gevonden, niets te doen", "node", nodeName)
		return nodeName, nil
	}

	log.Info("Geen egress node gevonden, control plane nodes zoeken")

	controlPlaneNodes := &corev1.NodeList{}
	if err := r.List(ctx, controlPlaneNodes, client.MatchingLabels{
		labelControlPlane: "",
	}); err != nil {
		return "", err
	}

	if len(controlPlaneNodes.Items) == 0 {
		log.Info("Geen control plane nodes gevonden, wachten")
		return "", nil
	}

	slices.SortFunc(controlPlaneNodes.Items, func(a, b corev1.Node) int {
		return strings.Compare(a.Name, b.Name)
	})

	target := &controlPlaneNodes.Items[0]
	log.Info("Labelen van egress node", "node", target.Name)

	patch := client.MergeFrom(target.DeepCopy())
	if target.Labels == nil {
		target.Labels = map[string]string{}
	}
	target.Labels[labelEgressNode] = labelValueTrue
	if err := r.Patch(ctx, target, patch); err != nil {
		return "", err
	}

	log.Info("Egress node succesvol gelabeld", "node", target.Name)
	return target.Name, nil
}

// reconcileDaemonSet zorgt dat de IP pinner DaemonSet bestaat en up-to-date is.
// De teruggegeven DaemonSet bevat de actuele status (voor egressIPConfirmed).
func (r *EgressGatewayReconciler) reconcileDaemonSet(ctx context.Context, eg *egressv1alpha1.EgressGateway) (*appsv1.DaemonSet, error) {
	log := logf.FromContext(ctx)

	dsName := fmt.Sprintf("egress-ip-pinner-%s", eg.Name)
	desired := r.buildDaemonSet(eg, dsName)

	if err := ctrl.SetControllerReference(eg, desired, r.Scheme); err != nil {
		return nil, err
	}

	existing := &appsv1.DaemonSet{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      dsName,
		Namespace: eg.Namespace,
	}, existing)

	if errors.IsNotFound(err) {
		log.Info("DaemonSet aanmaken", "daemonset", dsName)
		return desired, r.Create(ctx, desired)
	}
	if err != nil {
		return nil, err
	}

	log.Info("DaemonSet updaten", "daemonset", dsName)
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	return existing, r.Patch(ctx, existing, patch)
}

// buildPinnerScript bouwt het shell script voor de IP pinner container.
// Alle geïnterpoleerde waarden zijn door CRD validatiepatronen beperkt tot
// veilige tekens (IP's, CIDR's, interface namen) — geen shell injectie mogelijk.
func buildPinnerScript(eg *egressv1alpha1.EgressGateway, iface string) string {
	var routeLines strings.Builder
	if eg.Spec.CreateRoutes {
		for _, dest := range eg.Spec.Destinations {
			fmt.Fprintf(&routeLines, "  ensure_route %q %q\n", dest.CIDR, dest.NextHop)
		}
	}

	return fmt.Sprintf(`
set -eu
IFACE=%q
IP=%q

# Probeer iproute2 te installeren voor event-driven bewaking via ip monitor.
# Faalt stil in air-gapped omgevingen; dan periodieke controle als terugval.
command -v apk >/dev/null 2>&1 && apk add --no-cache iproute2 >/dev/null 2>&1 || true

ensure_ip() {
  ip addr show dev "$IFACE" | grep -q "inet $IP/32" || {
    echo "Egress IP $IP toevoegen aan $IFACE"
    ip addr add "$IP/32" dev "$IFACE" || true
  }
}

ensure_route() {
  CIDR=$1
  VIA=$2
  # Geen expliciete next-hop: de huidige default gateway van de node volgen
  if [ -z "$VIA" ]; then
    VIA=$(ip route show default 2>/dev/null | sed -n 's/.* via \([0-9.]*\).*/\1/p' | head -n 1)
  fi
  if [ -z "$VIA" ]; then
    echo "Geen next-hop bekend voor $CIDR (geen default gateway gevonden), route overgeslagen"
    return 0
  fi
  ip route show | grep -q "^$CIDR via $VIA dev $IFACE" || {
    echo "Route $CIDR via $VIA instellen op $IFACE"
    ip route del "$CIDR" 2>/dev/null || true
    ip route add "$CIDR" via "$VIA" dev "$IFACE" src "$IP" || true
  }
}

apply() {
  ensure_ip
%s}

apply
echo "Egress IP $IP actief op $IFACE"

# Bewaken: event-driven met ip monitor (iproute2), anders periodiek (busybox)
if ip -V 2>/dev/null | grep -qi iproute2; then
  echo "Bewaken via ip monitor"
  ip monitor address route | while read -r _; do apply; done
else
  echo "ip monitor niet beschikbaar, terugvallen op periodieke controle"
  while true; do sleep 60; apply; done
fi
`, iface, eg.Spec.EgressIP, routeLines.String())
}

// buildDaemonSet bouwt de gewenste DaemonSet spec voor de IP pinner.
// Het egress IP komt als extra IP op de interface — geen dummy interface.
// Bij OS reboot herstelt de DaemonSet het IP (en routes) automatisch.
func (r *EgressGatewayReconciler) buildDaemonSet(eg *egressv1alpha1.EgressGateway, name string) *appsv1.DaemonSet {
	iface := eg.Spec.Interface
	if iface == "" {
		iface = "eth0"
	}
	image := eg.Spec.PinnerImage
	if image == "" {
		image = "alpine:3.19"
	}

	pinnerScript := buildPinnerScript(eg, iface)

	dsLabels := pinnerLabels(eg)
	dsLabels["app.kubernetes.io/managed-by"] = "cilium-egress-operator"

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: eg.Namespace,
			Labels:    dsLabels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: pinnerLabels(eg),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: pinnerLabels(eg),
				},
				Spec: corev1.PodSpec{
					// Alleen op de egress node draaien
					NodeSelector: map[string]string{
						labelEgressNode: labelValueTrue,
					},
					// hostNetwork zodat we de echte host interfaces zien
					HostNetwork: true,
					Containers: []corev1.Container{
						{
							Name:  "ip-pinner",
							Image: image,
							Command: []string{
								"/bin/sh",
								"-c",
								pinnerScript,
							},
							// Geen privileged: NET_ADMIN volstaat voor ip addr/route
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
									Add:  []corev1.Capability{"NET_ADMIN"},
								},
							},
							// Ready zodra het egress IP daadwerkelijk op de interface staat;
							// de controller leest dit terug als status.egressIPConfirmed
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{
											"/bin/sh", "-c",
											fmt.Sprintf(`ip addr show dev %q | grep -q "inet %s/32"`, iface, eg.Spec.EgressIP),
										},
									},
								},
								InitialDelaySeconds: 2,
								PeriodSeconds:       30,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("5m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
							},
						},
					},
					// Toleration zodat de DaemonSet ook op control-plane nodes draait
					Tolerations: []corev1.Toleration{
						{
							Key:      "node-role.kubernetes.io/control-plane",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
		},
	}
}

func (r *EgressGatewayReconciler) updateStatus(ctx context.Context, eg *egressv1alpha1.EgressGateway, egressNode string, ds *appsv1.DaemonSet) error {
	patch := client.MergeFrom(eg.DeepCopy())

	now := metav1.NewTime(time.Now())
	eg.Status.EgressNode = egressNode
	eg.Status.LastReconciled = &now
	// De pinner pod is pas Ready als het IP op de interface staat (readiness probe)
	eg.Status.EgressIPConfirmed = ds != nil && ds.Status.NumberReady > 0

	return r.Status().Patch(ctx, eg, patch)
}

func (r *EgressGatewayReconciler) nodeToEgressGateway(ctx context.Context, obj client.Object) []reconcile.Request {
	egressList := &egressv1alpha1.EgressGatewayList{}
	if err := r.List(ctx, egressList); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, len(egressList.Items))
	for i, eg := range egressList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      eg.Name,
				Namespace: eg.Namespace,
			},
		}
	}
	return requests
}

func (r *EgressGatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&egressv1alpha1.EgressGateway{}).
		Owns(&appsv1.DaemonSet{}).
		Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToEgressGateway),
		).
		Named("egressgateway").
		Complete(r)
}
