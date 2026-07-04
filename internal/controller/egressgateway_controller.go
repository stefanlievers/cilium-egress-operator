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
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	egressv1alpha1 "github/stefanlievers/cilium-egress-operator/api/v1alpha1"
)

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
	egressNode, err := r.reconcileEgressNode(ctx, eg)
	if err != nil {
		log.Error(err, "Fout bij reconcilen egress node")
		return ctrl.Result{}, err
	}

	// Stap 2: zorg dat de IP pinner DaemonSet bestaat
	if err := r.reconcileDaemonSet(ctx, eg); err != nil {
		log.Error(err, "Fout bij reconcilen DaemonSet")
		return ctrl.Result{}, err
	}

	// Stap 3: status terugschrijven
	if err := r.updateStatus(ctx, eg, egressNode); err != nil {
		log.Error(err, "Fout bij updaten status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *EgressGatewayReconciler) reconcileEgressNode(ctx context.Context, eg *egressv1alpha1.EgressGateway) (string, error) {
	log := logf.FromContext(ctx)

	egressNodes := &corev1.NodeList{}
	if err := r.List(ctx, egressNodes, client.MatchingLabels{
		"egress-node": "true",
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
		"node-role.kubernetes.io/control-plane": "",
	}); err != nil {
		return "", err
	}

	if len(controlPlaneNodes.Items) == 0 {
		log.Info("Geen control plane nodes gevonden, wachten")
		return "", nil
	}

	sort.Slice(controlPlaneNodes.Items, func(i, j int) bool {
		return controlPlaneNodes.Items[i].Name < controlPlaneNodes.Items[j].Name
	})

	target := &controlPlaneNodes.Items[0]
	log.Info("Labelen van egress node", "node", target.Name)

	patch := client.MergeFrom(target.DeepCopy())
	target.Labels["egress-node"] = "true"
	if err := r.Patch(ctx, target, patch); err != nil {
		return "", err
	}

	log.Info("Egress node succesvol gelabeld", "node", target.Name)
	return target.Name, nil
}

// reconcileDaemonSet zorgt dat de IP pinner DaemonSet bestaat en up-to-date is.
func (r *EgressGatewayReconciler) reconcileDaemonSet(ctx context.Context, eg *egressv1alpha1.EgressGateway) error {
	log := logf.FromContext(ctx)

	dsName := fmt.Sprintf("egress-ip-pinner-%s", eg.Name)
	desired := r.buildDaemonSet(eg, dsName)

	if err := ctrl.SetControllerReference(eg, desired, r.Scheme); err != nil {
		return err
	}

	existing := &appsv1.DaemonSet{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      dsName,
		Namespace: eg.Namespace,
	}, existing)

	if errors.IsNotFound(err) {
		log.Info("DaemonSet aanmaken", "daemonset", dsName)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	log.Info("DaemonSet updaten", "daemonset", dsName)
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec = desired.Spec
	return r.Patch(ctx, existing, patch)
}

// buildDaemonSet bouwt de gewenste DaemonSet spec voor de IP pinner.
// Het egress IP wordt als extra adres op eth0 gezet — geen dummy interface.
// Bij OS reboot herstelt de DaemonSet het IP automatisch via ip monitor.
func (r *EgressGatewayReconciler) buildDaemonSet(eg *egressv1alpha1.EgressGateway, name string) *appsv1.DaemonSet {
	pinnerScript := fmt.Sprintf(`
set -e
IFACE=%s
IP=%s

# IP zetten als het er niet op staat
ip addr show $IFACE | grep -q $IP || ip addr add $IP/32 dev $IFACE
echo "Egress IP $IP actief op $IFACE"

# Bewaken via ip monitor — herstellen als het IP wegvalt
ip monitor address | while read -r event; do
  ip addr show $IFACE | grep -q $IP || {
    echo "Egress IP weggevallen, herstellen..."
    ip addr add $IP/32 dev $IFACE
  }
done
`, eg.Spec.Interface, eg.Spec.EgressIP)

	privileged := true

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: eg.Namespace,
			Labels: map[string]string{
				"app":                          "egress-ip-pinner",
				"egress-gateway":               eg.Name,
				"app.kubernetes.io/managed-by": "cilium-egress-operator",
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":            "egress-ip-pinner",
					"egress-gateway": eg.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":            "egress-ip-pinner",
						"egress-gateway": eg.Name,
					},
				},
				Spec: corev1.PodSpec{
					// Alleen op de egress node draaien
					NodeSelector: map[string]string{
						"egress-node": "true",
					},
					// hostNetwork zodat we de echte host interfaces zien
					HostNetwork: true,
					Containers: []corev1.Container{
						{
							Name:  "ip-pinner",
							Image: "alpine:3.19",
							Command: []string{
								"/bin/sh",
								"-c",
								pinnerScript,
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
								Capabilities: &corev1.Capabilities{
									Add: []corev1.Capability{"NET_ADMIN"},
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

func (r *EgressGatewayReconciler) updateStatus(ctx context.Context, eg *egressv1alpha1.EgressGateway, egressNode string) error {
	patch := client.MergeFrom(eg.DeepCopy())

	now := metav1.NewTime(time.Now())
	eg.Status.EgressNode = egressNode
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
		For(&egressv1alpha1.EgressGateway{}).
		Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToEgressGateway),
		).
		Named("egressgateway").
		Complete(r)
}
