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

type GPUDeviceVM struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	AllocatedCount int32  `json:"allocatedCount"`
}

type GPUDeviceInfo struct {
	ID       string `json:"id"`
	HostID   string `json:"hostId"`
	Model    string `json:"model"`
	VGPUType string `json:"vGpuType"`
	// AllocatedCount  the number that has been allocated.
	// For GPU devices, can be 0 or larger than 0.
	// For vGPU devices, can larger than vgpuInstanceNum.
	AllocatedCount int32 `json:"allocatedCount"`
	// AvailableCount is the number of GPU that can be allocated.
	// For GPU devices, can be 0 or 1.
	// For vGPU devices, can be 0 - vgpuInstanceNum.
	AvailableCount int32 `json:"availableCount"`
	// VMs(including STOPPED) allocated to the current GPU.
	VMs []GPUDeviceVM `json:"vms"`
}

func (g *GPUDeviceInfo) GetVMCount() int {
	return len(g.VMs)
}

func (g *GPUDeviceInfo) ContainsVM(vm string) bool {
	for i := 0; i < len(g.VMs); i++ {
		if g.VMs[i].ID == vm || g.VMs[i].Name == vm {
			return true
		}
	}

	return false
}

func (g *GPUDeviceInfo) String() string {
	return fmt.Sprintf("{id:%s, hostId:%s, model:%s, vGPUType:%s, allocatedCount:%d, availableCount:%d}", g.ID, g.HostID, g.Model, g.VGPUType, g.AllocatedCount, g.AvailableCount)
}
