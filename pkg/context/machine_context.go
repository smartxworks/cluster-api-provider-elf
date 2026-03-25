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

package context

import (
	goctx "context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

// MachineContext is a Go context used with an ElfMachine.
type MachineContext struct {
	Cluster     *clusterv1.Cluster
	Machine     *clusterv1.Machine
	ElfCluster  *infrav1.ElfCluster
	ElfMachine  *infrav1.ElfMachine
	PatchHelper *patch.Helper
	VMService   service.VMService
}

// String returns ElfMachineGroupVersionKindElfMachineNamespace/ElfMachineName.
func (c *MachineContext) String() string {
	return fmt.Sprintf("%s %s/%s", c.ElfMachine.GroupVersionKind(), c.ElfMachine.Namespace, c.ElfMachine.Name)
}

// Patch updates the object and its status on the API server.
func (c *MachineContext) Patch(ctx goctx.Context) error {
	return c.PatchHelper.Patch(ctx, c.ElfMachine)
}

func (c *MachineContext) GetFailureDomain() string {
	if c.Machine != nil && c.Machine.Spec.FailureDomain != nil {
		return *c.Machine.Spec.FailureDomain
	}

	return ""
}

// GetElfClusterID returns the appropriate cluster ID for a machine.
// If FailureDomains is configured and a cluster has been selected, returns the selected cluster.
// Otherwise, returns the default cluster from ElfCluster.
func (c *MachineContext) GetElfClusterID() string {
	fd := c.GetFailureDomain()
	if fd != "" {
		return fd
	} else if c.ElfCluster != nil {
		return c.ElfCluster.Spec.Cluster
	}

	return ""
}

// GetNetwork returns the network configuration for this machine.
// If FailureDomains is configured,
// returns the network configuration from the specified failure domain.
// Otherwise, returns the default network configuration from spec.
func (c *MachineContext) GetNetwork() *infrav1.NetworkSpec {
	failureDomain := c.GetFailureDomain()

	if c.ElfMachine == nil {
		return &infrav1.NetworkSpec{}
	}

	if failureDomain != "" {
		for i := range c.ElfMachine.Spec.FailureDomains {
			if c.ElfMachine.Spec.FailureDomains[i].ComputeCluster == failureDomain {
				return &c.ElfMachine.Spec.FailureDomains[i].Network
			}
		}

		return &infrav1.NetworkSpec{}
	}

	// Return the default network configuration
	return &c.ElfMachine.Spec.Network
}

// IsMachineStaticIP returns true if the IP is the static IP for the Machine.
func (c *MachineContext) IsMachineStaticIP(ip string) bool {
	network := c.GetNetwork()
	for index := range network.Devices {
		if network.Devices[index].NetworkType != infrav1.NetworkTypeIPV4 {
			continue
		}

		for _, staticIP := range network.Devices[index].IPAddrs {
			if ip == staticIP {
				return true
			}
		}
	}

	return false
}

// GetNetworkDevicesRequiringIP returns a slice of NetworkDeviceSpec which requires DHCP IP or static IP.
func (c *MachineContext) GetNetworkDevicesRequiringIP() []infrav1.NetworkDeviceSpec {
	networkDevices := []infrav1.NetworkDeviceSpec{}
	network := c.GetNetwork()

	for index := range network.Devices {
		if !network.Devices[index].HasNetworkType() {
			continue
		}

		networkDevices = append(networkDevices, network.Devices[index])
	}

	return networkDevices
}

// GetNetworkDevicesRequiringDHCP returns a slice of NetworkDeviceSpec which requires DHCP IP.
func (c *MachineContext) GetNetworkDevicesRequiringDHCP() []infrav1.NetworkDeviceSpec {
	networkDevices := []infrav1.NetworkDeviceSpec{}
	network := c.GetNetwork()

	for index := range network.Devices {
		if network.Devices[index].NetworkType == infrav1.NetworkTypeIPV4DHCP {
			networkDevices = append(networkDevices, network.Devices[index])
		}
	}

	return networkDevices
}

// GetLimitedNameservers returns a limited number of nameservers.
func (c *MachineContext) GetLimitedNameservers() []string {
	var nameservers []string
	network := c.GetNetwork()

	if len(network.Nameservers) > 0 {
		nameservers = append(nameservers, network.Nameservers...)
	}

	defaultRouteDeviceIndex := network.GetDefaultRouteDeviceIndex()
	if defaultRouteDeviceIndex < len(network.Devices) && len(network.Devices[defaultRouteDeviceIndex].Nameservers) > 0 {
		nameservers = append(nameservers, network.Devices[defaultRouteDeviceIndex].Nameservers...)
	}
	for i := range network.Devices {
		if i != defaultRouteDeviceIndex && len(network.Devices[i].Nameservers) > 0 {
			nameservers = append(nameservers, network.Devices[i].Nameservers...)
		}
	}

	limitedNameservers := []string{}
	nameserverSet := sets.NewString()
	for i := range nameservers {
		nameserver := nameservers[i]
		if nameserverSet.Has(nameserver) {
			continue
		}

		limitedNameservers = append(limitedNameservers, nameserver)
		nameserverSet.Insert(nameserver)
	}

	count := config.VM.NameserverLimit
	if count > len(limitedNameservers) {
		count = len(limitedNameservers)
	}

	return limitedNameservers[:count]
}
