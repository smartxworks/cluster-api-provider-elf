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

package controllers

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

const (
	clusterKey        = "clusterID"
	placementGroupKey = "getPlacementGroupName"
)

var _ = Describe("TowerCache", func() {
	BeforeEach(func() {
		resetClusterResourceMap()
	})

	It("should set memoryInsufficient/policyNotSatisfied", func() {
		for _, name := range []string{clusterKey, placementGroupKey} {
			resetClusterResourceMap()
			elfCluster, cluster, elfMachine, machine, secret := fake.NewClusterAndMachineObjects()
			elfCluster.Spec.Cluster = name
			md := fake.NewMD()
			md.Name = name
			fake.ToWorkerMachine(machine, md)
			fake.ToWorkerMachine(elfMachine, md)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, nil)
			key := getKey(machineContext, name)

			Expect(clusterResourceMap).NotTo(HaveKey(key))
			Expect(clusterResourceMap[key]).To(BeNil())

			recordIsUnmet(machineContext, name, true)
			Expect(clusterResourceMap[key].IsUnmet).To(BeTrue())
			Expect(clusterResourceMap[key].LastDetected).To(Equal(clusterResourceMap[key].LastRetried))

			recordIsUnmet(machineContext, name, true)
			Expect(clusterResourceMap[key].IsUnmet).To(BeTrue())
			Expect(clusterResourceMap[key].LastDetected).To(Equal(clusterResourceMap[key].LastRetried))

			recordIsUnmet(machineContext, name, false)
			Expect(clusterResourceMap[key].IsUnmet).To(BeFalse())
			Expect(clusterResourceMap[key].LastDetected).To(Equal(clusterResourceMap[key].LastRetried))

			resetClusterResourceMap()
			Expect(clusterResourceMap).NotTo(HaveKey(name))
			Expect(clusterResourceMap[key]).To(BeNil())

			recordIsUnmet(machineContext, name, false)
			Expect(clusterResourceMap[key].IsUnmet).To(BeFalse())
			Expect(clusterResourceMap[key].LastDetected).To(Equal(clusterResourceMap[key].LastRetried))

			recordIsUnmet(machineContext, name, false)
			Expect(clusterResourceMap[key].IsUnmet).To(BeFalse())
			Expect(clusterResourceMap[key].LastDetected).To(Equal(clusterResourceMap[key].LastRetried))

			recordIsUnmet(machineContext, name, true)
			Expect(clusterResourceMap[key].IsUnmet).To(BeTrue())
			Expect(clusterResourceMap[key].LastDetected).To(Equal(clusterResourceMap[key].LastRetried))
		}
	})

	It("should return whether need to detect", func() {
		for _, name := range []string{clusterKey, placementGroupKey} {
			resetClusterResourceMap()
			elfCluster, cluster, elfMachine, machine, secret := fake.NewClusterAndMachineObjects()
			elfCluster.Spec.Cluster = name
			md := fake.NewMD()
			md.Name = name
			fake.ToWorkerMachine(machine, md)
			fake.ToWorkerMachine(elfMachine, md)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, nil)
			key := getKey(machineContext, name)

			Expect(clusterResourceMap).NotTo(HaveKey(key))
			Expect(clusterResourceMap[key]).To(BeNil())
			ok, err := canRetryVMOperation(machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).ShouldNot(HaveOccurred())

			recordIsUnmet(machineContext, name, false)
			ok, err = canRetryVMOperation(machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).ShouldNot(HaveOccurred())

			recordIsUnmet(machineContext, name, true)
			ok, err = canRetryVMOperation(machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).ShouldNot(HaveOccurred())

			expireELFScheduleVMError(machineContext, name)
			ok, err = canRetryVMOperation(machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).ShouldNot(HaveOccurred())

			ok, err = canRetryVMOperation(machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).ShouldNot(HaveOccurred())
		}
	})

	It("isELFScheduleVMErrorRecorded", func() {
		resetClusterResourceMap()
		elfCluster, cluster, elfMachine, machine, secret := fake.NewClusterAndMachineObjects()
		elfCluster.Spec.Cluster = clusterKey
		md := fake.NewMD()
		md.Name = placementGroupKey
		fake.ToWorkerMachine(machine, md)
		fake.ToWorkerMachine(elfMachine, md)
		ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
		machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, nil)

		ok, msg, err := isELFScheduleVMErrorRecorded(machineContext)
		Expect(ok).To(BeFalse())
		Expect(msg).To(Equal(""))
		Expect(err).ShouldNot(HaveOccurred())

		recordIsUnmet(machineContext, clusterKey, true)
		ok, msg, err = isELFScheduleVMErrorRecorded(machineContext)
		Expect(ok).To(BeTrue())
		Expect(msg).To(ContainSubstring("Insufficient memory detected for the ELF cluster"))
		Expect(err).ShouldNot(HaveOccurred())

		resetClusterResourceMap()
		recordIsUnmet(machineContext, placementGroupKey, true)
		ok, msg, err = isELFScheduleVMErrorRecorded(machineContext)
		Expect(ok).To(BeTrue())
		Expect(msg).To(ContainSubstring("Not satisfy policy detected for the placement group"))
		Expect(err).ShouldNot(HaveOccurred())
	})
})

func getKey(ctx *context.MachineContext, name string) string {
	if name == clusterKey {
		return getMemoryKey(name)
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	Expect(err).ShouldNot(HaveOccurred())

	return getPlacementGroupKey(placementGroupName)
}

func recordIsUnmet(ctx *context.MachineContext, key string, isUnmet bool) {
	if strings.Contains(key, clusterKey) {
		recordElfClusterMemoryInsufficient(ctx, isUnmet)
		return
	}

	Expect(recordPlacementGroupPolicyNotSatisfied(ctx, isUnmet)).ShouldNot(HaveOccurred())
}

func expireELFScheduleVMError(ctx *context.MachineContext, name string) {
	key := getKey(ctx, name)
	clusterResourceMap[key].LastDetected = clusterResourceMap[key].LastDetected.Add(-silenceTime)
	clusterResourceMap[key].LastRetried = clusterResourceMap[key].LastRetried.Add(-silenceTime)
}

func resetClusterResourceMap() {
	clusterResourceMap = make(map[string]*clusterResource)
}
