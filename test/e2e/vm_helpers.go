package e2e

import (
	"context"

	. "github.com/onsi/gomega"

	"github.com/haijianyang/cloudtower-go-sdk/models"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

// PowerOffVMInput is the input for PowerOffVM.
type PowerOffVMInput struct {
	UUID               string
	VMService          service.VMService
	WaitVMJobIntervals []interface{}
}

// PowerOffVM power off a VM.
func PowerOffVM(ctx context.Context, input PowerOffVMInput) {
	task, err := input.VMService.PowerOff(input.UUID)
	Expect(err).ShouldNot(HaveOccurred())

	Eventually(func() (bool, error) {
		task, err = input.VMService.GetTask(*task.ID)
		if err != nil {
			return false, err
		}

		if *task.Status == models.TaskStatusSUCCESSED {
			return true, nil
		} else if *task.Status == models.TaskStatusFAILED {
			task, err = input.VMService.PowerOff(input.UUID)

			return false, err
		}

		return false, nil
	}, input.WaitVMJobIntervals...).Should(BeTrue())
}

// PowerOnVMInput is the input for PowerOnVM.
type PowerOnVMInput struct {
	UUID               string
	VMService          service.VMService
	WaitVMJobIntervals []interface{}
}

// PowerOnVM power on a VM.
func PowerOnVM(ctx context.Context, input PowerOnVMInput, intervals ...interface{}) {
	task, err := input.VMService.PowerOn(input.UUID)
	Expect(err).ShouldNot(HaveOccurred())

	Eventually(func() (bool, error) {
		task, err = input.VMService.GetTask(*task.ID)
		if err != nil {
			return false, err
		}

		if *task.Status == models.TaskStatusSUCCESSED {
			return true, nil
		} else if *task.Status == models.TaskStatusFAILED {
			task, err = input.VMService.PowerOn(input.UUID)

			return false, err
		}

		return false, nil
	}, input.WaitVMJobIntervals...).Should(BeTrue())
}
