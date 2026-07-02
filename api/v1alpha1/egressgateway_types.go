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
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EgressGatewaySpec defines the desired state of EgressGateway
type EgressGatewaySpec struct {
	// egressIP is the IP address to assign to the egress interface.
	// +kubebuilder:validation:Pattern=`^(\d{1,3}\.){3}\d{1,3}$`
	// +required
	EgressIP string `json:"egressIP"`

	// interface is the network interface on the egress node to assign the IP to.
	// +kubebuilder:default=egress0
	// +optional
	Interface string `json:"interface,omitempty"`

	// podSelector selects the pods that will use this egress gateway.
	// +required
	PodSelector metav1.LabelSelector `json:"podSelector"`

	// namespaceSelector selects the namespaces whose pods will use this egress gateway.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// destinations is the list of CIDRs that traffic will be routed through the egress gateway.
	// +kubebuilder:validation:MinItems=1
	// +required
	Destinations []Destination `json:"destinations"`
}

// Destination defines a CIDR that should be routed via the egress gateway.
type Destination struct {
	// cidr is the destination network in CIDR notation.
	// +kubebuilder:validation:Pattern=`^(\d{1,3}\.){3}\d{1,3}/\d{1,2}$`
	// +required
	CIDR string `json:"cidr"`
}

// EgressGatewayStatus defines the observed state of EgressGateway.
type EgressGatewayStatus struct {
	// egressNode is the name of the node currently acting as egress gateway.
	// +optional
	EgressNode string `json:"egressNode,omitempty"`

	// egressIPConfirmed indicates whether the egress IP is confirmed on the node interface.
	// +optional
	EgressIPConfirmed bool `json:"egressIPConfirmed,omitempty"`

	// ciliumPolicyReady indicates whether the CiliumEgressGatewayPolicy has been created successfully.
	// +optional
	CiliumPolicyReady bool `json:"ciliumPolicyReady,omitempty"`

	// lastReconciled is the timestamp of the last successful reconciliation.
	// +optional
	LastReconciled *metav1.Time `json:"lastReconciled,omitempty"`

	// conditions represent the current state of the EgressGateway resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="EgressIP",type=string,JSONPath=`.spec.egressIP`
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.status.egressNode`
// +kubebuilder:printcolumn:name="IPConfirmed",type=boolean,JSONPath=`.status.egressIPConfirmed`
// +kubebuilder:printcolumn:name="PolicyReady",type=boolean,JSONPath=`.status.ciliumPolicyReady`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// EgressGateway is the Schema for the egressgateways API
type EgressGateway struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`
	// +required
	Spec EgressGatewaySpec `json:"spec"`
	// +optional
	Status EgressGatewayStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// EgressGatewayList contains a list of EgressGateway
type EgressGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []EgressGateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &EgressGateway{}, &EgressGatewayList{})
		return nil
	})
}
