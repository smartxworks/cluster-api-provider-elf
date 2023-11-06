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
	if !ctx.ElfMachine.RequiresGPUOrVGPUDevices() {
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
		return nil, nil, nil
	}

	// Get all GPU devices of available hosts.
	gpuDeviceUsage := models.GpuDeviceUsagePASSTHROUGH
	if ctx.ElfMachine.RequiresVGPUDevices() {
		gpuDeviceUsage = models.GpuDeviceUsageVGPU
	}
	gpuDevices, err := ctx.VMService.FindGPUDevicesByHostIDs(availableHosts.IDs(), gpuDeviceUsage)
	if err != nil || len(gpuDevices) == 0 {
		return nil, nil, err
	}

	gpuDeviceIDs := make([]string, len(gpuDevices))
	for i := 0; i < len(gpuDevices); i++ {
		gpuDeviceIDs[i] = *gpuDevices[i].ID
	}
	// Get GPU devices with VMs and allocation details.
	gpuDeviceInfos, err := ctx.VMService.FindGPUDeviceInfos(gpuDeviceIDs)
	if err != nil {
		return nil, nil, err
	}

	service.AggregateUnusedGPUDevicesToGPUDeviceInfos(gpuDeviceInfos, gpuDevices)

	// Filter already used GPU devices.
	gpuDeviceInfos = gpuDeviceInfos.Filter(func(g *service.GPUDeviceInfo) bool {
		return g.AvailableCount > 0
	})

	// Filter locked GPU devices.
	gpuDeviceInfos = filterGPUDeviceInfosByLockGPUDevices(ctx.ElfCluster.Spec.Cluster, gpuDeviceInfos)

	// Group GPU deviceInfos by host.
	hostGPUDeviceInfoMap := make(map[string]service.GPUDeviceInfos)
	hostIDSet := sets.NewString()
	gpuDeviceInfos.Iterate(func(gpuDeviceInfo *service.GPUDeviceInfo) {
		hostIDSet.Insert(gpuDeviceInfo.HostID)
		if gpuInfos, ok := hostGPUDeviceInfoMap[gpuDeviceInfo.HostID]; !ok {
			hostGPUDeviceInfoMap[gpuDeviceInfo.HostID] = service.NewGPUDeviceInfos(gpuDeviceInfo)
		} else {
			gpuInfos.Insert(gpuDeviceInfo)
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
		hostGPUDeviceInfos, ok := hostGPUDeviceInfoMap[unsortedHostIDs[i]]
		if !ok {
			continue
		}

		var selectedGPUDeviceInfos []*service.GPUDeviceInfo
		if ctx.ElfMachine.RequiresGPUDevices() {
			selectedGPUDeviceInfos = selectGPUDevicesForVM(hostGPUDeviceInfos, ctx.ElfMachine.Spec.GPUDevices)
		} else {
			selectedGPUDeviceInfos = selectVGPUDevicesForVM(hostGPUDeviceInfos, ctx.ElfMachine.Spec.VGPUDevices)
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
func selectGPUDevicesForVM(hostGPUDeviceInfos service.GPUDeviceInfos, requiredGPUDevices []infrav1.GPUPassthroughDeviceSpec) []*service.GPUDeviceInfo {
	// Group GPU devices by model.
	modelGPUDeviceMap := make(map[string][]*service.GPUDeviceInfo)
	hostGPUDeviceInfos.Iterate(func(gpuDeviceInfo *service.GPUDeviceInfo) {
		if gpuInfos, ok := modelGPUDeviceMap[gpuDeviceInfo.Model]; !ok {
			modelGPUDeviceMap[gpuDeviceInfo.Model] = []*service.GPUDeviceInfo{gpuDeviceInfo}
		} else {
			modelGPUDeviceMap[gpuDeviceInfo.Model] = append(gpuInfos, gpuDeviceInfo)
		}
	})

	var selectedGPUDeviceInfos []*service.GPUDeviceInfo
	for i := 0; i < len(requiredGPUDevices); i++ {
		gpuDevices, ok := modelGPUDeviceMap[requiredGPUDevices[i].Model]
		if !ok || len(gpuDevices) < int(requiredGPUDevices[i].Count) {
			return nil
		}

		gpuInfos := gpuDevices[:int(requiredGPUDevices[i].Count)]
		for j := 0; j < len(gpuInfos); j++ {
			selectedGPUDeviceInfos = append(selectedGPUDeviceInfos, &service.GPUDeviceInfo{ID: gpuInfos[j].ID, AllocatedCount: 1, AvailableCount: 1})
		}
	}

	return selectedGPUDeviceInfos
}

// selectVGPUDevicesForVM selects the vGPU devices required by the virtual machine from the host's vGPU devices.
// Empty vGPU devices indicates that the host's vGPU devices cannot meet the vGPU requirements of the virtual machine.
func selectVGPUDevicesForVM(hostGPUDeviceInfos service.GPUDeviceInfos, requiredVGPUDevices []infrav1.VGPUDeviceSpec) []*service.GPUDeviceInfo {
	// Group vGPU devices by vGPU type.
	typeVGPUDeviceInfoMap := make(map[string][]*service.GPUDeviceInfo)
	hostGPUDeviceInfos.Iterate(func(gpuDeviceInfo *service.GPUDeviceInfo) {
		if gpuInfos, ok := typeVGPUDeviceInfoMap[gpuDeviceInfo.VGPUType]; !ok {
			typeVGPUDeviceInfoMap[gpuDeviceInfo.VGPUType] = []*service.GPUDeviceInfo{gpuDeviceInfo}
		} else {
			typeVGPUDeviceInfoMap[gpuDeviceInfo.VGPUType] = append(gpuInfos, gpuDeviceInfo)
		}
	})

	var selectedGPUDeviceInfos []*service.GPUDeviceInfo
	for i := 0; i < len(requiredVGPUDevices); i++ {
		gpuDeviceInfos, ok := typeVGPUDeviceInfoMap[requiredVGPUDevices[i].Type]
		if !ok {
			return nil
		}

		var gpuInfos []*service.GPUDeviceInfo
		requiredCount := requiredVGPUDevices[i].Count
		for j := 0; j < len(gpuDeviceInfos); j++ {
			if gpuDeviceInfos[j].AvailableCount <= 0 {
				continue
			}

			if gpuDeviceInfos[j].AvailableCount >= requiredCount {
				gpuInfos = append(gpuInfos, &service.GPUDeviceInfo{ID: gpuDeviceInfos[j].ID, AllocatedCount: requiredCount, AvailableCount: gpuDeviceInfos[j].AvailableCount})
				requiredCount = 0

				break
			} else {
				gpuInfos = append(gpuInfos, &service.GPUDeviceInfo{ID: gpuDeviceInfos[j].ID, AllocatedCount: gpuDeviceInfos[j].AvailableCount, AvailableCount: gpuDeviceInfos[j].AvailableCount})
				requiredCount -= gpuDeviceInfos[j].AvailableCount
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
	if !ctx.ElfMachine.RequiresGPUOrVGPUDevices() {
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
	if service.IsGPUAssignFailed(message) {
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
	staleGPUs := make([]*models.VMGpuOperationParams, len(vm.GpuDevices))
	for i := 0; i < len(vm.GpuDevices); i++ {
		staleGPUs[i] = &models.VMGpuOperationParams{
			GpuID:  vm.GpuDevices[i].ID,
			Amount: service.TowerInt32(1),
		}
	}

	task, err := ctx.VMService.RemoveGPUDevices(ctx.ElfMachine.Status.VMRef, staleGPUs)
	if err != nil {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.DetachingGPUFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

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
	gpuDevices, err := ctx.VMService.FindGPUDevicesByIDs(gpuDeviceIDs)
	if err != nil || len(gpuDevices) != len(gpuDeviceIDs) {
		return false, err
	}

	gpuDeviceInfos, err := ctx.VMService.FindGPUDeviceInfos(gpuDeviceIDs)
	if err != nil {
		return false, err
	}

	service.AggregateUnusedGPUDevicesToGPUDeviceInfos(gpuDeviceInfos, gpuDevices)

	if service.HasGPUsCanNotBeUsedForVM(gpuDeviceInfos, ctx.ElfMachine) {
		return false, nil
	}

	return true, nil
}
