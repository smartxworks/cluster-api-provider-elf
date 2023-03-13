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

package util

import (
	"fmt"
	"strings"

	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	labelsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/labels"
)

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

func TowerCPU(cpu int32) *int32 {
	return &cpu
}

func TowerMemory(memoryMiB int64) *int64 {
	memory := memoryMiB * 1024 * 1024

	return &memory
}

func TowerDisk(diskGiB int32) *int64 {
	disk := int64(diskGiB)
	disk = disk * 1024 * 1024 * 1024

	return &disk
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

func GetVMPlacementGroupName(machine *clusterv1.Machine) string {
	groupName := ""
	if IsControlPlaneMachine(machine) {
		groupName = labelsutil.GetControlPlaneLabel(machine)
	} else {
		groupName = labelsutil.GetDeploymentNameLabel(machine)
	}

	if groupName == "" {
		return ""
	}

	return fmt.Sprintf("cape-%s-cluster-%s-group", machine.Namespace, groupName)
}

func GetVMPlacementGroupPolicy(machine *clusterv1.Machine) models.VMVMPolicy {
	if IsControlPlaneMachine(machine) {
		return models.VMVMPolicyMUSTDIFFERENT
	}

	return models.VMVMPolicyPREFERDIFFERENT
}
