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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

var _ = Describe("VMLimiter", func() {
	var vmName string

	BeforeEach(func() {
		vmName = fake.UUID()
		resetVMConcurrentCache()
	})

	It("acquireTicketForCreateVM", func() {
		ok, msg := acquireTicketForCreateVM(vmName, true)
		Expect(ok).To(BeTrue())
		Expect(msg).To(Equal(""))
		_, found := memoryCache.Get(getKeyForVMDuplicate(vmName))
		Expect(found).To(BeFalse())

		setVMDuplicate(vmName)
		_, found = memoryCache.Get(getKeyForVMDuplicate(vmName))
		Expect(found).To(BeTrue())
		ok, msg = acquireTicketForCreateVM(vmName, true)
		Expect(ok).To(BeFalse())
		Expect(msg).To(Equal("Duplicate virtual machine detected"))
		memoryCache.Delete(getKeyForVMDuplicate(vmName))

		ok, msg = acquireTicketForCreateVM(vmName, false)
		Expect(ok).To(BeTrue())
		Expect(msg).To(Equal(""))
		_, found = vmConcurrentCache.Get(getKeyForVM(vmName))
		Expect(found).To(BeTrue())

		for i := 0; i < config.MaxConcurrentVMCreations-1; i++ {
			vmConcurrentCache.Set(fake.UUID(), nil, vmCreationTimeout)
		}
		ok, msg = acquireTicketForCreateVM(vmName, false)
		Expect(ok).To(BeFalse())
		Expect(msg).To(Equal(fmt.Sprintf("The number of concurrently created VMs has reached the limit %d", config.MaxConcurrentVMCreations)))
	})

	It("releaseTicketForCreateVM", func() {
		_, found := vmConcurrentCache.Get(getKeyForVM(vmName))
		Expect(found).To(BeFalse())
		ok, _ := acquireTicketForCreateVM(vmName, false)
		Expect(ok).To(BeTrue())
		_, found = vmConcurrentCache.Get(getKeyForVM(vmName))
		Expect(found).To(BeTrue())
		releaseTicketForCreateVM(vmName)
		_, found = vmConcurrentCache.Get(getKeyForVM(vmName))
		Expect(found).To(BeFalse())
	})
})

var _ = Describe("VM Operation Limiter", func() {
	var vmName string

	BeforeEach(func() {
		vmName = fake.UUID()
	})

	It("acquireTicketForUpdatingVM", func() {
		Expect(acquireTicketForUpdatingVM(vmName)).To(BeTrue())
		_, found := memoryCache.Get(getKeyForVM(vmName))
		Expect(found).To(BeTrue())
		Expect(acquireTicketForUpdatingVM(vmName)).To(BeFalse())
	})
})

var _ = Describe("Placement Group Operation Limiter", func() {
	var groupName string

	BeforeEach(func() {
		groupName = fake.UUID()
	})

	It("acquireTicketForPlacementGroupOperation", func() {
		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeTrue())
		_, found := memoryCache.Get(getKeyForPlacementGroup(groupName))
		Expect(found).To(BeTrue())

		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeFalse())
		releaseTicketForPlacementGroupOperation(groupName)

		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeTrue())
		_, found = memoryCache.Get(getKeyForPlacementGroup(groupName))
		Expect(found).To(BeTrue())
	})

	It("canCreatePlacementGroup", func() {
		key := getKeyForPlacementGroupDuplicate(groupName)

		_, found := memoryCache.Get(key)
		Expect(found).To(BeFalse())
		Expect(canCreatePlacementGroup(groupName)).To(BeTrue())

		setPlacementGroupDuplicate(groupName)
		_, found = memoryCache.Get(key)
		Expect(found).To(BeTrue())
		Expect(canCreatePlacementGroup(groupName)).To(BeFalse())
	})
})

var _ = Describe("Lock GPU devices for VM", func() {
	var clusterID, vmName, hostID string

	BeforeEach(func() {
		clusterID = fake.UUID()
		vmName = fake.UUID()
	})

	It("lockGPUsForVM", func() {
		lockedGPUVMInfo := fake.NewTowerVGPUVMInfo(0)
		lockedGPUVMInfo.AssignedVgpusNum = service.TowerInt32(1)
		lockedGPUVMInfo.AvailableVgpusNum = service.TowerInt32(1)
		gpuVMInfo := fake.NewTowerVGPUVMInfo(0)
		gpuVMInfo.AssignedVgpusNum = service.TowerInt32(0)
		gpuVMInfo.AvailableVgpusNum = service.TowerInt32(1)
		lockedGPUDeviceInfo := &service.GPUDeviceInfo{ID: *lockedGPUVMInfo.ID, AllocatedCount: 1, AvailableCount: 1}
		lockedGPUDeviceInfos := []*service.GPUDeviceInfo{lockedGPUDeviceInfo}
		filteredGPUVMInfos := []*models.GpuVMInfo{gpuVMInfo, lockedGPUVMInfo}

		lockedVMGPUs := getGPUDevicesLockedByVM(clusterID, vmName)
		Expect(lockedVMGPUs).To(BeNil())
		filteredGPUs := filterGPUVMInfosByLockGPUDevices(clusterID, service.NewGPUVMInfos(filteredGPUVMInfos...))
		Expect(filteredGPUs).To(HaveLen(2))

		Expect(lockGPUDevicesForVM(clusterID, vmName, hostID, lockedGPUDeviceInfos)).To(BeTrue())
		lockedVMGPUs = getGPUDevicesLockedByVM(clusterID, vmName)
		Expect(lockedVMGPUs.HostID).To(Equal(hostID))
		Expect(lockedVMGPUs.GPUDevices).To(HaveLen(1))
		Expect(lockedVMGPUs.GPUDevices[0].ID).To(Equal(*lockedGPUVMInfo.ID))
		Expect(lockedVMGPUs.GPUDevices[0].Count).To(Equal(int32(1)))
		Expect(lockedVMGPUs.LockedAt.Unix()).To(Equal(time.Now().Unix()))
		filteredGPUs = filterGPUVMInfosByLockGPUDevices(clusterID, service.NewGPUVMInfos(filteredGPUVMInfos...))
		Expect(filteredGPUs).To(HaveLen(1))
		Expect(filteredGPUs.Contains(*gpuVMInfo.ID)).To(BeTrue())

		Expect(lockGPUDevicesForVM(clusterID, vmName, hostID, lockedGPUDeviceInfos)).To(BeFalse())
		lockedVMGPUs = getGPUDevicesLockedByVM(clusterID, vmName)
		Expect(lockedVMGPUs.GPUDevices).To(HaveLen(1))

		unlockGPUDevicesLockedByVM(clusterID, vmName)
		lockedVMGPUs = getGPUDevicesLockedByVM(clusterID, vmName)
		Expect(lockedVMGPUs).To(BeNil())
		filteredGPUs = filterGPUVMInfosByLockGPUDevices(clusterID, service.NewGPUVMInfos(filteredGPUVMInfos...))
		Expect(filteredGPUs).To(HaveLen(2))

		Expect(lockGPUDevicesForVM(clusterID, vmName, hostID, lockedGPUDeviceInfos)).To(BeTrue())
		vmGPUs := lockedGPUMap[clusterID][vmName]
		vmGPUs.LockedAt = vmGPUs.LockedAt.Add(-gpuLockTimeout)
		lockedGPUMap[clusterID][vmName] = vmGPUs
		lockedVMGPUs = getGPUDevicesLockedByVM(clusterID, vmName)
		Expect(lockedVMGPUs).To(BeNil())
		filteredGPUs = filterGPUVMInfosByLockGPUDevices(clusterID, service.NewGPUVMInfos(filteredGPUVMInfos...))
		Expect(filteredGPUs).To(HaveLen(2))

		lockedGPUDeviceInfo.AvailableCount = 2
		Expect(lockGPUDevicesForVM(clusterID, vmName, hostID, lockedGPUDeviceInfos)).To(BeTrue())
		Expect(lockGPUDevicesForVM(clusterID, fake.UUID(), hostID, lockedGPUDeviceInfos)).To(BeTrue())
		Expect(lockGPUDevicesForVM(clusterID, fake.UUID(), hostID, lockedGPUDeviceInfos)).To(BeFalse())
		Expect(lockedGPUMap[clusterID]).To(HaveLen(2))
		filteredGPUs = filterGPUVMInfosByLockGPUDevices(clusterID, service.NewGPUVMInfos(filteredGPUVMInfos...))
		Expect(filteredGPUs).To(HaveLen(1))
	})
})

func resetVMConcurrentCache() {
	vmConcurrentCache.Flush()
}

func resetMemoryCache() {
	memoryCache.Flush()
}
