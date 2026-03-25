/*
Copyright 2025.

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
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

func newMachineCtx(failureDomain string, elfMachine *infrav1.ElfMachine, elfCluster *infrav1.ElfCluster) *MachineContext {
	machine := &clusterv1.Machine{}
	if failureDomain != "" {
		machine.Spec.FailureDomain = ptr.To(failureDomain)
	}
	return &MachineContext{
		Machine:    machine,
		ElfMachine: elfMachine,
		ElfCluster: elfCluster,
	}
}

func TestGetFailureDomain(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// no machine
	c := &MachineContext{}
	g.Expect(c.GetFailureDomain()).To(gomega.Equal(""))

	// machine with nil FailureDomain
	c = &MachineContext{Machine: &clusterv1.Machine{}}
	g.Expect(c.GetFailureDomain()).To(gomega.Equal(""))

	// machine with FailureDomain set
	c = &MachineContext{Machine: &clusterv1.Machine{}}
	c.Machine.Spec.FailureDomain = ptr.To("cluster-1")
	g.Expect(c.GetFailureDomain()).To(gomega.Equal("cluster-1"))
}

func TestGetElfClusterID(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// no machine, no elfCluster
	c := &MachineContext{}
	g.Expect(c.GetElfClusterID()).To(gomega.Equal(""))

	// no FailureDomain, returns ElfCluster.Spec.Cluster
	elfCluster := &infrav1.ElfCluster{}
	elfCluster.Spec.Cluster = "default-cluster"
	c = newMachineCtx("", &infrav1.ElfMachine{}, elfCluster)
	g.Expect(c.GetElfClusterID()).To(gomega.Equal("default-cluster"))

	// FailureDomain set, returns FailureDomain (overrides ElfCluster)
	c = newMachineCtx("fd-cluster", &infrav1.ElfMachine{}, elfCluster)
	g.Expect(c.GetElfClusterID()).To(gomega.Equal("fd-cluster"))
}

func TestGetNetwork(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// nil ElfMachine returns empty NetworkSpec
	c := &MachineContext{Machine: &clusterv1.Machine{}}
	g.Expect(c.GetNetwork()).To(gomega.Equal(&infrav1.NetworkSpec{}))

	defaultNetwork := infrav1.NetworkSpec{
		Devices: []infrav1.NetworkDeviceSpec{
			{NetworkType: infrav1.NetworkTypeIPV4DHCP, Vlan: "vlan0"},
		},
	}
	fdNetwork := infrav1.NetworkSpec{
		Devices: []infrav1.NetworkDeviceSpec{
			{NetworkType: infrav1.NetworkTypeIPV4, Vlan: "vlan1"},
		},
	}
	elfMachine := &infrav1.ElfMachine{
		Spec: infrav1.ElfMachineSpec{
			Network: defaultNetwork,
			FailureDomains: []infrav1.CloudFailureDomain{
				{ComputeCluster: "cluster-1", Network: fdNetwork},
			},
		},
	}

	// no FailureDomain: returns default network
	c = newMachineCtx("", elfMachine, nil)
	g.Expect(c.GetNetwork()).To(gomega.Equal(&defaultNetwork))

	// FailureDomain matches: returns failure domain network
	c = newMachineCtx("cluster-1", elfMachine, nil)
	g.Expect(c.GetNetwork()).To(gomega.Equal(&fdNetwork))

	// FailureDomain set but no match: returns empty NetworkSpec
	c = newMachineCtx("cluster-999", elfMachine, nil)
	g.Expect(c.GetNetwork()).To(gomega.Equal(&infrav1.NetworkSpec{}))
}

func TestIsMachineStaticIP(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	elfMachine := &infrav1.ElfMachine{
		Spec: infrav1.ElfMachineSpec{
			Network: infrav1.NetworkSpec{
				Devices: []infrav1.NetworkDeviceSpec{
					{NetworkType: infrav1.NetworkTypeIPV4, IPAddrs: []string{"192.168.1.10", "192.168.1.11"}},
					{NetworkType: infrav1.NetworkTypeIPV4DHCP},
				},
			},
		},
	}
	c := newMachineCtx("", elfMachine, nil)

	g.Expect(c.IsMachineStaticIP("192.168.1.10")).To(gomega.BeTrue())
	g.Expect(c.IsMachineStaticIP("192.168.1.11")).To(gomega.BeTrue())
	g.Expect(c.IsMachineStaticIP("10.0.0.1")).To(gomega.BeFalse())
	// DHCP device IP should not be treated as static
	g.Expect(c.IsMachineStaticIP("")).To(gomega.BeFalse())
}

func TestGetNetworkDevicesRequiringIP(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	elfMachine := &infrav1.ElfMachine{
		Spec: infrav1.ElfMachineSpec{
			Network: infrav1.NetworkSpec{
				Devices: []infrav1.NetworkDeviceSpec{
					{NetworkType: infrav1.NetworkTypeIPV4},
					{NetworkType: infrav1.NetworkTypeIPV4DHCP},
					{NetworkType: infrav1.NetworkTypeNone},
					{NetworkType: ""},
				},
			},
		},
	}
	c := newMachineCtx("", elfMachine, nil)
	devices := c.GetNetworkDevicesRequiringIP()
	// NetworkTypeIPV4 and NetworkTypeIPV4DHCP require IP; NONE and empty do not
	g.Expect(devices).To(gomega.HaveLen(2))
	g.Expect(devices[0].NetworkType).To(gomega.Equal(infrav1.NetworkTypeIPV4))
	g.Expect(devices[1].NetworkType).To(gomega.Equal(infrav1.NetworkTypeIPV4DHCP))
}

func TestGetNetworkDevicesRequiringDHCP(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	elfMachine := &infrav1.ElfMachine{
		Spec: infrav1.ElfMachineSpec{
			Network: infrav1.NetworkSpec{
				Devices: []infrav1.NetworkDeviceSpec{
					{NetworkType: infrav1.NetworkTypeIPV4},
					{NetworkType: infrav1.NetworkTypeIPV4DHCP},
					{NetworkType: infrav1.NetworkTypeIPV4DHCP},
					{NetworkType: infrav1.NetworkTypeNone},
				},
			},
		},
	}
	c := newMachineCtx("", elfMachine, nil)
	devices := c.GetNetworkDevicesRequiringDHCP()
	// only NetworkTypeIPV4DHCP devices
	g.Expect(devices).To(gomega.HaveLen(2))
	for _, d := range devices {
		g.Expect(d.NetworkType).To(gomega.Equal(infrav1.NetworkTypeIPV4DHCP))
	}
}

func TestGetLimitedNameservers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	testCases := []struct {
		name        string
		nameservers []string
		devices     []infrav1.NetworkDeviceSpec
		expected    []string
	}{
		{"should return empty server when no nameservers", nil, nil, []string{}},
		{"should return servers from nameservers", []string{"1.1.1.1"}, nil, []string{"1.1.1.1"}},
		{"should return servers from devices", nil, []infrav1.NetworkDeviceSpec{{Nameservers: []string{"6.6.6.6"}}, {Nameservers: nil}}, []string{"6.6.6.6"}},
		{"should return servers from nameservers and devices", []string{"1.1.1.1"}, []infrav1.NetworkDeviceSpec{{Nameservers: []string{"6.6.6.6"}}}, []string{"1.1.1.1", "6.6.6.6"}},
		{"should skip duplicate servers", []string{"1.1.1.1", "2.2.2.2"}, []infrav1.NetworkDeviceSpec{{Nameservers: []string{"2.2.2.2", "3.3.3.3"}}, {Nameservers: []string{"5.5.5.5", "6.6.6.6"}}}, []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}},
		{"should prioritize the default route servers", []string{"1.1.1.1"}, []infrav1.NetworkDeviceSpec{{Nameservers: []string{"1.1.1.1", "3.3.3.3", "9.9.9.9"}}, {Nameservers: []string{"1.1.1.1", "6.6.6.6"}, DefaultRoute: true}}, []string{"1.1.1.1", "6.6.6.6", "3.3.3.3"}},
	}

	elfMachine := &infrav1.ElfMachine{Spec: infrav1.ElfMachineSpec{Network: infrav1.NetworkSpec{}}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			elfMachine.Spec.Network.Nameservers = tc.nameservers
			elfMachine.Spec.Network.Devices = tc.devices
			machineCtx := newMachineCtx("", elfMachine, nil)
			dnsServers := machineCtx.GetLimitedNameservers()
			g.Expect(dnsServers).To(gomega.Equal(tc.expected))
		})
	}
}
