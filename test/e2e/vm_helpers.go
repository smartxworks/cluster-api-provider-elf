package e2e

import (
	"context"

	. "github.com/onsi/gomega"

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
	job, err := input.VMService.PowerOff(input.UUID)
	Expect(err).ShouldNot(HaveOccurred())

	Eventually(func() (bool, error) {
		job, err = input.VMService.GetJob(job.Id)
		if err != nil {
			return false, err
		}

		if job.IsDone() {
			return true, nil
		} else if job.IsFailed() {
			job, err = input.VMService.PowerOff(input.UUID)

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
	job, err := input.VMService.PowerOn(input.UUID)
	Expect(err).ShouldNot(HaveOccurred())

	Eventually(func() (bool, error) {
		job, err = input.VMService.GetJob(job.Id)
		if err != nil {
			return false, err
		}

		if job.IsDone() {
			return true, nil
		} else if job.IsFailed() {
			job, err = input.VMService.PowerOn(input.UUID)

			return false, err
		}

		return false, nil
	}, input.WaitVMJobIntervals...).Should(BeTrue())
}
