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

	"github.com/smartxworks/cluster-api-provider-elf/pkg/util"
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

func NewTowerVM() *models.VM {
	id := ID()
	localID := UUID()
	status := models.VMStatusRUNNING

	return &models.VM{
		ID:                &id,
		LocalID:           &localID,
		Status:            &status,
		EntityAsyncStatus: (*models.EntityAsyncStatus)(util.TowerString("CREATING")),
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
		vms = append(vms, &models.NestedVM{ID: util.TowerString(vmIDs[i])})
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
