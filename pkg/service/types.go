/*
Copyright 2023.

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

package service

import "fmt"

type GPUDeviceInfo struct {
	ID     string `json:"id"`
	HostID string `json:"hostId"`
	// AllocatedCount  the number that has been allocated.
	AllocatedCount int32 `json:"allocatedCount"`
	// AvailableCount is the number of GPU that can be allocated.
	AvailableCount int32 `json:"availableCount"`
}

func (g *GPUDeviceInfo) String() string {
	return fmt.Sprintf("{id:%s, hostId:%s, allocatedCount:%d, availableCount:%d}", g.ID, g.HostID, g.AllocatedCount, g.AvailableCount)
}
