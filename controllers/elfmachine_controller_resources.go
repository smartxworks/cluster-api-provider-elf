/*
Copyright 2024.
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
	goctx "context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	agentv1 "github.com/smartxworks/host-config-agent-api/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiremote "sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/hostagent"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

func (r *ElfMachineReconciler) reconcileVMResources(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) (bool, error) {
	if ok, err := r.reconcileVMCPUAndMemory(ctx, machineCtx, vm); err != nil || !ok {
		return ok, err
	}

	if ok, err := r.restartKubelet(ctx, machineCtx); err != nil || !ok {
		return ok, err
	}

	if ok, err := r.reconcieVMVolume(ctx, machineCtx, vm, infrav1.ResourcesHotUpdatedCondition); err != nil || !ok {
		return ok, err
	}

	if ok, err := r.expandVMRootPartition(ctx, machineCtx); err != nil || !ok {
		return ok, err
	}

	if conditions.IsFalse(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition) {
		conditions.MarkTrue(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition)
	}

	return true, nil
}

// reconcieVMVolume ensures that the vm disk size is as expected.
//
// The conditionType param: VMProvisionedCondition/ResourcesHotUpdatedCondition.
func (r *ElfMachineReconciler) reconcieVMVolume(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM, conditionType clusterv1.ConditionType) (bool, error) {
	// If the capacity is 0, it means that the disk size has not changed and returns directly.
	if machineCtx.ElfMachine.Spec.DiskGiB == 0 {
		return true, nil
	}

	log := ctrl.LoggerFrom(ctx)

	vmDiskIDs := make([]string, len(vm.VMDisks))
	for i := range len(vm.VMDisks) {
		vmDiskIDs[i] = *vm.VMDisks[i].ID
	}

	vmDisks, err := machineCtx.VMService.GetVMDisks(vmDiskIDs)
	if err != nil {
		return false, errors.Wrapf(err, "failed to get disks for vm %s/%s", *vm.ID, *vm.Name)
	} else if len(vmDisks) == 0 {
		return false, errors.Errorf("no disks found for vm %s/%s", *vm.ID, *vm.Name)
	}
	systemDisk := service.GetVMSystemDisk(vmDisks)

	vmVolume, err := machineCtx.VMService.GetVMVolume(*systemDisk.VMVolume.ID)
	if err != nil {
		return false, err
	}

	diskSize := service.ByteToGiB(*vmVolume.Size)
	machineCtx.ElfMachine.Status.Resources.Disk = diskSize

	if machineCtx.ElfMachine.Spec.DiskGiB > diskSize {
		return false, r.resizeVMVolume(ctx, machineCtx, vmVolume, *service.TowerDisk(machineCtx.ElfMachine.Spec.DiskGiB), conditionType)
	} else if machineCtx.ElfMachine.Spec.DiskGiB < diskSize {
		log.V(3).Info(fmt.Sprintf("Current disk capacity is larger than expected, skipping expand vm volume %s/%s", *vmVolume.ID, *vmVolume.Name), "currentSize", diskSize, "expectedSize", machineCtx.ElfMachine.Spec.DiskGiB)
	}

	return true, nil
}

// resizeVMVolume sets the volume to the specified size.
func (r *ElfMachineReconciler) resizeVMVolume(ctx goctx.Context, machineCtx *context.MachineContext, vmVolume *models.VMVolume, diskSize int64, conditionType clusterv1.ConditionType) error {
	log := ctrl.LoggerFrom(ctx)

	reason := conditions.GetReason(machineCtx.ElfMachine, conditionType)
	if reason == "" ||
		(reason != infrav1.ExpandingVMDiskReason && reason != infrav1.ExpandingVMDiskFailedReason) {
		conditions.MarkFalse(machineCtx.ElfMachine, conditionType, infrav1.ExpandingVMDiskReason, clusterv1.ConditionSeverityInfo, "")

		// Save the conditionType first, and then expand the disk capacity.
		// This prevents the disk expansion from succeeding but failing to save the
		// conditionType, causing ElfMachine to not record the conditionType.
		return nil
	}

	if service.IsTowerResourcePerformingAnOperation(vmVolume.EntityAsyncStatus) {
		log.Info("Waiting for vm volume task done", "volume", fmt.Sprintf("%s/%s", *vmVolume.ID, *vmVolume.Name))

		return nil
	}

	withTaskVMVolume, err := machineCtx.VMService.ResizeVMVolume(*vmVolume.ID, diskSize)
	if err != nil {
		conditions.MarkFalse(machineCtx.ElfMachine, conditionType, infrav1.ExpandingVMDiskFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return errors.Wrapf(err, "failed to trigger expand size from %d to %d for vm volume %s/%s", *vmVolume.Size, diskSize, *vmVolume.ID, *vmVolume.Name)
	}

	machineCtx.ElfMachine.SetTask(*withTaskVMVolume.TaskID)

	log.Info(fmt.Sprintf("Waiting for the vm volume %s/%s to be expanded", *vmVolume.ID, *vmVolume.Name), "taskRef", machineCtx.ElfMachine.Status.TaskRef, "oldSize", *vmVolume.Size, "newSize", diskSize)

	return nil
}

// expandVMRootPartition adds new disk capacity to root partition.
func (r *ElfMachineReconciler) expandVMRootPartition(ctx goctx.Context, machineCtx *context.MachineContext) (bool, error) {
	reason := conditions.GetReason(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition)
	if reason == "" {
		return true, nil
	} else if reason != infrav1.ExpandingVMDiskReason &&
		reason != infrav1.ExpandingVMDiskFailedReason &&
		reason != infrav1.ExpandingRootPartitionReason &&
		reason != infrav1.ExpandingRootPartitionFailedReason {
		return true, nil
	}

	if reason != infrav1.ExpandingRootPartitionFailedReason {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingRootPartitionReason, clusterv1.ConditionSeverityInfo, "")
	}

	return r.reconcileHostJob(ctx, machineCtx, hostagent.HostAgentJobTypeExpandRootPartition)
}

// reconcileVMCPUAndMemory ensures that the vm CPU and memory are as expected.
func (r *ElfMachineReconciler) reconcileVMCPUAndMemory(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) (bool, error) {
	machineCtx.ElfMachine.Status.Resources.CPUCores = *vm.Vcpu
	machineCtx.ElfMachine.Status.Resources.Memory = *resource.NewQuantity(service.ByteToMiB(*vm.Memory)*1024*1024, resource.BinarySI)

	if !(machineCtx.ElfMachine.Spec.NumCPUs > *vm.Vcpu ||
		machineCtx.ElfMachine.Spec.MemoryMiB > service.ByteToMiB(*vm.Memory)) {
		return true, nil
	}

	reason := conditions.GetReason(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition)
	if reason == "" ||
		(reason != infrav1.ExpandingVMComputeResourcesReason && reason != infrav1.ExpandingVMComputeResourcesFailedReason) {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMComputeResourcesReason, clusterv1.ConditionSeverityInfo, "")

		// Save the condition first, and then expand the resources capacity.
		// This prevents the resources expansion from succeeding but failing to save the
		// condition, causing ElfMachine to not record the condition.
		return false, nil
	}

	log := ctrl.LoggerFrom(ctx)

	if ok := acquireTicketForUpdatingVM(machineCtx.ElfMachine.Name); !ok {
		log.V(1).Info(fmt.Sprintf("The VM operation reaches rate limit, skip updating VM %s CPU and memory", machineCtx.ElfMachine.Status.VMRef))

		return false, nil
	}

	withTaskVM, err := machineCtx.VMService.UpdateVM(vm, machineCtx.ElfMachine)
	if err != nil {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMComputeResourcesFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return false, errors.Wrapf(err, "failed to trigger update CPU and memory for VM %s", *vm.Name)
	}

	machineCtx.ElfMachine.SetTask(*withTaskVM.TaskID)

	log.Info("Waiting for the VM to be updated CPU and memory", "vmRef", machineCtx.ElfMachine.Status.VMRef, "taskRef", machineCtx.ElfMachine.Status.TaskRef)

	return false, nil
}

func (r *ElfMachineReconciler) restartKubelet(ctx goctx.Context, machineCtx *context.MachineContext) (bool, error) {
	reason := conditions.GetReason(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition)
	if reason == "" {
		return true, nil
	} else if reason != infrav1.ExpandingVMComputeResourcesReason &&
		reason != infrav1.ExpandingVMComputeResourcesFailedReason &&
		reason != infrav1.RestartingKubeletReason &&
		reason != infrav1.RestartingKubeletFailedReason {
		return true, nil
	}

	if reason != infrav1.RestartingKubeletFailedReason {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.RestartingKubeletReason, clusterv1.ConditionSeverityInfo, "")
	}

	return r.reconcileHostJob(ctx, machineCtx, hostagent.HostAgentJobTypeRestartKubelet)
}

func (r *ElfMachineReconciler) reconcileHostJob(ctx goctx.Context, machineCtx *context.MachineContext, jobType hostagent.HostAgentJobType) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	// Agent needs to wait for the node exists before it can run and execute commands.
	if machineCtx.Machine.Status.NodeInfo == nil {
		log.Info("Waiting for node exists for host agent job", "jobType", jobType)

		return false, nil
	}

	kubeClient, err := capiremote.NewClusterClient(ctx, "", r.Client, client.ObjectKey{Namespace: machineCtx.Cluster.Namespace, Name: machineCtx.Cluster.Name})
	if err != nil {
		return false, err
	}

	agentJob, err := hostagent.GetHostJob(ctx, kubeClient, machineCtx.ElfMachine.Namespace, hostagent.GetJobName(machineCtx.ElfMachine, jobType))
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}

	if agentJob == nil {
		agentJob = hostagent.GenerateJob(machineCtx.ElfMachine, jobType)
		if err = kubeClient.Create(ctx, agentJob); err != nil {
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingRootPartitionFailedReason, clusterv1.ConditionSeverityInfo, err.Error())

			return false, err
		}

		log.Info("Waiting for job to complete", "hostAgentJob", agentJob.Name)

		return false, nil
	}

	switch agentJob.Status.Phase {
	case agentv1.PhaseSucceeded:
		log.Info("HostJob succeeded", "hostAgentJob", agentJob.Name)
	case agentv1.PhaseFailed:
		switch jobType {
		case hostagent.HostAgentJobTypeExpandRootPartition:
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingRootPartitionFailedReason, clusterv1.ConditionSeverityWarning, agentJob.Status.FailureMessage)
		case hostagent.HostAgentJobTypeRestartKubelet:
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.RestartingKubeletFailedReason, clusterv1.ConditionSeverityWarning, agentJob.Status.FailureMessage)
		}
		log.Info("HostJob failed, will try again after three minutes", "hostAgentJob", agentJob.Name, "failureMessage", agentJob.Status.FailureMessage)

		lastExecutionTime := agentJob.Status.LastExecutionTime
		if lastExecutionTime == nil {
			lastExecutionTime = &agentJob.CreationTimestamp
		}
		// Three minutes after the job fails, delete the job and try again.
		if time.Now().After(lastExecutionTime.Add(3 * time.Minute)) {
			if err := kubeClient.Delete(ctx, agentJob); err != nil {
				return false, errors.Wrapf(err, "failed to delete hostJob %s/%s for retry", agentJob.Namespace, agentJob.Name)
			}
		}

		return false, nil
	default:
		log.Info("Waiting for HostJob done", "hostAgentJob", agentJob.Name, "jobStatus", agentJob.Status.Phase)

		return false, nil
	}

	return true, nil
}
