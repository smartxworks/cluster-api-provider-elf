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

	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
)

const (
	creationTimeout      = time.Minute * 6
	vmOperationRateLimit = time.Second * 6
	vmSilenceTime        = time.Minute * 5
	// When Tower gets a placement group name duplicate error, it means the ELF API is responding slow.
	// Tower will sync this placement group from ELF cluster immediately and the sync usually can complete within 1~2 minute.
	// So set the placement group creation retry interval to 5 minutes.
	placementGroupSilenceTime = time.Minute * 5
)

var gcInterval = 6 * time.Minute
var vmConcurrentMap = newTTLMap(gcInterval)
var vmOperationMap = newTTLMap(gcInterval)
var vmOperationLock sync.Mutex

var placementGroupOperationMap = newTTLMap(gcInterval)
var placementGroupOperationLock sync.Mutex

// acquireTicketForCreateVM returns whether virtual machine create operation
// can be performed.
func acquireTicketForCreateVM(vmName string, isControlPlaneVM bool) (bool, string) {
	vmOperationLock.Lock()
	defer vmOperationLock.Unlock()

	if vmOperationMap.Has(getCreationLockKey(vmName)) {
		return false, "Duplicate virtual machine detected"
	}

	// Only limit the concurrent of worker virtual machines.
	if isControlPlaneVM {
		return true, ""
	}

	if vmConcurrentMap.Len() >= config.MaxConcurrentVMCreations {
		return false, "The number of concurrently created VMs has reached the limit"
	}

	vmConcurrentMap.Set(vmName, nil, creationTimeout)

	return true, ""
}

// releaseTicketForCreateVM releases the virtual machine being created.
func releaseTicketForCreateVM(vmName string) {
	vmOperationLock.Lock()
	defer vmOperationLock.Unlock()

	vmConcurrentMap.Del(vmName)
}

// acquireTicketForUpdatingVM returns whether virtual machine update operation
// can be performed.
// Tower API currently does not have a concurrency limit for operations on the same virtual machine,
// which may cause task to fail.
func acquireTicketForUpdatingVM(vmName string) bool {
	vmOperationLock.Lock()
	defer vmOperationLock.Unlock()

	if vmOperationMap.Has(vmName) {
		return false
	}

	vmOperationMap.Set(vmName, nil, vmOperationRateLimit)

	return true
}

// setVMDuplicate sets whether virtual machine is duplicated.
func setVMDuplicate(vmName string) {
	vmOperationLock.Lock()
	defer vmOperationLock.Unlock()

	vmOperationMap.Set(getCreationLockKey(vmName), nil, vmSilenceTime)
}

// acquireTicketForPlacementGroupOperation returns whether placement group operation
// can be performed.
func acquireTicketForPlacementGroupOperation(groupName string) bool {
	placementGroupOperationLock.Lock()
	defer placementGroupOperationLock.Unlock()

	if placementGroupOperationMap.Has(groupName) {
		return false
	}

	placementGroupOperationMap.Set(groupName, nil, placementGroupSilenceTime)

	return true
}

// releaseTicketForPlacementGroupOperation releases the placement group being operated.
func releaseTicketForPlacementGroupOperation(groupName string) {
	placementGroupOperationLock.Lock()
	defer placementGroupOperationLock.Unlock()

	placementGroupOperationMap.Del(groupName)
}

// setPlacementGroupDuplicate sets whether placement group is duplicated.
func setPlacementGroupDuplicate(groupName string) {
	placementGroupOperationLock.Lock()
	defer placementGroupOperationLock.Unlock()

	placementGroupOperationMap.Set(getCreationLockKey(groupName), nil, placementGroupSilenceTime)
}

// canCreatePlacementGroup returns whether placement group creation can be performed.
func canCreatePlacementGroup(groupName string) bool {
	placementGroupOperationLock.Lock()
	defer placementGroupOperationLock.Unlock()

	return !placementGroupOperationMap.Has(getCreationLockKey(groupName))
}

func getCreationLockKey(name string) string {
	return fmt.Sprintf("%s:creation", name)
}

// Go built-in map with TTL.
type ttlMap struct {
	Values     map[string]*ttlMapValue
	GCInterval time.Duration // interval time clearing expired values
	LastGCTime time.Time     // timestamp of the last cleanup of expired values
}

func newTTLMap(gcInterval time.Duration) *ttlMap {
	return &ttlMap{
		Values:     make(map[string]*ttlMapValue),
		GCInterval: gcInterval,
		LastGCTime: time.Now(),
	}
}

type ttlMapValue struct {
	Val        interface{}
	Expiration time.Time // expiration time
}

func (t *ttlMap) Set(key string, val interface{}, duration time.Duration) {
	t.Values[key] = &ttlMapValue{Val: val, Expiration: time.Now().Add(duration)}
}

func (t *ttlMap) Get(key string) (interface{}, bool) {
	if t.Has(key) {
		return t.Values[key].Val, true
	}

	return nil, false
}

// Has returns whether the key exists and has not expired.
func (t *ttlMap) Has(key string) bool {
	// Delete expired values lazily.
	if time.Now().After(t.LastGCTime.Add(t.GCInterval)) {
		for key, value := range t.Values {
			if time.Now().After(value.Expiration) {
				t.Del(key)
			}
		}
	}

	if value, ok := t.Values[key]; ok {
		if time.Now().After(value.Expiration) {
			t.Del(key)
		} else {
			return true
		}
	}

	return false
}

func (t *ttlMap) Del(key string) {
	delete(t.Values, key)
}

// Len returns a count of all values including expired values.
func (t *ttlMap) Len() int {
	return len(t.Values)
}
