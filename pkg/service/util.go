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
	typesutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/types"
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

func IsVMColdMigrationTask(task *models.Task) bool {
	return strings.Contains(GetTowerString(task.Description), "performing a cold migration")
}

func IsVMMigrationTask(task *models.Task) bool {
	return strings.Contains(GetTowerString(task.Description), "performing a live migration")
}

func IsPlacementGroupTask(task *models.Task) bool {
	return strings.Contains(GetTowerString(task.Description), "VM placement group") // Update VM placement group
}

// HasGPUsCanNotBeUsedForVM returns whether the specified GPUs contains GPU
// that cannot be used by the specified VM.
func HasGPUsCanNotBeUsedForVM(gpuVMInfos GPUVMInfos, elfMachine *infrav1.ElfMachine) bool {
	if elfMachine.RequiresPassThroughGPUDevices() {
		for gpuID := range gpuVMInfos {
			gpuVMInfo := gpuVMInfos[gpuID]
			if len(gpuVMInfo.Vms) >= 1 && !gpuVMInfoFirstVMIs(gpuVMInfo, elfMachine.Name) {
				return true
			}
		}

		return false
	}

	if gpuVMInfos.Len() == 0 {
		return false
	}

	gpuCountUsedByVM := 0
	availableCountMap := make(map[string]int32)
	for gpuID := range gpuVMInfos {
		gpuVMInfo := gpuVMInfos[gpuID]

		if gpuVMInfoContainsVM(gpuVMInfo, elfMachine.Name) {
			gpuCountUsedByVM += 1
		}

		availableCount := GetAvailableCountFromGPUVMInfo(gpuVMInfo)
		if count, ok := availableCountMap[*gpuVMInfo.UserVgpuTypeName]; ok {
			availableCountMap[*gpuVMInfo.UserVgpuTypeName] = count + availableCount
		} else {
			availableCountMap[*gpuVMInfo.UserVgpuTypeName] = availableCount
		}
	}

	if gpuCountUsedByVM > 0 {
		return gpuCountUsedByVM != gpuVMInfos.Len()
	}

	vGPUDevices := elfMachine.Spec.VGPUDevices
	for i := 0; i < len(vGPUDevices); i++ {
		if count, ok := availableCountMap[vGPUDevices[i].Type]; !ok || vGPUDevices[i].Count > count {
			return true
		}
	}

	return false
}

func gpuVMInfoFirstVMIs(gpuVMInfo *models.GpuVMInfo, vm string) bool {
	if len(gpuVMInfo.Vms) == 0 {
		return false
	}
	return *gpuVMInfo.Vms[0].ID == vm || *gpuVMInfo.Vms[0].Name == vm
}

func gpuVMInfoContainsVM(gpuVMInfo *models.GpuVMInfo, vm string) bool {
	for i := 0; i < len(gpuVMInfo.Vms); i++ {
		if *gpuVMInfo.Vms[i].ID == vm || *gpuVMInfo.Vms[i].Name == vm {
			return true
		}
	}

	return false
}

// GetAvailableCountFromGPUVMInfo returns the number of GPU that can be allocated.
func GetAvailableCountFromGPUVMInfo(gpuVMInfo *models.GpuVMInfo) int32 {
	if *gpuVMInfo.UserUsage == models.GpuDeviceUsagePASSTHROUGH {
		if len(gpuVMInfo.Vms) > 0 {
			return 0
		}

		return 1
	}

	return *gpuVMInfo.AvailableVgpusNum
}

// CalculateAssignedAndAvailableNumForGPUVMInfos count the number of vGPU used by virtual machines(including stopped).
func CalculateAssignedAndAvailableNumForGPUVMInfos(gpuVMInfos GPUVMInfos) {
	gpuVMInfos.Iterate(func(gpuVMInfo *models.GpuVMInfo) {
		if *gpuVMInfo.UserUsage == models.GpuDeviceUsagePASSTHROUGH {
			return
		}

		assignedVgpusNum := int32(0)
		for i := 0; i < len(gpuVMInfo.Vms); i++ {
			assignedVgpusNum += *gpuVMInfo.Vms[i].VgpuInstanceOnVMNum
		}
		gpuVMInfo.AssignedVgpusNum = &assignedVgpusNum
		availableVGPUNum := calGPUAvailableVgpusNum(*gpuVMInfo.VgpuInstanceNum, *gpuVMInfo.AssignedVgpusNum)
		gpuVMInfo.AvailableVgpusNum = &availableVGPUNum
	})
}

func calGPUAvailableVgpusNum(vgpuInstanceNum, assignedVGPUsNum int32) int32 {
	count := vgpuInstanceNum - assignedVGPUsNum
	if count < 0 {
		count = 0
	}

	return count
}

// parseOwnerFromCreatedBy parse owner from createdBy annotation.
//
// The owner can be in one of the following two formats:
// 1. ${Tower username}_${Tower auth_config_id}, e.g. caas.smartx_7e98ecbb-779e-43f6-8330-1bc1d29fffc7.
// 2. ${Tower username}, e.g. root. If auth_config_id is not set, it means it is a LOCAL user.
func parseOwnerFromCreatedBy(createdBy string) string {
	lastIndex := strings.LastIndex(createdBy, "@")
	if len(createdBy) <= 1 || lastIndex <= 0 || lastIndex == len(createdBy) {
		return createdBy
	}

	username := createdBy[0:lastIndex]
	authConfigID := createdBy[lastIndex+1:]

	// If authConfigID is not in UUID format, it means username contains the last `@` character,
	// return createdBy directly.
	if !typesutil.IsUUID(authConfigID) {
		return createdBy
	}

	// last `@` replaced with `_`.
	return fmt.Sprintf("%s_%s", username, authConfigID)
}
