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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capierrors "sigs.k8s.io/cluster-api/errors"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	capeerrors "github.com/smartxworks/cluster-api-provider-elf/pkg/errors"
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/util"
	labelsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/labels"
	machineutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/machine"
)

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/finalizers,verbs=update
//+kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments;machinedeployments/status,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch

// ElfMachineReconciler reconciles a ElfMachine object.
type ElfMachineReconciler struct {
	*context.ControllerContext
	NewVMService service.NewVMServiceFunc
}

// AddMachineControllerToManager adds the machine controller to the provided
// manager.
func AddMachineControllerToManager(ctx *context.ControllerManagerContext, mgr ctrlmgr.Manager) error {
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
			&source.Kind{Type: &clusterv1.Machine{}},
			handler.EnqueueRequestsFromMapFunc(capiutil.MachineToInfrastructureMapFunc(controlledTypeGVK)),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctx.MaxConcurrentReconciles}).
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
		"elfCluster", elfCluster.Name, "elfMachine", elfMachine.Name)

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
		// If VM shutdown timed out or VMGracefulShutdown is set to false, simply power off the VM.
		if service.IsShutDownTimeout(conditions.GetMessage(ctx.ElfMachine, infrav1.VMProvisionedCondition)) ||
			!ctx.ElfCluster.Spec.VMGracefulShutdown {
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

		if vm.LocalID != nil && len(*vm.LocalID) > 0 {
			ctx.ElfMachine.SetVM(*vm.LocalID)
		} else {
			ctx.ElfMachine.SetVM(*vm.ID)
		}
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
	ctrlutil.AddFinalizer(ctx.ElfMachine, infrav1.MachineFinalizer)

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

	if r.isWaitingForStaticIPAllocation(ctx) {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForStaticIPAllocationReason, clusterv1.ConditionSeverityInfo, "")
		ctx.Logger.Info("VM is waiting for static ip to be available")
		return reconcile.Result{}, nil
	}

	vm, err := r.reconcileVM(ctx)
	if ctx.ElfMachine.IsFailed() {
		return reconcile.Result{}, nil
	} else if err != nil {
		ctx.Logger.Error(err, "failed to reconcile VM")

		if service.IsVMNotFound(err) {
			return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
		}

		return reconcile.Result{}, errors.Wrapf(err, "failed to reconcile VM")
	}

	if vm == nil || vm.EntityAsyncStatus != nil || *vm.Status != models.VMStatusRUNNING ||
		!machineutil.IsUUID(ctx.ElfMachine.Status.VMRef) || ctx.ElfMachine.HasTask() {
		ctx.Logger.Info("VM state is not reconciled")

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
	if ok := r.reconcileNetwork(ctx, vm); !ok {
		ctx.Logger.Info("network is not reconciled",
			"namespace", ctx.ElfMachine.Namespace, "elfMachine", ctx.ElfMachine.Name)

		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForNetworkAddressesReason, clusterv1.ConditionSeverityInfo, "")

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

	return reconcile.Result{}, nil
}

// reconcileVM makes sure that the VM is in the desired state by:
//  1. Creating the VM with the bootstrap data if it does not exist, then...
//  2. Powering on the VM, and finally...
//  3. Returning the real-time state of the VM to the caller
func (r *ElfMachineReconciler) reconcileVM(ctx *context.MachineContext) (*models.VM, error) {
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

			return nil, err
		}
		if bootstrapData == "" {
			return nil, errors.New("bootstrapData is empty")
		}

		if ok := isElfClusterMemoryInsufficient(ctx.ElfCluster.Spec.Cluster); ok {
			if canRetry := canRetryVMOperation(ctx.ElfCluster.Spec.Cluster); !canRetry {
				ctx.Logger.V(1).Info(fmt.Sprintf("Insufficient memory for ELF cluster %s, skip creating VM", ctx.ElfCluster.Spec.Cluster))
				return nil, nil
			}

			ctx.Logger.V(1).Info(fmt.Sprintf("Insufficient memory for ELF cluster %s, try to create VM", ctx.ElfCluster.Spec.Cluster))
		}

		// Only limit the virtual machines of the worker nodes
		if !machineutil.IsControlPlaneMachine(ctx.ElfMachine) {
			if ok := acquireTicketForCreateVM(ctx.ElfMachine.Name); !ok {
				ctx.Logger.V(1).Info(fmt.Sprintf("The number of concurrently created VMs has reached the limit, skip creating VM %s", ctx.ElfMachine.Name))
				return nil, nil
			}
		}

		hostID, err := r.getVMHostForRollingUpdate(ctx)
		if err != nil {
			return nil, err
		}

		ctx.Logger.Info("Create VM for ElfMachine")

		withTaskVM, err := ctx.VMService.Clone(ctx.ElfCluster, ctx.Machine, ctx.ElfMachine, bootstrapData, hostID)
		if err != nil {
			releaseTicketForCreateVM(ctx.ElfMachine.Name)

			if service.IsVMDuplicate(err) {
				vm, err := ctx.VMService.GetByName(ctx.ElfMachine.Name)
				if err != nil {
					return nil, err
				}

				ctx.ElfMachine.SetVM(*vm.ID)
			} else {
				ctx.Logger.Error(err, "failed to create VM",
					"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

				conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.CloningFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

				return nil, err
			}
		} else {
			ctx.ElfMachine.SetVM(*withTaskVM.Data.ID)
			ctx.ElfMachine.SetTask(*withTaskVM.TaskID)
		}
	}

	vm, err := r.getVM(ctx)
	if err != nil {
		return nil, err
	}

	// Remove VM disconnection timestamp
	vmDisconnectionTimestamp := ctx.ElfMachine.GetVMDisconnectionTimestamp()
	if vmDisconnectionTimestamp != nil {
		ctx.ElfMachine.SetVMDisconnectionTimestamp(nil)

		ctx.Logger.Info("The VM was found again", "vmRef", ctx.ElfMachine.Status.VMRef, "disconnectionTimestamp", vmDisconnectionTimestamp.Format(time.RFC3339))
	}

	if ok, err := r.reconcileVMTask(ctx, vm); err != nil {
		return nil, err
	} else if !ok {
		return vm, nil
	}

	// The host of the virtual machine may change, such as rescheduling caused by HA.
	if vm.Host != nil && ctx.ElfMachine.Status.HostServerName != *vm.Host.Name {
		hostName := ctx.ElfMachine.Status.HostServerName
		ctx.ElfMachine.Status.HostServerRef = *vm.Host.ID
		ctx.ElfMachine.Status.HostServerName = *vm.Host.Name
		ctx.Logger.V(1).Info(fmt.Sprintf("Updated VM hostServerName from %s to %s", hostName, *vm.Host.Name))
	}

	vmLocalID := util.GetTowerString(vm.LocalID)
	// Before the ELF VM is created, Tower sets a "placeholder-{UUID}" format string to localId, such as "placeholder-7d8b6df1-c623-4750-a771-3ba6b46995fa".
	// After the ELF VM is created, Tower sets the VM ID in UUID format to localId.
	if !machineutil.IsUUID(vmLocalID) {
		return vm, nil
	}

	// When ELF VM created, set UUID to VMRef
	if !machineutil.IsUUID(ctx.ElfMachine.Status.VMRef) {
		ctx.ElfMachine.SetVM(vmLocalID)
	}

	// The VM was moved to the recycle bin. Treat the VM as deleted, and will not reconganize it even if it's moved back from the recycle bin.
	if util.IsVMInRecycleBin(vm) {
		message := fmt.Sprintf("The VM %s was moved to the Tower recycle bin by users, so treat it as deleted.", ctx.ElfMachine.Status.VMRef)
		ctx.ElfMachine.Status.FailureReason = capierrors.MachineStatusErrorPtr(capeerrors.MovedToRecycleBinError)
		ctx.ElfMachine.Status.FailureMessage = pointer.String(message)
		ctx.ElfMachine.SetVM("")
		ctx.Logger.Error(stderrors.New(message), "")

		return vm, nil
	}

	// Before the virtual machine is powered on, put the virtual machine into the specified placement group.
	if ok, err := r.reconcilePlacementGroup(ctx, vm); err != nil || !ok {
		return nil, err
	}

	// The VM may need to powered off
	if ok, err := r.reconcileVMStatus(ctx, vm); err != nil || !ok {
		return nil, err
	}

	return vm, nil
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

func (r *ElfMachineReconciler) reconcileVMStatus(ctx *context.MachineContext, vm *models.VM) (bool, error) {
	if *vm.Status != models.VMStatusSTOPPED {
		return true, nil
	}

	if ok := isElfClusterMemoryInsufficient(ctx.ElfCluster.Spec.Cluster); ok {
		if canRetry := canRetryVMOperation(ctx.ElfCluster.Spec.Cluster); !canRetry {
			ctx.Logger.V(1).Info(fmt.Sprintf("Insufficient memory for ELF cluster %s, skip powering on VM %s", ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Status.VMRef))

			return false, nil
		}

		ctx.Logger.V(1).Info(fmt.Sprintf("Insufficient memory for the ELF cluster %s was detected previously, try to power on VM %s to check if the ELF cluster has sufficient memory now", ctx.ElfCluster.Spec.Cluster, ctx.ElfMachine.Status.VMRef))
	}

	if ok := acquireTicketForUpdatingVM(ctx.ElfMachine.Name); !ok {
		ctx.Logger.V(1).Info(fmt.Sprintf("The VM operation reaches rate limit, skip power on VM %s", ctx.ElfMachine.Status.VMRef))

		return false, nil
	}

	task, err := ctx.VMService.PowerOn(ctx.ElfMachine.Status.VMRef)
	if err != nil {
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOnFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return false, errors.Wrapf(err, "failed to trigger power on for VM %s", ctx)
	}

	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOnReason, clusterv1.ConditionSeverityInfo, "")

	ctx.ElfMachine.SetTask(*task.ID)

	ctx.Logger.Info("Waiting for VM to be powered on", "vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return false, nil
}

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
	}()

	switch *task.Status {
	case models.TaskStatusFAILED:
		errorMessage := util.GetTowerString(task.ErrorMessage)
		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.TaskFailureReason, clusterv1.ConditionSeverityInfo, errorMessage)

		if service.IsCloudInitConfigError(errorMessage) {
			ctx.ElfMachine.Status.FailureReason = capierrors.MachineStatusErrorPtr(capeerrors.CloudInitConfigError)
			ctx.ElfMachine.Status.FailureMessage = pointer.String(fmt.Sprintf("VM cloud-init config error: %s", service.FormatCloudInitError(errorMessage)))
		}

		ctx.Logger.Error(errors.New("VM task failed"), "", "vmRef", vmRef, "taskRef", taskRef, "taskErrorMessage", errorMessage, "taskErrorCode", util.GetTowerString(task.ErrorCode), "taskDescription", util.GetTowerString(task.Description))

		switch {
		case util.IsCloneVMTask(task):
			releaseTicketForCreateVM(ctx.ElfMachine.Name)
		case util.IsVMMigrationTask(task):
			placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine)
			if err != nil {
				return false, err
			}
			releaseTicketForPlacementGroupVMMigration(placementGroupName)
		case service.IsMemoryInsufficientError(errorMessage):
			setElfClusterMemoryInsufficient(ctx.ElfCluster.Spec.Cluster, true)
			message := fmt.Sprintf("Insufficient memory detected for ELF cluster %s", ctx.ElfCluster.Spec.Cluster)
			ctx.Logger.Info(message)

			return true, errors.New(message)
		}
	case models.TaskStatusSUCCESSED:
		ctx.Logger.Info("VM task succeeded", "vmRef", vmRef, "taskRef", taskRef, "taskDescription", util.GetTowerString(task.Description))

		switch {
		case util.IsCloneVMTask(task) || util.IsPowerOnVMTask(task):
			setElfClusterMemoryInsufficient(ctx.ElfCluster.Spec.Cluster, false)
			releaseTicketForCreateVM(ctx.ElfMachine.Name)
		case util.IsVMMigrationTask(task):
			placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine)
			if err != nil {
				return false, err
			}
			releaseTicketForPlacementGroupVMMigration(placementGroupName)
		}
	default:
		ctx.Logger.Info("Waiting for VM task done", "vmRef", vmRef, "taskRef", taskRef, "taskStatus", util.GetTowerTaskStatus(task.Status), "taskDescription", util.GetTowerString(task.Description))
	}

	if *task.Status == models.TaskStatusFAILED || *task.Status == models.TaskStatusSUCCESSED {
		return true, nil
	}

	return false, nil
}

// reconcilePlacementGroup puts the virtual machine into the placement group.
func (r *ElfMachineReconciler) reconcilePlacementGroup(ctx *context.MachineContext, vm *models.VM) (ret bool, reterr error) {
	defer func() {
		if reterr != nil {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.JoiningPlacementGroupFailedReason, clusterv1.ConditionSeverityWarning, reterr.Error())
		} else if !ret {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.JoiningPlacementGroupReason, clusterv1.ConditionSeverityInfo, "")
		}
	}()

	towerCluster, err := ctx.VMService.GetCluster(ctx.ElfCluster.Spec.Cluster)
	if err != nil {
		return false, err
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine)
	if err != nil {
		return false, err
	}

	if ok := acquireTicketForPlacementGroupOperation(placementGroupName); ok {
		defer releaseTicketForPlacementGroupOperation(placementGroupName)
	} else {
		return false, nil
	}

	placementGroup, err := ctx.VMService.GetVMPlacementGroup(placementGroupName)
	if err != nil {
		if !service.IsVMPlacementGroupNotFound(err) {
			return false, err
		}

		placementGroup, err = r.createPlacementGroup(ctx, placementGroupName, *towerCluster.ID)
		if err != nil || placementGroup == nil {
			return false, err
		}
	}

	// Placement group is performing an operation
	if !machineutil.IsUUID(*placementGroup.LocalID) || placementGroup.EntityAsyncStatus != nil {
		ctx.Logger.Info("Waiting for placement group task done", "placementGroup", *placementGroup.Name)

		return false, nil
	}

	placementGroupVMSet := sets.NewString()
	for i := 0; i < len(placementGroup.Vms); i++ {
		placementGroupVMSet.Insert(*placementGroup.Vms[i].ID)
	}

	if placementGroupVMSet.Has(*vm.ID) {
		return true, nil
	}

	if machineutil.IsControlPlaneMachine(ctx.Machine) {
		if len(placementGroup.Vms) >= int(*towerCluster.HostNum) {
			ctx.Logger.Info("The placement group is full, skip adding VM to the placement group", "placementGroup", *placementGroup.Name, "placementGroupCapacity", *towerCluster.HostNum, "vmRef", ctx.ElfMachine.Status.VMRef, "vmId", *vm.ID)

			return true, nil
		}

		vms, err := ctx.VMService.FindByIDs(placementGroupVMSet.List())
		if err != nil {
			return false, err
		}

		vmHostSet := sets.NewString()
		for i := 0; i < len(vms); i++ {
			vmHostSet.Insert(*vms[i].Host.ID)
		}

		clusterHostSet := sets.NewString()
		for i := 0; i < len(towerCluster.Hosts); i++ {
			clusterHostSet.Insert(*towerCluster.Hosts[i].ID)
		}

		unusedHostSet := clusterHostSet.Difference(vmHostSet)
		if unusedHostSet.Len() == 0 {
			ctx.Logger.Info("The placement group still has capacity, but all hosts are already used", "placementGroup", *placementGroup.Name, "placementGroupCapacity", *towerCluster.HostNum, "usedHosts", vmHostSet.List())

			return false, nil
		}

		if !unusedHostSet.Has(*vm.Host.ID) {
			if *vm.Status != models.VMStatusSTOPPED {
				kcp, err := machineutil.GetKCPByMachine(ctx, ctx.Client, ctx.Machine)
				if err != nil {
					return false, err
				}

				if *kcp.Spec.Replicas != kcp.Status.UpdatedReplicas {
					ctx.Logger.Info("KCP rolling update in progress, skip migrate VM", "vmRef", ctx.ElfMachine.Status.VMRef, "vmId", *vm.ID)

					return true, nil
				}

				if ok := acquireTicketForPlacementGroupVMMigration(placementGroupName); !ok {
					ctx.Logger.V(1).Info("The placement group is performing another VM migration, skip migrate VM", "placementGroup", util.GetTowerString(placementGroup.Name), "vmRef", ctx.ElfMachine.Status.VMRef, "vmId", *vm.ID)

					return false, nil
				}

				targetHost := unusedHostSet.List()[0]
				withTaskVM, err := ctx.VMService.Migrate(util.GetTowerString(vm.ID), targetHost)
				if err != nil {
					return false, err
				}

				ctx.ElfMachine.SetTask(*withTaskVM.TaskID)

				ctx.Logger.Info(fmt.Sprintf("Waiting for the VM to be migrated from %s to %s", *vm.Host.ID, targetHost), "vmRef", ctx.ElfMachine.Status.VMRef, "vmId", *vm.ID, "taskRef", ctx.ElfMachine.Status.TaskRef)

				return false, nil
			}
		}
	}

	if !placementGroupVMSet.Has(*vm.ID) {
		placementGroupVMSet.Insert(*vm.ID)
		if ok, err := r.addVMsToPlacementGroup(ctx, placementGroup, placementGroupVMSet.List()); err != nil || !ok {
			return false, err
		}
	}

	return true, nil
}

func (r *ElfMachineReconciler) createPlacementGroup(ctx *context.MachineContext, placementGroupName, towerClusterID string) (*models.VMPlacementGroup, error) {
	placementGroupPolicy := towerresources.GetVMPlacementGroupPolicy(ctx.Machine)
	withTaskVMPlacementGroup, err := ctx.VMService.CreateVMPlacementGroup(placementGroupName, towerClusterID, placementGroupPolicy)
	if err != nil {
		return nil, err
	}

	task, err := ctx.VMService.WaitTask(*withTaskVMPlacementGroup.TaskID, config.WaitTaskTimeout, config.WaitTaskInterval)
	if err != nil {
		ctx.Logger.Info(fmt.Sprintf("Wait for placement group creation task done timed out in %s", config.WaitTaskTimeout), "placementGroup", placementGroupName, "taskID", *withTaskVMPlacementGroup.TaskID, "error", err)

		return nil, nil
	}

	if *task.Status == models.TaskStatusFAILED {
		return nil, errors.Errorf("failed to create placement group %s in task %s", placementGroupName, *task.ID)
	}

	ctx.Logger.Info("Creating placement group succeeded", "taskID", *task.ID, "placementGroup", placementGroupName)

	placementGroup, err := ctx.VMService.GetVMPlacementGroup(placementGroupName)
	if err != nil {
		return nil, err
	}

	return placementGroup, nil
}

func (r *ElfMachineReconciler) addVMsToPlacementGroup(ctx *context.MachineContext, placementGroup *models.VMPlacementGroup, vmIDs []string) (bool, error) {
	task, err := ctx.VMService.AddVMsToPlacementGroup(placementGroup, vmIDs)
	if err != nil {
		return false, err
	}

	taskID := *task.ID
	task, err = ctx.VMService.WaitTask(taskID, config.WaitTaskTimeout, config.WaitTaskInterval)
	if err != nil {
		ctx.Logger.Info(fmt.Sprintf("Wait for placement group updation task done timed out in %s", config.WaitTaskTimeout), "placementGroup", *placementGroup.Name, "taskID", taskID, "error", err)

		return false, nil
	}

	if *task.Status == models.TaskStatusFAILED {
		return false, errors.Errorf("failed to update placement group %s in task %s", *placementGroup.Name, taskID)
	}

	ctx.Logger.Info("Updating placement group succeeded", "taskID", taskID, "placementGroup", *placementGroup.Name)

	return true, nil
}

// getVMHostForRollingUpdate returns the target host server id for a virtual machine during rolling update.
// During KCP rolling update, machines will be deleted in the order of creation.
// Find the latest created machine in the placement group,
// and set the host where the machine is located to the first machine created by KCP rolling update.
// This prevents migration of virtual machine during KCP rolling update when using a placement group.
func (r *ElfMachineReconciler) getVMHostForRollingUpdate(ctx *context.MachineContext) (string, error) {
	if !machineutil.IsControlPlaneMachine(ctx.Machine) {
		return "", nil
	}

	kcp, err := machineutil.GetKCPByMachine(ctx, ctx.Client, ctx.Machine)
	if err != nil {
		return "", err
	}

	if *kcp.Spec.Replicas > kcp.Status.Replicas {
		// It means KCP is not in rolling update, but scaling out or being created. Then simply return.
		return "", nil
	}

	placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctx.Client, ctx.Machine)
	if err != nil {
		return "", err
	}
	placementGroup, err := ctx.VMService.GetVMPlacementGroup(placementGroupName)
	if err != nil {
		if service.IsVMPlacementGroupNotFound(err) {
			return "", nil
		}

		return "", err
	}

	towerCluster, err := ctx.VMService.GetCluster(ctx.ElfCluster.Spec.Cluster)
	if err != nil {
		return "", err
	}

	// Only when the placement group is full does it need to get the latest created machine.
	if len(placementGroup.Vms) < int(*towerCluster.HostNum) {
		return "", nil
	}

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
			ctx.Logger.Info("Set the host server for VM since the placement group is full", "hostID", *vm.Host.ID)

			return *vm.Host.ID, nil
		}
	}

	return "", nil
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

	nodeGroupName := machineutil.GetNodeGroupName(ctx.ElfMachine)
	payloads := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]string{
				infrav1.HostServerIDLabel:   ctx.ElfMachine.Status.HostServerRef,
				infrav1.HostServerNameLabel: ctx.ElfMachine.Status.HostServerName,
				infrav1.TowerVMIDLabel:      *vm.ID,
				infrav1.NodeGroupLabel:      nodeGroupName,
			},
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

// If the VM is powered on then issue requeues until all of the VM's
// networks have IP addresses.
func (r *ElfMachineReconciler) reconcileNetwork(ctx *context.MachineContext, vm *models.VM) bool {
	if vm.Ips == nil {
		return false
	}

	network := machineutil.GetNetworkStatus(*vm.Ips)
	if len(network) == 0 {
		return false
	}

	ctx.ElfMachine.Status.Network = network

	ipAddrs := make([]clusterv1.MachineAddress, 0, len(ctx.ElfMachine.Status.Network))
	for _, netStatus := range ctx.ElfMachine.Status.Network {
		ipAddrs = append(ipAddrs, clusterv1.MachineAddress{
			Type:    clusterv1.MachineInternalIP,
			Address: netStatus.IPAddrs[0],
		})
	}

	ctx.ElfMachine.Status.Addresses = ipAddrs

	return true
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
