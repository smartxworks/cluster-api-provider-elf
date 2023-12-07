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

package machine

import (
	"fmt"
	"testing"

	"github.com/onsi/gomega"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

func TestGetElfMachinesInCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	elfCluster, cluster := fake.NewClusterObjects()
	elfMachine, _ := fake.NewMachineObjects(elfCluster, cluster)
	ctx := fake.NewControllerManagerContext(elfMachine)

	t.Run("should return ElfMachines", func(t *testing.T) {
		elfMachines, err := GetElfMachinesInCluster(ctx, ctx.Client, cluster.Namespace, cluster.Name)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(elfMachines).To(gomega.HaveLen(1))
	})
}

func TestGetControlPlaneElfMachinesInCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	elfCluster, cluster := fake.NewClusterObjects()
	elfMachine1, _ := fake.NewMachineObjects(elfCluster, cluster)
	elfMachine2, _ := fake.NewMachineObjects(elfCluster, cluster)
	fake.ToControlPlaneMachine(elfMachine1, fake.NewKCP())
	ctx := fake.NewControllerManagerContext(elfMachine1, elfMachine2)

	t.Run("should return Control Plane ElfMachines", func(t *testing.T) {
		elfMachines, err := GetControlPlaneElfMachinesInCluster(ctx, ctx.Client, cluster.Namespace, cluster.Name)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(elfMachines).To(gomega.HaveLen(1))
		g.Expect(elfMachines[0].Name).To(gomega.Equal(elfMachine1.Name))
	})
}

func TestIsControlPlaneMachine(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	elfCluster, cluster := fake.NewClusterObjects()
	_, machine1 := fake.NewMachineObjects(elfCluster, cluster)
	_, machine2 := fake.NewMachineObjects(elfCluster, cluster)
	fake.ToControlPlaneMachine(machine1, fake.NewKCP())
	fake.ToWorkerMachine(machine2, fake.NewMD())

	t.Run("CP Machine returns true, Worker node returns false", func(t *testing.T) {
		g.Expect(IsControlPlaneMachine(machine1)).To(gomega.BeTrue())
		g.Expect(IsControlPlaneMachine(machine2)).To(gomega.BeFalse())
	})
}

func TestGetNodeGroupName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	elfCluster, cluster := fake.NewClusterObjects()
	_, machine1 := fake.NewMachineObjects(elfCluster, cluster)
	_, machine2 := fake.NewMachineObjects(elfCluster, cluster)
	kcp := fake.NewKCP()
	kcp.Name = fmt.Sprintf("%s-kcp", cluster.Name)
	md := fake.NewMD()
	md.Name = fmt.Sprintf("%s-md", cluster.Name)
	fake.ToControlPlaneMachine(machine1, kcp)
	fake.ToWorkerMachine(machine2, md)

	t.Run("CP Machine returns true, Worker node returns false", func(t *testing.T) {
		g.Expect(GetNodeGroupName(machine1)).To(gomega.Equal("kcp"))
		g.Expect(GetNodeGroupName(machine2)).To(gomega.Equal("md"))
	})
}

func TestConvertProviderIDToUUID(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	testCases := []struct {
		name         string
		providerID   *string
		expectedUUID string
	}{
		{
			name:         "nil providerID",
			providerID:   nil,
			expectedUUID: "",
		},
		{
			name:         "empty providerID",
			providerID:   toString(""),
			expectedUUID: "",
		},
		{
			name:         "invalid providerID",
			providerID:   toString("1234"),
			expectedUUID: "",
		},
		{
			name:         "missing prefix",
			providerID:   toString("12345678-1234-1234-1234-123456789abc"),
			expectedUUID: "",
		},
		{
			name:         "valid providerID",
			providerID:   toString("elf://12345678-1234-1234-1234-123456789abc"),
			expectedUUID: "12345678-1234-1234-1234-123456789abc",
		},
		{
			name:         "mixed case",
			providerID:   toString("elf://12345678-1234-1234-1234-123456789AbC"),
			expectedUUID: "12345678-1234-1234-1234-123456789AbC",
		},
		{
			name:         "invalid hex chars",
			providerID:   toString("elf://12345678-1234-1234-1234-123456789abg"),
			expectedUUID: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualUUID := ConvertProviderIDToUUID(tc.providerID)
			g.Expect(actualUUID).To(gomega.Equal(tc.expectedUUID))
		})
	}
}

func TestConvertUUIDtoProviderID(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	testCases := []struct {
		name               string
		uuid               string
		expectedProviderID string
	}{
		{
			name:               "empty uuid",
			uuid:               "",
			expectedProviderID: "",
		},
		{
			name:               "invalid uuid",
			uuid:               "1234",
			expectedProviderID: "",
		},
		{
			name:               "valid uuid",
			uuid:               "12345678-1234-1234-1234-123456789abc",
			expectedProviderID: "elf://12345678-1234-1234-1234-123456789abc",
		},
		{
			name:               "mixed case",
			uuid:               "12345678-1234-1234-1234-123456789AbC",
			expectedProviderID: "elf://12345678-1234-1234-1234-123456789AbC",
		},
		{
			name:               "invalid hex chars",
			uuid:               "12345678-1234-1234-1234-123456789abg",
			expectedProviderID: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualProviderID := ConvertUUIDToProviderID(tc.uuid)
			g.Expect(actualProviderID).To(gomega.Equal(tc.expectedProviderID))
		})
	}
}

func TestGetNetworkStatus(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	testCases := []struct {
		name          string
		ips           string
		networkStatus []infrav1.NetworkStatus
	}{
		{
			name:          "empty",
			ips:           "",
			networkStatus: []infrav1.NetworkStatus{},
		},
		{
			name:          "local ip",
			ips:           "127.0.0.1",
			networkStatus: []infrav1.NetworkStatus{},
		},
		{
			name:          "169.254 prefix",
			ips:           "169.254.0.1",
			networkStatus: []infrav1.NetworkStatus{},
		},
		{
			name:          "172.17.0 prefix",
			ips:           "172.17.0.1",
			networkStatus: []infrav1.NetworkStatus{},
		},
		{
			name: "valid IP",
			ips:  "116.116.116.116",
			networkStatus: []infrav1.NetworkStatus{{
				IPAddrs: []string{"116.116.116.116"},
			}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			networkStatus := GetNetworkStatus(tc.ips)
			g.Expect(networkStatus).To(gomega.Equal(tc.networkStatus))
		})
	}
}

func toString(s string) *string {
	return &s
}
