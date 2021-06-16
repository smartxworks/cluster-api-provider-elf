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

package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/errors"
)

const (
	// MachineFinalizer allows ReconcileElfMachine to clean up Elf
	// resources associated with ElfMachine before removing it from the
	// API Server.
	MachineFinalizer = "elfmachine.infrastructure.cluster.x-k8s.io"
)

// ElfMachineSpec defines the desired state of ElfMachine
type ElfMachineSpec struct {
	// ProviderID is the virtual machine's UUID formatted as
	// elf://f0f6f65d-0786-4170-9ab9-d02187a61ad6
	// +optional
	ProviderID *string `json:"providerID,omitempty"`

	// FailureDomain is the failure domain unique identifier this Machine should be attached to, as defined in Cluster API.
	// For this infrastructure provider, the name is equivalent to the name of the ElfDeploymentZone.
	FailureDomain *string `json:"failureDomain,omitempty"`

	//Template is the name or UUID of the template used to clone new machines.
	Template string `json:"template"`

	// Network is the network configuration for this machin's VM.
	// +optional
	Network NetworkSpec `json:"network,omitempty"`

	// NumCPUS is the number of virtual processors in a VM.
	// Defaults to the analogue property value in the template from which this
	// machine is cloned.
	// +optional
	NumCPUS int32 `json:"numCPUS,omitempty"`

	// NumCoresPerSocket is the number of cores among which to distribute CPUs
	// in this VM.
	// +optional
	NumCoresPerSocket int32 `json:"numCoresPerSocket,omitempty"`

	// +optional
	MemoryMiB int64 `json:"memoryMiB,omitempty"`

	// +optional
	DiskGiB int32 `json:"diskGiB,omitempty"`

	// +optional
	AutoSchedule bool `json:"auto_schedule,omitempty"`

	// +optional
	HA bool `json:"ha,omitempty"`
}

// ElfMachineStatus defines the observed state of ElfMachine
type ElfMachineStatus struct {
	// Ready is true when the provider resource is ready.
	// +optional
	Ready bool `json:"ready"`

	// Conditions defines current service state of the ElfMachine.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`

	// Addresses contains the Elf instance associated addresses.
	Addresses []clusterv1.MachineAddress `json:"addresses,omitempty"`

	// Network returns the network status for each of the machine's configured
	// network interfaces.
	// +optional
	Network []NetworkStatus `json:"network,omitempty"`

	// FailureReason will be set in the event that there is a terminal problem
	// reconciling the Machine and will contain a succinct value suitable
	// for machine interpretation.
	//
	// This field should not be set for transitive errors that a controller
	// faces that are expected to be fixed automatically over
	// time (like service outages), but instead indicate that something is
	// fundamentally wrong with the Machine's spec or the configuration of
	// the controller, and that manual intervention is required. Examples
	// of terminal errors would be invalid combinations of settings in the
	// spec, values that are unsupported by the controller, or the
	// responsible controller itself being critically misconfigured.
	//
	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	FailureReason *errors.MachineStatusError `json:"failureReason,omitempty"`

	// FailureMessage will be set in the event that there is a terminal problem
	// reconciling the Machine and will contain a more verbose string suitable
	// for logging and human consumption.
	//
	// This field should not be set for transitive errors that a controller
	// faces that are expected to be fixed automatically over
	// time (like service outages), but instead indicate that something is
	// fundamentally wrong with the Machine's spec or the configuration of
	// the controller, and that manual intervention is required. Examples
	// of terminal errors would be invalid combinations of settings in the
	// spec, values that are unsupported by the controller, or the
	// responsible controller itself being critically misconfigured.
	//
	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	FailureMessage *string `json:"failureMessage,omitempty"`

	// This value is set automatically at runtime and should not be set or
	// modified by users.
	// VMRef is used to lookup the VM.
	// +optional
	VMRef string `json:"vmRef,omitempty"`

	TaskRef string `json:"taskRef,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ElfMachine is the Schema for the elfmachines API
type ElfMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ElfMachineSpec   `json:"spec,omitempty"`
	Status ElfMachineStatus `json:"status,omitempty"`
}

func (m *ElfMachine) GetConditions() clusterv1.Conditions {
	return m.Status.Conditions
}

func (m *ElfMachine) SetConditions(conditions clusterv1.Conditions) {
	m.Status.Conditions = conditions
}

func (m *ElfMachine) SetVM(UUID string) {
	m.Status.TaskRef = ""
	m.Status.VMRef = UUID
}

func (m *ElfMachine) WithVM() bool {
	return m.Status.VMRef != "" || m.Status.TaskRef != ""
}

func (m *ElfMachine) HasVM() bool {
	return m.Status.VMRef != ""
}

func (m *ElfMachine) HasTask() bool {
	return m.Status.TaskRef != ""
}

func (m *ElfMachine) SetTask(jobId string) {
	m.Status.TaskRef = jobId
}

func (m *ElfMachine) IsFailed() bool {
	return m.Status.FailureReason != nil || m.Status.FailureMessage != nil
}

//+kubebuilder:object:root=true

// ElfMachineList contains a list of ElfMachine
type ElfMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ElfMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ElfMachine{}, &ElfMachineList{})
}
