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

import (
	"fmt"
	"strings"

	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/pointer"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
)

// GetUpdatedVMRestrictedFields returns the updated restricted fields of the VM compared to ElfMachine.
// restricted fields: vcpu/cpuCores/cpuSockets.
func GetUpdatedVMRestrictedFields(vm *models.VM, elfMachine *infrav1.ElfMachine) map[string]string {
	fieldMap := make(map[string]string)
	vCPU := TowerVCPU(elfMachine.Spec.NumCPUs)
	cpuCores := TowerCPUCores(*vCPU, elfMachine.Spec.NumCoresPerSocket)
	cpuSockets := TowerCPUSockets(*vCPU, *cpuCores)

	if *vm.Vcpu > *vCPU {
		fieldMap["vcpu"] = fmt.Sprintf("actual: %d, expected: %d", *vm.Vcpu, *vCPU)
	}
	if *vm.CPU.Cores > *cpuCores {
		fieldMap["cpuCores"] = fmt.Sprintf("actual: %d, expected: %d", *vm.CPU.Cores, *cpuCores)
	}
	if *vm.CPU.Sockets > *cpuSockets {
		fieldMap["cpuSockets"] = fmt.Sprintf("actual: %d, expected: %d", *vm.CPU.Sockets, *cpuSockets)
	}

	return fieldMap
}

// IsAvailableHost returns whether the host is available.
//
// Available means the host is not faulted, is not in maintenance mode,
// and has sufficient memory.
// If memory parameter is 0, it does not check whether the host's memory is sufficient.
func IsAvailableHost(host *models.Host, memory int64) (bool, string) {
	if host == nil || host.Status == nil {
		return false, ""
	}

	if *host.Status == models.HostStatusCONNECTEDERROR ||
		*host.Status == models.HostStatusSESSIONEXPIRED ||
		*host.Status == models.HostStatusINITIALIZING {
		return false, fmt.Sprintf("host is in %s status", *host.Status)
	}

	if host.HostState != nil && (*host.HostState.State == models.MaintenanceModeEnumMAINTENANCEMODE ||
		*host.HostState.State == models.MaintenanceModeEnumENTERINGMAINTENANCEMODE) {
		return false, fmt.Sprintf("host is in %s state", *host.HostState.State)
	}

	if memory > 0 && memory > *host.AllocatableMemoryBytes {
		return false, fmt.Sprintf("host has insufficient memory, excepted: %d, actual: %d", memory, *host.AllocatableMemoryBytes)
	}

	return true, ""
}

// GetVMsInPlacementGroup returns a Set of IDs of the virtual machines in the placement group.
func GetVMsInPlacementGroup(placementGroup *models.VMPlacementGroup) sets.Set[string] {
	placementGroupVMSet := sets.Set[string]{}
	for i := 0; i < len(placementGroup.Vms); i++ {
		placementGroupVMSet.Insert(*placementGroup.Vms[i].ID)
	}

	return placementGroupVMSet
}

func TowerMemory(memoryMiB int64) *int64 {
	memory := memoryMiB
	if memory <= 0 {
		memory = config.VMMemoryMiB
	}

	return pointer.Int64(memory * 1024 * 1024)
}

func TowerDisk(diskGiB int32) *int64 {
	disk := int64(diskGiB)
	disk = disk * 1024 * 1024 * 1024

	return &disk
}

func TowerInt32(v int) *int32 {
	val := int32(v)

	return &val
}

func TowerFloat64(v int) *float64 {
	val := float64(v)

	return &val
}

func TowerBool(v bool) *bool {
	return &v
}

func TowerString(v string) *string {
	return &v
}

func TowerVCPU(vCPU int32) *int32 {
	if vCPU <= 0 {
		vCPU = config.VMNumCPUs
	}

	return &vCPU
}

func TowerCPUCores(cpuCores, vCPU int32) *int32 {
	if cpuCores <= 0 {
		cpuCores = vCPU
	}

	return &cpuCores
}

func TowerCPUSockets(vCPU, cpuCores int32) *int32 {
	cpuSockets := vCPU / cpuCores

	return &cpuSockets
}

func IsVMInRecycleBin(vm *models.VM) bool {
	return vm.InRecycleBin != nil && *vm.InRecycleBin
}

func GetTowerString(ptr *string) string {
	if ptr == nil {
		return ""
	}

	return *ptr
}

func GetTowerInt32(ptr *int32) int32 {
	if ptr == nil {
		return 0
	}

	return *ptr
}

func GetTowerInt64(ptr *int64) int64 {
	if ptr == nil {
		return 0
	}

	return *ptr
}

func GetTowerTaskStatus(ptr *models.TaskStatus) string {
	if ptr == nil {
		return ""
	}

	return string(*ptr)
}

func IsCloneVMTask(task *models.Task) bool {
	return strings.Contains(GetTowerString(task.Description), "Create a VM")
}

func IsPowerOnVMTask(task *models.Task) bool {
	return strings.Contains(GetTowerString(task.Description), "Start VM")
}

func IsUpdateVMTask(task *models.Task) bool {
	return strings.Contains(GetTowerString(task.Description), "Edit VM")
}

func IsVMMigrationTask(task *models.Task) bool {
	return strings.Contains(GetTowerString(task.Description), "performing a live migration")
}

func IsPlacementGroupTask(task *models.Task) bool {
	return strings.Contains(GetTowerString(task.Description), "VM placement group") // Update VM placement group
}

// HasGPUsCanNotBeUsedForVM returns whether the specified GPUs contains GPU
// that cannot be used by the specified VM.
func HasGPUsCanNotBeUsedForVM(gpuDeviceInfos GPUDeviceInfos, elfMachine *infrav1.ElfMachine) bool {
	if elfMachine.RequiresGPUDevices() {
		for gpuID := range gpuDeviceInfos {
			gpuInfo := gpuDeviceInfos[gpuID]
			if gpuInfo.GetVMCount() > 1 || (gpuInfo.GetVMCount() == 1 && !gpuInfo.ContainsVM(elfMachine.Name)) {
				return true
			}
		}

		return false
	}

	gpuCountUsedByVM := 0
	availableCountMap := make(map[string]int32)
	for gpuID := range gpuDeviceInfos {
		gpuInfo := gpuDeviceInfos[gpuID]

		if gpuInfo.ContainsVM(elfMachine.Name) {
			gpuCountUsedByVM += 1
		}

		if count, ok := availableCountMap[gpuInfo.ID]; ok {
			availableCountMap[gpuInfo.VGPUType] = count + gpuInfo.AvailableCount
		} else {
			availableCountMap[gpuInfo.VGPUType] = gpuInfo.AvailableCount
		}
	}

	if gpuCountUsedByVM > 0 {
		return gpuCountUsedByVM != gpuDeviceInfos.Len()
	}

	vGPUDevices := elfMachine.Spec.VGPUDevices
	for i := 0; i < len(vGPUDevices); i++ {
		if count, ok := availableCountMap[vGPUDevices[i].Type]; !ok || vGPUDevices[i].Count > count {
			return true
		}
	}

	return false
}

// AggregateUnusedGPUDevicesToGPUDeviceInfos selects the GPU device
// that gpuDeviceInfos does not have from the specified GPU devices and add to it.
// It should be used in conjunction with FindGPUDeviceInfos.
//
// FindGPUDeviceInfos only returns the GPUs that has been used by the virtual machine,
// so need to aggregate the unused GPUs.
func AggregateUnusedGPUDevicesToGPUDeviceInfos(gpuDeviceInfos GPUDeviceInfos, gpuDevices []*models.GpuDevice) {
	for i := 0; i < len(gpuDevices); i++ {
		if !gpuDeviceInfos.Contains(*gpuDevices[i].ID) {
			gpuDeviceInfo := &GPUDeviceInfo{
				ID:       *gpuDevices[i].ID,
				HostID:   *gpuDevices[i].Host.ID,
				Model:    *gpuDevices[i].Model,
				VGPUType: *gpuDevices[i].UserVgpuTypeName,
				// Not yet allocated to a VM, the value is 0
				AllocatedCount: 0,
				// Not yet allocated to a VM, value of GPU Passthrough is 1,
				// value of vGPU is availableVgpusNum
				AvailableCount: 1,
			}
			if *gpuDevices[i].UserUsage == models.GpuDeviceUsageVGPU {
				gpuDeviceInfo.AvailableCount = *gpuDevices[i].AvailableVgpusNum
			}

			gpuDeviceInfos.Insert(gpuDeviceInfo)
		}
	}
}

// ConvertVMGpuInfosToGPUDeviceInfos Converts Tower's VMGpuInfo type to GPUDeviceInfos.
// It should be used in conjunction with FindGPUDeviceInfos.
//
// Tower does not provide API to obtain the detailes of the VM allocated by the GPU Device.
// So we need to get GPUDeviceInfos reversely through VMGpuInfo.
func ConvertVMGpuInfosToGPUDeviceInfos(vmGPUInfos []*models.VMGpuInfo) GPUDeviceInfos {
	gpuDeviceInfos := NewGPUDeviceInfos()
	for i := 0; i < len(vmGPUInfos); i++ {
		gpuDevices := vmGPUInfos[i].GpuDevices
		for j := 0; j < len(gpuDevices); j++ {
			allocatedCount := int32(1)
			if *gpuDevices[j].UserUsage == models.GpuDeviceUsageVGPU {
				allocatedCount = *gpuDevices[j].VgpuInstanceOnVMNum
			}

			gpuDeviceVM := GPUDeviceVM{
				ID:             *vmGPUInfos[i].ID,
				Name:           *vmGPUInfos[i].Name,
				AllocatedCount: allocatedCount,
			}

			if gpuDeviceInfos.Contains(*gpuDevices[j].ID) {
				gpuDeviceInfo := gpuDeviceInfos.Get(*gpuDevices[j].ID)
				gpuDeviceInfo.VMs = append(gpuDeviceInfo.VMs, gpuDeviceVM)
				gpuDeviceInfo.AllocatedCount += gpuDeviceVM.AllocatedCount
				if *gpuDevices[j].UserUsage == models.GpuDeviceUsageVGPU {
					gpuDeviceInfo.AvailableCount = calGPUAvailableCount(gpuDeviceInfo.AvailableCount, gpuDeviceVM.AllocatedCount)
				}
			} else {
				availableCount := int32(0)
				if *gpuDevices[j].UserUsage == models.GpuDeviceUsageVGPU {
					availableCount = calGPUAvailableCount(*gpuDevices[j].VgpuInstanceNum, gpuDeviceVM.AllocatedCount)
				}

				gpuDeviceInfos.Insert(&GPUDeviceInfo{
					ID:             *gpuDevices[j].ID,
					HostID:         *gpuDevices[j].Host.ID,
					Model:          *gpuDevices[j].Model,
					VGPUType:       *gpuDevices[j].UserVgpuTypeName,
					AllocatedCount: gpuDeviceVM.AllocatedCount,
					AvailableCount: availableCount,
					VMs:            []GPUDeviceVM{gpuDeviceVM},
				})
			}
		}
	}

	return gpuDeviceInfos
}

func calGPUAvailableCount(availableCount, allocatedCount int32) int32 {
	count := availableCount - allocatedCount
	if count < 0 {
		count = 0
	}

	return count
}
