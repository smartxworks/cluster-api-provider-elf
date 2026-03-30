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
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetNetworkDevicesRequiringIP(t *testing.T) {
	tests := []struct {
		name     string
		devices  []NetworkDeviceSpec
		expected []NetworkDeviceSpec
	}{
		{
			name:     "no devices",
			devices:  []NetworkDeviceSpec{},
			expected: []NetworkDeviceSpec{},
		},
		{
			name: "all NetworkTypeNone devices are excluded",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeNone},
				{NetworkType: NetworkTypeNone},
			},
			expected: []NetworkDeviceSpec{},
		},
		{
			name: "IPV4 device is included",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4, IPAddrs: []string{"192.168.1.10"}},
			},
			expected: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4, IPAddrs: []string{"192.168.1.10"}},
			},
		},
		{
			name: "IPV4DHCP device is included",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4DHCP},
			},
			expected: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4DHCP},
			},
		},
		{
			name: "mixed devices: None excluded, others included",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeNone},
				{NetworkType: NetworkTypeIPV4, IPAddrs: []string{"10.0.0.1"}},
				{NetworkType: NetworkTypeIPV4DHCP},
			},
			expected: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4, IPAddrs: []string{"10.0.0.1"}},
				{NetworkType: NetworkTypeIPV4DHCP},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			m := &ElfMachine{
				Spec: ElfMachineSpec{
					Network: NetworkSpec{Devices: tc.devices},
				},
			}
			g.Expect(m.GetNetworkDevicesRequiringIP()).To(Equal(tc.expected))
		})
	}
}

func TestGetNetworkDevicesRequiringDHCP(t *testing.T) {
	tests := []struct {
		name     string
		devices  []NetworkDeviceSpec
		expected []NetworkDeviceSpec
	}{
		{
			name:     "no devices",
			devices:  []NetworkDeviceSpec{},
			expected: []NetworkDeviceSpec{},
		},
		{
			name: "NetworkTypeNone devices are excluded",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeNone},
			},
			expected: []NetworkDeviceSpec{},
		},
		{
			name: "static IPV4 device is excluded",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4, IPAddrs: []string{"192.168.1.10"}},
			},
			expected: []NetworkDeviceSpec{},
		},
		{
			name: "IPV4DHCP device is included",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4DHCP},
			},
			expected: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4DHCP},
			},
		},
		{
			name: "mixed devices: only DHCP included",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeNone},
				{NetworkType: NetworkTypeIPV4, IPAddrs: []string{"10.0.0.1"}},
				{NetworkType: NetworkTypeIPV4DHCP},
				{NetworkType: NetworkTypeIPV4DHCP},
			},
			expected: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4DHCP},
				{NetworkType: NetworkTypeIPV4DHCP},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			m := &ElfMachine{
				Spec: ElfMachineSpec{
					Network: NetworkSpec{Devices: tc.devices},
				},
			}
			g.Expect(m.GetNetworkDevicesRequiringDHCP()).To(Equal(tc.expected))
		})
	}
}

func TestIsMachineStaticIP(t *testing.T) {
	tests := []struct {
		name     string
		devices  []NetworkDeviceSpec
		ip       string
		expected bool
	}{
		{
			name:     "no devices",
			devices:  []NetworkDeviceSpec{},
			ip:       "192.168.1.10",
			expected: false,
		},
		{
			name: "IP matches static IPV4 device",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4, IPAddrs: []string{"192.168.1.10", "192.168.1.11"}},
			},
			ip:       "192.168.1.10",
			expected: true,
		},
		{
			name: "IP matches second address in static IPV4 device",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4, IPAddrs: []string{"192.168.1.10", "192.168.1.11"}},
			},
			ip:       "192.168.1.11",
			expected: true,
		},
		{
			name: "IP does not match any static IPV4 device",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4, IPAddrs: []string{"192.168.1.10"}},
			},
			ip:       "10.0.0.1",
			expected: false,
		},
		{
			name: "IP is not checked against DHCP devices",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4DHCP, IPAddrs: []string{"192.168.1.10"}},
			},
			ip:       "192.168.1.10",
			expected: false,
		},
		{
			name: "IP is not checked against None-type devices",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeNone, IPAddrs: []string{"192.168.1.10"}},
			},
			ip:       "192.168.1.10",
			expected: false,
		},
		{
			name: "IP matches one of multiple devices",
			devices: []NetworkDeviceSpec{
				{NetworkType: NetworkTypeIPV4DHCP},
				{NetworkType: NetworkTypeIPV4, IPAddrs: []string{"10.0.0.5"}},
			},
			ip:       "10.0.0.5",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			m := &ElfMachine{
				Spec: ElfMachineSpec{
					Network: NetworkSpec{Devices: tc.devices},
				},
			}
			g.Expect(m.IsMachineStaticIP(tc.ip)).To(Equal(tc.expected))
		})
	}
}

func TestGetLimitedNameservers(t *testing.T) {
	tests := []struct {
		name     string
		network  NetworkSpec
		limit    int
		expected []string
	}{
		{
			name:     "no nameservers",
			network:  NetworkSpec{},
			limit:    3,
			expected: []string{},
		},
		{
			name: "global nameservers only, within limit",
			network: NetworkSpec{
				Nameservers: []string{"8.8.8.8", "8.8.4.4"},
			},
			limit:    3,
			expected: []string{"8.8.8.8", "8.8.4.4"},
		},
		{
			name: "global nameservers truncated by limit",
			network: NetworkSpec{
				Nameservers: []string{"8.8.8.8", "8.8.4.4", "1.1.1.1", "1.0.0.1"},
			},
			limit:    2,
			expected: []string{"8.8.8.8", "8.8.4.4"},
		},
		{
			name: "device nameservers from default-route device come before other devices",
			network: NetworkSpec{
				Devices: []NetworkDeviceSpec{
					{NetworkType: NetworkTypeIPV4, Nameservers: []string{"10.0.0.1"}},
					{NetworkType: NetworkTypeIPV4, DefaultRoute: true, Nameservers: []string{"172.16.0.1"}},
				},
			},
			limit:    3,
			expected: []string{"172.16.0.1", "10.0.0.1"},
		},
		{
			name: "global nameservers come first, then default-route device, then others",
			network: NetworkSpec{
				Nameservers: []string{"8.8.8.8"},
				Devices: []NetworkDeviceSpec{
					{NetworkType: NetworkTypeIPV4, Nameservers: []string{"10.0.0.1"}},
					{NetworkType: NetworkTypeIPV4, DefaultRoute: true, Nameservers: []string{"172.16.0.1"}},
				},
			},
			limit:    3,
			expected: []string{"8.8.8.8", "172.16.0.1", "10.0.0.1"},
		},
		{
			name: "duplicate nameservers are deduplicated",
			network: NetworkSpec{
				Nameservers: []string{"8.8.8.8"},
				Devices: []NetworkDeviceSpec{
					{NetworkType: NetworkTypeIPV4, DefaultRoute: true, Nameservers: []string{"8.8.8.8", "1.1.1.1"}},
				},
			},
			limit:    3,
			expected: []string{"8.8.8.8", "1.1.1.1"},
		},
		{
			name: "limit 0 returns empty",
			network: NetworkSpec{
				Nameservers: []string{"8.8.8.8", "1.1.1.1"},
			},
			limit:    0,
			expected: []string{},
		},
		{
			name: "limit larger than available nameservers returns all",
			network: NetworkSpec{
				Nameservers: []string{"8.8.8.8"},
			},
			limit:    10,
			expected: []string{"8.8.8.8"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			m := &ElfMachine{
				Spec: ElfMachineSpec{
					Network: tc.network,
				},
			}
			g.Expect(m.GetLimitedNameservers(tc.limit)).To(Equal(tc.expected))
		})
	}
}

func TestGetVMFirstBootTimestamp(t *testing.T) {
	tests := []struct {
		name         string
		annotations  map[string]string
		expectedNil  bool
		expectedTime time.Time
	}{
		{
			name:        "nil annotations returns nil",
			annotations: nil,
			expectedNil: true,
		},
		{
			name:        "missing annotation returns nil",
			annotations: map[string]string{},
			expectedNil: true,
		},
		{
			name: "invalid timestamp annotation returns nil",
			annotations: map[string]string{
				VMFirstBootTimestampAnnotation: "not-a-timestamp",
			},
			expectedNil: true,
		},
		{
			name: "valid timestamp annotation returns correct time",
			annotations: map[string]string{
				VMFirstBootTimestampAnnotation: "2024-01-15T10:30:00Z",
			},
			expectedNil:  false,
			expectedTime: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name: "valid timestamp with timezone offset",
			annotations: map[string]string{
				VMFirstBootTimestampAnnotation: "2024-06-01T08:00:00+08:00",
			},
			expectedNil:  false,
			expectedTime: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			m := &ElfMachine{}
			m.SetAnnotations(tc.annotations)
			result := m.GetVMFirstBootTimestamp()
			if tc.expectedNil {
				g.Expect(result).To(BeNil())
			} else {
				g.Expect(result).NotTo(BeNil())
				expected := metav1.NewTime(tc.expectedTime)
				g.Expect(result.UTC()).To(Equal(expected.UTC()))
			}
		})
	}
}
