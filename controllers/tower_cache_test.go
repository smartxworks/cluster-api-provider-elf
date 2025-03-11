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
	goctx "context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, md)
			machineCtx := newMachineContext(elfCluster, cluster, elfMachine, machine, nil)
			key := getKey(ctx, machineCtx, ctrlMgrCtx.Client, name)

			_, found := inMemoryCache.Get(key)
			Expect(found).To(BeFalse())

			recordOrClearError(ctx, machineCtx, ctrlMgrCtx.Client, name, true)
			_, found = inMemoryCache.Get(key)
			Expect(found).To(BeTrue())
			resource := getClusterResource(key)
			Expect(resource.LastDetected).To(Equal(resource.LastRetried))
			checkResourceRequestAmount(machineCtx, name, resource)

			recordOrClearError(ctx, machineCtx, ctrlMgrCtx.Client, name, true)
			lastDetected := resource.LastDetected
			resource = getClusterResource(key)
			Expect(resource.LastDetected).To(Equal(resource.LastRetried))
			Expect(resource.LastDetected.After(lastDetected)).To(BeTrue())
			checkResourceRequestAmount(machineCtx, name, resource)

			recordOrClearError(ctx, machineCtx, ctrlMgrCtx.Client, name, false)
			resource = getClusterResource(key)
			Expect(resource).To(BeNil())

			resetMemoryCache()
			_, found = inMemoryCache.Get(key)
			Expect(found).To(BeFalse())

			recordOrClearError(ctx, machineCtx, ctrlMgrCtx.Client, name, false)
			resource = getClusterResource(key)
			_, found = inMemoryCache.Get(key)
			Expect(found).To(BeFalse())
			Expect(resource).To(BeNil())

			recordOrClearError(ctx, machineCtx, ctrlMgrCtx.Client, name, false)
			resource = getClusterResource(key)
			Expect(resource).To(BeNil())

			recordOrClearError(ctx, machineCtx, ctrlMgrCtx.Client, name, true)
			_, found = inMemoryCache.Get(key)
			Expect(found).To(BeTrue())
			resource = getClusterResource(key)
			Expect(resource.LastDetected).To(Equal(resource.LastRetried))
			checkResourceRequestAmount(machineCtx, name, resource)
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
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, md)
			machineCtx := newMachineContext(elfCluster, cluster, elfMachine, machine, nil)
			key := getKey(ctx, machineCtx, ctrlMgrCtx.Client, name)

			_, found := inMemoryCache.Get(key)
			Expect(found).To(BeFalse())
			ok, err := canRetryVMOperation(ctx, machineCtx, ctrlMgrCtx.Client)
			Expect(ok).To(BeFalse())
			Expect(err).ShouldNot(HaveOccurred())

			recordOrClearError(ctx, machineCtx, ctrlMgrCtx.Client, name, false)
			ok, err = canRetryVMOperation(ctx, machineCtx, ctrlMgrCtx.Client)
			Expect(ok).To(BeFalse())
			Expect(err).ShouldNot(HaveOccurred())

			recordOrClearError(ctx, machineCtx, ctrlMgrCtx.Client, name, true)
			ok, err = canRetryVMOperation(ctx, machineCtx, ctrlMgrCtx.Client)
			Expect(ok).To(BeFalse())
			Expect(err).ShouldNot(HaveOccurred())

			expireELFScheduleVMError(ctx, machineCtx, ctrlMgrCtx.Client, name)
			ok, err = canRetryVMOperation(ctx, machineCtx, ctrlMgrCtx.Client)
			Expect(ok).To(BeTrue())
			Expect(err).ShouldNot(HaveOccurred())

			ok, err = canRetryVMOperation(ctx, machineCtx, ctrlMgrCtx.Client)
			Expect(ok).To(BeFalse())
			Expect(err).ShouldNot(HaveOccurred())

			checkOtherRequestAmount(ctx, machineCtx, ctrlMgrCtx.Client, name)
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
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, md)
		machineCtx := newMachineContext(elfCluster, cluster, elfMachine, machine, nil)

		ok, msg, err := isELFScheduleVMErrorRecorded(ctx, machineCtx, ctrlMgrCtx.Client)
		Expect(ok).To(BeFalse())
		Expect(msg).To(Equal(""))
		Expect(err).ShouldNot(HaveOccurred())
		expectConditions(elfMachine, []conditionAssertion{})

		elfCluster.Spec.Cluster = clusterInsufficientMemoryKey
		recordOrClearError(ctx, machineCtx, ctrlMgrCtx.Client, clusterInsufficientMemoryKey, true)
		ok, msg, err = isELFScheduleVMErrorRecorded(ctx, machineCtx, ctrlMgrCtx.Client)
		Expect(ok).To(BeTrue())
		Expect(msg).To(ContainSubstring("Insufficient memory detected for the ELF cluster"))
		Expect(err).ShouldNot(HaveOccurred())
		expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForELFClusterWithSufficientMemoryReason}})

		resetMemoryCache()
		elfCluster.Spec.Cluster = clusterInsufficientStorageKey
		recordOrClearError(ctx, machineCtx, ctrlMgrCtx.Client, clusterInsufficientStorageKey, true)
		ok, msg, err = isELFScheduleVMErrorRecorded(ctx, machineCtx, ctrlMgrCtx.Client)
		Expect(ok).To(BeTrue())
		Expect(msg).To(ContainSubstring("Insufficient storage detected for the ELF cluster clusterInsufficientStorage"))
		Expect(err).ShouldNot(HaveOccurred())
		expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForELFClusterWithSufficientStorageReason}})

		resetMemoryCache()
		recordOrClearError(ctx, machineCtx, ctrlMgrCtx.Client, placementGroupKey, true)
		ok, msg, err = isELFScheduleVMErrorRecorded(ctx, machineCtx, ctrlMgrCtx.Client)
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
	for i := range gpuIDs {
		inMemoryCache.Delete(getKeyForGPUVMInfo(gpuIDs[i]))
	}
}

func getKey(ctx goctx.Context, machineCtx *context.MachineContext, ctrlClient client.Client, name string) string {
	if name == clusterInsufficientMemoryKey {
		return getKeyForInsufficientMemoryError(name)
	} else if name == clusterInsufficientStorageKey {
		return getKeyForInsufficientStorageError(name)
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctrlClient, machineCtx.Machine, machineCtx.Cluster)
	Expect(err).ShouldNot(HaveOccurred())

	return getKeyForDuplicatePlacementGroupError(placementGroupName)
}

func recordOrClearError(ctx goctx.Context, machineCtx *context.MachineContext, ctrlClient client.Client, key string, record bool) {
	if strings.Contains(key, clusterInsufficientMemoryKey) {
		recordElfClusterMemoryInsufficient(machineCtx, record)
		return
	} else if strings.Contains(key, clusterInsufficientStorageKey) {
		recordElfClusterStorageInsufficient(machineCtx, record)
		return
	}

	Expect(recordPlacementGroupPolicyNotSatisfied(ctx, machineCtx, ctrlClient, record)).ShouldNot(HaveOccurred())
}

func expireELFScheduleVMError(ctx goctx.Context, machineCtx *context.MachineContext, ctrlClient client.Client, name string) {
	key := getKey(ctx, machineCtx, ctrlClient, name)
	resource := getClusterResource(key)
	resource.LastDetected = resource.LastDetected.Add(-resourceSilenceTime)
	resource.LastRetried = resource.LastRetried.Add(-resourceSilenceTime)
	inMemoryCache.Set(key, resource, resourceDuration)
}

func checkResourceRequestAmount(machineCtx *context.MachineContext, key string, resource *clusterResource) {
	if strings.Contains(key, clusterInsufficientMemoryKey) {
		Expect(resource.RequestAmount).To(Equal(getMemoryRequestAmount(machineCtx)))
		return
	} else if strings.Contains(key, clusterInsufficientStorageKey) {
		Expect(resource.RequestAmount).To(Equal(getStorageRequestAmount(machineCtx)))
		return
	}

	Expect(resource.RequestAmount).To(BeZero())
}

func checkOtherRequestAmount(ctx goctx.Context, machineCtx *context.MachineContext, c client.Client, key string) {
	if strings.Contains(key, clusterInsufficientMemoryKey) {
		recordOrClearError(ctx, machineCtx, c, key, true)
		memory := machineCtx.ElfMachine.Spec.MemoryMiB

		machineCtx.ElfMachine.Spec.MemoryMiB = memory + 1
		ok, err := canRetryVMOperation(ctx, machineCtx, c)
		Expect(ok).To(BeFalse())
		Expect(err).ShouldNot(HaveOccurred())

		machineCtx.ElfMachine.Spec.MemoryMiB = memory - 1
		ok, err = canRetryVMOperation(ctx, machineCtx, c)
		Expect(ok).To(BeTrue())
		Expect(err).ShouldNot(HaveOccurred())
	} else if strings.Contains(key, clusterInsufficientStorageKey) {
		recordOrClearError(ctx, machineCtx, c, key, true)
		disk := machineCtx.ElfMachine.Spec.DiskGiB

		machineCtx.ElfMachine.Spec.DiskGiB = disk + 1
		ok, err := canRetryVMOperation(ctx, machineCtx, c)
		Expect(ok).To(BeFalse())
		Expect(err).ShouldNot(HaveOccurred())

		machineCtx.ElfMachine.Spec.DiskGiB = disk - 1
		ok, err = canRetryVMOperation(ctx, machineCtx, c)
		Expect(ok).To(BeTrue())
		Expect(err).ShouldNot(HaveOccurred())
	}
}
