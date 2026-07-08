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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	egressv1alpha1 "github/stefanlievers/cilium-egress-operator/api/v1alpha1"
)

const (
	// labelEgressNode marks the node acting as the egress gateway
	labelEgressNode = "egress-node"
	// labelControlPlane is the standard Kubernetes control plane label
	labelControlPlane = "node-role.kubernetes.io/control-plane"
	// labelControlPlaneLegacy is the deprecated master label still present on older clusters
	labelControlPlaneLegacy = "node-role.kubernetes.io/master"
	labelValueTrue          = "true"
	// annotationSpecHash stores a hash of the generated DaemonSet spec so
	// reconciles can skip API writes when nothing changed
	annotationSpecHash = "egress.cilium-egress-operator.io/spec-hash"
)

// pinnerLabels are the selector labels of the pinner DaemonSet for a gateway
func pinnerLabels(eg *egressv1alpha1.EgressGateway) map[string]string {
	return map[string]string{
		"app":            "egress-ip-pinner",
		"egress-gateway": eg.Name,
	}
}

// egressNodeSelector returns the labels identifying this gateway's egress
// node: spec.nodeSelector when set, otherwise the default egress-node label.
// The CRD defaults the field, but objects created before that default (or
// bypassing admission defaulting) must still behave identically.
func egressNodeSelector(eg *egressv1alpha1.EgressGateway) map[string]string {
	if len(eg.Spec.NodeSelector) > 0 {
		return eg.Spec.NodeSelector
	}
	return map[string]string{labelEgressNode: labelValueTrue}
}

// EgressGatewayReconciler reconciles a EgressGateway object
type EgressGatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// The operator only reads its own CR and writes its status; it never creates,
// mutates, or deletes EgressGateway objects themselves (least privilege).
// +kubebuilder:rbac:groups=egress.cilium-egress-operator.io,resources=egressgateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=egress.cilium-egress-operator.io,resources=egressgateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete

func (r *EgressGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	eg := &egressv1alpha1.EgressGateway{}
	if err := r.Get(ctx, req.NamespacedName, eg); err != nil {
		if errors.IsNotFound(err) {
			log.Info("EgressGateway not found, probably deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to fetch EgressGateway")
		return ctrl.Result{}, err
	}

	log.Info("Reconciling EgressGateway",
		"name", eg.Name,
		"egressIP", eg.Spec.EgressIP,
	)

	// Step 1: make sure there is an egress node
	egressNode, err := r.reconcileEgressNode(ctx, eg)
	if err != nil {
		log.Error(err, "Failed to reconcile egress node")
		return ctrl.Result{}, err
	}

	// Step 2: make sure the IP pinner DaemonSet exists
	ds, err := r.reconcileDaemonSet(ctx, eg)
	if err != nil {
		log.Error(err, "Failed to reconcile DaemonSet")
		return ctrl.Result{}, err
	}

	// Step 3: write back status
	if err := r.updateStatus(ctx, eg, egressNode, ds); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *EgressGatewayReconciler) reconcileEgressNode(ctx context.Context, eg *egressv1alpha1.EgressGateway) (string, error) {
	log := logf.FromContext(ctx)

	selector := egressNodeSelector(eg)

	egressNodes := &corev1.NodeList{}
	if err := r.List(ctx, egressNodes, client.MatchingLabels(selector)); err != nil {
		return "", err
	}

	if len(egressNodes.Items) > 0 {
		nodeName := egressNodes.Items[0].Name
		log.Info("Egress node found, nothing to do", "node", nodeName)
		return nodeName, nil
	}

	role := eg.Spec.NodeRole
	if role == "" {
		role = egressv1alpha1.NodeRoleControlPlane
	}
	log.Info("No egress node found, looking for candidates", "nodeRole", role)

	candidates, err := r.listCandidateNodes(ctx, role)
	if err != nil {
		return "", err
	}

	if len(candidates) == 0 {
		log.Info("No candidate nodes found for role, waiting", "nodeRole", role)
		return "", nil
	}

	slices.SortFunc(candidates, func(a, b corev1.Node) int {
		return strings.Compare(a.Name, b.Name)
	})

	target := &candidates[0]
	log.Info("Labeling egress node", "node", target.Name, "nodeRole", role)

	patch := client.MergeFrom(target.DeepCopy())
	if target.Labels == nil {
		target.Labels = map[string]string{}
	}
	maps.Copy(target.Labels, selector)
	if err := r.Patch(ctx, target, patch); err != nil {
		return "", err
	}

	log.Info("Egress node labeled successfully", "node", target.Name)
	return target.Name, nil
}

// listCandidateNodes returns the nodes eligible for the egress label given the
// desired node role. Workers are nodes without a control plane label; there is
// no universal worker label across distributions. Terminating nodes are never
// candidates.
func (r *EgressGatewayReconciler) listCandidateNodes(ctx context.Context, role egressv1alpha1.NodeRole) ([]corev1.Node, error) {
	allNodes := &corev1.NodeList{}
	if err := r.List(ctx, allNodes); err != nil {
		return nil, err
	}

	var candidates []corev1.Node
	for _, node := range allNodes.Items {
		if node.DeletionTimestamp != nil {
			continue
		}
		_, isControlPlane := node.Labels[labelControlPlane]
		if !isControlPlane {
			_, isControlPlane = node.Labels[labelControlPlaneLegacy]
		}
		if (role == egressv1alpha1.NodeRoleControlPlane) == isControlPlane {
			candidates = append(candidates, node)
		}
	}
	return candidates, nil
}

// hashDaemonSetSpec returns a stable hash of the generated DaemonSet spec,
// used to detect whether an update is actually needed.
func hashDaemonSetSpec(ds *appsv1.DaemonSet) (string, error) {
	raw, err := json.Marshal(ds.Spec)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// reconcileDaemonSet ensures the IP pinner DaemonSet exists and is up to date.
// A spec hash annotation makes this a read-only no-op when nothing changed.
// The returned DaemonSet carries the current status (for egressIPConfirmed).
func (r *EgressGatewayReconciler) reconcileDaemonSet(ctx context.Context, eg *egressv1alpha1.EgressGateway) (*appsv1.DaemonSet, error) {
	log := logf.FromContext(ctx)

	dsName := fmt.Sprintf("egress-ip-pinner-%s", eg.Name)
	desired := r.buildDaemonSet(eg, dsName)

	specHash, err := hashDaemonSetSpec(desired)
	if err != nil {
		return nil, err
	}
	desired.Annotations = map[string]string{annotationSpecHash: specHash}

	if err := ctrl.SetControllerReference(eg, desired, r.Scheme); err != nil {
		return nil, err
	}

	existing := &appsv1.DaemonSet{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      dsName,
		Namespace: eg.Namespace,
	}, existing)

	if errors.IsNotFound(err) {
		log.Info("Creating DaemonSet", "daemonset", dsName)
		return desired, r.Create(ctx, desired)
	}
	if err != nil {
		return nil, err
	}

	if existing.Annotations[annotationSpecHash] == specHash {
		return existing, nil
	}

	log.Info("Updating DaemonSet", "daemonset", dsName)
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Annotations[annotationSpecHash] = specHash
	return existing, r.Patch(ctx, existing, patch)
}

// buildPinnerScript builds the shell script for the IP pinner container.
// All interpolated values are restricted by CRD validation patterns to
// safe characters (IPs, CIDRs, interface names) — no shell injection possible.
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

# Try to install iproute2 for event-driven monitoring via ip monitor.
# Fails silently in air-gapped environments; periodic checks are the fallback.
command -v apk >/dev/null 2>&1 && apk add --no-cache iproute2 >/dev/null 2>&1 || true

ensure_ip() {
  ip addr show dev "$IFACE" | grep -q "inet $IP/32" || {
    echo "Adding egress IP $IP to $IFACE"
    ip addr add "$IP/32" dev "$IFACE" || true
  }
}

ensure_route() {
  CIDR=$1
  VIA=$2
  # No explicit next-hop: follow the node's current default gateway
  if [ -z "$VIA" ]; then
    VIA=$(ip route show default 2>/dev/null | sed -n 's/.* via \([0-9.]*\).*/\1/p' | head -n 1)
  fi
  if [ -z "$VIA" ]; then
    echo "No next-hop known for $CIDR (no default gateway found), skipping route"
    return 0
  fi
  ip route show | grep -q "^$CIDR via $VIA dev $IFACE" || {
    echo "Setting route $CIDR via $VIA on $IFACE"
    ip route del "$CIDR" 2>/dev/null || true
    ip route add "$CIDR" via "$VIA" dev "$IFACE" src "$IP" || true
  }
}

apply() {
  ensure_ip
%s}

apply
echo "Egress IP $IP active on $IFACE"

# Monitoring: event-driven via ip monitor (iproute2), otherwise periodic (busybox)
if ip -V 2>/dev/null | grep -qi iproute2; then
  echo "Monitoring via ip monitor"
  ip monitor address route | while read -r _; do apply; done
else
  echo "ip monitor not available, falling back to periodic checks"
  while true; do sleep 60; apply; done
fi
`, iface, eg.Spec.EgressIP, routeLines.String())
}

// buildDaemonSet builds the desired DaemonSet spec for the IP pinner.
// The egress IP is added as an extra IP on the interface — no dummy interface.
// After an OS reboot the DaemonSet restores the IP (and routes) automatically.
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
					// Only run on this gateway's egress node
					NodeSelector: egressNodeSelector(eg),
					// hostNetwork so we see the real host interfaces
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
							// No privileged: NET_ADMIN suffices for ip addr/route
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
									Add:  []corev1.Capability{"NET_ADMIN"},
								},
							},
							// Ready once the egress IP is actually on the interface;
							// the controller reads this back as status.egressIPConfirmed
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
					// Toleration so the DaemonSet also runs on control-plane nodes
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
	// The pinner pod only becomes Ready once the IP is on the interface (readiness probe)
	ipConfirmed := ds != nil && ds.Status.NumberReady > 0

	// Skip the write when nothing changed: a status patch is itself a watch
	// event, and unconditional timestamp bumps would reconcile forever.
	if eg.Status.EgressNode == egressNode &&
		eg.Status.EgressIPConfirmed == ipConfirmed &&
		eg.Status.LastReconciled != nil {
		return nil
	}

	patch := client.MergeFrom(eg.DeepCopy())

	now := metav1.NewTime(time.Now())
	eg.Status.EgressNode = egressNode
	eg.Status.EgressIPConfirmed = ipConfirmed
	eg.Status.LastReconciled = &now

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
		// Only reconcile on spec changes; our own status patches must not
		// trigger new reconciles
		For(&egressv1alpha1.EgressGateway{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.DaemonSet{}).
		// Node creates/deletes and label changes matter for egress node
		// selection; status heartbeats of every node in the cluster do not
		Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToEgressGateway),
			builder.WithPredicates(predicate.LabelChangedPredicate{}),
		).
		Named("egressgateway").
		Complete(r)
}
