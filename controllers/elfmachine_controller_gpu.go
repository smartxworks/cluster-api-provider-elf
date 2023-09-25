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
func (r *ElfMachineReconciler) selectHostAndGPUsForVM(ctx *context.MachineContext, preferredHostID string) (rethost *string, gpudevices []*models.GpuDevice, reterr error) {
	if !ctx.ElfMachine.HasGPUDevice() {
		return pointer.String(""), nil, nil
	}

	defer func() {
		if rethost == nil {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForAvailableHostWithEnoughGPUsReason, clusterv1.ConditionSeverityInfo, "")

			ctx.Logger.V(1).Info("No host with the required GPU devices for the virtual machine, so wait for enough available hosts")
		}
	}()

	// If the GPU devices locked by the virtual machine still exist, use them directly.
	if lockedVMGPUs := getLockedVMGPUDevices(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name); lockedVMGPUs != nil {
		gpuDevices, err := r.checkGPUsCanBeUsedForVM(ctx, lockedVMGPUs.GPUDeviceIDs, ctx.ElfMachine.Name)
		if err != nil {
			return nil, nil, err
		}

		if len(gpuDevices) > 0 {
			ctx.Logger.V(1).Info("Found locked VM GPU devices, so skip allocation", "lockedVMGPUs", lockedVMGPUs)

			return &lockedVMGPUs.HostID, gpuDevices, nil
		}

		// If the GPU devices returned by Tower is inconsistent with the locked GPU,
		// delete the locked GPU devices and reallocate.
		ctx.Logger.V(1).Info("Locked VM GPU devices are invalid, so remove and reallocate", "lockedVMGPUs", lockedVMGPUs)

		unlockVMGPUDevices(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name)
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
	gpuDevices, err := ctx.VMService.FindGPUDevices(availableHosts.IDs())
	if err != nil {
		return nil, nil, err
	}
	gpuDevices = service.FilterGPUsCanNotBeUsedForVM(gpuDevices, ctx.ElfMachine.Name)

	lockedClusterGPUIDs := getLockedClusterGPUIDs(ctx.ElfCluster.Spec.Cluster)

	// Group GPU devices by host.
	hostGPUDeviceMap := make(map[string][]*models.GpuDevice)
	hostIDSet := sets.NewString()
	for i := 0; i < len(gpuDevices); i++ {
		// Filter locked GPU devices.
		if lockedClusterGPUIDs.Has(*gpuDevices[i].ID) {
			continue
		}

		hostIDSet.Insert(*gpuDevices[i].Host.ID)
		if gpus, ok := hostGPUDeviceMap[*gpuDevices[i].Host.ID]; !ok {
			hostGPUDeviceMap[*gpuDevices[i].Host.ID] = []*models.GpuDevice{gpuDevices[i]}
		} else {
			hostGPUDeviceMap[*gpuDevices[i].Host.ID] = append(gpus, gpuDevices[i])
		}
	}

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
		if hostGPUDevices, ok := hostGPUDeviceMap[unsortedHostIDs[i]]; ok {
			selectedGPUDevices := selectGPUDevicesForVM(hostGPUDevices, ctx.ElfMachine.Spec.GPUDevices)
			if len(selectedGPUDevices) > 0 {
				gpuDeviceIDs := make([]string, len(selectedGPUDevices))
				for i := 0; i < len(selectedGPUDevices); i++ {
					gpuDeviceIDs[i] = *selectedGPUDevices[i].ID
				}

				// Lock the selected GPU devices to prevent it from being allocated to multiple virtual machines.
				if !lockVMGPUDevices(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name, unsortedHostIDs[i], gpuDeviceIDs) {
					// Lock failure indicates that the GPU devices are locked by another virtual machine.
					// Just trying other hosts.
					continue
				}

				ctx.Logger.Info("Selected host and GPU devices for VM", "hostId", unsortedHostIDs[i], "gpuDeviceIds", gpuDeviceIDs)

				return &unsortedHostIDs[i], selectedGPUDevices, nil
			}
		}
	}

	return nil, nil, nil
}

// selectGPUDevicesForVM selects the GPU devices required by the virtual machine from the host's GPU devices.
// Empty GPU devices indicates that the host's GPU devices cannot meet the GPU requirements of the virtual machine.
func selectGPUDevicesForVM(hostGPUDevices []*models.GpuDevice, requiredGPUDevices []infrav1.GPUPassthroughDeviceSpec) []*models.GpuDevice {
	// Group GPU devices by model.
	modelGPUDeviceMap := make(map[string][]*models.GpuDevice)
	for i := 0; i < len(hostGPUDevices); i++ {
		if gpus, ok := modelGPUDeviceMap[*hostGPUDevices[i].Model]; !ok {
			modelGPUDeviceMap[*hostGPUDevices[i].Model] = []*models.GpuDevice{hostGPUDevices[i]}
		} else {
			modelGPUDeviceMap[*hostGPUDevices[i].Model] = append(gpus, hostGPUDevices[i])
		}
	}

	var selectedGPUDevices []*models.GpuDevice
	for i := 0; i < len(requiredGPUDevices); i++ {
		if gpus, ok := modelGPUDeviceMap[requiredGPUDevices[i].GPUModel]; !ok {
			return nil
		} else {
			if len(gpus) < int(requiredGPUDevices[i].Count) {
				return nil
			}

			selectedGPUDevices = append(selectedGPUDevices, gpus[:int(requiredGPUDevices[i].Count)]...)
			// Remove selected GPU devices.
			modelGPUDeviceMap[requiredGPUDevices[i].GPUModel] = gpus[int(requiredGPUDevices[i].Count):]
		}
	}

	return selectedGPUDevices
}

// reconcileVMGPUDevices ensures that the virtual machine has the expected GPU devices.
func (r *ElfMachineReconciler) reconcileVMGPUDevices(ctx *context.MachineContext, vm *models.VM) (bool, error) {
	if !ctx.ElfMachine.HasGPUDevice() {
		return true, nil
	}

	if *vm.Status != models.VMStatusSTOPPED {
		gpuDevices := make([]infrav1.GPUStatus, len(vm.GpuDevices))
		for i := 0; i < len(vm.GpuDevices); i++ {
			gpuDevices[i] = infrav1.GPUStatus{GPUID: *vm.GpuDevices[i].ID, Name: *vm.GpuDevices[i].Name}
		}
		ctx.ElfMachine.Status.GPUDevices = gpuDevices

		return true, nil
	}

	// GPU devices has been removed, need to select GPU devices.
	if len(vm.GpuDevices) == 0 {
		return r.addVMGPUDevices(ctx, vm)
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

	gpuDevices, err := r.checkGPUsCanBeUsedForVM(ctx, gpuIDs, ctx.ElfMachine.Name)
	if err != nil {
		return false, err
	}

	if len(gpuDevices) == 0 {
		// If the GPU devices are already in use,
		// remove the GPU devices first and then reallocate the new GPU devices.
		ctx.Logger.V(1).Info("GPU devices of VM are already in use, so remove and reallocate", "gpuIDs", gpuIDs)

		return false, r.removeVMGPUDevices(ctx, vm)
	}

	return true, nil
}

// addVMGPUDevices adds expected GPU devices to the virtual machine.
func (r *ElfMachineReconciler) addVMGPUDevices(ctx *context.MachineContext, vm *models.VM) (bool, error) {
	hostID, gpuDevices, err := r.selectHostAndGPUsForVM(ctx, *vm.Host.ID)
	if err != nil || hostID == nil {
		return false, err
	}

	if *vm.Host.ID != *hostID {
		ctx.Logger.Info("The current host does not have enough GPU devices, the virtual machine needs to be migrated to a host that meets the GPU device requirements.", "currentHost", *vm.Host.ID, "targetHost", *hostID)

		ok, err := r.migrateVM(ctx, vm, *hostID)
		if err != nil {
			unlockVMGPUDevices(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name)
		}

		return ok, err
	}

	gpus := make([]*models.VMGpuOperationParams, len(gpuDevices))
	for i := 0; i < len(gpuDevices); i++ {
		gpus[i] = &models.VMGpuOperationParams{
			GpuID:  gpuDevices[i].ID,
			Amount: service.TowerInt32(1),
		}
	}

	task, err := ctx.VMService.AddGPUDevices(ctx.ElfMachine.Status.VMRef, gpus)
	if err != nil {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOnFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		unlockVMGPUDevices(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name)

		return false, errors.Wrapf(err, "failed to trigger add GPU devices for VM %s", ctx)
	}

	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.UpdatingReason, clusterv1.ConditionSeverityInfo, "")

	ctx.ElfMachine.SetTask(*task.ID)

	ctx.Logger.Info("Waiting for VM to be added GPU devices", "vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

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
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOnFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return errors.Wrapf(err, "failed to trigger remove stale GPU devices for VM %s", ctx)
	}

	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.UpdatingReason, clusterv1.ConditionSeverityInfo, "")

	ctx.ElfMachine.SetTask(*task.ID)

	ctx.Logger.Info("Waiting for VM to be removed stale GPU devices", "vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return nil
}

// checkGPUsCanBeUsedForVM checks whether GPU devices can be used by the specified virtual machine.
// If one of the GPU devices cannot be used, an empty array is returned.
func (r *ElfMachineReconciler) checkGPUsCanBeUsedForVM(ctx *context.MachineContext, gpuDeviceIDs []string, vm string) ([]*models.GpuDevice, error) {
	gpuDevices, err := ctx.VMService.FindGPUDevicesByIDs(gpuDeviceIDs)
	if err != nil {
		return nil, err
	}

	if len(gpuDevices) != len(gpuDeviceIDs) {
		return nil, nil
	}

	if len(service.FilterGPUsCanNotBeUsedForVM(gpuDevices, vm)) != len(gpuDeviceIDs) {
		return nil, nil
	}

	return gpuDevices, nil
}
