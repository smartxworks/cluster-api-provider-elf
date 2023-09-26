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

package fake

import (
	"strings"

	"github.com/google/uuid"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	"k8s.io/utils/pointer"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

func ID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

func UUID() string {
	return uuid.New().String()
}

func NewTowerCluster() *models.Cluster {
	id := ID()
	localID := UUID()

	return &models.Cluster{
		ID:      &id,
		LocalID: &localID,
	}
}

func NewTowerHost() *models.Host {
	id := ID()
	localID := UUID()

	return &models.Host{
		ID:                     &id,
		LocalID:                &localID,
		Name:                   &id,
		Status:                 models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY),
		AllocatableMemoryBytes: pointer.Int64(1 * 1024 * 1024),
	}
}

func NewTowerVM() *models.VM {
	id := ID()
	localID := UUID()
	status := models.VMStatusRUNNING

	return &models.VM{
		ID:                &id,
		LocalID:           &localID,
		Name:              &id,
		Status:            &status,
		EntityAsyncStatus: (*models.EntityAsyncStatus)(pointer.String("CREATING")),
	}
}

func NewTowerVMFromElfMachine(elfMachine *infrav1.ElfMachine) *models.VM {
	vm := NewTowerVM()

	vm.Name = service.TowerString(elfMachine.Name)
	vm.Vcpu = service.TowerVCPU(elfMachine.Spec.NumCPUs)
	vm.CPU = &models.NestedCPU{
		Cores:   service.TowerCPUCores(*vm.Vcpu, elfMachine.Spec.NumCoresPerSocket),
		Sockets: service.TowerCPUSockets(*vm.Vcpu, *service.TowerCPUCores(*vm.Vcpu, elfMachine.Spec.NumCoresPerSocket)),
	}
	vm.Memory = service.TowerMemory(elfMachine.Spec.MemoryMiB)
	vm.Ha = service.TowerBool(elfMachine.Spec.HA)

	return vm
}

func NewTowerVMNic(order int) *models.VMNic {
	id := ID()
	localID := UUID()

	return &models.VMNic{
		ID:        &id,
		LocalID:   &localID,
		IPAddress: service.TowerString("192.168.0.1"),
		Order:     service.TowerInt32(order),
	}
}

func NewTowerTask() *models.Task {
	id := uuid.New().String()
	status := models.TaskStatusPENDING

	return &models.Task{
		ID:     &id,
		Status: &status,
	}
}

func NewWithTaskVM(vm *models.VM, task *models.Task) *models.WithTaskVM {
	return &models.WithTaskVM{
		Data:   vm,
		TaskID: task.ID,
	}
}

func NewTowerLabel() *models.Label {
	id := uuid.New().String()
	key := uuid.New().String()
	value := uuid.New().String()

	return &models.Label{
		ID:    &id,
		Key:   &key,
		Value: &value,
	}
}

func NewVMPlacementGroup(vmIDs []string) *models.VMPlacementGroup {
	id := ID()
	localID := UUID()
	vms := make([]*models.NestedVM, 0, len(vmIDs))
	for i := 0; i < len(vmIDs); i++ {
		vms = append(vms, &models.NestedVM{ID: &vmIDs[i]})
	}

	return &models.VMPlacementGroup{
		ID:      &id,
		Name:    &id,
		LocalID: &localID,
		Vms:     vms,
	}
}

func NewWithTaskVMPlacementGroup(placementGroup *models.VMPlacementGroup, task *models.Task) *models.WithTaskVMPlacementGroup {
	return &models.WithTaskVMPlacementGroup{
		Data:   placementGroup,
		TaskID: task.ID,
	}
}

func NewTowerGPU() *models.GpuDevice {
	return &models.GpuDevice{
		ID:      pointer.String(ID()),
		LocalID: pointer.String(UUID()),
		Name:    pointer.String(ID()),
		Model:   pointer.String("A16"),
	}
}
