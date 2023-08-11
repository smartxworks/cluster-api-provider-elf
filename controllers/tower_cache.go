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
)

const (
	silenceTime = time.Minute * 5
)

var clusterResourceMap = make(map[string]*clusterResource)
var lock sync.RWMutex

type clusterResource struct {
	// IsUnmet indicates whether the resource does not meet the requirement.
	// For example, true can indicate insufficient memory and not match placement group policy.
	IsUnmet bool
	// LastDetected records the last resource detection time
	LastDetected time.Time
	// LastRetried records the time of the last attempt to detect resource
	LastRetried time.Time
}

// isElfClusterMemoryInsufficientOrPlacementGroupNotMatchPolicy returns whether the ELF cluster has insufficient memory
// or the placement group not match policy.
func isElfClusterMemoryInsufficientOrPlacementGroupNotMatchPolicy(clusterID string, placementGroupName string) (bool, string) {
	lock.RLock()
	defer lock.RUnlock()

	if resource, ok := clusterResourceMap[getMemoryKey(clusterID)]; ok && resource.IsUnmet {
		return true, fmt.Sprintf("Insufficient memory for the ELF cluster %s", clusterID)
	}

	if resource, ok := clusterResourceMap[getPlacementGroupKey(placementGroupName)]; ok && resource.IsUnmet {
		return true, fmt.Sprintf("Not match policy for the placement group %s", placementGroupName)
	}

	return false, ""
}

// setElfClusterMemoryInsufficient sets whether the memory is insufficient.
func setElfClusterMemoryInsufficient(clusterID string, isInsufficient bool) {
	lock.Lock()
	defer lock.Unlock()

	clusterResourceMap[getMemoryKey(clusterID)] = newClusterResource(isInsufficient)
}

// setPlacementGroupNotMatchPolicy sets whether the placement group not match policy.
func setPlacementGroupNotMatchPolicy(placementGroupName string, notMatchPolicy bool) {
	lock.Lock()
	defer lock.Unlock()

	clusterResourceMap[getPlacementGroupKey(placementGroupName)] = newClusterResource(notMatchPolicy)
}

func newClusterResource(isUnmet bool) *clusterResource {
	now := time.Now()
	return &clusterResource{
		IsUnmet:      isUnmet,
		LastDetected: now,
		LastRetried:  now,
	}
}

// canRetryVMOperation returns whether virtual machine operations(Create/PowerOn)
// can be performed.
func canRetryVMOperation(clusterID string, placementGroupName string) bool {
	lock.Lock()
	defer lock.Unlock()

	return canRetry(getMemoryKey(clusterID)) || canRetry(getPlacementGroupKey(placementGroupName))
}

func canRetry(key string) bool {
	if resource, ok := clusterResourceMap[key]; ok {
		if !resource.IsUnmet {
			return false
		}

		if time.Now().Before(resource.LastDetected.Add(silenceTime)) {
			return false
		} else {
			if time.Now().Before(resource.LastRetried.Add(silenceTime)) {
				return false
			} else {
				resource.LastRetried = time.Now()
				return true
			}
		}
	}

	return false
}

func getMemoryKey(clusterID string) string {
	return fmt.Sprintf("%s-memory", clusterID)
}

func getPlacementGroupKey(placementGroup string) string {
	return fmt.Sprintf("%s-placement-group", placementGroup)
}
