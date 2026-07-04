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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	egressv1alpha1 "github/stefanlievers/cilium-egress-operator/api/v1alpha1"
)

var _ = Describe("EgressGateway Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = "default"
			nodeName          = "test-control-plane"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}

		BeforeEach(func() {
			By("creating a control plane node")
			node := &corev1.Node{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)
			if err != nil && errors.IsNotFound(err) {
				node = &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName,
						Labels: map[string]string{
							"node-role.kubernetes.io/control-plane": "",
						},
					},
				}
				Expect(k8sClient.Create(ctx, node)).To(Succeed())
			}

			By("creating the custom resource for the Kind EgressGateway")
			egressgateway := &egressv1alpha1.EgressGateway{}
			err = k8sClient.Get(ctx, typeNamespacedName, egressgateway)
			if err != nil && errors.IsNotFound(err) {
				resource := &egressv1alpha1.EgressGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: egressv1alpha1.EgressGatewaySpec{
						EgressIP: "192.0.2.10",
						PodSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						CreateRoutes: true,
						Destinations: []egressv1alpha1.Destination{
							{CIDR: "10.50.0.0/16", NextHop: "192.0.2.1"},
							{CIDR: "10.60.0.0/16"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &egressv1alpha1.EgressGateway{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance EgressGateway")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Cleanup the pinner DaemonSet")
			ds := &appsv1.DaemonSet{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "egress-ip-pinner-" + resourceName,
				Namespace: resourceNamespace,
			}, ds)
			if err == nil {
				Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
			}

			By("Cleanup the node")
			node := &corev1.Node{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)
			if err == nil {
				Expect(k8sClient.Delete(ctx, node)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &EgressGatewayReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("labeling the control plane node as egress node")
			node := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
			Expect(node.Labels).To(HaveKeyWithValue("egress-node", "true"))

			By("creating the pinner DaemonSet")
			ds := &appsv1.DaemonSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "egress-ip-pinner-" + resourceName,
				Namespace: resourceNamespace,
			}, ds)).To(Succeed())

			container := ds.Spec.Template.Spec.Containers[0]
			script := container.Command[2]
			Expect(script).To(ContainSubstring(`IP="192.0.2.10"`))
			Expect(script).To(ContainSubstring(`ensure_route "10.50.0.0/16" "192.0.2.1"`))
			Expect(script).To(ContainSubstring(`ensure_route "10.60.0.0/16" ""`))

			By("using a least-privilege security context")
			Expect(container.SecurityContext.Privileged).To(BeNil())
			Expect(container.SecurityContext.Capabilities.Add).To(ConsistOf(corev1.Capability("NET_ADMIN")))
			Expect(container.SecurityContext.Capabilities.Drop).To(ConsistOf(corev1.Capability("ALL")))

			By("writing back the status")
			resource := &egressv1alpha1.EgressGateway{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			Expect(resource.Status.EgressNode).To(Equal(nodeName))
			Expect(resource.Status.LastReconciled).NotTo(BeNil())
			// No kubelet in envtest, so the pinner pod never becomes Ready
			Expect(resource.Status.EgressIPConfirmed).To(BeFalse())
		})

		It("should label a worker node when nodeRole is worker", func() {
			const workerName = "test-worker"

			By("creating a worker node")
			worker := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: workerName},
			}
			Expect(k8sClient.Create(ctx, worker)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, worker)
			})

			By("setting nodeRole to worker on the resource")
			resource := &egressv1alpha1.EgressGateway{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.NodeRole = egressv1alpha1.NodeRoleWorker
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &EgressGatewayReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("labeling the worker node, not the control plane node")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: workerName}, worker)).To(Succeed())
			Expect(worker.Labels).To(HaveKeyWithValue("egress-node", "true"))

			cp := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, cp)).To(Succeed())
			Expect(cp.Labels).NotTo(HaveKey("egress-node"))

			By("writing the worker back to status")
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			Expect(resource.Status.EgressNode).To(Equal(workerName))
		})

		It("should not create routes in the script when createRoutes is false", func() {
			By("disabling createRoutes on the resource")
			resource := &egressv1alpha1.EgressGateway{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.CreateRoutes = false
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &EgressGatewayReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ds := &appsv1.DaemonSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "egress-ip-pinner-" + resourceName,
				Namespace: resourceNamespace,
			}, ds)).To(Succeed())

			script := ds.Spec.Template.Spec.Containers[0].Command[2]
			Expect(script).NotTo(ContainSubstring("ensure_route \"10."))
		})
	})
})
