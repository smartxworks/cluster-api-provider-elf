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

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

func TestMachineContextString(t *testing.T) {
	g := NewWithT(t)

	elfMachine := &infrav1.ElfMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-machine",
		},
	}
	ctx := &MachineContext{
		ElfMachine: elfMachine,
	}

	result := ctx.String()
	g.Expect(result).To(ContainSubstring("default/test-machine"))
}

func TestGetFailureDomain(t *testing.T) {
	tests := []struct {
		name     string
		machine  *clusterv1.Machine
		expected string
	}{
		{
			name:     "nil Machine returns empty string",
			machine:  nil,
			expected: "",
		},
		{
			name: "Machine with nil FailureDomain returns empty string",
			machine: &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					FailureDomain: nil,
				},
			},
			expected: "",
		},
		{
			name: "Machine with FailureDomain returns its value",
			machine: &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					FailureDomain: strPtr("zone-a"),
				},
			},
			expected: "zone-a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := &MachineContext{
				ElfMachine: &infrav1.ElfMachine{},
				Machine:    tc.machine,
			}
			g.Expect(ctx.GetFailureDomain()).To(Equal(tc.expected))
		})
	}
}

func TestGetCloudFailureDomain(t *testing.T) {
	tests := []struct {
		name       string
		machine    *clusterv1.Machine
		elfMachine *infrav1.ElfMachine
		expected   *infrav1.CloudFailureDomain
	}{
		{
			name:       "no FailureDomain on Machine returns nil",
			machine:    nil,
			elfMachine: &infrav1.ElfMachine{},
			expected:   nil,
		},
		{
			name: "empty FailureDomain returns nil",
			machine: &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					FailureDomain: strPtr(""),
				},
			},
			elfMachine: &infrav1.ElfMachine{},
			expected:   nil,
		},
		{
			name: "nil ElfMachine returns nil",
			machine: &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					FailureDomain: strPtr("zone-a"),
				},
			},
			elfMachine: nil,
			expected:   nil,
		},
		{
			name: "FailureDomain not found in ElfMachine FailureDomains returns nil",
			machine: &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					FailureDomain: strPtr("zone-a"),
				},
			},
			elfMachine: &infrav1.ElfMachine{
				Spec: infrav1.ElfMachineSpec{
					FailureDomains: []infrav1.CloudFailureDomain{
						{ComputeCluster: "zone-b"},
					},
				},
			},
			expected: nil,
		},
		{
			name: "matching FailureDomain returns the CloudFailureDomain",
			machine: &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					FailureDomain: strPtr("zone-a"),
				},
			},
			elfMachine: &infrav1.ElfMachine{
				Spec: infrav1.ElfMachineSpec{
					FailureDomains: []infrav1.CloudFailureDomain{
						{ComputeCluster: "zone-b"},
						{ComputeCluster: "zone-a"},
					},
				},
			},
			expected: &infrav1.CloudFailureDomain{ComputeCluster: "zone-a"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := &MachineContext{
				Machine:    tc.machine,
				ElfMachine: tc.elfMachine,
			}
			g.Expect(ctx.GetCloudFailureDomain()).To(Equal(tc.expected))
		})
	}
}

func TestGetElfClusterID(t *testing.T) {
	tests := []struct {
		name       string
		machine    *clusterv1.Machine
		elfCluster *infrav1.ElfCluster
		expected   string
	}{
		{
			name:       "no Machine and no ElfCluster returns empty string",
			machine:    nil,
			elfCluster: nil,
			expected:   "",
		},
		{
			name:     "no FailureDomain and nil ElfCluster returns empty string",
			machine:  &clusterv1.Machine{},
			expected: "",
		},
		{
			name: "FailureDomain set returns FailureDomain value",
			machine: &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					FailureDomain: strPtr("zone-a"),
				},
			},
			elfCluster: &infrav1.ElfCluster{
				Spec: infrav1.ElfClusterSpec{
					Cluster: "default-cluster",
				},
			},
			expected: "zone-a",
		},
		{
			name:    "no FailureDomain falls back to ElfCluster.Spec.Cluster",
			machine: nil,
			elfCluster: &infrav1.ElfCluster{
				Spec: infrav1.ElfClusterSpec{
					Cluster: "default-cluster",
				},
			},
			expected: "default-cluster",
		},
		{
			name: "empty FailureDomain falls back to ElfCluster.Spec.Cluster",
			machine: &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					FailureDomain: strPtr(""),
				},
			},
			elfCluster: &infrav1.ElfCluster{
				Spec: infrav1.ElfClusterSpec{
					Cluster: "default-cluster",
				},
			},
			expected: "default-cluster",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := &MachineContext{
				ElfMachine: &infrav1.ElfMachine{},
				Machine:    tc.machine,
				ElfCluster: tc.elfCluster,
			}
			g.Expect(ctx.GetElfClusterID()).To(Equal(tc.expected))
		})
	}
}

// strPtr is a helper to get a pointer to a string literal.
func strPtr(s string) *string {
	return &s
}
