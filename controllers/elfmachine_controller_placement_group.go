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
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	annotationsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/annotations"
	kcputil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/kcp"
	machineutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/machine"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/version"
)

// reconcilePlacementGroup makes sure that the placement group exist.
func (r *ElfMachineReconciler) reconcilePlacementGroup(ctx *context.MachineContext) (reconcile.Result, error) {
	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	if placementGroup, err := r.getPlacementGroup(ctx, placementGroupName); err != nil {
		if !service.IsVMPlacementGroupNotFound(err) {
			return reconcile.Result{}, err
		}

		if ok := acquireTicketForPlacementGroupOperation(placementGroupName); ok {
			defer releaseTicketForPlacementGroupOperation(placementGroupName)
		} else {
			return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
		}

		if placementGroup, err := r.createPlacementGroup(ctx, placementGroupName); err != nil {
			return reconcile.Result{}, err
		} else if placementGroup == nil {
			return reconcile.Result{RequeueAfter: config.VMPlacementGroupDuplicateTimeout}, err
		}
	} else if placementGroup == nil {
		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	return reconcile.Result{}, nil
}

func (r *ElfMachineReconciler) createPlacementGroup(ctx *context.MachineContext, placementGroupName string) (*models.VMPlacementGroup, error) {
	// TODO: This will be removed when Tower fixes issue with placement group data syncing.
	if ok := canCreatePlacementGroup(placementGroupName); !ok {
		ctx.Logger.V(2).Info(fmt.Sprintf("Tower has duplicate placement group, skip creating placement group %s", placementGroupName))

		return nil, nil
	}

	towerCluster, err := ctx.VMService.GetCluster(ctx.ElfCluster.Spec.Cluster)
	if err != nil {
		return nil, err
	}

	placementGroupPolicy := towerresources.GetVMPlacementGroupPolicy(ctx.Machine)
	withTaskVMPlacementGroup, err := ctx.VMService.CreateVMPlacementGroup(placementGroupName, *towerCluster.ID, placementGroupPolicy)
	if err != nil {
		return nil, err
	}

	task, err := ctx.VMService.WaitTask(*withTaskVMPlacementGroup.TaskID, config.WaitTaskTimeout, config.WaitTaskInterval)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to wait for placement group creation task done timed out in %s: placementName %s, taskID %s", config.WaitTaskTimeout, placementGroupName, *withTaskVMPlacementGroup.TaskID)
	}

	if *task.Status == models.TaskStatusFAILED {
		if service.IsVMPlacementGroupDuplicate(service.GetTowerString(task.ErrorMessage)) {
			setPlacementGroupDuplicate(placementGroupName)

			ctx.Logger.Info(fmt.Sprintf("Duplicate placement group detected, will try again in %s", placementGroupSilenceTime), "placementGroup", placementGroupName)
		}

		return nil, errors.Errorf("failed to create placement group %s in task %s", placementGroupName, *task.ID)
	}

	ctx.Logger.Info("Creating placement group succeeded", "taskID", *task.ID, "placementGroup", placementGroupName)

	placementGroup, err := ctx.VMService.GetVMPlacementGroup(placementGroupName)
	if err != nil {
		return nil, err
	}

	return placementGroup, nil
}

// preCheckPlacementGroup checks whether there are still available hosts before creating the virtual machine.
//
// The return value:
// 1. nil means there are not enough hosts.
// 2. An empty string indicates that there is an available host.
// 3. A non-empty string indicates that the specified host ID was returned.
func (r *ElfMachineReconciler) preCheckPlacementGroup(ctx *context.MachineContext) (rethost *string, reterr error) {
	defer func() {
		if rethost == nil {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForAvailableHostRequiredByPlacementGroupReason, clusterv1.ConditionSeverityInfo, "")
		}
	}()

	if !machineutil.IsControlPlaneMachine(ctx.Machine) {
		return pointer.String(""), nil
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	if err != nil {
		return nil, err
	}

	placementGroup, err := r.getPlacementGroup(ctx, placementGroupName)
	if err != nil || placementGroup == nil {
		return nil, err
	}

	usedHostSet, err := r.getHostsInPlacementGroup(ctx, placementGroup)
	if err != nil {
		return nil, err
	}

	hosts, err := ctx.VMService.GetHostsByCluster(ctx.ElfCluster.Spec.Cluster)
	if err != nil {
		return nil, err
	}

	availableHosts := service.GetAvailableHosts(hosts, *service.TowerMemory(ctx.ElfMachine.Spec.MemoryMiB))
	if len(availableHosts) < len(hosts) {
		unavailableHostInfo := service.GetUnavailableHostInfo(hosts, *service.TowerMemory(ctx.ElfMachine.Spec.MemoryMiB))
		ctx.Logger.V(1).Info("Unavailable hosts found", "unavailableHosts", unavailableHostInfo, "placementGroup", *placementGroup.Name, "vmRef", ctx.ElfMachine.Status.VMRef)
	}

	availableHostSet := service.HostsToSet(availableHosts)
	unusedHostSet := availableHostSet.Difference(usedHostSet)
	if unusedHostSet.Len() != 0 {
		ctx.Logger.V(2).Info("The placement group still has capacity", "placementGroup", *placementGroup.Name, "availableHosts", availableHostSet.UnsortedList(), "unusedHosts", unusedHostSet.UnsortedList())

		return pointer.String(""), nil
	}

	kcp, err := machineutil.GetKCPByMachine(ctx, ctx.Client, ctx.Machine)
	if err != nil {
		return nil, err
	}

	// KCP is not in scaling down/rolling update.
	if !(kcputil.IsKCPRollingUpdateFirstMachine(kcp) || kcputil.IsKCPInScalingDown(kcp)) {
		ctx.Logger.V(1).Info("The placement group is full, wait for enough available hosts", "placementGroup", *placementGroup.Name, "availableHosts", availableHostSet.UnsortedList(), "usedHosts", usedHostSet.UnsortedList())

		return nil, nil
	}

	// KCP is in scaling down.
	//
	// KCP scaling up will fail if the placement group is full, then the user will trigger KCP scaling down.
	// The Machine being created cannot pass the Preflight checks(https://github.com/kubernetes-sigs/cluster-api/blob/main/docs/proposals/20191017-kubeadm-based-control-plane.md#preflight-checks),
	// so the Machine will not be deleted.
	// We can add delete machine annotation on the Machine and KCP will delete it.
	if kcputil.IsKCPInScalingDown(kcp) {
		if annotationsutil.HasAnnotation(ctx.Machine, clusterv1.DeleteMachineAnnotation) {
			return nil, nil
		}

		newMachine := ctx.Machine.DeepCopy()
		patchHelper, err := patch.NewHelper(ctx.Machine, r.Client)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to init patch helper for %s %s/%s", ctx.Machine.GroupVersionKind(), ctx.Machine.Namespace, ctx.Machine.Name)
		}

		ctx.Logger.Info("Add the delete machine annotation on KCP Machine in order to delete it, because KCP is being scaled down after a failed scaling up", "placementGroup", *placementGroup.Name, "availableHosts", availableHostSet.UnsortedList())

		// Allow scaling down of KCP with the possibility of marking specific control plane machine(s) to be deleted with delete annotation key.
		// The presence of the annotation will affect the rollout strategy in a way that, it implements the following prioritization logic in descending order,
		// while selecting machines for scale down:
		//   1.outdatedMachines with the delete annotation
		//   2.machines with the delete annotation
		//   3.outdated machines
		//   4.all machines
		//
		// Refer to https://github.com/kubernetes-sigs/cluster-api/blob/main/docs/proposals/20191017-kubeadm-based-control-plane.md#scale-down
		annotations.AddAnnotations(newMachine, map[string]string{clusterv1.DeleteMachineAnnotation: ""})
		if err := patchHelper.Patch(r, newMachine); err != nil {
			return nil, errors.Wrapf(err, "failed to patch Machine %s to add delete machine annotation %s.", newMachine.Name, clusterv1.DeleteMachineAnnotation)
		}

		return nil, nil
	}

	// KCP is in rolling update.

	if !service.ContainsUnavailableHost(hosts, usedHostSet.UnsortedList(), *service.TowerMemory(ctx.ElfMachine.Spec.MemoryMiB)) &&
		int(*kcp.Spec.Replicas) == usedHostSet.Len() {
		// Only when KCP is in rolling update and the placement group is full
		// does it need to get the latest created machine.
		hostID, err := r.getVMHostForRollingUpdate(ctx, placementGroup, hosts)
		if err != nil || hostID == "" {
			return nil, err
		}

		return pointer.String(hostID), err
	}

	ctx.Logger.V(1).Info("The placement group is full, wait for enough available hosts", "placementGroup", *placementGroup.Name, "availableHosts", availableHostSet.UnsortedList(), "usedHosts", usedHostSet.UnsortedList())

	return nil, nil
}

// getVMHostForRollingUpdate returns the target host server id for a virtual machine during rolling update.
// During KCP rolling update, machines will be deleted in the order of creation.
// Find the latest created machine in the placement group,
// and set the host where the machine is located to the first machine created by KCP rolling update.
// This prevents migration of virtual machine during KCP rolling update when using a placement group.
func (r *ElfMachineReconciler) getVMHostForRollingUpdate(ctx *context.MachineContext, placementGroup *models.VMPlacementGroup, hosts []*models.Host) (string, error) {
	elfMachines, err := machineutil.GetControlPlaneElfMachinesInCluster(ctx, ctx.Client, ctx.Cluster.Namespace, ctx.Cluster.Name)
	if err != nil {
		return "", err
	}

	elfMachineMap := make(map[string]*infrav1.ElfMachine)
	for i := 0; i < len(elfMachines); i++ {
		if machineutil.IsUUID(elfMachines[i].Status.VMRef) {
			elfMachineMap[elfMachines[i].Name] = elfMachines[i]
		}
	}

	placementGroupMachines := make([]*clusterv1.Machine, 0, len(placementGroup.Vms))
	vmMap := make(map[string]string)
	for i := 0; i < len(placementGroup.Vms); i++ {
		if elfMachine, ok := elfMachineMap[*placementGroup.Vms[i].Name]; ok {
			machine, err := capiutil.GetOwnerMachine(r, r.Client, elfMachine.ObjectMeta)
			if err != nil {
				return "", err
			}

			placementGroupMachines = append(placementGroupMachines, machine)
			vmMap[machine.Name] = *(placementGroup.Vms[i].ID)
		}
	}

	machines := collections.FromMachines(placementGroupMachines...)
	if machine := machines.Newest(); machine != nil {
		if vm, err := ctx.VMService.Get(vmMap[machine.Name]); err != nil {
			return "", err
		} else {
			host := service.GetHostFromList(*vm.Host.ID, hosts)
			if host == nil {
				ctx.Logger.Info("Host not found, skip selecting host for VM", "hostID", *vm.Host.ID, "vmRef", ctx.ElfMachine.Status.VMRef)
			} else {
				ok, message := service.IsAvailableHost(host, *service.TowerMemory(ctx.ElfMachine.Spec.MemoryMiB))
				if ok {
					ctx.Logger.Info("Selected the host server for VM since the placement group is full", "hostID", *vm.Host.ID, "vmRef", ctx.ElfMachine.Status.VMRef)

					return *vm.Host.ID, nil
				}

				ctx.Logger.Info(fmt.Sprintf("Host is unavailable: %s, skip selecting host for VM", message), "hostID", *vm.Host.ID, "vmRef", ctx.ElfMachine.Status.VMRef)
			}
		}
	}

	return "", nil
}

// getHostsInPlacementGroup returns the hosts where all virtual machines of placement group located.
func (r *ElfMachineReconciler) getHostsInPlacementGroup(ctx *context.MachineContext, placementGroup *models.VMPlacementGroup) (sets.Set[string], error) {
	placementGroupVMSet := service.GetVMsInPlacementGroup(placementGroup)
	vms, err := ctx.VMService.FindByIDs(placementGroupVMSet.UnsortedList())
	if err != nil {
		return nil, err
	}

	hostSet := sets.Set[string]{}
	for i := 0; i < len(vms); i++ {
		hostSet.Insert(*vms[i].Host.ID)
	}

	return hostSet, nil
}

// getAvailableHosts returns available hosts.
func (r *ElfMachineReconciler) getAvailableHosts(ctx *context.MachineContext, hosts []*models.Host, usedHostSet sets.Set[string], vm *models.VM) []*models.Host {
	var availableHosts []*models.Host
	// If the VM is running, and the host where the VM is located
	// is not used by the placement group, then it is not necessary to
	// check the memory is sufficient to determine whether the host is available.
	// Otherwise, the VM may need to be migrated to another host,
	// and need to check whether the memory is sufficient.
	if *vm.Status == models.VMStatusRUNNING {
		availableHosts = service.GetAvailableHosts(hosts, 0)
		unusedHostSet := service.HostsToSet(availableHosts).Difference(usedHostSet)
		var unusedHosts []*models.Host
		for _, host := range availableHosts {
			if !usedHostSet.Has(*host.ID) {
				unusedHosts = append(unusedHosts, host)
			}
		}
		if !unusedHostSet.Has(*vm.Host.ID) {
			availableHosts = service.GetAvailableHosts(unusedHosts, *service.TowerMemory(ctx.ElfMachine.Spec.MemoryMiB))
		}
	} else {
		availableHosts = service.GetAvailableHosts(hosts, *service.TowerMemory(ctx.ElfMachine.Spec.MemoryMiB))
	}

	return availableHosts
}

func (r *ElfMachineReconciler) getPlacementGroup(ctx *context.MachineContext, placementGroupName string) (*models.VMPlacementGroup, error) {
	placementGroup, err := ctx.VMService.GetVMPlacementGroup(placementGroupName)
	if err != nil {
		return nil, err
	}

	// Placement group is performing an operation
	if !machineutil.IsUUID(*placementGroup.LocalID) || placementGroup.EntityAsyncStatus != nil {
		ctx.Logger.Info("Waiting for placement group task done", "placementGroup", *placementGroup.Name)

		return nil, nil
	}

	return placementGroup, nil
}

// joinPlacementGroup puts the virtual machine into the placement group.
func (r *ElfMachineReconciler) joinPlacementGroup(ctx *context.MachineContext, vm *models.VM) (ret bool, reterr error) {
	if !version.IsCompatibleWithPlacementGroup(ctx.ElfMachine) {
		ctx.Logger.V(1).Info(fmt.Sprintf("The capeVersion of ElfMachine is lower than %s, skip adding VM to the placement group", version.CAPEVersion1_2_0), "capeVersion", version.GetCAPEVersion(ctx.ElfMachine))

		return true, nil
	}

	defer func() {
		if reterr != nil {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.JoiningPlacementGroupFailedReason, clusterv1.ConditionSeverityWarning, reterr.Error())
		} else if !ret {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.JoiningPlacementGroupReason, clusterv1.ConditionSeverityInfo, "")
		}
	}()

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	if err != nil {
		return false, err
	}

	if ok := acquireTicketForPlacementGroupOperation(placementGroupName); ok {
		defer releaseTicketForPlacementGroupOperation(placementGroupName)
	} else {
		return false, nil
	}

	placementGroup, err := r.getPlacementGroup(ctx, placementGroupName)
	if err != nil || placementGroup == nil {
		return false, err
	}

	placementGroupVMSet := service.GetVMsInPlacementGroup(placementGroup)
	if placementGroupVMSet.Has(*vm.ID) {
		return true, nil
	}

	if machineutil.IsControlPlaneMachine(ctx.Machine) {
		usedHostSet, err := r.getHostsInPlacementGroup(ctx, placementGroup)
		if err != nil {
			return false, err
		}

		hosts, err := ctx.VMService.GetHostsByCluster(ctx.ElfCluster.Spec.Cluster)
		if err != nil {
			return false, err
		}

		availableHosts := r.getAvailableHosts(ctx, hosts, usedHostSet, vm)
		if len(availableHosts) < len(hosts) {
			unavailableHostInfo := service.GetUnavailableHostInfo(hosts, *service.TowerMemory(ctx.ElfMachine.Spec.MemoryMiB))
			ctx.Logger.Info("Unavailable hosts found", "unavailableHosts", unavailableHostInfo, "placementGroup", *placementGroup.Name, "vmRef", ctx.ElfMachine.Status.VMRef, "vmId", *vm.ID)
		}

		availableHostSet := service.HostsToSet(availableHosts)
		unusedHostSet := availableHostSet.Difference(usedHostSet)
		if unusedHostSet.Len() == 0 {
			kcp, err := machineutil.GetKCPByMachine(ctx, ctx.Client, ctx.Machine)
			if err != nil {
				return false, err
			}

			// Only when the KCP is in rolling update, the VM is stopped, and all the hosts used by the placement group are available,
			// will the upgrade be allowed with the same number of hosts and CP nodes.
			// In this case first machine created by KCP rolling update can be powered on without being added to the placement group.
			if kcputil.IsKCPRollingUpdateFirstMachine(kcp) &&
				*vm.Status == models.VMStatusSTOPPED &&
				!service.ContainsUnavailableHost(hosts, usedHostSet.UnsortedList(), *service.TowerMemory(ctx.ElfMachine.Spec.MemoryMiB)) &&
				int(*kcp.Spec.Replicas) == usedHostSet.Len() {
				ctx.Logger.Info("The placement group is full and KCP is in rolling update, skip adding VM to the placement group", "placementGroup", *placementGroup.Name, "availableHosts", availableHostSet.UnsortedList(), "usedHosts", usedHostSet.UnsortedList(), "vmRef", ctx.ElfMachine.Status.VMRef, "vmId", *vm.ID)

				return true, nil
			}

			if *vm.Status == models.VMStatusRUNNING || *vm.Status == models.VMStatusSUSPENDED {
				ctx.Logger.V(2).Info(fmt.Sprintf("The placement group is full and VM is in %s status, skip adding VM to the placement group", *vm.Status), "placementGroup", *placementGroup.Name, "availableHosts", availableHostSet.UnsortedList(), "usedHosts", usedHostSet.UnsortedList(), "vmRef", ctx.ElfMachine.Status.VMRef, "vmId", *vm.ID)

				return true, nil
			}

			// KCP is scaling out or being created.
			ctx.Logger.V(1).Info("The placement group is full, wait for enough available hosts", "placementGroup", *placementGroup.Name, "availableHosts", availableHostSet.UnsortedList(), "usedHosts", usedHostSet.UnsortedList(), "vmRef", ctx.ElfMachine.Status.VMRef, "vmId", *vm.ID)

			return false, nil
		}

		// If the virtual machine is not on a host that is not used by the placement group
		// and the virtual machine is not STOPPED, we need to migrate the virtual machine to a host that
		// is not used by the placement group before adding the virtual machine to the placement group.
		// Otherwise, just add the virtual machine to the placement group directly.
		if !unusedHostSet.Has(*vm.Host.ID) && *vm.Status != models.VMStatusSTOPPED {
			return r.migrateVMForPlacementGroup(ctx, vm, placementGroup, unusedHostSet.UnsortedList()[0])
		}
	}

	if !placementGroupVMSet.Has(*vm.ID) {
		placementGroupVMSet.Insert(*vm.ID)
		if err := r.addVMsToPlacementGroup(ctx, placementGroup, placementGroupVMSet.UnsortedList()); err != nil {
			return false, err
		}
	}

	return true, nil
}

// migrateVMForPlacementGroup migrates the virtual machine to the specified target host.
func (r *ElfMachineReconciler) migrateVMForPlacementGroup(ctx *context.MachineContext, vm *models.VM, placementGroup *models.VMPlacementGroup, targetHost string) (bool, error) {
	kcp, err := machineutil.GetKCPByMachine(ctx, ctx.Client, ctx.Machine)
	if err != nil {
		return false, err
	}

	// When *kcp.Spec.Replicas != kcp.Status.UpdatedReplicas, it must be in a KCP rolling update process.
	// When *kcp.Spec.Replicas == kcp.Status.UpdatedReplicas, it could be in one of the following cases:
	// 1) It's not in a KCP rolling update process. So kcp.Spec.Replicas == kcp.Status.Replicas.
	// 2) It's at the end of a KCP rolling update process, and the last KCP replica (i.e the last KCP ElfMachine) is created just now.
	//    There is still an old KCP ElfMachine, so kcp.Spec.Replicas + 1 == kcp.Status.Replicas.

	if *kcp.Spec.Replicas != kcp.Status.UpdatedReplicas {
		ctx.Logger.Info("KCP rolling update in progress, skip migrating VM", "vmRef", ctx.ElfMachine.Status.VMRef, "vmId", *vm.ID)
		return true, nil
	}

	if *kcp.Spec.Replicas != kcp.Status.Replicas {
		// The 1st new CP ElfMachine should wait for other new CP ElfMachines to join the target PlacementGroup.
		cpElfMachines, err := machineutil.GetControlPlaneElfMachinesInCluster(ctx, ctx.Client, ctx.Cluster.Namespace, ctx.Cluster.Name)
		if err != nil {
			return false, err
		}
		var newCPElfMachinesInPG []*infrav1.ElfMachine
		for i := 0; i < len(cpElfMachines); i++ {
			if ctx.ElfMachine.Name != cpElfMachines[i].Name &&
				cpElfMachines[i].Status.PlacementGroupRef == *placementGroup.ID &&
				cpElfMachines[i].CreationTimestamp.After(ctx.ElfMachine.CreationTimestamp.Time) {
				newCPElfMachinesInPG = append(newCPElfMachinesInPG, cpElfMachines[i])
			}
		}

		remainingCP := int(*kcp.Spec.Replicas-1) - len(newCPElfMachinesInPG)
		if remainingCP == 0 {
			// This is the last CP ElfMachine (i.e. the 1st new CP ElfMachine) which has not been added into the target PlacementGroup.
			// Migrate this VM to the target host, then it will be added into the target PlacementGroup.
			return false, r.migrateVM(ctx, vm, placementGroup, targetHost)
		} else {
			ctx.Logger.V(1).Info(fmt.Sprintf("%d new CP joined the target PlacementGroup, waiting for other %d CP to join", len(newCPElfMachinesInPG), remainingCP))
		}

		return true, nil
	} else {
		// KCP is not in rolling update process.
		// Migrate this VM to the target host, then it will be added into the target PlacementGroup.
		return false, r.migrateVM(ctx, vm, placementGroup, targetHost)
	}
}

func (r *ElfMachineReconciler) migrateVM(ctx *context.MachineContext, vm *models.VM, placementGroup *models.VMPlacementGroup, targetHost string) error {
	if ok := acquireTicketForPlacementGroupVMMigration(*placementGroup.Name); !ok {
		ctx.Logger.V(1).Info("The placement group is performing another VM migration, skip migrating VM", "placementGroup", service.GetTowerString(placementGroup.Name), "vmRef", ctx.ElfMachine.Status.VMRef, "vmId", *vm.ID)

		return nil
	}

	withTaskVM, err := ctx.VMService.Migrate(service.GetTowerString(vm.ID), targetHost)
	if err != nil {
		return err
	}

	ctx.ElfMachine.SetTask(*withTaskVM.TaskID)

	ctx.Logger.Info(fmt.Sprintf("Waiting for the VM to be migrated from %s to %s", *vm.Host.ID, targetHost), "vmRef", ctx.ElfMachine.Status.VMRef, "vmId", *vm.ID, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return nil
}

func (r *ElfMachineReconciler) addVMsToPlacementGroup(ctx *context.MachineContext, placementGroup *models.VMPlacementGroup, vmIDs []string) error {
	task, err := ctx.VMService.AddVMsToPlacementGroup(placementGroup, vmIDs)
	if err != nil {
		return err
	}

	taskID := *task.ID
	task, err = ctx.VMService.WaitTask(taskID, config.WaitTaskTimeout, config.WaitTaskInterval)
	if err != nil {
		return errors.Wrapf(err, "failed to wait for placement group updation task done timed out in %s: placementName %s, taskID %s", config.WaitTaskTimeout, *placementGroup.Name, taskID)
	}

	if *task.Status == models.TaskStatusFAILED {
		return errors.Errorf("failed to update placement group %s in task %s", *placementGroup.Name, taskID)
	}

	ctx.ElfMachine.Status.PlacementGroupRef = *placementGroup.ID

	ctx.Logger.Info("Updating placement group succeeded", "taskID", taskID, "placementGroup", *placementGroup.Name, "vmIDs", vmIDs)

	return nil
}

// deletePlacementGroup deletes the placement group when the MachineDeployment is deleted
// and the cluster is not deleted.
// If the cluster is deleted, all placement groups are deleted by the ElfCluster controller.
func (r *ElfMachineReconciler) deletePlacementGroup(ctx *context.MachineContext) (bool, error) {
	if !ctx.Cluster.DeletionTimestamp.IsZero() || machineutil.IsControlPlaneMachine(ctx.Machine) {
		return true, nil
	}

	md, err := machineutil.GetMDByMachine(ctx, ctx.Client, ctx.Machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}

		return false, err
	}

	// SKS will set replicas to 0 before deleting the MachineDeployment,
	// and then delete it after all the machines are deleted.
	// In this scenario, CAPE needs to delete the placement group first
	// when md.Spec.Replicas is 0.
	if md.DeletionTimestamp.IsZero() && *md.Spec.Replicas > 0 {
		return true, nil
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine, ctx.Cluster)
	if err != nil {
		return false, err
	}

	// Only delete the placement groups created by CAPE.
	if !strings.HasPrefix(placementGroupName, towerresources.GetVMPlacementGroupNamePrefix(ctx.Cluster)) {
		return true, nil
	}

	placementGroup, err := ctx.VMService.GetVMPlacementGroup(placementGroupName)
	if err != nil {
		if service.IsVMPlacementGroupNotFound(err) {
			return true, nil
		}

		return false, err
	}

	if ok := acquireTicketForPlacementGroupOperation(*placementGroup.Name); ok {
		defer releaseTicketForPlacementGroupOperation(*placementGroup.Name)
	} else {
		return false, nil
	}

	if err := ctx.VMService.DeleteVMPlacementGroupsByName(*placementGroup.Name); err != nil {
		return false, err
	} else {
		ctx.Logger.Info(fmt.Sprintf("Placement group %s deleted", *placementGroup.Name))
	}

	return true, nil
}
