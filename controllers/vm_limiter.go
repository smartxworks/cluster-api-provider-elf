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
	"sync"
	"time"

	"github.com/patrickmn/go-cache"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

const (
	vmCreationTimeout    = time.Minute * 6
	vmOperationRateLimit = time.Second * 6
	vmSilenceTime        = time.Minute * 5
	// When Tower gets a placement group name duplicate error, it means the ELF API is responding slow.
	// Tower will sync this placement group from ELF cluster immediately and the sync usually can complete within 1~2 minute.
	// So set the placement group creation retry interval to 5 minutes.
	placementGroupSilenceTime = time.Minute * 5
)

var vmTaskErrorCache = cache.New(5*time.Minute, 10*time.Minute)
var vmConcurrentCache = cache.New(5*time.Minute, 6*time.Minute)

var vmOperationLock sync.Mutex
var placementGroupOperationLock sync.Mutex

// acquireTicketForCreateVM returns whether virtual machine create operation
// can be performed.
func acquireTicketForCreateVM(vmName string, isControlPlaneVM bool) (bool, string) {
	vmOperationLock.Lock()
	defer vmOperationLock.Unlock()

	if _, found := vmTaskErrorCache.Get(getKeyForVMDuplicate(vmName)); found {
		return false, "Duplicate virtual machine detected"
	}

	// Only limit the concurrent of worker virtual machines.
	if isControlPlaneVM {
		return true, ""
	}

	concurrentCount := vmConcurrentCache.ItemCount()
	if concurrentCount >= config.MaxConcurrentVMCreations {
		return false, fmt.Sprintf("The number of concurrently created VMs has reached the limit %d", config.MaxConcurrentVMCreations)
	}

	vmConcurrentCache.Set(getKeyForVM(vmName), nil, vmCreationTimeout)

	return true, ""
}

// releaseTicketForCreateVM releases the virtual machine being created.
func releaseTicketForCreateVM(vmName string) {
	vmConcurrentCache.Delete(getKeyForVM(vmName))
}

// acquireTicketForUpdatingVM returns whether virtual machine update operation
// can be performed.
// Tower API currently does not have a concurrency limit for operations on the same virtual machine,
// which may cause task to fail.
func acquireTicketForUpdatingVM(vmName string) bool {
	if _, found := vmTaskErrorCache.Get(getKeyForVM(vmName)); found {
		return false
	}

	vmTaskErrorCache.Set(getKeyForVM(vmName), nil, vmOperationRateLimit)

	return true
}

// setVMDuplicate sets whether virtual machine is duplicated.
func setVMDuplicate(vmName string) {
	vmTaskErrorCache.Set(getKeyForVMDuplicate(vmName), nil, vmSilenceTime)
}

// acquireTicketForPlacementGroupOperation returns whether placement group operation
// can be performed.
func acquireTicketForPlacementGroupOperation(groupName string) bool {
	placementGroupOperationLock.Lock()
	defer placementGroupOperationLock.Unlock()

	if _, found := vmTaskErrorCache.Get(getKeyForPlacementGroup(groupName)); found {
		return false
	}

	vmTaskErrorCache.Set(getKeyForPlacementGroup(groupName), nil, cache.NoExpiration)

	return true
}

// releaseTicketForPlacementGroupOperation releases the placement group being operated.
func releaseTicketForPlacementGroupOperation(groupName string) {
	vmTaskErrorCache.Delete(getKeyForPlacementGroup(groupName))
}

// setPlacementGroupDuplicate sets whether placement group is duplicated.
func setPlacementGroupDuplicate(groupName string) {
	vmTaskErrorCache.Set(getKeyForPlacementGroupDuplicate(groupName), nil, placementGroupSilenceTime)
}

// canCreatePlacementGroup returns whether placement group creation can be performed.
func canCreatePlacementGroup(groupName string) bool {
	_, found := vmTaskErrorCache.Get(getKeyForPlacementGroupDuplicate(groupName))

	return !found
}

func getKeyForPlacementGroup(name string) string {
	return fmt.Sprintf("pg:%s", name)
}

func getKeyForPlacementGroupDuplicate(name string) string {
	return fmt.Sprintf("pg:duplicate:%s", name)
}

func getKeyForVM(name string) string {
	return fmt.Sprintf("vm:%s", name)
}

func getKeyForVMDuplicate(name string) string {
	return fmt.Sprintf("vm:duplicate:%s", name)
}

/* GPU */

type lockedGPUDevice struct {
	ID    string `json:"id"`
	Count int32  `json:"count"`
}

type lockedVMGPUs struct {
	HostID     string            `json:"hostId"`
	GPUDevices []lockedGPUDevice `json:"gpuDevices"`
	LockedAt   time.Time         `json:"lockedAt"`
}

func (g *lockedVMGPUs) GetGPUIDs() []string {
	ids := make([]string, len(g.GPUDevices))
	for i := 0; i < len(g.GPUDevices); i++ {
		ids[i] = g.GPUDevices[i].ID
	}

	return ids
}

func (g *lockedVMGPUs) GetGPUDeviceInfos() []*service.GPUDeviceInfo {
	gpuDeviceInfos := make([]*service.GPUDeviceInfo, len(g.GPUDevices))
	for i := 0; i < len(g.GPUDevices); i++ {
		gpuDeviceInfos[i] = &service.GPUDeviceInfo{ID: g.GPUDevices[i].ID, AllocatedCount: g.GPUDevices[i].Count}
	}

	return gpuDeviceInfos
}

type lockedClusterGPUMap map[string]lockedVMGPUs

const gpuLockTimeout = time.Minute * 8

var gpuLock sync.Mutex
var lockedGPUMap = make(map[string]lockedClusterGPUMap)

// lockGPUDevicesForVM locks the GPU devices required to create or start a virtual machine.
// The GPU devices will be unlocked when the task is completed or times out.
// This prevents multiple virtual machines from being allocated the same GPU.
func lockGPUDevicesForVM(clusterID, vmName, hostID string, gpuDeviceInfos []*service.GPUDeviceInfo) bool {
	gpuLock.Lock()
	defer gpuLock.Unlock()

	availableCountMap := make(map[string]int32)
	lockedGPUs := lockedVMGPUs{HostID: hostID, LockedAt: time.Now(), GPUDevices: make([]lockedGPUDevice, len(gpuDeviceInfos))}
	for i := 0; i < len(gpuDeviceInfos); i++ {
		availableCountMap[gpuDeviceInfos[i].ID] = gpuDeviceInfos[i].AvailableCount - gpuDeviceInfos[i].AllocatedCount
		lockedGPUs.GPUDevices[i] = lockedGPUDevice{ID: gpuDeviceInfos[i].ID, Count: gpuDeviceInfos[i].AllocatedCount}
	}

	lockedClusterGPUs := getLockedClusterGPUsWithoutLock(clusterID)
	lockedCountMap := getLockedCountMapWithoutLock(lockedClusterGPUs)

	for gpuID, availableCount := range availableCountMap {
		if lockedCount, ok := lockedCountMap[gpuID]; ok && lockedCount > availableCount {
			return false
		}
	}

	lockedClusterGPUs[vmName] = lockedGPUs
	lockedGPUMap[clusterID] = lockedClusterGPUs

	return true
}

func filterGPUDeviceInfosByLockGPUDevices(clusterID string, gpuDeviceInfos service.GPUDeviceInfos) service.GPUDeviceInfos {
	gpuLock.Lock()
	defer gpuLock.Unlock()

	lockedClusterGPUs := getLockedClusterGPUsWithoutLock(clusterID)
	lockedCountMap := getLockedCountMapWithoutLock(lockedClusterGPUs)

	return gpuDeviceInfos.Filter(func(g *service.GPUDeviceInfo) bool {
		if lockedCount, ok := lockedCountMap[g.ID]; ok && lockedCount >= g.AvailableCount {
			return false
		}

		return true
	})
}

func getGPUDevicesLockedByVM(clusterID, vmName string) *lockedVMGPUs {
	gpuLock.Lock()
	defer gpuLock.Unlock()

	lockedClusterGPUs := getLockedClusterGPUsWithoutLock(clusterID)
	if vmGPUs, ok := lockedClusterGPUs[vmName]; ok {
		return &vmGPUs
	}

	return nil
}

// unlockGPUDevicesLockedByVM unlocks the GPU devices locked by the virtual machine.
func unlockGPUDevicesLockedByVM(clusterID, vmName string) {
	gpuLock.Lock()
	defer gpuLock.Unlock()

	lockedClusterGPUs := getLockedClusterGPUsWithoutLock(clusterID)
	delete(lockedClusterGPUs, vmName)

	if len(lockedClusterGPUs) == 0 {
		delete(lockedGPUMap, clusterID)
	} else {
		lockedGPUMap[clusterID] = lockedClusterGPUs
	}
}

func getLockedClusterGPUsWithoutLock(clusterID string) lockedClusterGPUMap {
	if _, ok := lockedGPUMap[clusterID]; !ok {
		return make(map[string]lockedVMGPUs)
	}

	lockedClusterGPUs := lockedGPUMap[clusterID]
	for vmName, lockedGPUs := range lockedClusterGPUs {
		if !time.Now().Before(lockedGPUs.LockedAt.Add(gpuLockTimeout)) {
			// Delete expired data
			delete(lockedClusterGPUs, vmName)
		}
	}

	return lockedClusterGPUs
}

// getLockedCountMapWithoutLock counts and returns the number of locks for each GPU.
func getLockedCountMapWithoutLock(lockedClusterGPUs lockedClusterGPUMap) map[string]int32 {
	lockedCountMap := make(map[string]int32)
	for _, lockedGPUs := range lockedClusterGPUs {
		for i := 0; i < len(lockedGPUs.GPUDevices); i++ {
			if count, ok := lockedCountMap[lockedGPUs.GPUDevices[i].ID]; ok {
				lockedCountMap[lockedGPUs.GPUDevices[i].ID] = count + lockedGPUs.GPUDevices[i].Count
			} else {
				lockedCountMap[lockedGPUs.GPUDevices[i].ID] = lockedGPUs.GPUDevices[i].Count
			}
		}
	}

	return lockedCountMap
}
