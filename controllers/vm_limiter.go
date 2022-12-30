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
	creationTimeout = time.Minute * 6
)

var vmStatusMap = make(map[string]time.Time)
var limiterLock sync.Mutex

// canCreateVM returns whether virtual machine create operation
// can be performed.
func canCreateVM(vmName string) bool {
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

// releaseVM releases the virtual machine being created.
func releaseVM(vmName string) {
	limiterLock.Lock()
	defer limiterLock.Unlock()

	delete(vmStatusMap, vmName)
}
