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

package service

import (
	"fmt"

	"github.com/smartxworks/cloudtower-go-sdk/v2/models"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/util"
)

func GetVMModifiedFields(vm *models.VM, elfMachine *infrav1.ElfMachine) map[string]string {
	fieldMap := make(map[string]string)
	numCPUs := elfMachine.Spec.NumCPUs
	if numCPUs <= 0 {
		numCPUs = config.VMNumCPUs
	}
	numCoresPerSocket := elfMachine.Spec.NumCoresPerSocket
	if numCoresPerSocket <= 0 {
		numCoresPerSocket = numCPUs
	}
	numCPUSockets := numCPUs / numCoresPerSocket

	if *vm.Vcpu > *util.TowerCPU(numCPUs) {
		fieldMap["vcpu"] = fmt.Sprintf("actual: %d, expected: %d", *vm.Vcpu, *util.TowerCPU(numCPUs))
	}
	if *vm.CPU.Cores > *util.TowerCPU(numCoresPerSocket) {
		fieldMap["cpuCores"] = fmt.Sprintf("actual: %d, expected: %d", *vm.CPU.Cores, *util.TowerCPU(numCoresPerSocket))
	}
	if *vm.CPU.Sockets > *util.TowerCPU(numCPUSockets) {
		fieldMap["cpuSockets"] = fmt.Sprintf("actual: %d, expected: %d", *vm.CPU.Sockets, *util.TowerCPU(numCPUSockets))
	}

	return fieldMap
}
