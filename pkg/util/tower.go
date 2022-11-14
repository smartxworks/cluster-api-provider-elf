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
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
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
