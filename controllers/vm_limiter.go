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
	"sync"
	"time"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
)

const (
	creationTimeout                = time.Minute * 6
	vmOperationRateLimit           = time.Second * 6
	placementGroupOperationTimeout = time.Minute * 3
	vmMigrationTimeout             = time.Minute * 20
)

var vmStatusMap = make(map[string]time.Time)
var limiterLock sync.Mutex
var vmOperationMap = make(map[string]time.Time)
var placementGroupVMMigrationMap = make(map[string]time.Time)
var vmOperationLock sync.Mutex

var placementGroupStatusMap = make(map[string]time.Time)
var placementGroupStatusLock sync.Mutex
var placementGroupOperationMap = make(map[string]time.Time)
var placementGroupOperationLock sync.Mutex

// acquireTicketForCreateVM returns whether virtual machine create operation
// can be performed.
func acquireTicketForCreateVM(vmName string) bool {
	limiterLock.Lock()
	defer limiterLock.Unlock()

	if len(vmStatusMap) >= config.MaxConcurrentVMCreations {
		for name, timestamp := range vmStatusMap {
			if !time.Now().Before(timestamp.Add(creationTimeout)) {
				delete(vmStatusMap, name)
			}
		}
	}

	if len(vmStatusMap) >= config.MaxConcurrentVMCreations {
		return false
	}

	vmStatusMap[vmName] = time.Now()

	return true
}

// releaseTicketForCreateVM releases the virtual machine being created.
func releaseTicketForCreateVM(vmName string) {
	limiterLock.Lock()
	defer limiterLock.Unlock()

	delete(vmStatusMap, vmName)
}

// acquireTicketForUpdateVM returns whether virtual machine update operation
// can be performed.
func acquireTicketForUpdateVM(vmName string) bool {
	vmOperationLock.Lock()
	defer vmOperationLock.Unlock()

	if status, ok := vmOperationMap[vmName]; ok {
		if !time.Now().After(status.Add(vmOperationRateLimit)) {
			return false
		}
	}

	vmOperationMap[vmName] = time.Now()

	return true
}

// acquireTicketForPlacementGroupVMMigration returns whether virtual machine migration
// of placement group operation can be performed.
func acquireTicketForPlacementGroupVMMigration(groupName string) bool {
	vmOperationLock.Lock()
	defer vmOperationLock.Unlock()

	if status, ok := placementGroupVMMigrationMap[groupName]; ok {
		if !time.Now().After(status.Add(vmMigrationTimeout)) {
			return false
		}
	}

	placementGroupVMMigrationMap[groupName] = time.Now()

	return true
}

// releaseTicketForPlacementGroupVMMigration releases the virtual machine migration
// of placement group being operated.
func releaseTicketForPlacementGroupVMMigration(groupName string) {
	vmOperationLock.Lock()
	defer vmOperationLock.Unlock()

	delete(placementGroupVMMigrationMap, groupName)
}

// acquireTicketForUpdatePlacementGroup returns whether placement group update operation
// can be performed.
func acquireTicketForUpdatePlacementGroup(groupName string) bool {
	placementGroupStatusLock.Lock()
	defer placementGroupStatusLock.Unlock()

	if status, ok := placementGroupStatusMap[groupName]; ok {
		if !time.Now().After(status.Add(placementGroupOperationTimeout)) {
			return false
		}
	}

	placementGroupStatusMap[groupName] = time.Now()

	return true
}

// releaseTicketForUpdatePlacementGroup releases the placement group being updated.
func releaseTicketForUpdatePlacementGroup(groupName string) {
	placementGroupStatusLock.Lock()
	defer placementGroupStatusLock.Unlock()

	delete(placementGroupStatusMap, groupName)
}

// acquireTicketForPlacementGroupOperation returns whether placement group operation
// can be performed.
func acquireTicketForPlacementGroupOperation(groupName string) bool {
	placementGroupOperationLock.Lock()
	defer placementGroupOperationLock.Unlock()

	if _, ok := placementGroupOperationMap[groupName]; ok {
		return false
	}

	placementGroupOperationMap[groupName] = time.Now()

	return true
}

// releaseTicketForPlacementGroupOperation releases the placement group being operated.
func releaseTicketForPlacementGroupOperation(groupName string) {
	placementGroupOperationLock.Lock()
	defer placementGroupOperationLock.Unlock()

	delete(placementGroupOperationMap, groupName)
}
