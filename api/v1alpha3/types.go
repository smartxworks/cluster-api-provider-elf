package v1alpha3

import (
	"fmt"
)

type ElfAuth struct {
	Host     string `json:"host"`
	Username string `json:"user"`
	Password string `json:"password"`
}

// ElfMachineTemplateResource describes the data needed to create a ElfMachine from a template
type ElfMachineTemplateResource struct {
	// Spec is the specification of the desired behavior of the machine.
	Spec ElfMachineSpec `json:"spec"`
}

// APIEndpoint represents a reachable Kubernetes API endpoint.
type APIEndpoint struct {
	// The hostname on which the API server is serving.
	Host string `json:"host"`

	// The port on which the API server is serving.
	Port int32 `json:"port"`
}

// IsZero returns true if either the host or the port are zero values.
func (v APIEndpoint) IsZero() bool {
	return v.Host == "" || v.Port == 0
}

// String returns a formatted version HOST:PORT of this APIEndpoint.
func (v APIEndpoint) String() string {
	return fmt.Sprintf("%s:%d", v.Host, v.Port)
}

// NetworkStatus provides information about one of a VM's networks.
type NetworkStatus struct {
	// Connected is a flag that indicates whether this network is currently
	// connected to the VM.
	Connected bool `json:"connected,omitempty"`

	// IPAddrs is one or more IP addresses reported by vm-tools.
	// +optional
	IPAddrs string `json:"ipAddrs,omitempty"`

	// MACAddr is the MAC address of the network device.
	MACAddr string `json:"macAddr"`

	// NetworkName is the name of the network.
	// +optional
	NetworkName string `json:"networkName,omitempty"`

	NetworkIndex int `json:"networkIndex"`
}

// NetworkSpec defines the virtual machine's network configuration.
type NetworkSpec struct {
	// Devices is the list of network devices used by the virtual machine.
	Devices []NetworkDeviceSpec `json:"devices"`

	// PreferredAPIServeCIDR is the preferred CIDR for the Kubernetes API
	// server endpoint on this machine
	PreferredAPIServerCIDR string `json:"preferredAPIServerCidr,omitempty"`
}

// NetworkDeviceSpec defines the network configuration for a virtual machine's
// network device.
type NetworkDeviceSpec struct {
	NetworkIndex int `json:"networkIndex"`

	NetworkType string `json:"networkType"`
	// IPAddrs is a list of one or more IPv4 and/or IPv6 addresses to assign
	// to this device.
	// Required when DHCP4 and DHCP6 are both false.
	IPAddrs string `json:"ipAddrs,omitempty"`

	Netmask string `json:"netmask,omitempty"`

	// Gateway4 is the IPv4 gateway used by this device.
	// Required when DHCP4 is false.
	Gateway string `json:"gateway,omitempty"`
}

// VirtualMachineState describes the state of a VM
type VirtualMachineState string

const (
	// VirtualMachineStateReady is the string representing a powered-on VM with reported IP addresses.
	VirtualMachineStateReady = "ready"

	// VirtualMachineStatePoweredOn is the string representing a VM in powered on state
	VirtualMachineStatePoweredOn = "running"
)

// VirtualMachine represents data about a Elf virtual machine object.
type VirtualMachine struct {
	// VM's UUID
	UUID string `json:"uuid"`

	// Name is the VM's name.
	Name string `json:"name"`

	// State is the VM's state.
	State VirtualMachineState `json:"state"`

	// Network is the status of the VM's network devices.
	Network []NetworkStatus `json:"network"`
}

func (vm *VirtualMachine) IsReady() bool {
	return vm.State == VirtualMachineStateReady
}

func (vm *VirtualMachine) IsPoweredOn() bool {
	return vm.State == VirtualMachineStatePoweredOn
}

const (
	VMJobPending   string = "pending"
	VMJobProcesing string = "processing"
	VMJobDone      string = "done"
	VMJobFailed    string = "failed"
)

//+kubebuilder:object:generate=false
type VMJob struct {
	Id          string      `json:"id"`
	Description string      `json:"description"`
	State       string      `json:"state"`
	Resources   interface{} `json:"resources"`
}

func (j *VMJob) IsDone() bool {
	return j.State == VMJobDone
}

func (j *VMJob) IsFailed() bool {
	return j.State == VMJobFailed
}

func (j *VMJob) IsFinished() bool {
	return j.State == VMJobDone || j.State == VMJobFailed
}

func (j *VMJob) GetVMUUID() string {
	res := j.Resources.(map[string]interface{})
	uuid := ""

	for _, v := range res {
		resource := v.(map[string]interface{})
		if resource["type"] != "KVM_VM" {
			continue
		}

		uuid = resource["uuid"].(string)
	}

	return uuid
}

//+kubebuilder:object:generate=false
type PatchStringValue struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}
