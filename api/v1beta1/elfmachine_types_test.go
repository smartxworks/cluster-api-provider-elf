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

package v1beta1

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestLimitDNSServers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	testCases := []struct {
		name         string
		nameserevers []string
		devices      []NetworkDeviceSpec
		expected     []string
	}{
		{"should return empty server when no nameservers", nil, nil, []string{}},
		{"should return servers from nameservers", []string{"1.1.1.1"}, nil, []string{"1.1.1.1"}},
		{"should return servers from devices", nil, []NetworkDeviceSpec{{Nameservers: []string{"6.6.6.6"}}, {Nameservers: nil}}, []string{"6.6.6.6"}},
		{"should return servers from nameservers and devices", []string{"1.1.1.1"}, []NetworkDeviceSpec{{Nameservers: []string{"6.6.6.6"}}}, []string{"1.1.1.1", "6.6.6.6"}},
		{"should skip duplicate servers", []string{"1.1.1.1", "2.2.2.2"}, []NetworkDeviceSpec{{Nameservers: []string{"2.2.2.2", "3.3.3.3"}}, {Nameservers: []string{"5.5.5.5", "6.6.6.6"}}}, []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}},
		{"should prioritize the default route servers", []string{"1.1.1.1"}, []NetworkDeviceSpec{{Nameservers: []string{"1.1.1.1", "3.3.3.3", "9.9.9.9"}}, {Nameservers: []string{"1.1.1.1", "6.6.6.6"}, DefaultRoute: true}}, []string{"1.1.1.1", "6.6.6.6", "3.3.3.3"}},
	}

	elfMachine := &ElfMachine{Spec: ElfMachineSpec{Network: NetworkSpec{}}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			elfMachine.Spec.Network.Nameservers = tc.nameserevers
			elfMachine.Spec.Network.Devices = tc.devices
			dnsServers := elfMachine.GetLimitedNameservers(3)
			g.Expect(dnsServers).To(gomega.Equal(tc.expected))
		})
	}
}
