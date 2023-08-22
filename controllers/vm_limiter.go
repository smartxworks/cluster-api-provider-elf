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

var goCache = cache.New(5*time.Minute, 10*time.Minute)
var vmConcurrentCache = cache.New(5*time.Minute, 6*time.Minute)

var vmOperationLock sync.Mutex
var placementGroupOperationLock sync.Mutex

// acquireTicketForCreateVM returns whether virtual machine create operation
// can be performed.
func acquireTicketForCreateVM(vmName string, isControlPlaneVM bool) (bool, string) {
	vmOperationLock.Lock()
	defer vmOperationLock.Unlock()

	if _, found := goCache.Get(getKeyForVMDuplicate(vmName)); found {
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
	if _, found := goCache.Get(getKeyForVM(vmName)); found {
		return false
	}

	goCache.Set(getKeyForVM(vmName), nil, vmOperationRateLimit)

	return true
}

// setVMDuplicate sets whether virtual machine is duplicated.
func setVMDuplicate(vmName string) {
	goCache.Set(getKeyForVMDuplicate(vmName), nil, vmSilenceTime)
}

// acquireTicketForPlacementGroupOperation returns whether placement group operation
// can be performed.
func acquireTicketForPlacementGroupOperation(groupName string) bool {
	placementGroupOperationLock.Lock()
	defer placementGroupOperationLock.Unlock()

	if _, found := goCache.Get(getKeyForPlacementGroup(groupName)); found {
		return false
	}

	goCache.Set(getKeyForPlacementGroup(groupName), nil, cache.NoExpiration)

	return true
}

// releaseTicketForPlacementGroupOperation releases the placement group being operated.
func releaseTicketForPlacementGroupOperation(groupName string) {
	goCache.Delete(getKeyForPlacementGroup(groupName))
}

// setPlacementGroupDuplicate sets whether placement group is duplicated.
func setPlacementGroupDuplicate(groupName string) {
	goCache.Set(getKeyForPlacementGroupDuplicate(groupName), nil, placementGroupSilenceTime)
}

// canCreatePlacementGroup returns whether placement group creation can be performed.
func canCreatePlacementGroup(groupName string) bool {
	_, found := goCache.Get(getKeyForPlacementGroupDuplicate(groupName))

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
