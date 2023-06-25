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

func GetAvailableHosts(hosts []*models.Host, memory int64) []*models.Host {
	var availableHosts []*models.Host
	for i := 0; i < len(hosts); i++ {
		if ok, _ := IsAvailableHost(hosts[i], memory); ok {
			availableHosts = append(availableHosts, hosts[i])
		}
	}

	return availableHosts
}

func GetUnavailableHostInfo(hosts []*models.Host, memory int64) map[string]string {
	info := make(map[string]string)
	for i := 0; i < len(hosts); i++ {
		ok, message := IsAvailableHost(hosts[i], memory)
		if !ok {
			info[*hosts[i].Name] = message
		}
	}

	return info
}

func ContainsUnavailableHost(hosts []*models.Host, hostIDs []string, memory int64) bool {
	if len(hosts) == 0 || len(hostIDs) == 0 {
		return true
	}

	hostMap := make(map[string]*models.Host)
	for i := 0; i < len(hosts); i++ {
		hostMap[*hosts[i].ID] = hosts[i]
	}

	for i := 0; i < len(hostIDs); i++ {
		host, ok := hostMap[hostIDs[i]]
		if !ok {
			return true
		}

		if ok, _ := IsAvailableHost(host, memory); !ok {
			return true
		}
	}

	return false
}

func GetHostFromList(hostID string, hosts []*models.Host) *models.Host {
	for i := 0; i < len(hosts); i++ {
		if *hosts[i].ID == hostID {
			return hosts[i]
		}
	}

	return nil
}

func HostsToSet(hosts []*models.Host) sets.Set[string] {
	hostSet := sets.Set[string]{}
	for i := 0; i < len(hosts); i++ {
		hostSet.Insert(*hosts[i].ID)
	}

	return hostSet
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

func IsVMMigrationTask(task *models.Task) bool {
	return strings.Contains(GetTowerString(task.Description), "performing a live migration")
}

func IsPlacementGroupTask(task *models.Task) bool {
	return strings.Contains(GetTowerString(task.Description), "VM placement group") // Update VM placement group
}
