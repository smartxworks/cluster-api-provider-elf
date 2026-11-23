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
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

func TestMachineContextString(t *testing.T) {
	g := NewWithT(t)

	elfMachine := &capev1.ElfMachine{
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
		machine  *capiv1.Machine
		expected string
	}{
		{
			name:     "nil Machine returns empty string",
			machine:  nil,
			expected: "",
		},
		{
			name: "Machine with nil FailureDomain returns empty string",
			machine: &capiv1.Machine{
				Spec: capiv1.MachineSpec{
					FailureDomain: "",
				},
			},
			expected: "",
		},
		{
			name: "Machine with FailureDomain returns its value",
			machine: &capiv1.Machine{
				Spec: capiv1.MachineSpec{
					FailureDomain: "zone-a",
				},
			},
			expected: "zone-a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := &MachineContext{
				ElfMachine: &capev1.ElfMachine{},
				Machine:    tc.machine,
			}
			g.Expect(ctx.GetFailureDomain()).To(Equal(tc.expected))
		})
	}
}

func TestGetCloudFailureDomain(t *testing.T) {
	tests := []struct {
		name       string
		machine    *capiv1.Machine
		elfMachine *capev1.ElfMachine
		expected   *capev1.CloudFailureDomain
	}{
		{
			name:       "no FailureDomain on Machine returns nil",
			machine:    nil,
			elfMachine: &capev1.ElfMachine{},
			expected:   nil,
		},
		{
			name: "empty FailureDomain returns nil",
			machine: &capiv1.Machine{
				Spec: capiv1.MachineSpec{
					FailureDomain: "",
				},
			},
			elfMachine: &capev1.ElfMachine{},
			expected:   nil,
		},
		{
			name: "nil ElfMachine returns nil",
			machine: &capiv1.Machine{
				Spec: capiv1.MachineSpec{
					FailureDomain: "zone-a",
				},
			},
			elfMachine: nil,
			expected:   nil,
		},
		{
			name: "FailureDomain not found in ElfMachine FailureDomains returns nil",
			machine: &capiv1.Machine{
				Spec: capiv1.MachineSpec{
					FailureDomain: "zone-a",
				},
			},
			elfMachine: &capev1.ElfMachine{
				Spec: capev1.ElfMachineSpec{
					FailureDomains: []capev1.CloudFailureDomain{
						{ComputeCluster: "zone-b"},
					},
				},
			},
			expected: nil,
		},
		{
			name: "matching FailureDomain returns the CloudFailureDomain",
			machine: &capiv1.Machine{
				Spec: capiv1.MachineSpec{
					FailureDomain: "zone-a",
				},
			},
			elfMachine: &capev1.ElfMachine{
				Spec: capev1.ElfMachineSpec{
					FailureDomains: []capev1.CloudFailureDomain{
						{ComputeCluster: "zone-b"},
						{ComputeCluster: "zone-a"},
					},
				},
			},
			expected: &capev1.CloudFailureDomain{ComputeCluster: "zone-a"},
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
		machine    *capiv1.Machine
		elfCluster *capev1.ElfCluster
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
			machine:  &capiv1.Machine{},
			expected: "",
		},
		{
			name: "FailureDomain set returns FailureDomain value",
			machine: &capiv1.Machine{
				Spec: capiv1.MachineSpec{
					FailureDomain: "zone-a",
				},
			},
			elfCluster: &capev1.ElfCluster{
				Spec: capev1.ElfClusterSpec{
					Cluster: "default-cluster",
				},
			},
			expected: "zone-a",
		},
		{
			name:    "no FailureDomain falls back to ElfCluster.Spec.Cluster",
			machine: nil,
			elfCluster: &capev1.ElfCluster{
				Spec: capev1.ElfClusterSpec{
					Cluster: "default-cluster",
				},
			},
			expected: "default-cluster",
		},
		{
			name: "empty FailureDomain falls back to ElfCluster.Spec.Cluster",
			machine: &capiv1.Machine{
				Spec: capiv1.MachineSpec{
					FailureDomain: "",
				},
			},
			elfCluster: &capev1.ElfCluster{
				Spec: capev1.ElfClusterSpec{
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
				ElfMachine: &capev1.ElfMachine{},
				Machine:    tc.machine,
				ElfCluster: tc.elfCluster,
			}
			g.Expect(ctx.GetElfClusterID()).To(Equal(tc.expected))
		})
	}
}

func TestGetStorageConfig(t *testing.T) {
	tests := []struct {
		name       string
		elfCluster *capev1.ElfCluster
		expected   *models.StorageConfig
	}{
		{
			name:       "When ElfCluster is nil should return nil",
			elfCluster: nil,
			expected:   nil,
		},
		{
			name:       "When storage config is not set should return nil",
			elfCluster: &capev1.ElfCluster{},
			expected:   nil,
		},
		{
			name: "When one storage config is set should return it",
			elfCluster: &capev1.ElfCluster{
				Spec: capev1.ElfClusterSpec{
					StorageCluster: capev1.StorageCluster{StorageClusterID: "storage-cluster"},
				},
			},
			expected: &models.StorageConfig{StorageClusterID: service.TowerString("storage-cluster")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := &MachineContext{
				ElfCluster: tc.elfCluster,
			}
			g.Expect(ctx.GetStorageConfig()).To(Equal(tc.expected))
		})
	}
}
