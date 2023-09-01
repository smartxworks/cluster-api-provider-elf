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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capierrors "sigs.k8s.io/cluster-api/errors"
)

const (
	// MachineFinalizer allows ReconcileElfMachine to clean up ELF
	// resources associated with ElfMachine before removing it from the
	// API Server.
	MachineFinalizer = "elfmachine.infrastructure.cluster.x-k8s.io"

	// VMDisconnectionTimestampAnnotation is the annotation identifying the VM of ElfMachine disconnection time.
	VMDisconnectionTimestampAnnotation = "cape.infrastructure.cluster.x-k8s.io/vm-disconnection-timestamp"
)

// ElfMachineSpec defines the desired state of ElfMachine.
type ElfMachineSpec struct {
	// ProviderID is the virtual machine's UUID formatted as
	// elf://f0f6f65d-0786-4170-9ab9-d02187a61ad6
	// +optional
	ProviderID *string `json:"providerID,omitempty"`

	// FailureDomain is the failure domain unique identifier this Machine should be attached to, as defined in Cluster API.
	// For this infrastructure provider, the name is equivalent to the name of the ElfDeploymentZone.
	FailureDomain *string `json:"failureDomain,omitempty"`

	// Template is the name or ID of the template used to clone new machines.
	Template string `json:"template"`

	// OSType is the operation system type of the virtual machine.
	// +kubebuilder:validation:Enum=LINUX;WINDOWS
	// +kubebuilder:default=LINUX
	// +optional
	OSType string `json:"osType,omitempty"`

	// Network is the network configuration for this machin's VM.
	// +optional
	Network NetworkSpec `json:"network,omitempty"`

	// NumCPUs is the number of virtual processors in a VM.
	// Defaults to the analogue property value in the template from which this
	// machine is cloned.
	// +optional
	NumCPUs int32 `json:"numCPUS,omitempty"`

	// NumCoresPerSocket is the number of cores among which to distribute CPUs
	// in this VM.
	// +optional
	NumCoresPerSocket int32 `json:"numCoresPerSocket,omitempty"`

	// +optional
	MemoryMiB int64 `json:"memoryMiB,omitempty"`

	// +optional
	DiskGiB int32 `json:"diskGiB,omitempty"`

	// +optional
	HA bool `json:"ha,omitempty"`

	// +optional
	CloneMode CloneMode `json:"cloneMode,omitempty"`

	// Host is a unique identifier for a ELF host.
	// Required when cloneMode is FullClone.
	// Defaults to AUTO_SCHEDULE.
	// +optional
	Host string `json:"host,omitempty"`
}

// ElfMachineStatus defines the observed state of ElfMachine.
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
	FailureReason *capierrors.MachineStatusError `json:"failureReason,omitempty"`

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

	// VMRef is used to lookup the VM.
	// This value is set automatically at runtime and should not be set or
	// modified by users.
	// +optional
	VMRef string `json:"vmRef,omitempty"`

	// TaskRef is a managed object reference to a Task related to the machine.
	// This value is set automatically at runtime and should not be set or
	// modified by users.
	// +optional
	TaskRef string `json:"taskRef,omitempty"`

	// HostServerRef is the Tower ID of host server where the virtual machine runs on.
	// This value is set automatically at runtime and should not be set or
	// modified by users.
	// +optional
	HostServerRef string `json:"hostServerRef,omitempty"`

	// HostServerName is the name of host server where the virtual machine runs on.
	// This value is set automatically at runtime and should not be set or
	// modified by users.
	// +optional
	HostServerName string `json:"hostServerName,omitempty"`

	// PlacementGroupRef is the reference to the Tower PlacementGroup which this ElfMachine belongs to.
	// This value is set automatically at runtime and should not be set or
	// modified by users.
	// +optional
	PlacementGroupRef string `json:"placementGroupRef,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="ElfMachine ready status"
//+kubebuilder:printcolumn:name="ProviderID",type="string",JSONPath=".spec.providerID",description="ElfMachine instance ID"
//+kubebuilder:printcolumn:name="IP",type="string",JSONPath=".status.addresses[?(@.address!=\"240.255.0.1\")].address",description="IP addresses of the virtual machine"
//+kubebuilder:printcolumn:name="HOST",type="string",JSONPath=".status.hostServerName",description="Name of host server where the virtual machine runs on"
//+kubebuilder:printcolumn:name="PLACEMENTGROUP",type="string",JSONPath=".status.placementGroupRef",description="ID of Tower placement group which this ElfMachine belongs to"
//+kubebuilder:printcolumn:name="Machine",type="string",JSONPath=".metadata.ownerReferences[?(@.kind==\"Machine\")].name",description="Machine object which owns with this ElfMachine"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of ElfMachine"

// ElfMachine is the Schema for the elfmachines API.
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

func (m *ElfMachine) SetVM(uuid string) {
	m.Status.TaskRef = ""
	m.Status.VMRef = uuid
}

func (m *ElfMachine) HasVM() bool {
	return m.Status.VMRef != ""
}

func (m *ElfMachine) HasTask() bool {
	return m.Status.TaskRef != ""
}

func (m *ElfMachine) SetTask(taskID string) {
	m.Status.TaskRef = taskID
}

func (m *ElfMachine) IsFailed() bool {
	return m.Status.FailureReason != nil || m.Status.FailureMessage != nil
}

func (m *ElfMachine) SetVMDisconnectionTimestamp(timestamp *metav1.Time) {
	if m.Annotations == nil {
		m.Annotations = make(map[string]string)
	}

	if timestamp == nil {
		delete(m.Annotations, VMDisconnectionTimestampAnnotation)
	} else {
		m.Annotations[VMDisconnectionTimestampAnnotation] = timestamp.Format(time.RFC3339)
	}
}

// GetNetworkDevicesRequiringIP returns a slice of NetworkDeviceSpec which requires DHCP IP or static IP.
func (m *ElfMachine) GetNetworkDevicesRequiringIP() []NetworkDeviceSpec {
	networkDevices := []NetworkDeviceSpec{}

	for index := range m.Spec.Network.Devices {
		if m.Spec.Network.Devices[index].NetworkType == NetworkTypeNone {
			continue
		}

		networkDevices = append(networkDevices, m.Spec.Network.Devices[index])
	}

	return networkDevices
}

// GetNetworkDevicesRequiringDHCP returns a slice of NetworkDeviceSpec which requires DHCP IP.
func (m *ElfMachine) GetNetworkDevicesRequiringDHCP() []NetworkDeviceSpec {
	networkDevices := []NetworkDeviceSpec{}

	for index := range m.Spec.Network.Devices {
		if m.Spec.Network.Devices[index].NetworkType == NetworkTypeIPV4DHCP {
			networkDevices = append(networkDevices, m.Spec.Network.Devices[index])
		}
	}

	return networkDevices
}

// IsMachineStaticIP returns true if the IP is the static IP for the Machine.
func (m *ElfMachine) IsMachineStaticIP(ip string) bool {
	for index := range m.Spec.Network.Devices {
		if m.Spec.Network.Devices[index].NetworkType != NetworkTypeIPV4 {
			continue
		}

		for _, staticIP := range m.Spec.Network.Devices[index].IPAddrs {
			if ip == staticIP {
				return true
			}
		}
	}

	return false
}

func (m *ElfMachine) GetVMDisconnectionTimestamp() *metav1.Time {
	if m.Annotations == nil {
		return nil
	}

	if _, ok := m.Annotations[VMDisconnectionTimestampAnnotation]; ok {
		timestampAnnotation := m.Annotations[VMDisconnectionTimestampAnnotation]
		timestamp, err := time.Parse(time.RFC3339, timestampAnnotation)
		if err != nil {
			return nil
		}

		disconnectionTimestamp := metav1.NewTime(timestamp)

		return &disconnectionTimestamp
	}

	return nil
}

//+kubebuilder:object:root=true

// ElfMachineList contains a list of ElfMachine.
type ElfMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ElfMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ElfMachine{}, &ElfMachineList{})
}
