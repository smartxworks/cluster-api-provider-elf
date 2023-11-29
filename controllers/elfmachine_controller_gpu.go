/*
Copyright 2023.

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

package controllers

import (
	"github.com/pkg/errors"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

// selectHostAndGPUsForVM returns the host running the virtual machine
// and the GPU devices allocated to the virtual machine.
//
// By default, randomly select a host from the available hosts
// that meets the GPU requirements of the virtual machine.
// If preferredHostID is specified, the specified host will be given priority
// if it meets the GPU requirements.
//
// The return rethost:
// 1. nil means there are not enough hosts.
// 2. An empty string indicates that the host does not need to be specified.
// 3. A non-empty string indicates that the specified host ID was returned.
//
// The return gpudevices: the GPU devices for virtual machine.
func (r *ElfMachineReconciler) selectHostAndGPUsForVM(ctx *context.MachineContext, preferredHostID string) (rethost *string, gpudevices []*service.GPUDeviceInfo, reterr error) {
	if !ctx.ElfMachine.RequiresGPUDevices() {
		return pointer.String(""), nil, nil
	}

	defer func() {
		if rethost == nil {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForAvailableHostWithEnoughGPUsReason, clusterv1.ConditionSeverityInfo, "")

			ctx.Logger.V(1).Info("No host with the required GPU devices for the virtual machine, so wait for enough available hosts")
		}
	}()

	// If the GPU devices locked by the virtual machine still exist, use them directly.
	if lockedVMGPUs := getGPUDevicesLockedByVM(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name); lockedVMGPUs != nil {
		if ok, err := r.checkGPUsCanBeUsedForVM(ctx, lockedVMGPUs.GetGPUIDs()); err != nil {
			return nil, nil, err
		} else if ok {
			ctx.Logger.V(1).Info("Found locked VM GPU devices, so skip allocation", "lockedVMGPUs", lockedVMGPUs)

			return &lockedVMGPUs.HostID, lockedVMGPUs.GetGPUDeviceInfos(), nil
		}

		// If the GPU devices returned by Tower is inconsistent with the locked GPU,
		// delete the locked GPU devices and reallocate.
		ctx.Logger.V(1).Info("Locked VM GPU devices are invalid, so remove and reallocate", "lockedVMGPUs", lockedVMGPUs)

		unlockGPUDevicesLockedByVM(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name)
	}

	hosts, err := ctx.VMService.GetHostsByCluster(ctx.ElfCluster.Spec.Cluster)
	if err != nil {
		return nil, nil, err
	}

	availableHosts := hosts.FilterAvailableHostsWithEnoughMemory(*service.TowerMemory(ctx.ElfMachine.Spec.MemoryMiB))
	if len(availableHosts) == 0 {
		ctx.Logger.V(2).Info("Waiting for enough available hosts")
		return nil, nil, nil
	}

	// Get all GPU devices of available hosts.
	gpuDeviceUsage := models.GpuDeviceUsagePASSTHROUGH
	if ctx.ElfMachine.RequiresVGPUDevices() {
		gpuDeviceUsage = models.GpuDeviceUsageVGPU
	}
	gpuVMInfos, err := ctx.VMService.GetGPUDevicesAllocationInfoByHostIDs(availableHosts.IDs(), gpuDeviceUsage)
	if err != nil || len(gpuVMInfos) == 0 {
		return nil, nil, err
	}

	service.CalculateAssignedAndAvailableNumForGPUVMInfos(gpuVMInfos)

	// Filter available GPU devices.
	gpuVMInfos = gpuVMInfos.FilterAvailableGPUVMInfos()

	// Filter locked GPU devices.
	gpuVMInfos = filterGPUVMInfosByLockGPUDevices(ctx.ElfCluster.Spec.Cluster, gpuVMInfos)

	// Group GPU deviceInfos by host.
	hostGPUVMInfoMap := make(map[string]service.GPUVMInfos)
	hostIDSet := sets.NewString()
	gpuVMInfos.Iterate(func(gpuVMInfo *models.GpuVMInfo) {
		hostIDSet.Insert(*gpuVMInfo.Host.ID)
		if gpuInfos, ok := hostGPUVMInfoMap[*gpuVMInfo.Host.ID]; !ok {
			hostGPUVMInfoMap[*gpuVMInfo.Host.ID] = service.NewGPUVMInfos(gpuVMInfo)
		} else {
			gpuInfos.Insert(gpuVMInfo)
		}
	})

	// Choose a host that meets ElfMachine GPU needs.
	// Use a random host list to reduce the probability of the same host being selected at the same time.
	var unsortedHostIDs []string
	if hostIDSet.Has(preferredHostID) {
		hostIDSet.Delete(preferredHostID)
		// Prioritize the preferred host
		unsortedHostIDs = append(unsortedHostIDs, preferredHostID)
		unsortedHostIDs = append(unsortedHostIDs, hostIDSet.UnsortedList()...)
	} else {
		unsortedHostIDs = hostIDSet.UnsortedList()
	}

	for i := 0; i < len(unsortedHostIDs); i++ {
		hostGPUVMInfos, ok := hostGPUVMInfoMap[unsortedHostIDs[i]]
		if !ok {
			continue
		}

		var selectedGPUDeviceInfos []*service.GPUDeviceInfo
		if ctx.ElfMachine.RequiresPassThroughGPUDevices() {
			selectedGPUDeviceInfos = selectGPUDevicesForVM(hostGPUVMInfos, ctx.ElfMachine.Spec.GPUDevices)
		} else {
			selectedGPUDeviceInfos = selectVGPUDevicesForVM(hostGPUVMInfos, ctx.ElfMachine.Spec.VGPUDevices)
		}

		if len(selectedGPUDeviceInfos) > 0 {
			// Lock the selected GPU devices to prevent it from being allocated to multiple virtual machines.
			if !lockGPUDevicesForVM(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name, unsortedHostIDs[i], selectedGPUDeviceInfos) {
				// Lock failure indicates that the GPU devices are locked by another virtual machine.
				// Just trying other hosts.
				continue
			}

			ctx.Logger.Info("Selected host and GPU devices for VM", "hostId", unsortedHostIDs[i], "gpuDevices", selectedGPUDeviceInfos)

			return &unsortedHostIDs[i], selectedGPUDeviceInfos, nil
		}
	}

	return nil, nil, nil
}

// selectGPUDevicesForVM selects the GPU devices required by the virtual machine from the host's GPU devices.
// Empty GPU devices indicates that the host's GPU devices cannot meet the GPU requirements of the virtual machine.
func selectGPUDevicesForVM(hostGPUVMInfos service.GPUVMInfos, requiredGPUDevices []infrav1.GPUPassthroughDeviceSpec) []*service.GPUDeviceInfo {
	// Group GPU devices by model.
	modelGPUVMInfoMap := make(map[string][]*models.GpuVMInfo)
	hostGPUVMInfos.Iterate(func(gpuVMInfo *models.GpuVMInfo) {
		if gpuVMInfos, ok := modelGPUVMInfoMap[*gpuVMInfo.Model]; !ok {
			modelGPUVMInfoMap[*gpuVMInfo.Model] = []*models.GpuVMInfo{gpuVMInfo}
		} else {
			modelGPUVMInfoMap[*gpuVMInfo.Model] = append(gpuVMInfos, gpuVMInfo)
		}
	})

	var selectedGPUDeviceInfos []*service.GPUDeviceInfo
	for i := 0; i < len(requiredGPUDevices); i++ {
		gpuVMInfos, ok := modelGPUVMInfoMap[requiredGPUDevices[i].Model]
		if !ok || len(gpuVMInfos) < int(requiredGPUDevices[i].Count) {
			return nil
		}

		gpuInfos := gpuVMInfos[:int(requiredGPUDevices[i].Count)]
		for j := 0; j < len(gpuInfos); j++ {
			selectedGPUDeviceInfos = append(selectedGPUDeviceInfos, &service.GPUDeviceInfo{ID: *gpuInfos[j].ID, AllocatedCount: 1, AvailableCount: 1})
		}
	}

	return selectedGPUDeviceInfos
}

// selectVGPUDevicesForVM selects the vGPU devices required by the virtual machine from the host's vGPU devices.
// Empty vGPU devices indicates that the host's vGPU devices cannot meet the vGPU requirements of the virtual machine.
func selectVGPUDevicesForVM(hostGPUVMInfos service.GPUVMInfos, requiredVGPUDevices []infrav1.VGPUDeviceSpec) []*service.GPUDeviceInfo {
	// Group vGPU devices by vGPU type.
	typeVGPUVMInfoMap := make(map[string][]*models.GpuVMInfo)
	hostGPUVMInfos.Iterate(func(gpuVMInfo *models.GpuVMInfo) {
		if gpuVMInfos, ok := typeVGPUVMInfoMap[*gpuVMInfo.UserVgpuTypeName]; !ok {
			typeVGPUVMInfoMap[*gpuVMInfo.UserVgpuTypeName] = []*models.GpuVMInfo{gpuVMInfo}
		} else {
			typeVGPUVMInfoMap[*gpuVMInfo.UserVgpuTypeName] = append(gpuVMInfos, gpuVMInfo)
		}
	})

	var selectedGPUDeviceInfos []*service.GPUDeviceInfo
	for i := 0; i < len(requiredVGPUDevices); i++ {
		gpuVMInfos, ok := typeVGPUVMInfoMap[requiredVGPUDevices[i].Type]
		if !ok {
			return nil
		}

		var gpuInfos []*service.GPUDeviceInfo
		requiredCount := requiredVGPUDevices[i].Count
		for j := 0; j < len(gpuVMInfos); j++ {
			availableCount := service.GetAvailableCountFromGPUVMInfo(gpuVMInfos[j])
			if availableCount <= 0 {
				continue
			}

			if availableCount >= requiredCount {
				gpuInfos = append(gpuInfos, &service.GPUDeviceInfo{ID: *gpuVMInfos[j].ID, AllocatedCount: requiredCount, AvailableCount: availableCount})
				requiredCount = 0

				break
			} else {
				gpuInfos = append(gpuInfos, &service.GPUDeviceInfo{ID: *gpuVMInfos[j].ID, AllocatedCount: *gpuVMInfos[j].AvailableVgpusNum, AvailableCount: availableCount})
				requiredCount -= *gpuVMInfos[j].AvailableVgpusNum
			}
		}

		// If requiredCount is greater than 0, it means there are not enough vGPUs,
		// just return directly.
		if requiredCount > 0 {
			return nil
		}

		selectedGPUDeviceInfos = append(selectedGPUDeviceInfos, gpuInfos...)
	}

	return selectedGPUDeviceInfos
}

// reconcileGPUDevices ensures that the virtual machine has the expected GPU devices.
func (r *ElfMachineReconciler) reconcileGPUDevices(ctx *context.MachineContext, vm *models.VM) (bool, error) {
	if !ctx.ElfMachine.RequiresGPUDevices() {
		return true, nil
	}

	// Ensure GPUStatus is set or up to date.
	gpuDevices := make([]infrav1.GPUStatus, len(vm.GpuDevices))
	for i := 0; i < len(vm.GpuDevices); i++ {
		gpuDevices[i] = infrav1.GPUStatus{GPUID: *vm.GpuDevices[i].ID, Name: *vm.GpuDevices[i].Name}
	}
	ctx.ElfMachine.Status.GPUDevices = gpuDevices

	if *vm.Status != models.VMStatusSTOPPED {
		return true, nil
	}

	// GPU devices has been removed, need to select GPU devices.
	if len(vm.GpuDevices) == 0 {
		return r.addGPUDevicesForVM(ctx, vm)
	}

	// If the GPU devices are already in use, remove the GPU devices first and then reselect the new GPU devices.
	message := conditions.GetMessage(ctx.ElfMachine, infrav1.VMProvisionedCondition)
	if service.IsGPUAssignFailed(message) || service.IsVGPUInsufficientError(message) {
		ctx.Logger.Info("GPU devices of the host are not sufficient and the virtual machine cannot be started, so remove the GPU devices and reallocate.")

		return false, r.removeVMGPUDevices(ctx, vm)
	}

	gpuIDs := make([]string, len(vm.GpuDevices))
	for i := 0; i < len(vm.GpuDevices); i++ {
		gpuIDs[i] = *vm.GpuDevices[i].ID
	}

	if ok, err := r.checkGPUsCanBeUsedForVM(ctx, gpuIDs); err != nil {
		return false, err
	} else if !ok {
		// If the GPU devices are already in use,
		// remove the GPU devices first and then reallocate the new GPU devices.
		ctx.Logger.V(1).Info("GPU devices of VM are already in use, so remove and reallocate", "gpuIDs", gpuIDs)

		return false, r.removeVMGPUDevices(ctx, vm)
	}

	return true, nil
}

// addGPUDevicesForVM adds expected GPU devices to the virtual machine.
func (r *ElfMachineReconciler) addGPUDevicesForVM(ctx *context.MachineContext, vm *models.VM) (bool, error) {
	hostID, gpuDeviceInfos, err := r.selectHostAndGPUsForVM(ctx, *vm.Host.ID)
	if err != nil || hostID == nil {
		return false, err
	}

	if *vm.Host.ID != *hostID {
		ctx.Logger.Info("The current host does not have enough GPU devices, the virtual machine needs to be migrated to a host that meets the GPU device requirements.", "currentHost", *vm.Host.ID, "targetHost", *hostID)

		ok, err := r.migrateVM(ctx, vm, *hostID)
		if err != nil {
			unlockGPUDevicesLockedByVM(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name)
		}

		return ok, err
	}

	task, err := ctx.VMService.AddGPUDevices(ctx.ElfMachine.Status.VMRef, gpuDeviceInfos)
	if err != nil {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.AttachingGPUFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		unlockGPUDevicesLockedByVM(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name)

		return false, errors.Wrapf(err, "failed to trigger attaching GPU devices for VM %s", ctx)
	}

	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.UpdatingReason, clusterv1.ConditionSeverityInfo, "")

	ctx.ElfMachine.SetTask(*task.ID)

	ctx.Logger.Info("Waiting for VM to attach GPU devices", "vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return false, nil
}

// removeVMGPUDevices removes all GPU devices from the virtual machine.
func (r *ElfMachineReconciler) removeVMGPUDevices(ctx *context.MachineContext, vm *models.VM) error {
	var staleGPUs []*models.VMGpuOperationParams
	if ctx.ElfMachine.RequiresVGPUDevices() {
		vmGPUInfo, err := ctx.VMService.GetVMGPUAllocationInfo(*vm.ID)
		if err != nil {
			return err
		}

		for i := 0; i < len(vmGPUInfo.GpuDevices); i++ {
			staleGPUs = append(staleGPUs, &models.VMGpuOperationParams{
				GpuID:  vmGPUInfo.GpuDevices[i].ID,
				Amount: vmGPUInfo.GpuDevices[i].VgpuInstanceOnVMNum,
			})
		}
	} else {
		for i := 0; i < len(vm.GpuDevices); i++ {
			staleGPUs = append(staleGPUs, &models.VMGpuOperationParams{
				GpuID:  vm.GpuDevices[i].ID,
				Amount: service.TowerInt32(1),
			})
		}
	}

	task, err := ctx.VMService.RemoveGPUDevices(ctx.ElfMachine.Status.VMRef, staleGPUs)
	if err != nil {
		// If the GPU/vGPU is removed due to insufficient GPU/vGPU,
		// the original error message will not be overwritten if the remove fails.
		message := conditions.GetMessage(ctx.ElfMachine, infrav1.VMProvisionedCondition)
		if !(service.IsGPUAssignFailed(message) || service.IsVGPUInsufficientError(message)) {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.DetachingGPUFailedReason, clusterv1.ConditionSeverityWarning, err.Error())
		}

		return errors.Wrapf(err, "failed to trigger detaching stale GPU devices for VM %s", ctx)
	}

	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.UpdatingReason, clusterv1.ConditionSeverityInfo, "")

	ctx.ElfMachine.SetTask(*task.ID)

	ctx.Logger.Info("Waiting for VM to be removed stale GPU devices", "vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return nil
}

// checkGPUsCanBeUsedForVM checks whether GPU devices can be used by the specified virtual machine.
// The return true means the GPU devices can be used for the virtual machine.
func (r *ElfMachineReconciler) checkGPUsCanBeUsedForVM(ctx *context.MachineContext, gpuDeviceIDs []string) (bool, error) {
	gpuVMInfos := getGPUVMInfosFromCache(gpuDeviceIDs)
	if gpuVMInfos.Len() != len(gpuDeviceIDs) {
		var err error
		gpuVMInfos, err = ctx.VMService.GetGPUDevicesAllocationInfoByIDs(gpuDeviceIDs)
		if err != nil || len(gpuVMInfos) != len(gpuDeviceIDs) {
			return false, err
		}

		service.CalculateAssignedAndAvailableNumForGPUVMInfos(gpuVMInfos)

		setGPUVMInfosCache(gpuVMInfos)
	}

	if service.HasGPUsCanNotBeUsedForVM(gpuVMInfos, ctx.ElfMachine) {
		return false, nil
	}

	return true, nil
}
