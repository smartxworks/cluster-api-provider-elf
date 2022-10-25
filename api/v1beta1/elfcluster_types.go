/*
Copyright 2022.

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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

const (
	// ClusterFinalizer allows ReconcileElfCluster to clean up ELF
	// resources associated with ElfCluster before removing it from the
	// API server.
	ClusterFinalizer = "elfcluster.infrastructure.cluster.x-k8s.io"

	// ElfClusterForceDeleteAnnotation means to skip the deletion of infrastructure resources in Tower (e.g. VM and labels)
	// when deleting an ElfCluster. This is useful when the Tower server or SMTX ELF cluster is disconnected.
	ElfClusterForceDeleteAnnotation = "cape.infrastructure.cluster.x-k8s.io/force-delete-cluster"
)

// ElfClusterSpec defines the desired state of ElfCluster.
type ElfClusterSpec struct {
	// Cluster is a unique identifier for a ELF cluster.
	Cluster string `json:"cluster,omitempty"`

	// Tower is the config of tower.
	Tower Tower `json:"tower,omitempty"`

	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint APIEndpoint `json:"controlPlaneEndpoint"`

	// VMGracefulShutdown indicates the VMs in this ElfCluster should shutdown gracefully when deleting the VMs.
	// Default to false because sometimes the OS stuck when shutting down gracefully.
	// +optional
	VMGracefulShutdown bool `json:"vmGracefulShutdown,omitempty"`
}

// ElfClusterStatus defines the observed state of ElfCluster.
type ElfClusterStatus struct {
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Conditions defines current service state of the ElfCluster.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="Cluster infrastructure is ready"
//+kubebuilder:printcolumn:name="Tower",type="string",JSONPath=".spec.tower.server",description="Tower is the address of the Tower endpoint"
//+kubebuilder:printcolumn:name="ControlPlaneEndpoint",type="string",JSONPath=".spec.controlPlaneEndpoint",description="API Endpoint",priority=1
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of Cluster"

// ElfCluster is the Schema for the elfclusters API.
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

func (c *ElfCluster) GetTower() Tower {
	return c.Spec.Tower
}

func (c *ElfCluster) HasForceDeleteCluster() bool {
	if c.Annotations == nil {
		return false
	}
	_, ok := c.Annotations[ElfClusterForceDeleteAnnotation]
	return ok
}

//+kubebuilder:object:root=true

// ElfClusterList contains a list of ElfCluster.
type ElfClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ElfCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ElfCluster{}, &ElfClusterList{})
}
