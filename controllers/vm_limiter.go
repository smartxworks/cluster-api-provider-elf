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
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
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

type lockedVMGPUs struct {
	HostID       string    `json:"hostId"`
	GPUDeviceIDs []string  `json:"gpuDeviceIds"`
	LockedAt     time.Time `json:"lockedAt"`
}

type lockedClusterGPUMap map[string]lockedVMGPUs

const gpuLockTimeout = time.Minute * 8

var gpuLock sync.Mutex
var lockedGPUMap = make(map[string]lockedClusterGPUMap)

// lockGPUDevicesForVM locks the GPU devices required to create or start a virtual machine.
// The GPU devices will be unlocked when the task is completed or times out.
// This prevents multiple virtual machines from being allocated the same GPU.
func lockGPUDevicesForVM(clusterID, vmName, hostID string, gpuDeviceIDs []string) bool {
	gpuLock.Lock()
	defer gpuLock.Unlock()

	lockedClusterGPUIDs := getLockedClusterGPUIDsWithoutLock(clusterID)
	for i := 0; i < len(gpuDeviceIDs); i++ {
		if lockedClusterGPUIDs.Has(gpuDeviceIDs[i]) {
			return false
		}
	}

	lockedClusterGPUs := getLockedClusterGPUs(clusterID)
	lockedClusterGPUs[vmName] = lockedVMGPUs{
		HostID:       hostID,
		GPUDeviceIDs: gpuDeviceIDs,
		LockedAt:     time.Now(),
	}

	lockedGPUMap[clusterID] = lockedClusterGPUs

	return true
}

// getLockedClusterGPUIDs returns the locked GPU devices of the specified cluster.
func getLockedClusterGPUIDs(clusterID string) sets.Set[string] {
	gpuLock.Lock()
	defer gpuLock.Unlock()

	return getLockedClusterGPUIDsWithoutLock(clusterID)
}

func getGPUDevicesLockedByVM(clusterID, vmName string) *lockedVMGPUs {
	gpuLock.Lock()
	defer gpuLock.Unlock()

	lockedClusterGPUs := getLockedClusterGPUs(clusterID)
	if vmGPUs, ok := lockedClusterGPUs[vmName]; ok {
		if time.Now().Before(vmGPUs.LockedAt.Add(gpuLockTimeout)) {
			return &vmGPUs
		}

		delete(lockedClusterGPUs, vmName)
	}

	return nil
}

// unlockGPUDevicesLockedByVM unlocks the GPU devices locked by the virtual machine.
func unlockGPUDevicesLockedByVM(clusterID, vmName string) {
	gpuLock.Lock()
	defer gpuLock.Unlock()

	lockedClusterGPUs := getLockedClusterGPUs(clusterID)
	delete(lockedClusterGPUs, vmName)

	if len(lockedClusterGPUs) == 0 {
		delete(lockedGPUMap, clusterID)
	} else {
		lockedGPUMap[clusterID] = lockedClusterGPUs
	}
}

func getLockedClusterGPUs(clusterID string) lockedClusterGPUMap {
	if _, ok := lockedGPUMap[clusterID]; ok {
		return lockedGPUMap[clusterID]
	}

	return make(map[string]lockedVMGPUs)
}

func getLockedClusterGPUIDsWithoutLock(clusterID string) sets.Set[string] {
	gpuIDs := sets.Set[string]{}

	lockedClusterGPUs := getLockedClusterGPUs(clusterID)
	for vmName, lockedGPUs := range lockedClusterGPUs {
		if time.Now().Before(lockedGPUs.LockedAt.Add(gpuLockTimeout)) {
			gpuIDs.Insert(lockedGPUs.GPUDeviceIDs...)
		} else {
			delete(lockedClusterGPUs, vmName)
		}
	}

	return gpuIDs
}
