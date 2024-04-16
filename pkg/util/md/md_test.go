package md

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

/*
Copyright 2024.

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

func TestMaxUnavailable(t *testing.T) {
	deployment := func(replicas int32, maxUnavailable intstr.IntOrString) clusterv1.MachineDeployment {
		return clusterv1.MachineDeployment{
			Spec: clusterv1.MachineDeploymentSpec{
				Replicas: func(i int32) *int32 { return &i }(replicas),
				Strategy: &clusterv1.MachineDeploymentStrategy{
					RollingUpdate: &clusterv1.MachineRollingUpdateDeployment{
						MaxSurge:       func(i int) *intstr.IntOrString { x := intstr.FromInt(i); return &x }(int(1)),
						MaxUnavailable: &maxUnavailable,
					},
					Type: clusterv1.RollingUpdateMachineDeploymentStrategyType,
				},
			},
		}
	}
	tests := []struct {
		name       string
		deployment clusterv1.MachineDeployment
		expected   int32
	}{
		{
			name:       "maxUnavailable less than replicas",
			deployment: deployment(10, intstr.FromInt(5)),
			expected:   int32(5),
		},
		{
			name:       "maxUnavailable equal replicas",
			deployment: deployment(10, intstr.FromInt(10)),
			expected:   int32(10),
		},
		{
			name:       "maxUnavailable greater than replicas",
			deployment: deployment(5, intstr.FromInt(10)),
			expected:   int32(5),
		},
		{
			name:       "maxUnavailable with replicas is 0",
			deployment: deployment(0, intstr.FromInt(10)),
			expected:   int32(0),
		},
		{
			name:       "maxUnavailable less than replicas with percents",
			deployment: deployment(10, intstr.FromString("50%")),
			expected:   int32(5),
		},
		{
			name:       "maxUnavailable equal replicas with percents",
			deployment: deployment(10, intstr.FromString("100%")),
			expected:   int32(10),
		},
		{
			name:       "maxUnavailable greater than replicas with percents",
			deployment: deployment(5, intstr.FromString("100%")),
			expected:   int32(5),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(MaxUnavailable(test.deployment)).To(Equal(test.expected))
		})
	}
}

func TestResolveFenceposts(t *testing.T) {
	tests := []struct {
		maxSurge          string
		maxUnavailable    string
		desired           int32
		expectSurge       int32
		expectUnavailable int32
		expectError       bool
	}{
		{
			maxSurge:          "0%",
			maxUnavailable:    "0%",
			desired:           0,
			expectSurge:       0,
			expectUnavailable: 1,
			expectError:       false,
		},
		{
			maxSurge:          "39%",
			maxUnavailable:    "39%",
			desired:           10,
			expectSurge:       4,
			expectUnavailable: 3,
			expectError:       false,
		},
		{
			maxSurge:          "oops",
			maxUnavailable:    "39%",
			desired:           10,
			expectSurge:       0,
			expectUnavailable: 0,
			expectError:       true,
		},
		{
			maxSurge:          "55%",
			maxUnavailable:    "urg",
			desired:           10,
			expectSurge:       0,
			expectUnavailable: 0,
			expectError:       true,
		},
		{
			maxSurge:          "5",
			maxUnavailable:    "1",
			desired:           7,
			expectSurge:       0,
			expectUnavailable: 0,
			expectError:       true,
		},
	}

	for _, test := range tests {
		t.Run("maxSurge="+test.maxSurge, func(t *testing.T) {
			g := NewWithT(t)

			maxSurge := intstr.FromString(test.maxSurge)
			maxUnavail := intstr.FromString(test.maxUnavailable)
			surge, unavail, err := ResolveFenceposts(&maxSurge, &maxUnavail, test.desired)
			if test.expectError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
			g.Expect(surge).To(Equal(test.expectSurge))
			g.Expect(unavail).To(Equal(test.expectUnavailable))
		})
	}
}

func TestMaxSurge(t *testing.T) {
	maxSurge := intstr.FromInt(1)
	maxUnavailable := intstr.FromInt(1)
	tests := []struct {
		strategy    *clusterv1.MachineDeploymentStrategy
		expectSurge int32
	}{
		{
			strategy: &clusterv1.MachineDeploymentStrategy{
				Type: clusterv1.OnDeleteMachineDeploymentStrategyType,
			},
			expectSurge: 0,
		},
		{
			strategy: &clusterv1.MachineDeploymentStrategy{
				Type: clusterv1.RollingUpdateMachineDeploymentStrategyType,
				RollingUpdate: &clusterv1.MachineRollingUpdateDeployment{
					MaxSurge:       &maxSurge,
					MaxUnavailable: &maxUnavailable,
				},
			},
			expectSurge: 1,
		},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("maxSurge=%d", tc.expectSurge), func(t *testing.T) {
			g := NewWithT(t)

			deployment := clusterv1.MachineDeployment{
				Spec: clusterv1.MachineDeploymentSpec{
					Replicas: pointer.Int32(1),
					Strategy: tc.strategy,
				},
			}
			surge := MaxSurge(deployment)
			g.Expect(surge).To(Equal(tc.expectSurge))
		})
	}
}

func TestIsMDInRollingUpdate(t *testing.T) {
	tests := []struct {
		specReplicas    int32
		statusReplicas  int32
		updatedReplicas int32
		isUpdated       bool
	}{
		{
			specReplicas:    1,
			statusReplicas:  1,
			updatedReplicas: 1,
			isUpdated:       false,
		},
		{
			specReplicas:    3,
			statusReplicas:  3,
			updatedReplicas: 3,
			isUpdated:       false,
		},
		{
			specReplicas:    3,
			statusReplicas:  3,
			updatedReplicas: 2,
			isUpdated:       true,
		},
		{
			specReplicas:    3,
			statusReplicas:  3,
			updatedReplicas: 1,
			isUpdated:       true,
		},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%v", tc), func(t *testing.T) {
			g := NewWithT(t)

			deployment := clusterv1.MachineDeployment{
				Spec: clusterv1.MachineDeploymentSpec{
					Replicas: pointer.Int32(tc.specReplicas),
				},
				Status: clusterv1.MachineDeploymentStatus{
					Replicas:        tc.statusReplicas,
					UpdatedReplicas: tc.updatedReplicas,
				},
			}
			g.Expect(IsMDInRollingUpdate(&deployment)).To(Equal(tc.isUpdated))
		})
	}
}
