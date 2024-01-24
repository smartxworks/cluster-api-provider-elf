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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

const (
	clusterKey                    = "clusterID"
	clusterInsufficientMemoryKey  = "clusterInsufficientMemory"
	clusterInsufficientStorageKey = "clusterInsufficientStorage"
	placementGroupKey             = "getPlacementGroupName"
)

var _ = Describe("TowerCache", func() {
	BeforeEach(func() {
		resetMemoryCache()
	})

	It("should set memoryInsufficient/policyNotSatisfied", func() {
		for _, name := range []string{clusterInsufficientMemoryKey, clusterInsufficientStorageKey, placementGroupKey} {
			resetMemoryCache()
			elfCluster, cluster, elfMachine, machine, secret := fake.NewClusterAndMachineObjects()
			elfCluster.Spec.Cluster = name
			md := fake.NewMD()
			md.Name = name
			fake.ToWorkerMachine(machine, md)
			fake.ToWorkerMachine(elfMachine, md)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, nil)
			key := getKey(machineContext, name)

			_, found := inMemoryCache.Get(key)
			Expect(found).To(BeFalse())

			recordIsUnmet(machineContext, name, true)
			_, found = inMemoryCache.Get(key)
			Expect(found).To(BeTrue())
			resource := getClusterResource(key)
			Expect(resource.LastDetected).To(Equal(resource.LastRetried))

			recordIsUnmet(machineContext, name, true)
			lastDetected := resource.LastDetected
			resource = getClusterResource(key)
			Expect(resource.LastDetected).To(Equal(resource.LastRetried))
			Expect(resource.LastDetected.After(lastDetected)).To(BeTrue())

			recordIsUnmet(machineContext, name, false)
			resource = getClusterResource(key)
			Expect(resource).To(BeNil())

			resetMemoryCache()
			_, found = inMemoryCache.Get(key)
			Expect(found).To(BeFalse())

			recordIsUnmet(machineContext, name, false)
			resource = getClusterResource(key)
			_, found = inMemoryCache.Get(key)
			Expect(found).To(BeFalse())
			Expect(resource).To(BeNil())

			recordIsUnmet(machineContext, name, false)
			resource = getClusterResource(key)
			Expect(resource).To(BeNil())

			recordIsUnmet(machineContext, name, true)
			_, found = inMemoryCache.Get(key)
			Expect(found).To(BeTrue())
			resource = getClusterResource(key)
			Expect(resource.LastDetected).To(Equal(resource.LastRetried))
		}
	})

	It("should return whether need to detect", func() {
		for _, name := range []string{clusterInsufficientMemoryKey, clusterInsufficientStorageKey, placementGroupKey} {
			resetMemoryCache()
			elfCluster, cluster, elfMachine, machine, secret := fake.NewClusterAndMachineObjects()
			elfCluster.Spec.Cluster = name
			md := fake.NewMD()
			md.Name = name
			fake.ToWorkerMachine(machine, md)
			fake.ToWorkerMachine(elfMachine, md)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, nil)
			key := getKey(machineContext, name)

			_, found := inMemoryCache.Get(key)
			Expect(found).To(BeFalse())
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
		resetMemoryCache()
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
		expectConditions(elfMachine, []conditionAssertion{})

		elfCluster.Spec.Cluster = clusterInsufficientMemoryKey
		recordIsUnmet(machineContext, clusterInsufficientMemoryKey, true)
		ok, msg, err = isELFScheduleVMErrorRecorded(machineContext)
		Expect(ok).To(BeTrue())
		Expect(msg).To(ContainSubstring("Insufficient memory detected for the ELF cluster"))
		Expect(err).ShouldNot(HaveOccurred())
		expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForELFClusterWithSufficientMemoryReason}})

		resetMemoryCache()
		elfCluster.Spec.Cluster = clusterInsufficientStorageKey
		recordIsUnmet(machineContext, clusterInsufficientStorageKey, true)
		ok, msg, err = isELFScheduleVMErrorRecorded(machineContext)
		Expect(ok).To(BeTrue())
		Expect(msg).To(ContainSubstring("Insufficient storage detected for the ELF cluster clusterInsufficientStorage"))
		Expect(err).ShouldNot(HaveOccurred())
		expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForELFClusterWithSufficientStorageReason}})

		resetMemoryCache()
		recordIsUnmet(machineContext, placementGroupKey, true)
		ok, msg, err = isELFScheduleVMErrorRecorded(machineContext)
		Expect(ok).To(BeTrue())
		Expect(msg).To(ContainSubstring("Not satisfy policy detected for the placement group"))
		Expect(err).ShouldNot(HaveOccurred())
		expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForPlacementGroupPolicySatisfiedReason}})
	})

	It("PG Cache", func() {
		pgName := "pg"
		pg := fake.NewVMPlacementGroup(nil)
		pg.Name = &pgName
		Expect(getPGFromCache(pgName)).To(BeNil())

		setPGCache(pg)
		Expect(getPGFromCache(pgName)).To(Equal(pg))
		time.Sleep(pgCacheDuration)
		Expect(getPGFromCache(pgName)).To(BeNil())

		setPGCache(pg)
		delPGCaches([]string{pgName})
		Expect(getPGFromCache(pgName)).To(BeNil())
	})

	It("Label Cache", func() {
		label := &models.Label{
			ID:    service.TowerString("label-id"),
			Key:   service.TowerString("label-key"),
			Value: service.TowerString("label-name"),
		}

		Expect(getPGFromCache(*label.Key)).To(BeNil())
		setLabelInCache(label)
		Expect(getLabelFromCache(*label.Key)).To(Equal(label))
		delLabelCache(*label.Key)
		Expect(getLabelFromCache(*label.Key)).To(BeNil())
	})

	It("GPU Cache", func() {
		gpuID := "gpu"
		gpuVMInfo := models.GpuVMInfo{ID: service.TowerString(gpuID)}
		gpuVMInfos := service.NewGPUVMInfos(&gpuVMInfo)
		setGPUVMInfosCache(gpuVMInfos)
		cachedGPUDeviceInfos := getGPUVMInfosFromCache([]string{gpuID})
		Expect(cachedGPUDeviceInfos.Len()).To(Equal(1))
		Expect(*cachedGPUDeviceInfos.Get(*gpuVMInfo.ID)).To(Equal(gpuVMInfo))
		time.Sleep(gpuCacheDuration)
		Expect(getGPUVMInfosFromCache([]string{gpuID})).To(BeEmpty())
	})
})

func removeGPUVMInfosCache(gpuIDs []string) {
	for i := 0; i < len(gpuIDs); i++ {
		inMemoryCache.Delete(getKeyForGPUVMInfo(gpuIDs[i]))
	}
}

func getKey(ctx *context.MachineContext, name string) string {
	if name == clusterInsufficientMemoryKey {
		return getKeyForInsufficientMemoryError(name)
	} else if name == clusterInsufficientStorageKey {
		return getKeyForInsufficientStorageError(name)
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	Expect(err).ShouldNot(HaveOccurred())

	return getKeyForDuplicatePlacementGroupError(placementGroupName)
}

func recordIsUnmet(ctx *context.MachineContext, key string, isUnmet bool) {
	if strings.Contains(key, clusterInsufficientMemoryKey) {
		recordElfClusterMemoryInsufficient(ctx, isUnmet)
		return
	} else if strings.Contains(key, clusterInsufficientStorageKey) {
		recordElfClusterStorageInsufficient(ctx, isUnmet)
		return
	}

	Expect(recordPlacementGroupPolicyNotSatisfied(ctx, isUnmet)).ShouldNot(HaveOccurred())
}

func expireELFScheduleVMError(ctx *context.MachineContext, name string) {
	key := getKey(ctx, name)
	resource := getClusterResource(key)
	resource.LastDetected = resource.LastDetected.Add(-resourceSilenceTime)
	resource.LastRetried = resource.LastRetried.Add(-resourceSilenceTime)
	inMemoryCache.Set(key, resource, resourceDuration)
}
