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
	"github.com/smartxworks/cloudtower-go-sdk/models"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/util"
)

func NewTowerVM() *models.VM {
	id := strings.ReplaceAll(uuid.New().String(), "-", "")
	localID := uuid.New().String()
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
