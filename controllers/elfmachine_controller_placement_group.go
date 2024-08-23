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
	goctx "context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	annotationsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/annotations"
	kcputil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/kcp"
	machineutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/machine"
	typesutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/types"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/version"
)

// reconcilePlacementGroup makes sure that the placement group exist.
func (r *ElfMachineReconciler) reconcilePlacementGroup(ctx goctx.Context, machineCtx *context.MachineContext) (reconcile.Result, error) {
	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, r.Client, machineCtx.Machine, machineCtx.Cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	if placementGroup, err := r.getPlacementGroup(ctx, machineCtx, placementGroupName); err != nil {
		if !service.IsVMPlacementGroupNotFound(err) {
			return reconcile.Result{}, err
		}

		if ok := acquireTicketForPlacementGroupOperation(placementGroupName); ok {
			defer releaseTicketForPlacementGroupOperation(placementGroupName)
		} else {
			return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
		}

		if placementGroup, err := r.createPlacementGroup(ctx, machineCtx, placementGroupName); err != nil {
			return reconcile.Result{}, err
		} else if placementGroup == nil {
			return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, err
		}
	} else if placementGroup == nil {
		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	return reconcile.Result{}, nil
}

func (r *ElfMachineReconciler) createPlacementGroup(ctx goctx.Context, machineCtx *context.MachineContext, placementGroupName string) (*models.VMPlacementGroup, error) {
	log := ctrl.LoggerFrom(ctx)

	// TODO: This will be removed when Tower fixes issue with placement group data syncing.
	if ok := canCreatePlacementGroup(placementGroupName); !ok {
		log.V(2).Info("Tower has duplicate placement group, skip creating placement group " + placementGroupName)

		return nil, nil
	}

	towerCluster, err := machineCtx.VMService.GetCluster(machineCtx.ElfCluster.Spec.Cluster)
	if err != nil {
		return nil, err
	}

	placementGroupPolicy := towerresources.GetVMPlacementGroupPolicy(machineCtx.Machine)
	withTaskVMPlacementGroup, err := machineCtx.VMService.CreateVMPlacementGroup(placementGroupName, *towerCluster.ID, placementGroupPolicy)
	if err != nil {
		return nil, err
	}

	task, err := machineCtx.VMService.WaitTask(ctx, *withTaskVMPlacementGroup.TaskID, config.WaitTaskTimeoutForPlacementGroupOperation, config.WaitTaskInterval)
	if err != nil {
		// The default timeout for Tower to create a placement group is one minute.
		// When current task times out, duplicate placement groups may or may not appear.
		// For simplicity, both cases are treated as duplicate placement groups.
		setPlacementGroupDuplicate(placementGroupName)

		return nil, errors.Wrapf(err, "failed to wait for placement group creating task to complete in %s: pgName %s, taskID %s", config.WaitTaskTimeoutForPlacementGroupOperation, placementGroupName, *withTaskVMPlacementGroup.TaskID)
	}

	if *task.Status == models.TaskStatusFAILED {
		if service.IsVMPlacementGroupDuplicate(service.GetTowerString(task.ErrorMessage)) {
			setPlacementGroupDuplicate(placementGroupName)

			log.Info(fmt.Sprintf("Duplicate placement group detected, will try again in %s", placementGroupSilenceTime), "placementGroup", placementGroupName)
		}

		return nil, errors.Errorf("failed to create placement group %s in task %s", placementGroupName, *task.ID)
	}

	log.Info("Creating placement group succeeded", "taskID", *task.ID, "placementGroup", placementGroupName)

	placementGroup, err := r.getPlacementGroup(ctx, machineCtx, placementGroupName)
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
func (r *ElfMachineReconciler) preCheckPlacementGroup(ctx goctx.Context, machineCtx *context.MachineContext) (rethost *string, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	defer func() {
		if rethost == nil {
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForAvailableHostRequiredByPlacementGroupReason, clusterv1.ConditionSeverityInfo, "")
		}
	}()

	if !machineutil.IsControlPlaneMachine(machineCtx.Machine) {
		return ptr.To(""), nil
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, r.Client, machineCtx.Machine, machineCtx.Cluster)
	if err != nil {
		return nil, err
	}

	placementGroup, err := r.getPlacementGroup(ctx, machineCtx, placementGroupName)
	if err != nil || placementGroup == nil {
		return nil, err
	}

	usedHostSetByPG, err := r.getHostsInPlacementGroup(machineCtx, placementGroup)
	if err != nil {
		return nil, err
	}

	hosts, err := machineCtx.VMService.GetHostsByCluster(machineCtx.ElfCluster.Spec.Cluster)
	if err != nil {
		return nil, err
	}

	usedHostsByPG := hosts.Find(usedHostSetByPG)
	availableHosts := r.getAvailableHostsForVM(machineCtx, hosts, usedHostsByPG, nil)
	if !availableHosts.IsEmpty() {
		log.V(1).Info("The placement group still has capacity", "placementGroup", *placementGroup.Name, "availableHosts", availableHosts.String())

		return ptr.To(""), nil
	}

	kcp, err := machineutil.GetKCPByMachine(ctx, r.Client, machineCtx.Machine)
	if err != nil {
		return nil, err
	}

	// When KCP is not in rolling update and not in scaling down, just return since the placement group is full.
	if !kcputil.IsKCPInRollingUpdate(kcp) && !kcputil.IsKCPInScalingDown(kcp) {
		log.V(1).Info("KCP is not in rolling update and not in scaling down, the placement group is full, so wait for enough available hosts", "placementGroup", *placementGroup.Name, "availableHosts", availableHosts.String(), "usedHostsByPG", usedHostsByPG.String())

		return nil, nil
	}

	// KCP is in scaling down.
	//
	// KCP scaling up will fail if the placement group is full, then the user will trigger KCP scaling down.
	// The Machine being created cannot pass the Preflight checks(https://github.com/kubernetes-sigs/cluster-api/blob/main/docs/proposals/20191017-kubeadm-based-control-plane.md#preflight-checks),
	// so the Machine will not be deleted.
	// We can add delete machine annotation on the Machine and KCP will delete it.
	if kcputil.IsKCPInScalingDown(kcp) {
		if annotationsutil.HasAnnotation(machineCtx.Machine, clusterv1.DeleteMachineAnnotation) {
			return nil, nil
		}

		newMachine := machineCtx.Machine.DeepCopy()
		patchHelper, err := patch.NewHelper(machineCtx.Machine, r.Client)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to init patch helper")
		}

		log.Info("Add the delete machine annotation on KCP Machine in order to delete it, because KCP is being scaled down after a failed scaling up", "placementGroup", *placementGroup.Name, "availableHosts", availableHosts.String())

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
		if err := patchHelper.Patch(ctx, newMachine); err != nil {
			return nil, errors.Wrapf(err, "failed to patch Machine %s to add delete machine annotation %s.", klog.KObj(newMachine), clusterv1.DeleteMachineAnnotation)
		}

		return nil, nil
	}

	// Now since the PlacementGroup is full and KCP is in rolling update,
	// we will place the target CP VM on a selected host without joining the PlacementGroup and power it on.
	// After other CP VMs are rolled out successfully, these VMs must already join the PlacementGroup
	// since there is always an empty seat in the PlacementGroup, we will add the target CP VM into the PlacementGroup.

	// If KCP.Spec.Replicas is greater than the host count,
	// do not allow creating more KCP VM because there is no more host to place the new VM.
	if int(*kcp.Spec.Replicas) > usedHostsByPG.Len() {
		log.V(1).Info("KCP is in rolling update, the placement group is full and no more host for placing more KCP VM, so wait for enough available hosts", "placementGroup", *placementGroup.Name, "usedHostsByPG", usedHostsByPG.String(), "usedHostsCount", usedHostsByPG.Len(), "kcpReplicas", *kcp.Spec.Replicas)

		return nil, nil
	}

	unusableHosts := usedHostsByPG.FilterUnavailableHostsOrWithoutEnoughMemory(*service.TowerMemory(machineCtx.ElfMachine.Spec.MemoryMiB))
	if !unusableHosts.IsEmpty() {
		log.V(1).Info("KCP is in rolling update, the placement group is full and has unusable hosts, so wait for enough available hosts", "placementGroup", *placementGroup.Name, "unusableHosts", unusableHosts.String(), "usedHostsByPG", usedHostsByPG.String())

		return nil, nil
	}

	hostID, err := r.getVMHostForRollingUpdate(ctx, machineCtx, placementGroup, hosts)
	if err != nil || hostID == "" {
		return nil, err
	}

	return ptr.To(hostID), err
}

// getVMHostForRollingUpdate returns the target host server id for a virtual machine during rolling update.
// During KCP rolling update, machines will be deleted in the order of creation.
// Find the latest created machine in the placement group,
// and set the host where the machine is located to the machine created by KCP rolling update.
// This prevents migration of virtual machine during KCP rolling update when using a placement group.
func (r *ElfMachineReconciler) getVMHostForRollingUpdate(ctx goctx.Context, machineCtx *context.MachineContext, placementGroup *models.VMPlacementGroup, hosts service.Hosts) (string, error) {
	log := ctrl.LoggerFrom(ctx)

	elfMachines, err := machineutil.GetControlPlaneElfMachinesInCluster(ctx, r.Client, machineCtx.Cluster.Namespace, machineCtx.Cluster.Name)
	if err != nil {
		return "", err
	}

	elfMachineMap := make(map[string]*infrav1.ElfMachine)
	for i := range len(elfMachines) {
		if typesutil.IsUUID(elfMachines[i].Status.VMRef) {
			elfMachineMap[elfMachines[i].Name] = elfMachines[i]
		}
	}

	placementGroupMachines := make([]*clusterv1.Machine, 0, len(placementGroup.Vms))
	vmMap := make(map[string]string)
	for i := range len(placementGroup.Vms) {
		if elfMachine, ok := elfMachineMap[*placementGroup.Vms[i].Name]; ok {
			machine, err := capiutil.GetOwnerMachine(ctx, r.Client, elfMachine.ObjectMeta)
			if err != nil {
				return "", err
			}

			placementGroupMachines = append(placementGroupMachines, machine)
			vmMap[machine.Name] = *(placementGroup.Vms[i].ID)
		}
	}

	machines := collections.FromMachines(placementGroupMachines...)
	newestMachine := machines.Newest()
	if newestMachine == nil {
		log.Info("Newest machine not found, skip selecting host for VM", "vmRef", machineCtx.ElfMachine.Status.VMRef)
		return "", nil
	}

	vm, err := machineCtx.VMService.Get(vmMap[newestMachine.Name])
	if err != nil {
		return "", err
	}

	host := hosts.Get(*vm.Host.ID)
	if host == nil {
		log.Info("Host not found, skip selecting host for VM", "host", formatNestedHost(vm.Host), "vmRef", machineCtx.ElfMachine.Status.VMRef)
		return "", err
	}

	ok, message := service.IsAvailableHost(host, *service.TowerMemory(machineCtx.ElfMachine.Spec.MemoryMiB))
	if ok {
		log.Info("Select a host to power on the VM since the placement group is full", "host", formatNestedHost(vm.Host), "vmRef", machineCtx.ElfMachine.Status.VMRef)
		return *vm.Host.ID, nil
	}

	log.Info(fmt.Sprintf("Host is unavailable: %s, skip selecting host for VM", message), "host", formatNestedHost(vm.Host), "vmRef", machineCtx.ElfMachine.Status.VMRef)
	return "", err
}

// getHostsInPlacementGroup returns the hosts where all virtual machines of placement group located.
func (r *ElfMachineReconciler) getHostsInPlacementGroup(machineCtx *context.MachineContext, placementGroup *models.VMPlacementGroup) (sets.Set[string], error) {
	placementGroupVMSet := service.GetVMsInPlacementGroup(placementGroup)
	vms, err := machineCtx.VMService.FindByIDs(placementGroupVMSet.UnsortedList())
	if err != nil {
		return nil, err
	}

	hostSet := sets.Set[string]{}
	for i := range len(vms) {
		hostSet.Insert(*vms[i].Host.ID)
	}

	return hostSet, nil
}

// getAvailableHostsForVM returns the available hosts for running the specified VM.
//
// The 'Available' means that the specified VM can run on these hosts.
// It returns hosts that are not in faulted state, not in the given 'usedHostsByPG',
// and have sufficient memory for running this VM.
func (r *ElfMachineReconciler) getAvailableHostsForVM(machineCtx *context.MachineContext, hosts service.Hosts, usedHostsByPG service.Hosts, vm *models.VM) service.Hosts {
	availableHosts := hosts.FilterAvailableHostsWithEnoughMemory(*service.TowerMemory(machineCtx.ElfMachine.Spec.MemoryMiB)).Difference(usedHostsByPG)

	// If the VM is running, and the host where the VM is located
	// is not used by the placement group, then it is not necessary to
	// check the memory is sufficient to determine whether the host is available.
	if vm != nil &&
		(*vm.Status == models.VMStatusRUNNING || *vm.Status == models.VMStatusSUSPENDED) &&
		!availableHosts.Contains(*vm.Host.ID) {
		host := hosts.Get(*vm.Host.ID)
		available, _ := service.IsAvailableHost(host, 0)
		if available && !usedHostsByPG.Contains(*host.ID) {
			availableHosts.Insert(host)
		}
	}

	return availableHosts
}

// getPlacementGroup returns the specified placement group.
// getPlacementGroup will get the placement group from the cache first.
// If the placement group does not exist in the cache, it will be fetched from Tower and saved to the cache(expiration time is 10s).
func (r *ElfMachineReconciler) getPlacementGroup(ctx goctx.Context, machineCtx *context.MachineContext, placementGroupName string) (*models.VMPlacementGroup, error) {
	log := ctrl.LoggerFrom(ctx)

	if placementGroup := getPGFromCache(placementGroupName); placementGroup != nil {
		return placementGroup, nil
	}

	placementGroup, err := machineCtx.VMService.GetVMPlacementGroup(placementGroupName)
	if err != nil {
		return nil, err
	}

	// Placement group is performing an operation
	if !typesutil.IsUUID(*placementGroup.LocalID) || placementGroup.EntityAsyncStatus != nil {
		log.Info("Waiting for placement group task done", "placementGroup", *placementGroup.Name)

		return nil, nil
	}

	// Save placement group cache.
	setPGCache(placementGroup)

	return placementGroup, nil
}

// joinPlacementGroup puts the virtual machine into the placement group.
//
// The return value:
//  1. true means that the virtual machine does not need to join the placement group.
//     For example, the virtual machine has joined the placement group.
//  2. false and error is nil means the virtual machine has not joined the placement group.
//     For example, the placement group is full or the virtual machine is being migrated.
//
//nolint:gocyclo
func (r *ElfMachineReconciler) joinPlacementGroup(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) (ret bool, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	if !version.IsCompatibleWithPlacementGroup(machineCtx.ElfMachine) {
		log.V(1).Info(fmt.Sprintf("The capeVersion of ElfMachine is lower than %s, skip adding VM to the placement group", version.CAPEVersion1_2_0), "capeVersion", version.GetCAPEVersion(machineCtx.ElfMachine))

		return true, nil
	}

	defer func() {
		if reterr != nil {
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.JoiningPlacementGroupFailedReason, clusterv1.ConditionSeverityWarning, reterr.Error())
		} else if !ret {
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.JoiningPlacementGroupReason, clusterv1.ConditionSeverityInfo, "")
		}
	}()

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, r.Client, machineCtx.Machine, machineCtx.Cluster)
	if err != nil {
		return false, err
	}

	// Locking ensures that only one coroutine operates on the placement group at the same time,
	// and concurrent operations will cause data inconsistency in the placement group.
	if ok := acquireTicketForPlacementGroupOperation(placementGroupName); ok {
		defer releaseTicketForPlacementGroupOperation(placementGroupName)
	} else {
		return false, nil
	}

	placementGroup, err := r.getPlacementGroup(ctx, machineCtx, placementGroupName)
	if err != nil || placementGroup == nil {
		return false, err
	}

	placementGroupVMSet := service.GetVMsInPlacementGroup(placementGroup)
	if placementGroupVMSet.Has(*vm.ID) {
		// Ensure PlacementGroupRef is set or up to date.
		machineCtx.ElfMachine.Status.PlacementGroupRef = *placementGroup.ID
		return true, nil
	}

	if machineutil.IsControlPlaneMachine(machineCtx.Machine) {
		hosts, err := machineCtx.VMService.GetHostsByCluster(machineCtx.ElfCluster.Spec.Cluster)
		if err != nil {
			return false, err
		}

		usedHostSetByPG, err := r.getHostsInPlacementGroup(machineCtx, placementGroup)
		if err != nil {
			return false, err
		}

		usedHostsByPG := hosts.Find(usedHostSetByPG)

		availableHosts := r.getAvailableHostsForVM(machineCtx, hosts, usedHostsByPG, vm)
		if availableHosts.IsEmpty() {
			kcp, err := machineutil.GetKCPByMachine(ctx, r.Client, machineCtx.Machine)
			if err != nil {
				return false, err
			}

			// Proceed only when the KCP is in rolling update, the VM is stopped, and all the hosts used by the placement group are available.
			// In this case the machine created by KCP rolling update can be powered on without being added to the placement group,
			// so return true and nil to let reconcileVMStatus() power it on.
			if kcputil.IsKCPInRollingUpdate(kcp) && *vm.Status == models.VMStatusSTOPPED {
				unusablehosts := usedHostsByPG.FilterUnavailableHostsOrWithoutEnoughMemory(*service.TowerMemory(machineCtx.ElfMachine.Spec.MemoryMiB))
				if unusablehosts.IsEmpty() {
					log.Info("KCP is in rolling update, the placement group is full and has no unusable hosts, so skip adding VM to the placement group and power it on", "placementGroup", *placementGroup.Name, "availableHosts", availableHosts.String(), "usedHostsByPG", usedHostsByPG.String(), "vmRef", machineCtx.ElfMachine.Status.VMRef, "vmId", *vm.ID)
					return true, nil
				}

				log.Info("KCP is in rolling update, the placement group is full and has unusable hosts, so wait for enough available hosts", "placementGroup", *placementGroup.Name, "unusablehosts", unusablehosts.String(), "usedHostsByPG", usedHostsByPG.String(), "vmRef", machineCtx.ElfMachine.Status.VMRef, "vmId", *vm.ID)

				return false, nil
			}

			if *vm.Status != models.VMStatusSTOPPED {
				log.V(1).Info(fmt.Sprintf("The placement group is full and VM is in %s status, skip adding VM to the placement group", *vm.Status), "placementGroup", *placementGroup.Name, "availableHosts", availableHosts.String(), "usedHostsByPG", usedHostsByPG.String(), "vmRef", machineCtx.ElfMachine.Status.VMRef, "vmId", *vm.ID)

				return true, nil
			}

			// KCP is scaling out or being created.
			log.V(1).Info("KCP is in scaling up or being created, the placement group is full, so wait for enough available hosts", "placementGroup", *placementGroup.Name, "availableHosts", availableHosts.String(), "usedHostsByPG", usedHostsByPG.String(), "vmRef", machineCtx.ElfMachine.Status.VMRef, "vmId", *vm.ID)

			return false, nil
		}

		// If the virtual machine is not on a host that is not used by the placement group
		// and the virtual machine is not STOPPED, we need to migrate the virtual machine to a host that
		// is not used by the placement group before adding the virtual machine to the placement group.
		// Otherwise, just add the virtual machine to the placement group directly.
		log.V(1).Info("The availableHosts for migrating the VM", "hosts", availableHosts.String(), "vmHost", formatNestedHost(vm.Host))
		if !availableHosts.Contains(*vm.Host.ID) && *vm.Status != models.VMStatusSTOPPED {
			log.V(1).Info("Try to migrate the virtual machine to the specified target host if needed")

			kcp, err := machineutil.GetKCPByMachine(ctx, r.Client, machineCtx.Machine)
			if err != nil {
				return false, err
			}

			if kcputil.IsKCPInRollingUpdate(kcp) {
				log.Info("KCP rolling update in progress, skip migrating VM", "vmRef", machineCtx.ElfMachine.Status.VMRef, "vmId", *vm.ID)
				return true, nil
			}

			// The powered on CP ElfMachine which is not in the PlacementGroup should wait for other new CP ElfMachines to join the target PlacementGroup.
			// The code below double checks the recommended target host for migration is valid.
			cpElfMachines, err := machineutil.GetControlPlaneElfMachinesInCluster(ctx, r.Client, machineCtx.Cluster.Namespace, machineCtx.Cluster.Name)
			if err != nil {
				return false, err
			}

			usedHostsByPG := sets.Set[string]{}
			cpElfMachineNames := make([]string, 0, len(cpElfMachines))
			for i := range len(cpElfMachines) {
				cpElfMachineNames = append(cpElfMachineNames, cpElfMachines[i].Name)
				if machineCtx.ElfMachine.Name != cpElfMachines[i].Name &&
					cpElfMachines[i].Status.PlacementGroupRef == *placementGroup.ID {
					usedHostsByPG.Insert(cpElfMachines[i].Status.HostServerRef)
				}
			}

			// During KCP rolling update, when the last new CP is just created,
			// it may happen that kcp.Spec.Replicas == kcp.Status.Replicas == kcp.Status.UpdatedReplicas
			// and kcp.Status.UnavailableReplicas == 0.
			// So we need to check if the number of CP ElfMachine is equal to kcp.Spec.Replicas.
			if len(cpElfMachines) != int(*kcp.Spec.Replicas) {
				log.V(1).Info("The number of CP ElfMachine does not match the expected", "kcp", formatKCP(kcp), "cpElfMachines", cpElfMachineNames)
				return true, nil
			}

			targetHost := availableHosts.UnsortedList()[0]
			usedHostsCount := usedHostsByPG.Len()
			log.V(1).Info("The hosts used by the PlacementGroup", "usedHosts", usedHostsByPG, "count", usedHostsCount, "targetHost", formatHost(targetHost), "kcp", formatKCP(kcp), "cpElfMachines", cpElfMachineNames)
			if usedHostsCount < int(*kcp.Spec.Replicas-1) {
				log.V(1).Info("Not all other CPs joined the PlacementGroup, skip migrating VM")
				return true, nil
			}

			if usedHostsByPG.Has(*targetHost.ID) {
				log.V(1).Info("The recommended target host for VM migration is used by the PlacementGroup, skip migrating VM")
				return true, nil
			}

			// KCP is not in rolling update process.
			// This is the last CP ElfMachine (i.e. the 1st new CP ElfMachine) which has not been added into the target PlacementGroup.
			// Migrate this VM to the target host, then it will be added into the target PlacementGroup.

			log.V(1).Info("Start migrating VM since KCP is not in rolling update process", "targetHost", formatHost(targetHost))

			return r.migrateVM(ctx, machineCtx, vm, *targetHost.ID)
		}
	}

	if !placementGroupVMSet.Has(*vm.ID) {
		placementGroupVMSet.Insert(*vm.ID)
		if err := r.addVMsToPlacementGroup(ctx, machineCtx, placementGroup, placementGroupVMSet.UnsortedList()); err != nil {
			return false, err
		}
	}

	return true, nil
}

// migrateVM migrates the virtual machine to the specified target host.
//
// The return value:
// 1. true means that the virtual machine does not need to be migrated.
// 2. false and error is nil means the virtual machine is being migrated.
func (r *ElfMachineReconciler) migrateVM(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM, targetHost string) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	if *vm.Host.ID == targetHost {
		log.V(1).Info(fmt.Sprintf("The VM is already on the recommended target host %s, skip migrating VM", targetHost))
		return true, nil
	}

	withTaskVM, err := machineCtx.VMService.Migrate(service.GetTowerString(vm.ID), targetHost)
	if err != nil {
		return false, err
	}

	machineCtx.ElfMachine.SetTask(*withTaskVM.TaskID)

	log.Info(fmt.Sprintf("Waiting for the VM to be migrated from %s to %s", formatNestedHost(vm.Host), targetHost), "vmRef", machineCtx.ElfMachine.Status.VMRef, "vmId", *vm.ID, "taskRef", machineCtx.ElfMachine.Status.TaskRef)

	return false, nil
}

func (r *ElfMachineReconciler) addVMsToPlacementGroup(ctx goctx.Context, machineCtx *context.MachineContext, placementGroup *models.VMPlacementGroup, vmIDs []string) error {
	log := ctrl.LoggerFrom(ctx)
	task, err := machineCtx.VMService.AddVMsToPlacementGroup(placementGroup, vmIDs)
	if err != nil {
		return err
	}

	// Delete placement group cache.
	delPGCaches([]string{*placementGroup.Name})

	taskID := *task.ID
	task, err = machineCtx.VMService.WaitTask(ctx, taskID, config.WaitTaskTimeoutForPlacementGroupOperation, config.WaitTaskInterval)
	if err != nil {
		return errors.Wrapf(err, "failed to wait for placement group updating task to complete in %s: pgName %s, taskID %s", config.WaitTaskTimeoutForPlacementGroupOperation, *placementGroup.Name, taskID)
	}

	if *task.Status == models.TaskStatusFAILED {
		return errors.Errorf("failed to update placement group %s in task %s", *placementGroup.Name, taskID)
	}

	machineCtx.ElfMachine.Status.PlacementGroupRef = *placementGroup.ID

	log.Info("Updating placement group succeeded", "taskID", taskID, "placementGroup", *placementGroup.Name, "vmIDs", vmIDs)

	return nil
}

// deletePlacementGroup deletes the placement group when the MachineDeployment is deleted
// and the cluster is not deleted.
// If the cluster is deleted, all placement groups are deleted by the ElfCluster controller.
func (r *ElfMachineReconciler) deletePlacementGroup(ctx goctx.Context, machineCtx *context.MachineContext) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	if !machineCtx.Cluster.DeletionTimestamp.IsZero() || machineutil.IsControlPlaneMachine(machineCtx.Machine) {
		return true, nil
	}

	md, err := machineutil.GetMDByMachine(ctx, r.Client, machineCtx.Machine)
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

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, r.Client, machineCtx.Machine, machineCtx.Cluster)
	if err != nil {
		return false, err
	}

	// Only delete the placement groups created by CAPE.
	if !strings.HasPrefix(placementGroupName, towerresources.GetVMPlacementGroupNamePrefix(machineCtx.Cluster)) {
		return true, nil
	}

	placementGroup, err := machineCtx.VMService.GetVMPlacementGroup(placementGroupName)
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

	if ok, err := machineCtx.VMService.DeleteVMPlacementGroupByID(ctx, *placementGroup.ID); err != nil {
		return false, err
	} else if !ok {
		log.Info(fmt.Sprintf("Waiting for the placement group %s to be deleted", *placementGroup.Name))

		return false, nil
	} else {
		log.Info(fmt.Sprintf("Placement group %s deleted", *placementGroup.Name))

		// Delete placement group cache.
		delPGCaches([]string{*placementGroup.Name})
	}

	return true, nil
}

// formatNestedHost returns the basic information of the NestedHost (ID, name).
func formatNestedHost(host *models.NestedHost) string {
	if host == nil {
		return "{}"
	}

	return fmt.Sprintf("{id: %s,name: %s}", service.GetTowerString(host.ID), service.GetTowerString(host.Name))
}

// formatHost returns the basic information of the Host (ID, name).
func formatHost(host *models.Host) string {
	if host == nil {
		return "{}"
	}

	return fmt.Sprintf("{id: %s,name: %s}", service.GetTowerString(host.ID), service.GetTowerString(host.Name))
}

// formatKCP returns the basic information of the KCP (name, namespace, replicas).
func formatKCP(kcp *controlplanev1.KubeadmControlPlane) string {
	if kcp == nil {
		return "{}"
	}

	return fmt.Sprintf("{name:%s,namespace:%s,spec:{replicas:%d},status:{replicas:%d,readyReplicas:%d,updatedReplicas:%d,unavailableReplicas:%d}}", kcp.Name, kcp.Namespace, *kcp.Spec.Replicas, kcp.Status.Replicas, kcp.Status.ReadyReplicas, kcp.Status.UpdatedReplicas, kcp.Status.UnavailableReplicas)
}
