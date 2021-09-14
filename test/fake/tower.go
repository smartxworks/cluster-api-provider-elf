package fake

import (
	"strings"

	"github.com/google/uuid"
	"github.com/haijianyang/cloudtower-go-sdk/models"
)

func NewTowerVM() *models.VM {
	id := strings.ReplaceAll(uuid.New().String(), "-", "")
	localID := uuid.New().String()
	status := models.VMStatusRUNNING

	return &models.VM{
		ID:                &id,
		LocalID:           &localID,
		Status:            &status,
		EntityAsyncStatus: "CREATING",
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
