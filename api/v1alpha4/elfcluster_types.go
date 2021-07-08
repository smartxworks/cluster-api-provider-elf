/*
Copyright 2021.

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

package v1alpha4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
)

const (
	ClusterFinalizer         = "elfcluster.infrastructure.cluster.x-k8s.io"
	ControlPlaneEndpointPort = 6443
)

// ElfClusterSpec defines the desired state of ElfCluster
type ElfClusterSpec struct {
	// Server is address of the elf VIP
	Server string `json:"server,omitempty"`

	// Username is the name used to log into the Elf server.
	Username string `json:"username,omitempty"`

	// Password is the password used to log into the Elf server.
	Password string `json:"password,omitempty"`

	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint APIEndpoint `json:"controlPlaneEndpoint"`
}

// ElfClusterStatus defines the observed state of ElfCluster
type ElfClusterStatus struct {
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Conditions defines current service state of the ElfCluster.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ElfCluster is the Schema for the elfclusters API
type ElfCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ElfClusterSpec   `json:"spec,omitempty"`
	Status ElfClusterStatus `json:"status,omitempty"`
}

func (c *ElfCluster) GetConditions() clusterv1.Conditions {
	return c.Status.Conditions
}

func (c *ElfCluster) SetConditions(conditions clusterv1.Conditions) {
	c.Status.Conditions = conditions
}

func (c *ElfCluster) Auth() ElfAuth {
	return ElfAuth{
		Host:     c.Spec.Server,
		Username: c.Spec.Username,
		Password: c.Spec.Password,
	}
}

//+kubebuilder:object:root=true

// ElfClusterList contains a list of ElfCluster
type ElfClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ElfCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ElfCluster{}, &ElfClusterList{})
}
