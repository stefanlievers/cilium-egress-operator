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
	"sort"
	"time"

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

	// Reconcile node selectie en haal de geselecteerde node terug
	egressNode, err := r.reconcileEgressNode(ctx, eg)
	if err != nil {
		log.Error(err, "Fout bij reconcilen egress node")
		return ctrl.Result{}, err
	}

	// Status terugschrijven
	if err := r.updateStatus(ctx, eg, egressNode); err != nil {
		log.Error(err, "Fout bij updaten status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileEgressNode zorgt dat exact één node het label egress-node: "true" heeft.
// Geeft de naam van de geselecteerde node terug.
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

// updateStatus schrijft de huidige staat terug naar de EgressGateway status.
func (r *EgressGatewayReconciler) updateStatus(ctx context.Context, eg *egressv1alpha1.EgressGateway, egressNode string) error {
	patch := client.MergeFrom(eg.DeepCopy())

	now := metav1.NewTime(time.Now())
	eg.Status.EgressNode = egressNode
	eg.Status.LastReconciled = &now

	return r.Status().Patch(ctx, eg, patch)
}

// nodeToEgressGateway mapt een Node event naar alle EgressGateway reconcile requests.
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

// SetupWithManager sets up the controller with the Manager.
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
