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
)

const (
	silenceTime = time.Minute * 5
)

var clusterStatusMap = make(map[string]*clusterStatus)
var lock sync.RWMutex

type clusterStatus struct {
	Resources resources
}

type resources struct {
	IsMemoryInsufficient bool
	// LastDetected records the last memory detection time
	LastDetected time.Time
	// LastRetried records the time of the last attempt to detect memory
	LastRetried time.Time
}

// isElfClusterMemoryInsufficient returns whether the ELF cluster has insufficient memory.
func isElfClusterMemoryInsufficient(clusterID string) bool {
	lock.RLock()
	defer lock.RUnlock()

	if status, ok := clusterStatusMap[clusterID]; ok {
		return status.Resources.IsMemoryInsufficient
	}

	return false
}

// setElfClusterMemoryInsufficient sets whether the memory is insufficient.
func setElfClusterMemoryInsufficient(clusterID string, isInsufficient bool) {
	lock.Lock()
	defer lock.Unlock()

	now := time.Now()
	resources := resources{
		IsMemoryInsufficient: isInsufficient,
		LastDetected:         now,
		LastRetried:          now,
	}

	if status, ok := clusterStatusMap[clusterID]; ok {
		status.Resources = resources
	} else {
		clusterStatusMap[clusterID] = &clusterStatus{Resources: resources}
	}
}

// canRetryVMOperation returns whether virtual machine operations(Create/PowerOn)
// can be performed.
func canRetryVMOperation(clusterID string) bool {
	lock.Lock()
	defer lock.Unlock()

	if status, ok := clusterStatusMap[clusterID]; ok {
		if !status.Resources.IsMemoryInsufficient {
			return false
		}

		if time.Now().Before(status.Resources.LastDetected.Add(silenceTime)) {
			return false
		} else {
			if time.Now().Before(status.Resources.LastRetried.Add(silenceTime)) {
				return false
			} else {
				status.Resources.LastRetried = time.Now()
				return true
			}
		}
	}

	return false
}
