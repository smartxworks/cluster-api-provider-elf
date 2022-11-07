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
	"github.com/smartxworks/cluster-api-provider-elf/pkg/label"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/util"
)

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/finalizers,verbs=update
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

	if ok, err := r.shouldWaitForELFCSIVMVolumeDetach(ctx); ok || err != nil {
		if err != nil {
			return ctrl.Result{}, err
		}
		ctx.Logger.Info("Waiting for VM volumes which created by ELF CSI to be detached", "elfMachine", ctx.ElfMachine.Name)
		return ctrl.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
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
		if !util.IsControlPlaneMachine(ctx.ElfMachine) && !conditions.IsTrue(ctx.Cluster, clusterv1.ControlPlaneInitializedCondition) {
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
	if err != nil {
		if service.IsVMNotFound(err) {
			if ctx.ElfMachine.IsFailed() {
				return reconcile.Result{}, nil
			}

			return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
		}

		return reconcile.Result{}, errors.Wrapf(err, "failed to reconcile VM")
	}
	if vm == nil || *vm.Status != models.VMStatusRUNNING || !util.IsUUID(ctx.ElfMachine.Status.VMRef) {
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

	// Reconcile node providerID
	if ok, err := r.reconcileNodeProviderID(ctx, vm); !ok {
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
//   1. Creating the VM with the bootstrap data if it does not exist, then...
//   2. Powering on the VM, and finally...
//   3. Returning the real-time state of the VM to the caller
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

		ctx.Logger.Info("Create VM for ElfMachine")

		withTaskVM, err := ctx.VMService.Clone(ctx.ElfCluster, ctx.Machine, ctx.ElfMachine, bootstrapData)
		if err != nil {
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

	vm, err := ctx.VMService.Get(ctx.ElfMachine.Status.VMRef)
	if err != nil {
		if !service.IsVMNotFound(err) {
			return nil, err
		}

		if util.IsUUID(ctx.ElfMachine.Status.VMRef) {
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
			ctx.ElfMachine.Status.FailureReason = capierrors.MachineStatusErrorPtr(capierrors.UpdateMachineError)
			ctx.ElfMachine.Status.FailureMessage = pointer.StringPtr(fmt.Sprintf("Unable to find VM by UUID %s. The VM was removed from infrastructure.", ctx.ElfMachine.Status.VMRef))
			ctx.Logger.Error(err, fmt.Sprintf("failed to get VM by UUID %s in %s", ctx.ElfMachine.Status.VMRef, infrav1.VMDisconnectionTimeout.String()), "message", ctx.ElfMachine.Status.FailureMessage)

			return nil, err
		}

		// Create VM failed

		if _, err := r.reconcileVMTask(ctx, nil); err != nil {
			return nil, err
		}

		// If create VM failed, tower deletes the VM
		ctx.ElfMachine.SetVM("")

		return nil, errors.Wrapf(err, "failed to create VM for ElfMachine %s/%s", ctx.ElfMachine.Namespace, ctx.ElfMachine.Name)
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

	// When ELF VM created, set UUID to VMRef
	if !util.IsUUID(ctx.ElfMachine.Status.VMRef) {
		ctx.ElfMachine.SetVM(*vm.LocalID)
	}

	// The newly created VM may need to powered off
	if *vm.Status == models.VMStatusSTOPPED {
		task, err := ctx.VMService.PowerOn(ctx.ElfMachine.Status.VMRef)
		if err != nil {
			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOnFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

			return nil, errors.Wrapf(err, "failed to trigger power on for VM %s", ctx)
		}

		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOnReason, clusterv1.ConditionSeverityInfo, "")

		ctx.ElfMachine.SetTask(*task.ID)

		ctx.Logger.Info("Waiting for VM to be powered on",
			"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)
	}

	return vm, nil
}

// shouldWaitForELFCSIVMVolumeDetach returns true if VM still have volumes which created by ELF CSI attached or cluster is being deleted.
// VM deletion and volume detach happen asynchronously, so VM shutdown or deletion and volume detach may occur at the same time
// this could cause issue for SMTX OS ELF, the volume which has been detached will be deleted together with the VM,
// so we need to check if all volumes are detached before deleting the VM.
func (r *ElfMachineReconciler) shouldWaitForELFCSIVMVolumeDetach(ctx *context.MachineContext) (bool, error) {
	// Return early if the cluster is being deleted.
	if !ctx.Cluster.DeletionTimestamp.IsZero() {
		return false, nil
	}

	// Get all VM Disk in this VM
	vmDisks, err := ctx.VMService.GetVMDisksByVMID(ctx.ElfMachine.Status.VMRef)
	if err != nil {
		return false, err
	}

	// Return early if the VM disks length is 0.
	if len(vmDisks) == 0 {
		return false, nil
	}

	// Get all labels which create by ELF CSI
	systemLabelsCreatedByELFCSI, err := ctx.VMService.GetLabelsByKey(infrav1.DefaultELFCSIVMVolumeClusterLabel)
	if err != nil {
		return false, err
	}

	// the label value created by ELF CSI like this sks.{{cluster.Namespace}}.{{cluster.Name}}.UUID
	// If label value contains {{cluster.Namespace}}.{{cluster.Name}},
	// we can make sure that the label is created by this cluster's ELF CSI.
	labelValueContains := fmt.Sprintf("%s.%s", ctx.Cluster.Namespace, ctx.Cluster.Name)
	currentClusterLabelCreatedByELFCSIMap := make(map[string]string)
	for i := 0; i < len(systemLabelsCreatedByELFCSI); i++ {
		if strings.Contains(*systemLabelsCreatedByELFCSI[i].Value, labelValueContains) {
			currentClusterLabelCreatedByELFCSIMap[*systemLabelsCreatedByELFCSI[i].ID] = *systemLabelsCreatedByELFCSI[i].ID
		}
	}

	if len(currentClusterLabelCreatedByELFCSIMap) == 0 {
		return false, fmt.Errorf("the label which create by ELF CSI in this cluster %s/%s is not found", ctx.Cluster.Namespace, ctx.Cluster.Name)
	}

	for i := 0; i < len(vmDisks); i++ {
		ok, err := r.isVMDiskAttachedByELFCSI(ctx, vmDisks[i], currentClusterLabelCreatedByELFCSIMap)
		if err != nil {
			return false, err
		}

		// if VM Disk is attached by ELF CSI,
		// should wait for VM Disk to be detached.
		if ok {
			return true, nil
		}
	}

	return false, nil
}

// isVMDiskAttachedByELFCSI return true if VM Disk attached by ELF CSI.
func (r *ElfMachineReconciler) isVMDiskAttachedByELFCSI(ctx *context.MachineContext, vmDisk *models.VMDisk,
	currentClusterLabelCreatedByELFCSIMap map[string]string) (bool, error) {
	// Skip VM Disk which type is CD_ROM
	if *vmDisk.Type == models.VMDiskTypeCDROM {
		return false, nil
	}

	if vmDisk.VMVolume == nil || vmDisk.ID == nil {
		return false, fmt.Errorf("failed to get VM volume associated with the VM Disk %s, because VM Disk VM Volume is nil", *vmDisk.ID)
	}

	vmVolume, err := ctx.VMService.GetVMVolumeByID(*vmDisk.VMVolume.ID)
	if err != nil {
		return false, err
	}

	// If the volume associated with the VM Disk has a label created by the cluster's ELF CSI,
	// we can make sure that the volume was created by ELF CSI,
	// this is means the VM Disk is attached by ELF CSI.
	for j := 0; j < len(vmVolume.Labels); j++ {
		if _, ok := currentClusterLabelCreatedByELFCSIMap[*vmVolume.Labels[j].ID]; ok {
			return true, nil
		}
	}

	return false, nil
}

func (r *ElfMachineReconciler) reconcileVMTask(ctx *context.MachineContext, vm *models.VM) (bool, error) {
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

	switch *task.Status {
	case models.TaskStatusFAILED:
		errorMessage := ""
		if task.ErrorMessage != nil {
			errorMessage = *task.ErrorMessage
		}

		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.TaskFailureReason, clusterv1.ConditionSeverityInfo, errorMessage)

		if service.IsCloudInitConfigError(errorMessage) {
			ctx.ElfMachine.Status.FailureReason = capierrors.MachineStatusErrorPtr(capierrors.CreateMachineError)
			ctx.ElfMachine.Status.FailureMessage = pointer.StringPtr(fmt.Sprintf("VM cloud-init config error: %s", service.FormatCloudInitError(errorMessage)))
		}

		ctx.ElfMachine.SetTask("")

		ctx.Logger.Error(errors.New("VM task failed"), "", "vmRef", vmRef, "taskRef", taskRef, "message", errorMessage)

		return true, nil
	case models.TaskStatusSUCCESSED:
		ctx.ElfMachine.SetTask("")

		ctx.Logger.Info("VM task successful", "vmRef", vmRef, "taskRef", taskRef)

		return true, nil
	default:
		status := ""
		if task.Status != nil {
			status = string(*task.Status)
		}

		ctx.Logger.Info("Waiting for VM task done", "vmRef", vmRef, "taskRef", taskRef, "taskStatus", status)
	}

	return false, nil
}

func (r *ElfMachineReconciler) reconcileProviderID(ctx *context.MachineContext, vm *models.VM) error {
	providerID := util.ConvertUUIDToProviderID(*vm.LocalID)
	if providerID == "" {
		return errors.Errorf("invalid VM UUID %s from %s %s/%s for %s",
			*vm.LocalID,
			ctx.ElfCluster.GroupVersionKind(),
			ctx.ElfCluster.GetNamespace(),
			ctx.ElfCluster.GetName(),
			ctx)
	}

	if ctx.ElfMachine.Spec.ProviderID == nil || *ctx.ElfMachine.Spec.ProviderID != providerID {
		ctx.ElfMachine.Spec.ProviderID = pointer.StringPtr(providerID)

		ctx.Logger.Info("updated providerID", "providerID", providerID)
	}

	return nil
}

// ELF without cloud provider.
func (r *ElfMachineReconciler) reconcileNodeProviderID(ctx *context.MachineContext, vm *models.VM) (bool, error) {
	providerID := util.ConvertUUIDToProviderID(*vm.LocalID)
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
		return false, errors.Wrapf(err,
			"failed to get client for Cluster %s/%s",
			ctx.Cluster.Namespace, ctx.Cluster.Name,
		)
	}

	node, err := kubeClient.CoreV1().Nodes().Get(ctx, ctx.ElfMachine.Name, metav1.GetOptions{})
	if err != nil {
		return false, errors.Wrapf(err,
			"waiting for node add providerID k8s cluster %s/%s/%s",
			ctx.Cluster.Namespace, ctx.Cluster.Name, ctx.ElfMachine.Name,
		)
	}

	if node.Spec.ProviderID != "" {
		return true, nil
	}

	node.Spec.ProviderID = providerID
	var payloads []interface{}
	payloads = append(payloads,
		infrav1.PatchStringValue{
			Op:    "add",
			Path:  "/spec/providerID",
			Value: providerID,
		})
	payloadBytes, err := json.Marshal(payloads)
	if err != nil {
		return false, err
	}
	_, err = kubeClient.CoreV1().Nodes().Patch(ctx, node.Name, apitypes.JSONPatchType, payloadBytes, metav1.PatchOptions{})
	if err != nil {
		return false, err
	}

	ctx.Logger.Info("Set node providerID success",
		"cluster", ctx.Cluster.Name,
		"node", node.Name,
		"providerID", providerID)

	return true, nil
}

// If the VM is powered on then issue requeues until all of the VM's
// networks have IP addresses.
func (r *ElfMachineReconciler) reconcileNetwork(ctx *context.MachineContext, vm *models.VM) bool {
	if vm.Ips == nil {
		return false
	}

	network := util.GetNetworkStatus(*vm.Ips)
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
	creatorLabel, err := ctx.VMService.UpsertLabel(label.GetVMLabelManaged(), "true")
	if err != nil {
		return false, errors.Wrapf(err, "failed to upsert label "+label.GetVMLabelManaged())
	}
	namespaceLabel, err := ctx.VMService.UpsertLabel(label.GetVMLabelNamespace(), ctx.ElfMachine.Namespace)
	if err != nil {
		return false, errors.Wrapf(err, "failed to upsert label "+label.GetVMLabelNamespace())
	}
	clusterNameLabel, err := ctx.VMService.UpsertLabel(label.GetVMLabelClusterName(), ctx.ElfCluster.Name)
	if err != nil {
		return false, errors.Wrapf(err, "failed to upsert label "+label.GetVMLabelClusterName())
	}

	var vipLabel *models.Label
	if util.IsControlPlaneMachine(ctx.ElfMachine) {
		vipLabel, err = ctx.VMService.UpsertLabel(label.GetVMLabelVIP(), ctx.ElfCluster.Spec.ControlPlaneEndpoint.Host)
		if err != nil {
			return false, errors.Wrapf(err, "failed to upsert label "+label.GetVMLabelVIP())
		}
	}

	labelIDs := []string{*namespaceLabel.ID, *clusterNameLabel.ID, *creatorLabel.ID}
	if util.IsControlPlaneMachine(ctx.ElfMachine) {
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
