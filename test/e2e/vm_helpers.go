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

package e2e

import (
	"context"

	. "github.com/onsi/gomega"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

// ShutDownVMInput is the input for ShutDownVM.
type ShutDownVMInput struct {
	UUID               string
	VMService          service.VMService
	WaitVMJobIntervals []interface{}
}

// ShutDownVM shut down a VM.
func ShutDownVM(ctx context.Context, input ShutDownVMInput) {
	task, err := input.VMService.ShutDown(input.UUID)
	Expect(err).ShouldNot(HaveOccurred())

	Eventually(func() (bool, error) {
		task, err = input.VMService.GetTask(*task.ID)
		if err != nil {
			return false, err
		}

		if *task.Status == models.TaskStatusSUCCESSED {
			return true, nil
		} else if *task.Status == models.TaskStatusFAILED {
			task, err = input.VMService.ShutDown(input.UUID)

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
	task, err := input.VMService.PowerOn(input.UUID, "")
	Expect(err).ShouldNot(HaveOccurred())

	Eventually(func() (bool, error) {
		task, err = input.VMService.GetTask(*task.ID)
		if err != nil {
			return false, err
		}

		if *task.Status == models.TaskStatusSUCCESSED {
			return true, nil
		} else if *task.Status == models.TaskStatusFAILED {
			task, err = input.VMService.PowerOn(input.UUID, "")

			return false, err
		}

		return false, nil
	}, input.WaitVMJobIntervals...).Should(BeTrue())
}
