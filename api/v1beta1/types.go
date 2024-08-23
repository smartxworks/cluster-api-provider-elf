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
	corev1 "k8s.io/api/core/v1"
)

// CloneMode is the type of clone operation used to clone a VM from a template.
type CloneMode string

const (
	// FullClone indicates a VM will have no relationship to the source of the
	// clone operation once the operation is complete. This is the safest clone
	// mode, but it is not the fastest.
	FullClone CloneMode = "FullClone"

	// FastClone means resulting VMs will be dependent upon the snapshot of
	// the source VM/template from which the VM was cloned. This is the fastest
	// clone mode.
	FastClone CloneMode = "FastClone"
)

// NetworkType is the VM network type.
type NetworkType string

// Network types.
const (
	NetworkTypeNone     NetworkType = "NONE"
	NetworkTypeIPV4     NetworkType = "IPV4"
	NetworkTypeIPV4DHCP NetworkType = "IPV4_DHCP"
)

type Tower struct {
	// Server is address of the tower server.
	Server string `json:"server,omitempty"`

	// Username is the name used to log into the tower server.
	Username string `json:"username,omitempty"`

	// Password is the password used to access the tower server.
	Password string `json:"password,omitempty"`

	// AuthMode is the authentication mode of tower server.
	// +kubebuilder:validation:Enum=LOCAL;LDAP
	AuthMode string `json:"authMode,omitempty"`

	// SkipTLSVerify indicates whether to skip verification for the SSL certificate of the tower server.
	SkipTLSVerify bool `json:"skipTLSVerify,omitempty"`
}

// ElfMachineTemplateResource describes the data needed to create an ElfMachine from a template.
type ElfMachineTemplateResource struct {
	// Spec is the specification of the desired behavior of the machine.
	Spec ElfMachineSpec `json:"spec"`
}

// NetworkStatus provides information about one of a VM's networks.
type NetworkStatus struct {
	// Connected is a flag that indicates whether this network is currently
	// connected to the VM.
	Connected bool `json:"connected,omitempty"`

	// IPAddrs is one or more IP addresses reported by vm-tools.
	// +optional
	IPAddrs []string `json:"ipAddrs,omitempty"`

	// MACAddr is the MAC address of the network device.
	MACAddr string `json:"macAddr"`

	// NetworkName is the name of the network.
	// +optional
	NetworkName string `json:"networkName,omitempty"`
}

// NetworkSpec defines the virtual machine's network configuration.
type NetworkSpec struct {
	// Devices is the list of network devices used by the virtual machine.
	Devices []NetworkDeviceSpec `json:"devices"`

	// Nameservers is a list of IPv4 and/or IPv6 addresses used as DNS
	// nameservers.
	// Please note that Linux allows only three nameservers (https://linux.die.net/man/5/resolv.conf).
	// +optional
	Nameservers []string `json:"nameservers,omitempty"`

	// PreferredAPIServeCIDR is the preferred CIDR for the Kubernetes API
	// server endpoint on this machine
	PreferredAPIServerCIDR string `json:"preferredAPIServerCidr,omitempty"`
}

func (n *NetworkSpec) RequiresStaticIPs() bool {
	for i := range len(n.Devices) {
		if n.Devices[i].NetworkType == NetworkTypeIPV4 && len(n.Devices[i].IPAddrs) == 0 {
			return true
		}
	}

	return false
}

// NetworkDeviceSpec defines the network configuration for a virtual machine's
// network device.
type NetworkDeviceSpec struct {
	NetworkType NetworkType `json:"networkType"`

	// Vlan is the virtual LAN used by the virtual machine.
	Vlan string `json:"vlan,omitempty"`

	// IPAddrs is a list of one or more IPv4 and/or IPv6 addresses to assign
	// to this device.
	// Required when DHCP4 and DHCP6 are both false.
	// +optional
	IPAddrs []string `json:"ipAddrs,omitempty"`

	// Netmask is the subnet mask used by this device.
	// Required when DHCP4 is false.
	// +optional
	Netmask string `json:"netmask,omitempty"`

	// MACAddr is the MAC address used by this device.
	// It is generally a good idea to omit this field and allow a MAC address
	// to be generated.
	// +optional
	MACAddr string `json:"macAddr,omitempty"`

	// Required when DHCP4 is false.
	// +optional
	Routes []NetworkDeviceRouteSpec `json:"routes,omitempty"`

	// AddressesFromPools is a list of IPAddressPools that should be assigned
	// to IPAddressClaims.
	// +optional
	AddressesFromPools []corev1.TypedLocalObjectReference `json:"addressesFromPools,omitempty"`
}

func (d *NetworkDeviceSpec) HasNetworkType() bool {
	return !(d.NetworkType == "" || d.NetworkType == NetworkTypeNone)
}

// NetworkDeviceRouteSpec defines the network configuration for a virtual machine's
// network device route.
type NetworkDeviceRouteSpec struct {
	// Gateway is the IPv4 gateway used by this route.
	Gateway string `json:"gateway,omitempty"`

	// Netmask is the subnet mask used by this route.
	Netmask string `json:"netmask,omitempty"`

	// Network is the route network address.
	Network string `json:"network,omitempty"`
}

// GPUPassthroughDeviceSpec defines virtual machine's GPU configuration
type GPUPassthroughDeviceSpec struct {
	// Model is the model name of a physical GPU, e.g. 'A16'.
	Model string `json:"model,omitempty"`

	// Count is the number of GPU. Defaults to 1.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Count int32 `json:"count,omitempty"`
}

// VGPUDeviceSpec defines virtual machine's VGPU configuration
type VGPUDeviceSpec struct {
	// Type is the type name of a virtual GPU, e.g. 'NVIDIA A16-16A'.
	// +kubebuilder:validation:Required
	Type string `json:"type,omitempty"`

	// Count is the number of vGPU. Defaults to 1.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Count int32 `json:"count,omitempty"`
}

// GPUStatus provides information about one of a VM's GPU device.
type GPUStatus struct {
	GPUID string `json:"gpuId,omitempty"`
	Name  string `json:"name,omitempty"`
}

// ResourcesStatus records the resources allocated to the virtual machine.
type ResourcesStatus struct {
	Disk int32 `json:"disk,omitempty"`
}

//+kubebuilder:object:generate=false

// PatchStringValue is for patching resources.
type PatchStringValue struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}
