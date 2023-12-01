/*
Copyright 2021.

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
	"encoding/json"
	stderrors "errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capierrors "sigs.k8s.io/cluster-api/errors"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	capeerrors "github.com/smartxworks/cluster-api-provider-elf/pkg/errors"
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/util"
	labelsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/labels"
	machineutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/machine"
	patchutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/patch"
)

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/finalizers,verbs=update
//+kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments;machinedeployments/status,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;patch
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch

// ElfMachineReconciler reconciles an ElfMachine object.
type ElfMachineReconciler struct {
	*context.ControllerContext
	NewVMService service.NewVMServiceFunc
}

// AddMachineControllerToManager adds the machine controller to the provided
// manager.
func AddMachineControllerToManager(ctx *context.ControllerManagerContext, mgr ctrlmgr.Manager, options controller.Options) error {
	var (
		controlledType     = &infrav1.ElfMachine{}
		controlledTypeName = reflect.TypeOf(controlledType).Elem().Name()
		controlledTypeGVK  = infrav1.GroupVersion.WithKind(controlledTypeName)

		controllerNameShort = fmt.Sprintf("%s-controller", strings.ToLower(controlledTypeName))
	)

	// Build the controller context.
	controllerContext := &context.ControllerContext{
		ControllerManagerContext: ctx,
		Name:                     controllerNameShort,
		Logger:                   ctx.Logger.WithName(controllerNameShort),
	}

	reconciler := &ElfMachineReconciler{
		ControllerContext: controllerContext,
		NewVMService:      service.NewVMService,
	}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(controlledType).
		// Watch the CAPI resource that owns this infrastructure resource.
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(capiutil.MachineToInfrastructureMapFunc(controlledTypeGVK)),
		).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(ctrl.LoggerFrom(ctx), ctx.WatchFilterValue)).
		Complete(reconciler)
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r *ElfMachineReconciler) Reconcile(ctx goctx.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	// Get the ElfMachine resource for this request.
	var elfMachine infrav1.ElfMachine
	if err := r.Client.Get(r, req.NamespacedName, &elfMachine); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("ElfMachine not found, won't reconcile", "key", req.NamespacedName)

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	// Fetch the CAPI Machine.
	machine, err := capiutil.GetOwnerMachine(r, r.Client, elfMachine.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if machine == nil {
		r.Logger.Info("Waiting for Machine Controller to set OwnerRef on ElfMachine",
			"namespace", elfMachine.Namespace, "elfMachine", elfMachine.Name)

		return reconcile.Result{}, nil
	}

	// Fetch the CAPI Cluster.
	cluster, err := capiutil.GetClusterFromMetadata(r, r.Client, machine.ObjectMeta)
	if err != nil {
		r.Logger.Info("Machine is missing cluster label or cluster does not exist",
			"namespace", machine.Namespace, "machine", machine.Name)

		return reconcile.Result{}, nil
	}
	if annotations.IsPaused(cluster, &elfMachine) {
		r.Logger.V(4).Info("ElfMachine linked to a cluster that is paused",
			"namespace", elfMachine.Namespace, "elfMachine", elfMachine.Name)

		return reconcile.Result{}, nil
	}

	// Fetch the ElfCluster
	var elfCluster infrav1.ElfCluster
	elfClusterName := client.ObjectKey{
		Namespace: elfMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(r, elfClusterName, &elfCluster); err != nil {
		r.Logger.Info("ElfMachine Waiting for ElfCluster",
			"namespace", elfMachine.Namespace, "elfMachine", elfMachine.Name)

		return reconcile.Result{}, nil
	}

	// Create the patch helper.
	patchHelper, err := patch.NewHelper(&elfMachine, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(
			err,
			"failed to init patch helper for %s %s/%s",
			elfMachine.GroupVersionKind(),
			elfMachine.Namespace,
			elfMachine.Name)
	}

	logger := r.Logger.WithValues("namespace", elfMachine.Namespace,
		"elfCluster", elfCluster.Name, "elfMachine", elfMachine.Name, "machine", machine.Name)

	// Create the machine context for this request.
	machineContext := &context.MachineContext{
		ControllerContext: r.ControllerContext,
		Cluster:           cluster,
		ElfCluster:        &elfCluster,
		Machine:           machine,
		ElfMachine:        &elfMachine,
		Logger:            logger,
		PatchHelper:       patchHelper,
	}

	// If ElfMachine is being deleting and ElfCLuster ForceDeleteCluster flag is set, skip creating the VMService object,
	// because Tower server may be out of service. So we can force delete ElfCluster.
	if elfMachine.ObjectMeta.DeletionTimestamp.IsZero() || !elfCluster.HasForceDeleteCluster() {
		vmService, err := r.NewVMService(r.Context, elfCluster.GetTower(), logger)
		if err != nil {
			conditions.MarkFalse(&elfMachine, infrav1.TowerAvailableCondition, infrav1.TowerUnreachableReason, clusterv1.ConditionSeverityError, err.Error())

			return reconcile.Result{}, err
		}
		conditions.MarkTrue(&elfMachine, infrav1.TowerAvailableCondition)

		machineContext.VMService = vmService
	}

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
		// always update the readyCondition.
		conditions.SetSummary(machineContext.ElfMachine,
			conditions.WithConditions(
				infrav1.VMProvisionedCondition,
				infrav1.TowerAvailableCondition,
			),
		)

		// Patch the ElfMachine resource.
		if err := machineContext.Patch(); err != nil {
			if reterr == nil {
				reterr = err
			}

			machineContext.Logger.Error(err, "patch failed", "elfMachine", machineContext.String())
		}
	}()

	// Handle deleted machines
	if !elfMachine.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(machineContext)
	}

	// Handle non-deleted machines
	return r.reconcileNormal(machineContext)
}

func (r *ElfMachineReconciler) reconcileDeleteVM(ctx *context.MachineContext) error {
	vm, err := ctx.VMService.Get(ctx.ElfMachine.Status.VMRef)
	if err != nil {
		if service.IsVMNotFound(err) {
			ctx.Logger.Info("VM already deleted")

			ctx.ElfMachine.SetVM("")
		}

		return err
	}

	if ok, err := r.reconcileVMTask(ctx, vm); err != nil {
		return err
	} else if !ok {
		return nil
	}

	// Shut down the VM
	if *vm.Status == models.VMStatusRUNNING {
		var task *models.Task
		var err error
		// The vGPU license release logic requires the VM to be shutdown gracefully, so if ElfMachine is configured with vGPU,
		// we should perform a graceful shutdown to ensure that the vGPU license can be released.
		// Therefore, if the ElfMachine is configured with vGPU or ElfCluster.Spec.VMGracefulShutdown is true, the virtual machine will be shutdown normally.
		// But if the VM shutdown timed out, simply power off the VM.
		if service.IsShutDownTimeout(conditions.GetMessage(ctx.ElfMachine, infrav1.VMProvisionedCondition)) ||
			!(ctx.ElfMachine.RequiresVGPUDevices() || ctx.ElfCluster.Spec.VMGracefulShutdown) {
			task, err = ctx.VMService.PowerOff(ctx.ElfMachine.Status.VMRef)
		} else {
			task, err = ctx.VMService.ShutDown(ctx.ElfMachine.Status.VMRef)
		}

		if err != nil {
			return err
		}

		ctx.ElfMachine.SetTask(*task.ID)

		ctx.Logger.Info("Waiting for VM shut down",
			"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

		return nil
	}

	// Before destroying VM, attempt to delete kubernetes node.
	err = r.deleteNode(ctx, ctx.ElfMachine.Name)
	if err != nil {
		return err
	}

	ctx.Logger.Info("Destroying VM",
		"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	// Delete the VM
	task, err := ctx.VMService.Delete(ctx.ElfMachine.Status.VMRef)
	if err != nil {
		return err
	} else {
		ctx.ElfMachine.SetTask(*task.ID)
	}

	ctx.Logger.Info("Waiting for VM to be deleted",
		"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return nil
}

func (r *ElfMachineReconciler) reconcileDelete(ctx *context.MachineContext) (reconcile.Result, error) {
	ctx.Logger.Info("Reconciling ElfMachine delete")

	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, clusterv1.DeletingReason, clusterv1.ConditionSeverityInfo, "")

	defer func() {
		// When deleting a virtual machine, the GPU device
		// locked by the virtual machine may not be unlocked.
		// For example, the Cluster or ElfMachine was deleted during a pause.
		if !ctrlutil.ContainsFinalizer(ctx.ElfMachine, infrav1.MachineFinalizer) &&
			ctx.ElfMachine.RequiresGPUDevices() {
			unlockGPUDevicesLockedByVM(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name)
		}
	}()

	if ok, err := r.deletePlacementGroup(ctx); err != nil {
		return reconcile.Result{}, err
	} else if !ok {
		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	// if cluster need to force delete, skipping VM deletion and remove the finalizer.
	if ctx.ElfCluster.HasForceDeleteCluster() {
		ctx.Logger.Info("Skip VM deletion due to the force-delete-cluster annotation")

		ctrlutil.RemoveFinalizer(ctx.ElfMachine, infrav1.MachineFinalizer)
		return reconcile.Result{}, nil
	}

	if !ctx.ElfMachine.HasVM() {
		// ElfMachine may not have saved the created virtual machine when deleting ElfMachine
		vm, err := ctx.VMService.GetByName(ctx.ElfMachine.Name)
		if err != nil {
			if !service.IsVMNotFound(err) {
				return reconcile.Result{}, err
			}

			ctx.Logger.Info("VM already deleted")

			ctrlutil.RemoveFinalizer(ctx.ElfMachine, infrav1.MachineFinalizer)

			return reconcile.Result{}, nil
		}

		ctx.ElfMachine.SetVM(util.GetVMRef(vm))
	}

	if result, err := r.deleteDuplicateVMs(ctx); err != nil || !result.IsZero() {
		return result, err
	}

	err := r.reconcileDeleteVM(ctx)
	if err != nil {
		if service.IsVMNotFound(err) {
			// The VM is deleted so remove the finalizer.
			ctrlutil.RemoveFinalizer(ctx.ElfMachine, infrav1.MachineFinalizer)

			return reconcile.Result{}, nil
		}

		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, clusterv1.DeletionFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return reconcile.Result{}, err
	}

	ctx.Logger.Info("Waiting for VM to be deleted",
		"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
}

func (r *ElfMachineReconciler) reconcileNormal(ctx *context.MachineContext) (reconcile.Result, error) {
	// If the ElfMachine is in an error state, return early.
	if ctx.ElfMachine.IsFailed() {
		ctx.Logger.Info("Error state detected, skipping reconciliation")

		return reconcile.Result{}, nil
	}

	// If the ElfMachine doesn't have our finalizer, add it.
	if !ctrlutil.ContainsFinalizer(ctx.ElfMachine, infrav1.MachineFinalizer) {
		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, patchutil.AddFinalizerWithOptimisticLock(ctx, r.Client, ctx.ElfMachine, infrav1.MachineFinalizer)
	}

	// If ElfMachine requires static IPs for devices, should wait for CAPE-IP to set MachineStaticIPFinalizer first
	// to prevent CAPE from overwriting MachineStaticIPFinalizer when setting MachineFinalizer.
	// If ElfMachine happens to be deleted at this time, CAPE-IP may not have time to release the IPs.
	if ctx.ElfMachine.Spec.Network.RequiresStaticIPs() && !ctrlutil.ContainsFinalizer(ctx.ElfMachine, infrav1.MachineStaticIPFinalizer) {
		r.Logger.V(2).Info("Waiting for CAPE-IP to set MachineStaticIPFinalizer on ElfMachine")

		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	if !ctx.Cluster.Status.InfrastructureReady {
		ctx.Logger.Info("Cluster infrastructure is not ready yet",
			"cluster", ctx.Cluster.Name)

		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForClusterInfrastructureReason, clusterv1.ConditionSeverityInfo, "")

		return reconcile.Result{}, nil
	}

	// Make sure bootstrap data is available and populated.
	if ctx.Machine.Spec.Bootstrap.DataSecretName == nil {
		if !machineutil.IsControlPlaneMachine(ctx.ElfMachine) && !conditions.IsTrue(ctx.Cluster, clusterv1.ControlPlaneInitializedCondition) {
			ctx.Logger.Info("Waiting for the control plane to be initialized")

			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, clusterv1.WaitingForControlPlaneAvailableReason, clusterv1.ConditionSeverityInfo, "")

			return ctrl.Result{}, nil
		}

		ctx.Logger.Info("Waiting for bootstrap data to be available")

		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForBootstrapDataReason, clusterv1.ConditionSeverityInfo, "")

		return reconcile.Result{}, nil
	}

	if result, err := r.reconcilePlacementGroup(ctx); err != nil || !result.IsZero() {
		return result, err
	}

	if r.isWaitingForStaticIPAllocation(ctx) {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForStaticIPAllocationReason, clusterv1.ConditionSeverityInfo, "")
		ctx.Logger.Info("VM is waiting for static ip to be available")
		return reconcile.Result{}, nil
	}

	vm, ok, err := r.reconcileVM(ctx)
	switch {
	case ctx.ElfMachine.IsFailed():
		return reconcile.Result{}, nil
	case err != nil:
		ctx.Logger.Error(err, "failed to reconcile VM")

		if service.IsVMNotFound(err) {
			return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
		}

		return reconcile.Result{}, errors.Wrapf(err, "failed to reconcile VM")
	case !ok || ctx.ElfMachine.HasTask():
		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	// Reconcile the ElfMachine's Labels using the cluster info
	if len(vm.Labels) == 0 {
		if ok, err := r.reconcileLabels(ctx, vm); !ok {
			return reconcile.Result{}, errors.Wrapf(err, "failed to reconcile labels")
		}
	}

	// Reconcile the ElfMachine's providerID using the VM's UUID.
	if err := r.reconcileProviderID(ctx, vm); err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "unexpected error while reconciling providerID for %s", ctx)
	}

	// Reconcile the ElfMachine's node addresses from the VM's IP addresses.
	if ok, err := r.reconcileNetwork(ctx, vm); err != nil {
		return reconcile.Result{}, err
	} else if !ok {
		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	ctx.ElfMachine.Status.Ready = true
	conditions.MarkTrue(ctx.ElfMachine, infrav1.VMProvisionedCondition)

	if ok, err := r.reconcileNode(ctx, vm); !ok {
		if err != nil {
			return reconcile.Result{}, err
		}

		ctx.Logger.Info("Node providerID is not reconciled",
			"namespace", ctx.ElfMachine.Namespace, "elfMachine", ctx.ElfMachine.Name)

		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	if result, err := r.deleteDuplicateVMs(ctx); err != nil || !result.IsZero() {
		return result, err
	}

	return reconcile.Result{}, nil
}

// reconcileVM makes sure that the VM is in the desired state by:
//  1. Creating the VM with the bootstrap data if it does not exist, then...
//  2. Adding the VM to the placement group if needed, then...
//  3. Powering on the VM, and finally...
//  4. Returning the real-time state of the VM to the caller
//
// The return bool value:
// 1. true means that the VM is running and joined a placement group (if needed).
// 2. false and error is nil means the VM is not running or wait to join the placement group.
//
//nolint:gocyclo
func (r *ElfMachineReconciler) reconcileVM(ctx *context.MachineContext) (*models.VM, bool, error) {
	// If there is no vmRef then no VM exists, create one
	if !ctx.ElfMachine.HasVM() {
		// We are setting this condition only in case it does not exists so we avoid to get flickering LastConditionTime
		// in case of cloning errors or powering on errors.
		if !conditions.Has(ctx.ElfMachine, infrav1.VMProvisionedCondition) {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.CloningReason, clusterv1.ConditionSeverityInfo, "")
		}

		bootstrapData, err := r.getBootstrapData(ctx)
		if err != nil {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.CloningFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

			return nil, false, err
		}
		if bootstrapData == "" {
			return nil, false, errors.New("bootstrapData is empty")
		}

		if ok, message, err := isELFScheduleVMErrorRecorded(ctx); err != nil {
			return nil, false, err
		} else if ok {
			if canRetry, err := canRetryVMOperation(ctx); err != nil {
				return nil, false, err
			} else if !canRetry {
				ctx.Logger.V(1).Info(fmt.Sprintf("%s, skip creating VM", message))
				return nil, false, nil
			}

			ctx.Logger.V(1).Info(fmt.Sprintf("%s and the retry silence period passes, will try to create the VM again", message))
		}

		if ok, msg := acquireTicketForCreateVM(ctx.ElfMachine.Name, machineutil.IsControlPlaneMachine(ctx.ElfMachine)); !ok {
			ctx.Logger.V(1).Info(fmt.Sprintf("%s, skip creating VM", msg))
			return nil, false, nil
		}

		var hostID *string
		var gpuDeviceInfos []*service.GPUDeviceInfo
		// The virtual machine of the Control Plane does not support GPU Devices.
		if machineutil.IsControlPlaneMachine(ctx.Machine) {
			hostID, err = r.preCheckPlacementGroup(ctx)
			if err != nil || hostID == nil {
				releaseTicketForCreateVM(ctx.ElfMachine.Name)
				return nil, false, err
			}
		} else {
			hostID, gpuDeviceInfos, err = r.selectHostAndGPUsForVM(ctx, "")
			if err != nil || hostID == nil {
				releaseTicketForCreateVM(ctx.ElfMachine.Name)
				return nil, false, err
			}
		}

		ctx.Logger.Info("Create VM for ElfMachine")

		withTaskVM, err := ctx.VMService.Clone(ctx.ElfCluster, ctx.ElfMachine, bootstrapData, *hostID, gpuDeviceInfos)
		if err != nil {
			releaseTicketForCreateVM(ctx.ElfMachine.Name)

			if service.IsVMDuplicate(err) {
				vm, err := ctx.VMService.GetByName(ctx.ElfMachine.Name)
				if err != nil {
					return nil, false, err
				}

				ctx.ElfMachine.SetVM(util.GetVMRef(vm))
			} else {
				// Duplicate VM error does not require unlocking GPU devices.
				if ctx.ElfMachine.RequiresGPUDevices() {
					unlockGPUDevicesLockedByVM(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name)
				}

				ctx.Logger.Error(err, "failed to create VM",
					"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

				conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.CloningFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

				return nil, false, err
			}
		} else {
			ctx.ElfMachine.SetVM(*withTaskVM.Data.ID)
			ctx.ElfMachine.SetTask(*withTaskVM.TaskID)
		}
	}

	vm, err := r.getVM(ctx)
	if err != nil {
		return nil, false, err
	}

	// Remove VM disconnection timestamp
	vmDisconnectionTimestamp := ctx.ElfMachine.GetVMDisconnectionTimestamp()
	if vmDisconnectionTimestamp != nil {
		ctx.ElfMachine.SetVMDisconnectionTimestamp(nil)

		ctx.Logger.Info("The VM was found again", "vmRef", ctx.ElfMachine.Status.VMRef, "disconnectionTimestamp", vmDisconnectionTimestamp.Format(time.RFC3339))
	}

	if ok, err := r.reconcileVMTask(ctx, vm); err != nil {
		return nil, false, err
	} else if !ok {
		return vm, false, nil
	}

	// The host of the virtual machine may change, such as rescheduling caused by HA.
	if vm.Host != nil && ctx.ElfMachine.Status.HostServerName != *vm.Host.Name {
		hostName := ctx.ElfMachine.Status.HostServerName
		ctx.ElfMachine.Status.HostServerRef = *vm.Host.ID
		ctx.ElfMachine.Status.HostServerName = *vm.Host.Name
		ctx.Logger.V(1).Info(fmt.Sprintf("Updated VM hostServerName from %s to %s", hostName, *vm.Host.Name))
	}

	vmRef := util.GetVMRef(vm)
	// If vmRef is in UUID format, it means that the ELF VM created.
	if !machineutil.IsUUID(vmRef) {
		ctx.Logger.Info("The VM is being created", "vmRef", vmRef)

		return vm, false, nil
	}

	// When ELF VM created, set UUID to VMRef
	if !machineutil.IsUUID(ctx.ElfMachine.Status.VMRef) {
		ctx.ElfMachine.SetVM(vmRef)
	}

	// The VM was moved to the recycle bin. Treat the VM as deleted, and will not reconganize it even if it's moved back from the recycle bin.
	if service.IsVMInRecycleBin(vm) {
		message := fmt.Sprintf("The VM %s was moved to the Tower recycle bin by users, so treat it as deleted.", ctx.ElfMachine.Status.VMRef)
		ctx.ElfMachine.Status.FailureReason = capierrors.MachineStatusErrorPtr(capeerrors.MovedToRecycleBinError)
		ctx.ElfMachine.Status.FailureMessage = pointer.String(message)
		ctx.ElfMachine.SetVM("")
		ctx.Logger.Error(stderrors.New(message), "")

		return vm, false, nil
	}

	// Before the virtual machine is powered on, put the virtual machine into the specified placement group.
	if ok, err := r.joinPlacementGroup(ctx, vm); err != nil || !ok {
		return vm, false, err
	}

	if ok, err := r.reconcileGPUDevices(ctx, vm); err != nil || !ok {
		return vm, false, err
	}

	if ok, err := r.reconcileVMStatus(ctx, vm); err != nil || !ok {
		return vm, false, err
	}

	return vm, true, nil
}

func (r *ElfMachineReconciler) getVM(ctx *context.MachineContext) (*models.VM, error) {
	vm, err := ctx.VMService.Get(ctx.ElfMachine.Status.VMRef)
	if err == nil {
		return vm, nil
	}

	if !service.IsVMNotFound(err) {
		return nil, err
	}

	if machineutil.IsUUID(ctx.ElfMachine.Status.VMRef) {
		vmDisconnectionTimestamp := ctx.ElfMachine.GetVMDisconnectionTimestamp()
		if vmDisconnectionTimestamp == nil {
			now := metav1.Now()
			vmDisconnectionTimestamp = &now
			ctx.ElfMachine.SetVMDisconnectionTimestamp(vmDisconnectionTimestamp)
		}

		// The machine may only be temporarily disconnected before timeout
		if !vmDisconnectionTimestamp.Add(infrav1.VMDisconnectionTimeout).Before(time.Now()) {
			ctx.Logger.Error(err, "the VM has been disconnected, will try to reconnect", "vmRef", ctx.ElfMachine.Status.VMRef, "disconnectionTimestamp", vmDisconnectionTimestamp.Format(time.RFC3339))

			return nil, err
		}

		// If the machine was not found by UUID and timed out it means that it got deleted directly
		ctx.ElfMachine.Status.FailureReason = capierrors.MachineStatusErrorPtr(capeerrors.RemovedFromInfrastructureError)
		ctx.ElfMachine.Status.FailureMessage = pointer.String(fmt.Sprintf("Unable to find VM by UUID %s. The VM was removed from infrastructure.", ctx.ElfMachine.Status.VMRef))
		ctx.Logger.Error(err, fmt.Sprintf("failed to get VM by UUID %s in %s", ctx.ElfMachine.Status.VMRef, infrav1.VMDisconnectionTimeout.String()), "message", ctx.ElfMachine.Status.FailureMessage)

		return nil, err
	}

	// Create VM failed

	if _, err := r.reconcileVMTask(ctx, nil); err != nil {
		return nil, err
	}

	// If Tower fails to create VM, the temporary DB record for this VM will be deleted.
	ctx.ElfMachine.SetVM("")

	return nil, errors.Wrapf(err, "failed to create VM for ElfMachine %s/%s", ctx.ElfMachine.Namespace, ctx.ElfMachine.Name)
}

// reconcileVMStatus ensures the VM is in Running status and configured as expected.
// 1. VM in STOPPED status will be powered on.
// 2. VM in SUSPENDED status will be powered off, then powered on in future reconcile.
// 3. Set VM configuration to the expected values.
//
// The return value:
// 1. true means that the VM is in Running status.
// 2. false and error is nil means the VM is not in Running status.
func (r *ElfMachineReconciler) reconcileVMStatus(ctx *context.MachineContext, vm *models.VM) (bool, error) {
	if vm.Status == nil {
		ctx.Logger.Info("The status of VM is an unexpected value nil", "vmRef", ctx.ElfMachine.Status.VMRef)

		return false, nil
	}

	updatedVMRestrictedFields := service.GetUpdatedVMRestrictedFields(vm, ctx.ElfMachine)
	switch *vm.Status {
	case models.VMStatusRUNNING:
		if len(updatedVMRestrictedFields) > 0 && towerresources.IsAllowCustomVMConfig() {
			// If VM shutdown timed out, simply power off the VM.
			if service.IsShutDownTimeout(conditions.GetMessage(ctx.ElfMachine, infrav1.VMProvisionedCondition)) {
				ctx.Logger.Info("The VM configuration has been modified, power off the VM first and then restore the VM configuration", "vmRef", ctx.ElfMachine.Status.VMRef, "updatedVMRestrictedFields", updatedVMRestrictedFields)

				return false, r.powerOffVM(ctx)
			} else {
				ctx.Logger.Info("The VM configuration has been modified, shut down the VM first and then restore the VM configuration", "vmRef", ctx.ElfMachine.Status.VMRef, "updatedVMRestrictedFields", updatedVMRestrictedFields)

				return false, r.shutDownVM(ctx)
			}
		}

		return true, nil
	case models.VMStatusSTOPPED:
		if len(updatedVMRestrictedFields) > 0 && towerresources.IsAllowCustomVMConfig() {
			ctx.Logger.Info("The VM configuration has been modified, and the VM is stopped, just restore the VM configuration to expected values", "vmRef", ctx.ElfMachine.Status.VMRef, "updatedVMRestrictedFields", updatedVMRestrictedFields)

			return false, r.updateVM(ctx, vm)
		}

		return false, r.powerOnVM(ctx, vm)
	case models.VMStatusSUSPENDED:
		// In some abnormal conditions, the VM will be in a suspended state,
		// e.g. wrong settings in VM or an exception occurred in the Guest OS.
		// try to 'Power off VM -> Power on VM' resumes the VM from a suspended state.
		// See issue http://jira.smartx.com/browse/SKS-1351 for details.
		return false, r.powerOffVM(ctx)
	default:
		ctx.Logger.Info(fmt.Sprintf("The VM is in an unexpected status %s", string(*vm.Status)), "vmRef", ctx.ElfMachine.Status.VMRef)

		return false, nil
	}
}

func (r *ElfMachineReconciler) shutDownVM(ctx *context.MachineContext) error {
	if ok := acquireTicketForUpdatingVM(ctx.ElfMachine.Name); !ok {
		ctx.Logger.V(1).Info("The VM operation reaches rate limit, skip shut down VM")

		return nil
	}

	task, err := ctx.VMService.ShutDown(ctx.ElfMachine.Status.VMRef)
	if err != nil {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.ShuttingDownFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return errors.Wrapf(err, "failed to trigger shut down for VM %s", ctx)
	}

	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.ShuttingDownReason, clusterv1.ConditionSeverityInfo, "")

	ctx.ElfMachine.SetTask(*task.ID)

	ctx.Logger.Info("Waiting for VM to be shut down", "vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return nil
}

func (r *ElfMachineReconciler) powerOffVM(ctx *context.MachineContext) error {
	if ok := acquireTicketForUpdatingVM(ctx.ElfMachine.Name); !ok {
		ctx.Logger.V(1).Info(fmt.Sprintf("The VM operation reaches rate limit, skip powering off VM %s", ctx.ElfMachine.Status.VMRef))

		return nil
	}

	task, err := ctx.VMService.PowerOff(ctx.ElfMachine.Status.VMRef)
	if err != nil {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOffFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return errors.Wrapf(err, "failed to trigger powering off for VM %s", ctx)
	}

	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PowerOffReason, clusterv1.ConditionSeverityInfo, "")

	ctx.ElfMachine.SetTask(*task.ID)

	ctx.Logger.Info("Waiting for VM to be powered off", "vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return nil
}

func (r *ElfMachineReconciler) powerOnVM(ctx *context.MachineContext, vm *models.VM) error {
	if ok, message, err := isELFScheduleVMErrorRecorded(ctx); err != nil {
		return err
	} else if ok {
		if canRetry, err := canRetryVMOperation(ctx); err != nil {
			return err
		} else if !canRetry {
			ctx.Logger.V(1).Info(fmt.Sprintf("%s, skip powering on VM %s", message, ctx.ElfMachine.Status.VMRef))

			return nil
		}

		ctx.Logger.V(1).Info(fmt.Sprintf("%s and the retry silence period passes, will try to power on the VM again", message))
	}

	if ok := acquireTicketForUpdatingVM(ctx.ElfMachine.Name); !ok {
		ctx.Logger.V(1).Info(fmt.Sprintf("The VM operation reaches rate limit, skip power on VM %s", ctx.ElfMachine.Status.VMRef))

		return nil
	}

	hostID := ""
	// Starting a virtual machine with GPU/vGPU does not support automatic scheduling,
	// and need to specify the host where the GPU/vGPU is allocated.
	if ctx.ElfMachine.RequiresGPUDevices() {
		hostID = *vm.Host.ID
	}

	task, err := ctx.VMService.PowerOn(ctx.ElfMachine.Status.VMRef, hostID)
	if err != nil {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOnFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return errors.Wrapf(err, "failed to trigger power on for VM %s", ctx)
	}

	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOnReason, clusterv1.ConditionSeverityInfo, "")

	ctx.ElfMachine.SetTask(*task.ID)

	ctx.Logger.Info("Waiting for VM to be powered on", "vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return nil
}

func (r *ElfMachineReconciler) updateVM(ctx *context.MachineContext, vm *models.VM) error {
	if ok := acquireTicketForUpdatingVM(ctx.ElfMachine.Name); !ok {
		ctx.Logger.V(1).Info(fmt.Sprintf("The VM operation reaches rate limit, skip updating VM %s", ctx.ElfMachine.Status.VMRef))

		return nil
	}

	withTaskVM, err := ctx.VMService.UpdateVM(vm, ctx.ElfMachine)
	if err != nil {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.UpdatingFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return errors.Wrapf(err, "failed to trigger update for VM %s", ctx)
	}

	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.UpdatingReason, clusterv1.ConditionSeverityInfo, "")

	ctx.ElfMachine.SetTask(*withTaskVM.TaskID)

	ctx.Logger.Info("Waiting for the VM to be updated", "vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return nil
}

// reconcileVMTask handles virtual machine tasks.
//
// The return value:
// 1. true indicates that the virtual machine task has completed (success or failure).
// 2. false indicates that the virtual machine task has not been completed yet.
func (r *ElfMachineReconciler) reconcileVMTask(ctx *context.MachineContext, vm *models.VM) (taskDone bool, reterr error) {
	taskRef := ctx.ElfMachine.Status.TaskRef
	vmRef := ctx.ElfMachine.Status.VMRef

	var err error
	var task *models.Task
	if ctx.ElfMachine.HasTask() {
		task, err = ctx.VMService.GetTask(taskRef)
		if err != nil {
			if service.IsTaskNotFound(err) {
				ctx.ElfMachine.SetTask("")
				ctx.Logger.Error(err, fmt.Sprintf("task %s of VM %s is missing", taskRef, vmRef))
			} else {
				return false, errors.Wrapf(err, "failed to get task %s for VM %s", taskRef, vmRef)
			}
		}
	}

	if task == nil {
		// VM is performing an operation
		if vm != nil && vm.EntityAsyncStatus != nil {
			ctx.Logger.Info("Waiting for VM task done", "vmRef", vmRef, "taskRef", taskRef)

			return false, nil
		}

		return true, nil
	}

	defer func() {
		if taskDone {
			ctx.ElfMachine.SetTask("")
		}

		// The task is completed but entityAsyncStatus may not be equal to nil.
		// So need to wait for the current operation of the virtual machine to complete.
		// Avoid resource lock conflicts caused by concurrent operations of ELF.
		if vm != nil && vm.EntityAsyncStatus != nil {
			taskDone = false
		}
	}()

	switch *task.Status {
	case models.TaskStatusFAILED:
		if err := r.reconcileVMFailedTask(ctx, task, taskRef, vmRef); err != nil {
			return true, err
		}
	case models.TaskStatusSUCCESSED:
		ctx.Logger.Info("VM task succeeded", "vmRef", vmRef, "taskRef", taskRef, "taskDescription", service.GetTowerString(task.Description))

		if ctx.ElfMachine.RequiresGPUDevices() &&
			(service.IsCloneVMTask(task) || service.IsPowerOnVMTask(task) || service.IsUpdateVMTask(task)) {
			unlockGPUDevicesLockedByVM(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name)
		}

		if service.IsCloneVMTask(task) || service.IsPowerOnVMTask(task) {
			releaseTicketForCreateVM(ctx.ElfMachine.Name)
			recordElfClusterMemoryInsufficient(ctx, false)

			if err := recordPlacementGroupPolicyNotSatisfied(ctx, false); err != nil {
				return true, err
			}
		}
	default:
		ctx.Logger.Info("Waiting for VM task done", "vmRef", vmRef, "taskRef", taskRef, "taskStatus", service.GetTowerTaskStatus(task.Status), "taskDescription", service.GetTowerString(task.Description))
	}

	if *task.Status == models.TaskStatusFAILED || *task.Status == models.TaskStatusSUCCESSED {
		return true, nil
	}

	return false, nil
}

// reconcileVMFailedTask handles failed virtual machine tasks.
func (r *ElfMachineReconciler) reconcileVMFailedTask(ctx *context.MachineContext, task *models.Task, taskRef, vmRef string) error {
	errorMessage := service.GetTowerString(task.ErrorMessage)
	if service.IsGPUAssignFailed(errorMessage) {
		errorMessage = service.ParseGPUAssignFailed(errorMessage)
	}
	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.TaskFailureReason, clusterv1.ConditionSeverityInfo, errorMessage)

	if service.IsCloudInitConfigError(errorMessage) {
		ctx.ElfMachine.Status.FailureReason = capierrors.MachineStatusErrorPtr(capeerrors.CloudInitConfigError)
		ctx.ElfMachine.Status.FailureMessage = pointer.String(fmt.Sprintf("VM cloud-init config error: %s", service.FormatCloudInitError(errorMessage)))
	}

	ctx.Logger.Error(errors.New("VM task failed"), "", "vmRef", vmRef, "taskRef", taskRef, "taskErrorMessage", errorMessage, "taskErrorCode", service.GetTowerString(task.ErrorCode), "taskDescription", service.GetTowerString(task.Description))

	switch {
	case service.IsCloneVMTask(task):
		releaseTicketForCreateVM(ctx.ElfMachine.Name)

		if service.IsVMDuplicateError(errorMessage) {
			setVMDuplicate(ctx.ElfMachine.Name)
		}

		if ctx.ElfMachine.RequiresGPUDevices() {
			unlockGPUDevicesLockedByVM(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name)
		}
	case service.IsPowerOnVMTask(task) || service.IsUpdateVMTask(task) || service.IsVMColdMigrationTask(task):
		if ctx.ElfMachine.RequiresGPUDevices() {
			unlockGPUDevicesLockedByVM(ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Name)
		}
	case service.IsMemoryInsufficientError(errorMessage):
		recordElfClusterMemoryInsufficient(ctx, true)
		message := fmt.Sprintf("Insufficient memory detected for the ELF cluster %s", ctx.ElfCluster.Spec.Cluster)
		ctx.Logger.Info(message)

		return errors.New(message)
	case service.IsPlacementGroupError(errorMessage):
		if err := recordPlacementGroupPolicyNotSatisfied(ctx, true); err != nil {
			return err
		}
		message := "The placement group policy can not be satisfied"
		ctx.Logger.Info(message)

		return errors.New(message)
	}

	return nil
}

func (r *ElfMachineReconciler) reconcileProviderID(ctx *context.MachineContext, vm *models.VM) error {
	providerID := machineutil.ConvertUUIDToProviderID(*vm.LocalID)
	if providerID == "" {
		return errors.Errorf("invalid VM UUID %s from %s %s/%s for %s",
			*vm.LocalID,
			ctx.ElfCluster.GroupVersionKind(),
			ctx.ElfCluster.GetNamespace(),
			ctx.ElfCluster.GetName(),
			ctx)
	}

	if ctx.ElfMachine.Spec.ProviderID == nil || *ctx.ElfMachine.Spec.ProviderID != providerID {
		ctx.ElfMachine.Spec.ProviderID = pointer.String(providerID)

		ctx.Logger.Info("updated providerID", "providerID", providerID)
	}

	return nil
}

// reconcileNode sets providerID and host server labels for node.
func (r *ElfMachineReconciler) reconcileNode(ctx *context.MachineContext, vm *models.VM) (bool, error) {
	providerID := machineutil.ConvertUUIDToProviderID(*vm.LocalID)
	if providerID == "" {
		return false, errors.Errorf("invalid VM UUID %s from %s %s/%s for %s",
			*vm.LocalID,
			ctx.ElfCluster.GroupVersionKind(),
			ctx.ElfCluster.GetNamespace(),
			ctx.ElfCluster.GetName(),
			ctx)
	}

	kubeClient, err := util.NewKubeClient(ctx, ctx.Client, ctx.Cluster)
	if err != nil {
		return false, errors.Wrapf(err, "failed to get client for Cluster %s/%s", ctx.Cluster.Namespace, ctx.Cluster.Name)
	}

	node, err := kubeClient.CoreV1().Nodes().Get(ctx, ctx.ElfMachine.Name, metav1.GetOptions{})
	if err != nil {
		return false, errors.Wrapf(err, "failed to get node %s for setting providerID and labels", ctx.ElfMachine.Name)
	}

	nodeHostID := labelsutil.GetHostServerIDLabel(node)
	nodeHostName := labelsutil.GetHostServerNameLabel(node)
	towerVMID := labelsutil.GetTowerVMIDLabel(node)
	if node.Spec.ProviderID != "" && nodeHostID == ctx.ElfMachine.Status.HostServerRef &&
		nodeHostName == ctx.ElfMachine.Status.HostServerName && towerVMID == *vm.ID {
		return true, nil
	}

	nodeGroupName := machineutil.GetNodeGroupName(ctx.Machine)
	labels := map[string]string{
		infrav1.HostServerIDLabel:   ctx.ElfMachine.Status.HostServerRef,
		infrav1.HostServerNameLabel: ctx.ElfMachine.Status.HostServerName,
		infrav1.TowerVMIDLabel:      *vm.ID,
		infrav1.NodeGroupLabel:      nodeGroupName,
	}
	if len(ctx.ElfMachine.Spec.GPUDevices) > 0 {
		labels[labelsutil.ClusterAutoscalerCAPIGPULabel] = labelsutil.ConvertToLabelValue(ctx.ElfMachine.Spec.GPUDevices[0].Model)
	} else if len(ctx.ElfMachine.Spec.VGPUDevices) > 0 {
		labels[labelsutil.ClusterAutoscalerCAPIGPULabel] = labelsutil.ConvertToLabelValue(ctx.ElfMachine.Spec.VGPUDevices[0].Type)
	}

	payloads := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": labels,
		},
	}
	// providerID cannot be modified after setting a valid value.
	if node.Spec.ProviderID == "" {
		payloads["spec"] = map[string]interface{}{
			"providerID": providerID,
		}
	}

	payloadBytes, err := json.Marshal(payloads)
	if err != nil {
		return false, err
	}

	_, err = kubeClient.CoreV1().Nodes().Patch(ctx, node.Name, apitypes.MergePatchType, payloadBytes, metav1.PatchOptions{})
	if err != nil {
		return false, err
	}

	ctx.Logger.Info("Setting node providerID and labels succeeded",
		"cluster", ctx.Cluster.Name, "node", node.Name,
		"providerID", providerID, "hostID", ctx.ElfMachine.Status.HostServerRef, "hostName", ctx.ElfMachine.Status.HostServerName)

	return true, nil
}

// Ensure all the VM's NICs that need IP have obtained IP addresses and VM network ready, otherwise requeue.
// 1. If there are DHCP VM NICs, it will ensure that all DHCP VM NICs obtain IP.
// 2. If none of the VM NICs is the DHCP type, at least one IP must be obtained from Tower API or k8s node to ensure that the network is ready.
//
// In the scenario with many virtual machines, it could be slow for SMTX OS to synchronize VM information via vmtools.
// So if not enough IPs can be obtained from Tower API, try to get its IP address from the corresponding K8s Node.
func (r *ElfMachineReconciler) reconcileNetwork(ctx *context.MachineContext, vm *models.VM) (ret bool, reterr error) {
	defer func() {
		if reterr != nil {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForNetworkAddressesReason, clusterv1.ConditionSeverityWarning, reterr.Error())
		} else if !ret {
			ctx.Logger.V(1).Info("VM network is not ready yet", "nicStatus", ctx.ElfMachine.Status.Network)
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForNetworkAddressesReason, clusterv1.ConditionSeverityInfo, "")
		}
	}()

	ctx.ElfMachine.Status.Network = []infrav1.NetworkStatus{}
	ctx.ElfMachine.Status.Addresses = []clusterv1.MachineAddress{}
	// A Map of IP to MachineAddress
	ipToMachineAddressMap := make(map[string]clusterv1.MachineAddress)

	nics, err := ctx.VMService.GetVMNics(*vm.ID)
	if err != nil {
		return false, err
	}

	for i := 0; i < len(nics); i++ {
		nic := nics[i]
		ip := service.GetTowerString(nic.IPAddress)

		// Add to Status.Network even if IP is empty.
		ctx.ElfMachine.Status.Network = append(ctx.ElfMachine.Status.Network, infrav1.NetworkStatus{
			IPAddrs: []string{ip},
			MACAddr: service.GetTowerString(nic.MacAddress),
		})

		if ip == "" {
			continue
		}

		ipToMachineAddressMap[ip] = clusterv1.MachineAddress{
			Type:    clusterv1.MachineInternalIP,
			Address: ip,
		}
	}

	networkDevicesWithIP := ctx.ElfMachine.GetNetworkDevicesRequiringIP()
	networkDevicesWithDHCP := ctx.ElfMachine.GetNetworkDevicesRequiringDHCP()

	if len(ipToMachineAddressMap) < len(networkDevicesWithIP) {
		// Try to get VM NIC IP address from the K8s Node.
		nodeIP, err := r.getK8sNodeIP(ctx, ctx.ElfMachine.Name)
		if err == nil && nodeIP != "" {
			ipToMachineAddressMap[nodeIP] = clusterv1.MachineAddress{
				Address: nodeIP,
				Type:    clusterv1.MachineInternalIP,
			}
		} else if err != nil {
			ctx.Logger.Error(err, "failed to get VM NIC IP address from the K8s node", "Node", ctx.ElfMachine.Name)
		}
	}

	if len(networkDevicesWithDHCP) > 0 {
		dhcpIPNum := 0
		for _, ip := range ipToMachineAddressMap {
			if !ctx.ElfMachine.IsMachineStaticIP(ip.Address) {
				dhcpIPNum++
			}
		}
		// If not all DHCP NICs get IP, return false and wait for next requeue.
		if dhcpIPNum < len(networkDevicesWithDHCP) {
			return false, nil
		}
	} else if len(ipToMachineAddressMap) < 1 {
		// If none of the VM NICs is the DHCP type,
		// at least one IP must be obtained from Tower API or k8s node to ensure that the network is ready, otherwise requeue.
		return false, nil
	}

	for _, machineAddress := range ipToMachineAddressMap {
		ctx.ElfMachine.Status.Addresses = append(ctx.ElfMachine.Status.Addresses, machineAddress)
	}

	return true, nil
}

func (r *ElfMachineReconciler) getBootstrapData(ctx *context.MachineContext) (string, error) {
	secret := &corev1.Secret{}
	secretKey := apitypes.NamespacedName{
		Namespace: ctx.Machine.Namespace,
		Name:      *ctx.Machine.Spec.Bootstrap.DataSecretName,
	}

	if err := ctx.Client.Get(ctx, secretKey, secret); err != nil {
		return "", errors.Wrapf(err, "failed to retrieve bootstrap data secret for %s %s", secretKey.Namespace, secretKey.Name)
	}

	value, ok := secret.Data["value"]
	if !ok {
		return "", errors.New("error retrieving bootstrap data: secret value key is missing")
	}

	return string(value), nil
}

func (r *ElfMachineReconciler) reconcileLabels(ctx *context.MachineContext, vm *models.VM) (bool, error) {
	creatorLabel, err := ctx.VMService.UpsertLabel(towerresources.GetVMLabelManaged(), "true")
	if err != nil {
		return false, errors.Wrapf(err, "failed to upsert label "+towerresources.GetVMLabelManaged())
	}
	namespaceLabel, err := ctx.VMService.UpsertLabel(towerresources.GetVMLabelNamespace(), ctx.ElfMachine.Namespace)
	if err != nil {
		return false, errors.Wrapf(err, "failed to upsert label "+towerresources.GetVMLabelNamespace())
	}
	clusterNameLabel, err := ctx.VMService.UpsertLabel(towerresources.GetVMLabelClusterName(), ctx.ElfCluster.Name)
	if err != nil {
		return false, errors.Wrapf(err, "failed to upsert label "+towerresources.GetVMLabelClusterName())
	}

	var vipLabel *models.Label
	if machineutil.IsControlPlaneMachine(ctx.ElfMachine) {
		vipLabel, err = ctx.VMService.UpsertLabel(towerresources.GetVMLabelVIP(), ctx.ElfCluster.Spec.ControlPlaneEndpoint.Host)
		if err != nil {
			return false, errors.Wrapf(err, "failed to upsert label "+towerresources.GetVMLabelVIP())
		}
	}

	labelIDs := []string{*namespaceLabel.ID, *clusterNameLabel.ID, *creatorLabel.ID}
	if machineutil.IsControlPlaneMachine(ctx.ElfMachine) {
		labelIDs = append(labelIDs, *vipLabel.ID)
	}
	r.Logger.V(3).Info("Upsert labels", "labelIds", labelIDs)
	_, err = ctx.VMService.AddLabelsToVM(*vm.ID, labelIDs)
	if err != nil {
		return false, err
	}
	return true, nil
}

// isWaitingForStaticIPAllocation checks whether the VM should wait for a static IP
// to be allocated.
func (r *ElfMachineReconciler) isWaitingForStaticIPAllocation(ctx *context.MachineContext) bool {
	devices := ctx.ElfMachine.Spec.Network.Devices
	for _, device := range devices {
		if device.NetworkType == infrav1.NetworkTypeIPV4 && len(device.IPAddrs) == 0 {
			// Static IP is not available yet
			return true
		}
	}

	return false
}

// deleteNode attempts to delete the node corresponding to the VM.
// This is necessary since CAPI does not set the nodeRef field on the owner Machine object
// until the node moves to Ready state. Hence, on Machine deletion it is unable to delete
// the kubernetes node corresponding to the VM.
func (r *ElfMachineReconciler) deleteNode(ctx *context.MachineContext, nodeName string) error {
	// When the cluster needs to be deleted, there is no need to delete the k8s node.
	if ctx.Cluster.DeletionTimestamp != nil {
		return nil
	}

	// when the control plane is not ready, there is no need to delete the k8s node.
	if !ctx.Cluster.Status.ControlPlaneReady {
		return nil
	}

	kubeClient, err := util.NewKubeClient(ctx, ctx.Client, ctx.Cluster)
	if err != nil {
		return errors.Wrapf(err, "failed to get client for Cluster %s/%s", ctx.Cluster.Namespace, ctx.Cluster.Name)
	}

	// Attempt to delete the corresponding node.
	err = kubeClient.CoreV1().Nodes().Delete(ctx, nodeName, metav1.DeleteOptions{})
	// k8s node is already deleted.
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return errors.Wrapf(err, "failed to delete K8s node %s for Cluster %s/%s", nodeName, ctx.Cluster.Namespace, ctx.Cluster.Name)
	}

	return nil
}

// getK8sNodeIP get the default network IP of K8s Node.
func (r *ElfMachineReconciler) getK8sNodeIP(ctx *context.MachineContext, nodeName string) (string, error) {
	kubeClient, err := util.NewKubeClient(ctx, ctx.Client, ctx.Cluster)
	if err != nil {
		return "", errors.Wrapf(err, "failed to get client for Cluster %s/%s", ctx.Cluster.Namespace, ctx.Cluster.Name)
	}

	k8sNode, err := kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil && apierrors.IsNotFound(err) {
		return "", nil
	}

	if err != nil {
		return "", errors.Wrapf(err, "failed to get K8s Node %s for Cluster %s/%s", nodeName, ctx.Cluster.Namespace, ctx.Cluster.Name)
	}

	if len(k8sNode.Status.Addresses) == 0 {
		return "", nil
	}

	for _, address := range k8sNode.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			return address.Address, nil
		}
	}

	return "", nil
}

// Handling duplicate virtual machines.
//
// NOTE: These will be removed when Tower fixes issue with duplicate virtual machines.

const (
	// The time duration for check duplicate VM,
	// starting from when the virtual machine was created.
	// Tower syncs virtual machines from ELF every 30 minutes.
	checkDuplicateVMDuration = 1 * time.Hour
)

// deleteDuplicateVMs deletes the duplicate virtual machines.
// Only be used to delete duplicate VMs before the ElfCluster is deleted.
func (r *ElfMachineReconciler) deleteDuplicateVMs(ctx *context.MachineContext) (reconcile.Result, error) {
	// Duplicate virtual machines appear in the process of creating virtual machines,
	// only need to check within half an hour after creating virtual machines.
	if ctx.ElfMachine.DeletionTimestamp.IsZero() &&
		time.Now().After(ctx.ElfMachine.CreationTimestamp.Add(checkDuplicateVMDuration)) {
		return reconcile.Result{}, nil
	}

	vms, err := ctx.VMService.FindVMsByName(ctx.ElfMachine.Name)
	if err != nil {
		return reconcile.Result{}, err
	}

	if len(vms) <= 1 {
		return reconcile.Result{}, nil
	}

	if ctx.ElfMachine.Status.VMRef == "" {
		vmIDs := make([]string, 0, len(vms))
		for i := 0; i < len(vms); i++ {
			vmIDs = append(vmIDs, *vms[i].ID)
		}
		ctx.Logger.Info("Waiting for ElfMachine to select one of the duplicate VMs before deleting the other", "vms", vmIDs)
		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	for i := 0; i < len(vms); i++ {
		// Do not delete already running virtual machines to avoid deleting already used virtual machines.
		if *vms[i].ID == ctx.ElfMachine.Status.VMRef ||
			*vms[i].LocalID == ctx.ElfMachine.Status.VMRef ||
			*vms[i].Status != models.VMStatusSTOPPED {
			continue
		}

		// When there are duplicate virtual machines, the service of Tower is unstable.
		// If there is a deletion operation error, just return and try again.
		if err := r.deleteVM(ctx, vms[i]); err != nil {
			return reconcile.Result{}, err
		} else {
			return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
		}
	}

	return reconcile.Result{}, nil
}

// deleteVM deletes the specified virtual machine.
func (r *ElfMachineReconciler) deleteVM(ctx *context.MachineContext, vm *models.VM) error {
	// VM is performing an operation
	if vm.EntityAsyncStatus != nil {
		ctx.Logger.V(1).Info("Waiting for VM task done before deleting the duplicate VM", "vmID", *vm.ID, "name", *vm.Name)
		return nil
	}

	// Delete the VM.
	// Delete duplicate virtual machines asynchronously,
	// because synchronous deletion will affect the performance of reconcile.
	task, err := ctx.VMService.Delete(*vm.ID)
	if err != nil {
		return err
	}

	ctx.Logger.Info(fmt.Sprintf("Destroying duplicate VM %s in task %s", *vm.ID, *task.ID))

	return nil
}
