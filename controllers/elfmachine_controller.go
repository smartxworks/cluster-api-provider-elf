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
	"time"

	"github.com/pkg/errors"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
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
	typesutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/types"
)

const failedToUpsertLabelMsg = "failed to upsert label"

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/finalizers,verbs=update
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachinetemplates,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments;machinedeployments/status,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;patch
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch

// ElfMachineReconciler reconciles an ElfMachine object.
type ElfMachineReconciler struct {
	*context.ControllerManagerContext
	NewVMService service.NewVMServiceFunc
}

// AddMachineControllerToManager adds the machine controller to the provided
// manager.
func AddMachineControllerToManager(ctx goctx.Context, ctrlMgrCtx *context.ControllerManagerContext, mgr ctrlmgr.Manager, options controller.Options) error {
	var (
		controlledType     = &infrav1.ElfMachine{}
		controlledTypeName = reflect.TypeOf(controlledType).Elem().Name()
		controlledTypeGVK  = infrav1.GroupVersion.WithKind(controlledTypeName)
	)

	reconciler := &ElfMachineReconciler{
		ControllerManagerContext: ctrlMgrCtx,
		NewVMService:             service.NewVMService,
	}
	predicateLog := ctrl.LoggerFrom(ctx).WithValues("controller", "elfmachine")

	return ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(controlledType).
		// Watch the CAPI resource that owns this infrastructure resource.
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(capiutil.MachineToInfrastructureMapFunc(controlledTypeGVK)),
		).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(mgr.GetScheme(), predicateLog, ctrlMgrCtx.WatchFilterValue)).
		Complete(reconciler)
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r *ElfMachineReconciler) Reconcile(ctx goctx.Context, req ctrl.Request) (result ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	// Get the ElfMachine resource for this request.
	var elfMachine infrav1.ElfMachine
	if err := r.Client.Get(ctx, req.NamespacedName, &elfMachine); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ElfMachine not found, won't reconcile", "key", req.NamespacedName)

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	// Fetch the CAPI Machine.
	machine, err := capiutil.GetOwnerMachine(ctx, r.Client, elfMachine.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if machine == nil {
		log.Info("Waiting for Machine Controller to set OwnerRef on ElfMachine")

		return reconcile.Result{}, nil
	}
	log = log.WithValues("Machine", klog.KObj(machine))
	ctx = ctrl.LoggerInto(ctx, log)

	// Fetch the CAPI Cluster.
	cluster, err := capiutil.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		log.Info("Machine is missing cluster label or cluster does not exist")

		return reconcile.Result{}, err
	}
	if cluster == nil {
		log.Info(fmt.Sprintf("Please associate this machine with a cluster using the label %s: <name of cluster>", clusterv1.ClusterNameLabel))

		return ctrl.Result{}, nil
	}
	log = log.WithValues("Cluster", klog.KObj(cluster))
	ctx = ctrl.LoggerInto(ctx, log)

	if annotations.IsPaused(cluster, &elfMachine) {
		log.V(4).Info("ElfMachine linked to a cluster that is paused")

		return reconcile.Result{}, nil
	}

	// Fetch the ElfCluster
	var elfCluster infrav1.ElfCluster
	elfClusterName := client.ObjectKey{
		Namespace: elfMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(ctx, elfClusterName, &elfCluster); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ElfMachine Waiting for ElfCluster")

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}
	log = log.WithValues("ElfCluster", klog.KObj(&elfCluster))
	ctx = ctrl.LoggerInto(ctx, log)

	// Create the patch helper.
	patchHelper, err := patch.NewHelper(&elfMachine, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to init patch helper")
	}

	// Create the machine context for this request.
	machineCtx := &context.MachineContext{
		Cluster:     cluster,
		ElfCluster:  &elfCluster,
		Machine:     machine,
		ElfMachine:  &elfMachine,
		PatchHelper: patchHelper,
	}

	// If ElfMachine is being deleting and ElfCLuster ForceDeleteCluster flag is set, skip creating the VMService object,
	// because Tower server may be out of service. So we can force delete ElfCluster.
	if elfMachine.ObjectMeta.DeletionTimestamp.IsZero() || !elfCluster.HasForceDeleteCluster() {
		vmService, err := r.NewVMService(ctx, elfCluster.GetTower(), log)
		if err != nil {
			conditions.MarkFalse(&elfMachine, infrav1.TowerAvailableCondition, infrav1.TowerUnreachableReason, clusterv1.ConditionSeverityError, err.Error())

			return reconcile.Result{}, err
		}
		conditions.MarkTrue(&elfMachine, infrav1.TowerAvailableCondition)

		machineCtx.VMService = vmService
	}

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
		// always update the readyCondition.
		conditions.SetSummary(machineCtx.ElfMachine,
			conditions.WithConditions(
				infrav1.ResourcesHotUpdatedCondition,
				infrav1.VMProvisionedCondition,
				infrav1.TowerAvailableCondition,
			),
		)

		// Patch the ElfMachine resource.
		if err := machineCtx.Patch(ctx); err != nil {
			if reterr == nil {
				reterr = err
			}

			log.Error(err, "patch failed", "elfMachine", machineCtx.String())
		}

		// If the node's healthy condition is unknown, the virtual machine may
		// have been shut down through Tower or directly on the virtual machine.
		// We need to try to reconcile to ensure that the virtual machine is powered on.
		if err == nil && result.IsZero() &&
			!machineutil.IsMachineFailed(machineCtx.Machine) &&
			machineCtx.Machine.DeletionTimestamp.IsZero() &&
			machineCtx.ElfMachine.DeletionTimestamp.IsZero() &&
			machineutil.IsNodeHealthyConditionUnknown(machineCtx.Machine) {
			lastTransitionTime := conditions.GetLastTransitionTime(machineCtx.Machine, clusterv1.MachineNodeHealthyCondition)
			if lastTransitionTime != nil && time.Now().Before(lastTransitionTime.Add(config.Task.VMPowerStatusCheckingDuration)) {
				result.RequeueAfter = config.Cape.DefaultRequeueTimeout

				log.Info(fmt.Sprintf("The node's healthy condition is unknown, virtual machine may have been shut down, will reconcile after %s", result.RequeueAfter), "nodeConditionUnknownTime", lastTransitionTime)
			}
		}
	}()

	// Handle deleted machines
	if !elfMachine.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, machineCtx)
	}

	// Handle non-deleted machines
	return r.reconcileNormal(ctx, machineCtx)
}

func (r *ElfMachineReconciler) reconcileDeleteVM(ctx goctx.Context, machineCtx *context.MachineContext) error {
	log := ctrl.LoggerFrom(ctx)

	vm, err := machineCtx.VMService.Get(machineCtx.ElfMachine.Status.VMRef)
	if err != nil {
		if service.IsVMNotFound(err) {
			log.Info("VM already deleted")

			machineCtx.ElfMachine.SetVM("")
		}

		return err
	}

	if ok, err := r.reconcileVMTask(ctx, machineCtx, vm); err != nil {
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
		if service.IsShutDownTimeout(conditions.GetMessage(machineCtx.ElfMachine, infrav1.VMProvisionedCondition)) ||
			!(machineCtx.ElfMachine.RequiresVGPUDevices() || machineCtx.ElfCluster.Spec.VMGracefulShutdown) {
			task, err = machineCtx.VMService.PowerOff(machineCtx.ElfMachine.Status.VMRef)
		} else {
			task, err = machineCtx.VMService.ShutDown(machineCtx.ElfMachine.Status.VMRef)
		}

		if err != nil {
			return err
		}

		machineCtx.ElfMachine.SetTask(*task.ID)

		log.Info("Waiting for VM shut down",
			"vmRef", machineCtx.ElfMachine.Status.VMRef, "taskRef", machineCtx.ElfMachine.Status.TaskRef)

		return nil
	}

	// Before destroying VM, attempt to delete kubernetes node.
	err = r.deleteNode(ctx, machineCtx, machineCtx.ElfMachine.Name)
	if err != nil {
		return err
	}

	log.Info("Destroying VM",
		"vmRef", machineCtx.ElfMachine.Status.VMRef, "taskRef", machineCtx.ElfMachine.Status.TaskRef)

	// Delete the VM
	task, err := machineCtx.VMService.Delete(machineCtx.ElfMachine.Status.VMRef)
	if err != nil {
		return err
	} else {
		machineCtx.ElfMachine.SetTask(*task.ID)
	}

	log.Info("Waiting for VM to be deleted",
		"vmRef", machineCtx.ElfMachine.Status.VMRef, "taskRef", machineCtx.ElfMachine.Status.TaskRef)

	return nil
}

func (r *ElfMachineReconciler) reconcileDelete(ctx goctx.Context, machineCtx *context.MachineContext) (reconcile.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling ElfMachine delete")

	conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, clusterv1.DeletingReason, clusterv1.ConditionSeverityInfo, "")

	defer func() {
		// When deleting a virtual machine, the GPU device
		// locked by the virtual machine may not be unlocked.
		// For example, the Cluster or ElfMachine was deleted during a pause.
		if !ctrlutil.ContainsFinalizer(machineCtx.ElfMachine, infrav1.MachineFinalizer) &&
			machineCtx.ElfMachine.RequiresGPUDevices() {
			unlockGPUDevicesLockedByVM(machineCtx.ElfCluster.Spec.Cluster, machineCtx.ElfMachine.Name)
		}
	}()

	if ok, err := r.deletePlacementGroup(ctx, machineCtx); err != nil {
		return reconcile.Result{}, err
	} else if !ok {
		return reconcile.Result{RequeueAfter: config.Cape.DefaultRequeueTimeout}, nil
	}

	// if cluster need to force delete, skipping VM deletion and remove the finalizer.
	if machineCtx.ElfCluster.HasForceDeleteCluster() {
		log.Info("Skip VM deletion due to the force-delete-cluster annotation")

		ctrlutil.RemoveFinalizer(machineCtx.ElfMachine, infrav1.MachineFinalizer)
		return reconcile.Result{}, nil
	}

	if !machineCtx.ElfMachine.HasVM() {
		// ElfMachine may not have saved the created virtual machine when deleting ElfMachine
		vm, err := machineCtx.VMService.GetByName(machineCtx.ElfMachine.Name)
		if err != nil {
			if !service.IsVMNotFound(err) {
				return reconcile.Result{}, err
			}

			log.Info("VM already deleted")

			ctrlutil.RemoveFinalizer(machineCtx.ElfMachine, infrav1.MachineFinalizer)

			return reconcile.Result{}, nil
		}

		machineCtx.ElfMachine.SetVM(util.GetVMRef(vm))
	}

	if result, err := r.deleteDuplicateVMs(ctx, machineCtx); err != nil || !result.IsZero() {
		return result, err
	}

	err := r.reconcileDeleteVM(ctx, machineCtx)
	if err != nil {
		if service.IsVMNotFound(err) {
			// The VM is deleted so remove the finalizer.
			ctrlutil.RemoveFinalizer(machineCtx.ElfMachine, infrav1.MachineFinalizer)

			return reconcile.Result{}, nil
		}

		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, clusterv1.DeletionFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return reconcile.Result{}, err
	}

	log.Info("Waiting for VM to be deleted",
		"vmRef", machineCtx.ElfMachine.Status.VMRef, "taskRef", machineCtx.ElfMachine.Status.TaskRef)

	return reconcile.Result{RequeueAfter: config.Cape.DefaultRequeueTimeout}, nil
}

func (r *ElfMachineReconciler) reconcileNormal(ctx goctx.Context, machineCtx *context.MachineContext) (reconcile.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// If the ElfMachine is in an error state, return early.
	if machineCtx.ElfMachine.IsFailed() {
		log.Info("Error state detected, skipping reconciliation")

		return reconcile.Result{}, nil
	}

	// If the ElfMachine doesn't have our finalizer, add it.
	if !ctrlutil.ContainsFinalizer(machineCtx.ElfMachine, infrav1.MachineFinalizer) {
		return reconcile.Result{RequeueAfter: config.Cape.DefaultRequeueTimeout}, patchutil.AddFinalizerWithOptimisticLock(ctx, r.Client, machineCtx.ElfMachine, infrav1.MachineFinalizer)
	}

	// If ElfMachine requires static IPs for devices, should wait for CAPE-IP to set MachineStaticIPFinalizer first
	// to prevent CAPE from overwriting MachineStaticIPFinalizer when setting MachineFinalizer.
	// If ElfMachine happens to be deleted at this time, CAPE-IP may not have time to release the IPs.
	if machineCtx.ElfMachine.Spec.Network.RequiresStaticIPs() && !ctrlutil.ContainsFinalizer(machineCtx.ElfMachine, infrav1.MachineStaticIPFinalizer) {
		log.V(2).Info("Waiting for CAPE-IP to set MachineStaticIPFinalizer on ElfMachine")

		return reconcile.Result{RequeueAfter: config.Cape.DefaultRequeueTimeout}, nil
	}

	if !machineCtx.Cluster.Status.InfrastructureReady {
		log.Info("Cluster infrastructure is not ready yet")

		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForClusterInfrastructureReason, clusterv1.ConditionSeverityInfo, "")

		return reconcile.Result{}, nil
	}

	// Make sure bootstrap data is available and populated.
	if machineCtx.Machine.Spec.Bootstrap.DataSecretName == nil {
		if !machineutil.IsControlPlaneMachine(machineCtx.ElfMachine) && !conditions.IsTrue(machineCtx.Cluster, clusterv1.ControlPlaneInitializedCondition) {
			log.Info("Waiting for the control plane to be initialized")

			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, clusterv1.WaitingForControlPlaneAvailableReason, clusterv1.ConditionSeverityInfo, "")

			return ctrl.Result{}, nil
		}

		log.Info("Waiting for bootstrap data to be available")

		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForBootstrapDataReason, clusterv1.ConditionSeverityInfo, "")

		return reconcile.Result{}, nil
	}

	if result, err := r.reconcilePlacementGroup(ctx, machineCtx); err != nil || !result.IsZero() {
		return result, err
	}

	if r.isWaitingForStaticIPAllocation(machineCtx) {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForStaticIPAllocationReason, clusterv1.ConditionSeverityInfo, "")
		log.Info("VM is waiting for static ip to be available")
		return reconcile.Result{}, nil
	}

	vm, ok, err := r.reconcileVM(ctx, machineCtx)
	switch {
	case machineCtx.ElfMachine.IsFailed():
		return reconcile.Result{}, nil
	case err != nil:
		log.Error(err, "failed to reconcile VM")

		if service.IsVMNotFound(err) {
			return reconcile.Result{RequeueAfter: config.Cape.DefaultRequeueTimeout}, nil
		}

		return reconcile.Result{}, errors.Wrapf(err, "failed to reconcile VM")
	case !ok || machineCtx.ElfMachine.HasTask():
		return reconcile.Result{RequeueAfter: config.Cape.DefaultRequeueTimeout}, nil
	}

	// Reconcile the ElfMachine's Labels using the cluster info
	if ok, err := r.reconcileLabels(ctx, machineCtx, vm); !ok {
		return reconcile.Result{}, errors.Wrapf(err, "failed to reconcile labels")
	}

	// Reconcile the ElfMachine's providerID using the VM's UUID.
	if err := r.reconcileProviderID(ctx, machineCtx, vm); err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "unexpected error while reconciling providerID for %s", machineCtx)
	}

	// Reconcile the ElfMachine's node addresses from the VM's IP addresses.
	if ok, err := r.reconcileNetwork(ctx, machineCtx, vm); err != nil {
		return reconcile.Result{}, err
	} else if !ok {
		return reconcile.Result{RequeueAfter: config.Cape.DefaultRequeueTimeout}, nil
	}

	machineCtx.ElfMachine.Status.Ready = true
	conditions.MarkTrue(machineCtx.ElfMachine, infrav1.VMProvisionedCondition)

	if ok, err := r.reconcileNode(ctx, machineCtx, vm); !ok {
		if err != nil {
			return reconcile.Result{}, err
		}

		log.Info("Node providerID is not reconciled")

		return reconcile.Result{RequeueAfter: config.Cape.DefaultRequeueTimeout}, nil
	}

	if result, err := r.deleteDuplicateVMs(ctx, machineCtx); err != nil || !result.IsZero() {
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
func (r *ElfMachineReconciler) reconcileVM(ctx goctx.Context, machineCtx *context.MachineContext) (*models.VM, bool, error) {
	log := ctrl.LoggerFrom(ctx)

	// If there is no vmRef then no VM exists, create one
	if !machineCtx.ElfMachine.HasVM() {
		// We are setting this condition only in case it does not exists so we avoid to get flickering LastConditionTime
		// in case of cloning errors or powering on errors.
		if !conditions.Has(machineCtx.ElfMachine, infrav1.VMProvisionedCondition) {
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.CloningReason, clusterv1.ConditionSeverityInfo, "")
		}

		bootstrapData, err := r.getBootstrapData(ctx, machineCtx)
		if err != nil {
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.CloningFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

			return nil, false, err
		}
		if bootstrapData == "" {
			return nil, false, errors.New("bootstrapData is empty")
		}

		if ok, message, err := isELFScheduleVMErrorRecorded(ctx, machineCtx, r.Client); err != nil {
			return nil, false, err
		} else if ok {
			if canRetry, err := canRetryVMOperation(ctx, machineCtx, r.Client); err != nil {
				return nil, false, err
			} else if !canRetry {
				log.V(1).Info(message + ", skip creating VM")
				return nil, false, nil
			}

			log.V(1).Info(message + " and the retry silence period passes, will try to create the VM again")
		}

		if ok, msg := acquireTicketForCreateVM(machineCtx.ElfMachine.Name, machineutil.IsControlPlaneMachine(machineCtx.ElfMachine)); !ok {
			log.V(1).Info(msg + ", skip creating VM")
			return nil, false, nil
		}

		var hostID *string
		var gpuDeviceInfos []*service.GPUDeviceInfo
		// The virtual machine of the Control Plane does not support GPU Devices.
		if machineutil.IsControlPlaneMachine(machineCtx.Machine) {
			hostID, err = r.preCheckPlacementGroup(ctx, machineCtx)
			if err != nil || hostID == nil {
				releaseTicketForCreateVM(machineCtx.ElfMachine.Name)
				return nil, false, err
			}
		} else {
			hostID, gpuDeviceInfos, err = r.selectHostAndGPUsForVM(ctx, machineCtx, "")
			if err != nil || hostID == nil {
				releaseTicketForCreateVM(machineCtx.ElfMachine.Name)
				return nil, false, err
			}
		}

		log.Info("Create VM for ElfMachine")

		withTaskVM, err := machineCtx.VMService.Clone(machineCtx.ElfCluster, machineCtx.ElfMachine, bootstrapData, *hostID, gpuDeviceInfos)
		if err != nil {
			releaseTicketForCreateVM(machineCtx.ElfMachine.Name)

			if service.IsVMDuplicate(err) {
				vm, err := machineCtx.VMService.GetByName(machineCtx.ElfMachine.Name)
				if err != nil {
					return nil, false, err
				}

				machineCtx.ElfMachine.SetVM(util.GetVMRef(vm))
			} else {
				// Duplicate VM error does not require unlocking GPU devices.
				if machineCtx.ElfMachine.RequiresGPUDevices() {
					unlockGPUDevicesLockedByVM(machineCtx.ElfCluster.Spec.Cluster, machineCtx.ElfMachine.Name)
				}

				log.Error(err, "failed to create VM",
					"vmRef", machineCtx.ElfMachine.Status.VMRef, "taskRef", machineCtx.ElfMachine.Status.TaskRef)

				conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.CloningFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

				return nil, false, err
			}
		} else {
			machineCtx.ElfMachine.SetVM(*withTaskVM.Data.ID)
			machineCtx.ElfMachine.SetTask(*withTaskVM.TaskID)
		}
	}

	vm, err := r.getVM(ctx, machineCtx)
	if err != nil {
		return nil, false, err
	}

	// Remove VM disconnection timestamp
	vmDisconnectionTimestamp := machineCtx.ElfMachine.GetVMDisconnectionTimestamp()
	if vmDisconnectionTimestamp != nil {
		machineCtx.ElfMachine.SetVMDisconnectionTimestamp(nil)

		log.Info("The VM was found again", "vmRef", machineCtx.ElfMachine.Status.VMRef, "disconnectionTimestamp", vmDisconnectionTimestamp.Format(time.RFC3339))
	}

	if ok, err := r.reconcileVMTask(ctx, machineCtx, vm); err != nil {
		return nil, false, err
	} else if !ok {
		return vm, false, nil
	}

	vmRef := util.GetVMRef(vm)
	// If vmRef is in UUID format, it means that the ELF VM created.
	if !typesutil.IsUUID(vmRef) {
		log.Info("The VM is being created", "vmRef", vmRef)

		return vm, false, nil
	}

	// When ELF VM created, set UUID to VMRef
	if !typesutil.IsUUID(machineCtx.ElfMachine.Status.VMRef) {
		machineCtx.ElfMachine.SetVM(vmRef)
	}

	if err := r.reconcileHostAndZone(ctx, machineCtx, vm); err != nil {
		return nil, false, err
	}

	// The VM was moved to the recycle bin. Treat the VM as deleted, and will not reconganize it even if it's moved back from the recycle bin.
	if service.IsVMInRecycleBin(vm) {
		message := fmt.Sprintf("The VM %s was moved to the Tower recycle bin by users, so treat it as deleted.", machineCtx.ElfMachine.Status.VMRef)
		machineCtx.ElfMachine.Status.FailureReason = ptr.To(capeerrors.MovedToRecycleBinError)
		machineCtx.ElfMachine.Status.FailureMessage = ptr.To(message)
		machineCtx.ElfMachine.SetVM("")
		log.Error(stderrors.New(message), "")

		return vm, false, nil
	}

	// Before the virtual machine is powered on, put the virtual machine into the specified placement group.
	if ok, err := r.joinPlacementGroup(ctx, machineCtx, vm); err != nil || !ok {
		return vm, false, err
	}

	if ok, err := r.reconcileGPUDevices(ctx, machineCtx, vm); err != nil || !ok {
		return vm, false, err
	}

	if ok, err := r.reconcileVMStatus(ctx, machineCtx, vm); err != nil || !ok {
		return vm, false, err
	}

	if ok, err := r.reconcileVMResources(ctx, machineCtx, vm); err != nil || !ok {
		return vm, false, err
	}

	return vm, true, nil
}

func (r *ElfMachineReconciler) getVM(ctx goctx.Context, machineCtx *context.MachineContext) (*models.VM, error) {
	log := ctrl.LoggerFrom(ctx)

	vm, err := machineCtx.VMService.Get(machineCtx.ElfMachine.Status.VMRef)
	if err == nil {
		return vm, nil
	}

	if !service.IsVMNotFound(err) {
		return nil, err
	}

	if typesutil.IsUUID(machineCtx.ElfMachine.Status.VMRef) {
		vmDisconnectionTimestamp := machineCtx.ElfMachine.GetVMDisconnectionTimestamp()
		if vmDisconnectionTimestamp == nil {
			now := metav1.Now()
			vmDisconnectionTimestamp = &now
			machineCtx.ElfMachine.SetVMDisconnectionTimestamp(vmDisconnectionTimestamp)
		}

		// The machine may only be temporarily disconnected before timeout
		if !vmDisconnectionTimestamp.Add(infrav1.VMDisconnectionTimeout).Before(time.Now()) {
			log.Error(err, "the VM has been disconnected, will try to reconnect", "vmRef", machineCtx.ElfMachine.Status.VMRef, "disconnectionTimestamp", vmDisconnectionTimestamp.Format(time.RFC3339))

			return nil, err
		}

		// If the machine was not found by UUID and timed out it means that it got deleted directly
		machineCtx.ElfMachine.Status.FailureReason = ptr.To(capeerrors.RemovedFromInfrastructureError)
		machineCtx.ElfMachine.Status.FailureMessage = ptr.To(fmt.Sprintf("Unable to find VM by UUID %s. The VM was removed from infrastructure.", machineCtx.ElfMachine.Status.VMRef))
		log.Error(err, fmt.Sprintf("failed to get VM by UUID %s in %s", machineCtx.ElfMachine.Status.VMRef, infrav1.VMDisconnectionTimeout.String()), "message", machineCtx.ElfMachine.Status.FailureMessage)

		return nil, err
	}

	// Create VM failed

	if _, err := r.reconcileVMTask(ctx, machineCtx, nil); err != nil {
		return nil, err
	}

	// If Tower fails to create VM, the temporary DB record for this VM will be deleted.
	machineCtx.ElfMachine.SetVM("")

	return nil, errors.Wrapf(err, "failed to create VM for ElfMachine %s", klog.KObj(machineCtx.ElfMachine))
}

// reconcileVMStatus ensures the VM is in Running status and configured as expected.
// 1. VM in STOPPED status will be powered on.
// 2. VM in SUSPENDED status will be powered off, then powered on in future reconcile.
// 3. Set VM configuration to the expected values.
//
// The return value:
// 1. true means that the VM is in Running status.
// 2. false and error is nil means the VM is not in Running status.
func (r *ElfMachineReconciler) reconcileVMStatus(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	if vm.Status == nil {
		log.Info("The status of VM is an unexpected value nil", "vmRef", machineCtx.ElfMachine.Status.VMRef)

		return false, nil
	}

	updatedVMRestrictedFields := service.GetUpdatedVMRestrictedFields(vm, machineCtx.ElfMachine)
	switch *vm.Status {
	case models.VMStatusRUNNING:
		if len(updatedVMRestrictedFields) > 0 && towerresources.IsAllowCustomVMConfig() {
			// If VM shutdown timed out, simply power off the VM.
			if service.IsShutDownTimeout(conditions.GetMessage(machineCtx.ElfMachine, infrav1.VMProvisionedCondition)) {
				log.Info("The VM configuration has been modified, power off the VM first and then restore the VM configuration", "vmRef", machineCtx.ElfMachine.Status.VMRef, "updatedVMRestrictedFields", updatedVMRestrictedFields)

				return false, r.powerOffVM(ctx, machineCtx)
			} else {
				log.Info("The VM configuration has been modified, shut down the VM first and then restore the VM configuration", "vmRef", machineCtx.ElfMachine.Status.VMRef, "updatedVMRestrictedFields", updatedVMRestrictedFields)

				return false, r.shutDownVM(ctx, machineCtx)
			}
		}

		return true, nil
	case models.VMStatusSTOPPED:
		if len(updatedVMRestrictedFields) > 0 && towerresources.IsAllowCustomVMConfig() {
			log.Info("The VM configuration has been modified, and the VM is stopped, just restore the VM configuration to expected values", "vmRef", machineCtx.ElfMachine.Status.VMRef, "updatedVMRestrictedFields", updatedVMRestrictedFields)

			return false, r.updateVM(ctx, machineCtx, vm)
		}

		// Before the virtual machine is started for the first time, if the
		// current disk capacity of the virtual machine is smaller than expected,
		// expand the disk capacity first and then start it. cloud-init will
		// add the new disk capacity to root.
		if machineCtx.ElfMachine.GetVMFirstBootTimestamp() == nil &&
			!machineCtx.ElfMachine.IsHotUpdating() {
			if ok, err := r.reconcieVMVolume(ctx, machineCtx, vm, infrav1.VMProvisionedCondition); err != nil || !ok {
				return ok, err
			}
		}

		return false, r.powerOnVM(ctx, machineCtx, vm)
	case models.VMStatusSUSPENDED:
		// In some abnormal conditions, the VM will be in a suspended state,
		// e.g. wrong settings in VM or an exception occurred in the Guest OS.
		// try to 'Power off VM -> Power on VM' resumes the VM from a suspended state.
		// See issue http://jira.smartx.com/browse/SKS-1351 for details.
		return false, r.powerOffVM(ctx, machineCtx)
	default:
		log.Info("The VM is in an unexpected status "+string(*vm.Status), "vmRef", machineCtx.ElfMachine.Status.VMRef)

		return false, nil
	}
}

func (r *ElfMachineReconciler) shutDownVM(ctx goctx.Context, machineCtx *context.MachineContext) error {
	log := ctrl.LoggerFrom(ctx)

	if ok := acquireTicketForUpdatingVM(machineCtx.ElfMachine.Name); !ok {
		log.V(1).Info("The VM operation reaches rate limit, skip shut down VM")

		return nil
	}

	task, err := machineCtx.VMService.ShutDown(machineCtx.ElfMachine.Status.VMRef)
	if err != nil {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.ShuttingDownFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return errors.Wrapf(err, "failed to trigger shut down for VM %s", ctx)
	}

	conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.ShuttingDownReason, clusterv1.ConditionSeverityInfo, "")

	machineCtx.ElfMachine.SetTask(*task.ID)

	log.Info("Waiting for VM to be shut down", "vmRef", machineCtx.ElfMachine.Status.VMRef, "taskRef", machineCtx.ElfMachine.Status.TaskRef)

	return nil
}

func (r *ElfMachineReconciler) powerOffVM(ctx goctx.Context, machineCtx *context.MachineContext) error {
	log := ctrl.LoggerFrom(ctx)

	if ok := acquireTicketForUpdatingVM(machineCtx.ElfMachine.Name); !ok {
		log.V(1).Info("The VM operation reaches rate limit, skip powering off VM " + machineCtx.ElfMachine.Status.VMRef)

		return nil
	}

	task, err := machineCtx.VMService.PowerOff(machineCtx.ElfMachine.Status.VMRef)
	if err != nil {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOffFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return errors.Wrapf(err, "failed to trigger powering off for VM %s", machineCtx.ElfMachine.Status.VMRef)
	}

	conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PowerOffReason, clusterv1.ConditionSeverityInfo, "")

	machineCtx.ElfMachine.SetTask(*task.ID)

	log.Info("Waiting for VM to be powered off", "vmRef", machineCtx.ElfMachine.Status.VMRef, "taskRef", machineCtx.ElfMachine.Status.TaskRef)

	return nil
}

func (r *ElfMachineReconciler) powerOnVM(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) error {
	log := ctrl.LoggerFrom(ctx)

	if ok, message, err := isELFScheduleVMErrorRecorded(ctx, machineCtx, r.Client); err != nil {
		return err
	} else if ok {
		if canRetry, err := canRetryVMOperation(ctx, machineCtx, r.Client); err != nil {
			return err
		} else if !canRetry {
			log.V(1).Info(fmt.Sprintf("%s, skip powering on VM %s", message, machineCtx.ElfMachine.Status.VMRef))

			return nil
		}

		log.V(1).Info(message + " and the retry silence period passes, will try to power on the VM again")
	}

	if ok := acquireTicketForUpdatingVM(machineCtx.ElfMachine.Name); !ok {
		log.V(1).Info("The VM operation reaches rate limit, skip power on VM " + machineCtx.ElfMachine.Status.VMRef)

		return nil
	}

	hostID := ""
	// Starting a virtual machine with GPU/vGPU does not support automatic scheduling,
	// and need to specify the host where the GPU/vGPU is allocated.
	if machineCtx.ElfMachine.RequiresGPUDevices() {
		hostID = *vm.Host.ID
	}

	task, err := machineCtx.VMService.PowerOn(machineCtx.ElfMachine.Status.VMRef, hostID)
	if err != nil {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOnFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return errors.Wrapf(err, "failed to trigger power on for VM %s", machineCtx.ElfMachine.Status.VMRef)
	}

	conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.PoweringOnReason, clusterv1.ConditionSeverityInfo, "")

	machineCtx.ElfMachine.SetTask(*task.ID)

	log.Info("Waiting for VM to be powered on", "vmRef", machineCtx.ElfMachine.Status.VMRef, "taskRef", machineCtx.ElfMachine.Status.TaskRef)

	return nil
}

func (r *ElfMachineReconciler) updateVM(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) error {
	log := ctrl.LoggerFrom(ctx)

	if ok := acquireTicketForUpdatingVM(machineCtx.ElfMachine.Name); !ok {
		log.V(1).Info("The VM operation reaches rate limit, skip updating VM " + machineCtx.ElfMachine.Status.VMRef)

		return nil
	}

	withTaskVM, err := machineCtx.VMService.UpdateVM(vm, machineCtx.ElfMachine)
	if err != nil {
		conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.UpdatingFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return errors.Wrapf(err, "failed to trigger update for VM %s", ctx)
	}

	conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.UpdatingReason, clusterv1.ConditionSeverityInfo, "")

	machineCtx.ElfMachine.SetTask(*withTaskVM.TaskID)

	log.Info("Waiting for the VM to be updated", "vmRef", machineCtx.ElfMachine.Status.VMRef, "taskRef", machineCtx.ElfMachine.Status.TaskRef)

	return nil
}

// reconcileVMTask handles virtual machine tasks.
//
// The return value:
// 1. true indicates that the virtual machine task has completed (success or failure).
// 2. false indicates that the virtual machine task has not been completed yet.
func (r *ElfMachineReconciler) reconcileVMTask(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) (taskDone bool, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	taskRef := machineCtx.ElfMachine.Status.TaskRef
	vmRef := machineCtx.ElfMachine.Status.VMRef

	var err error
	var task *models.Task
	if machineCtx.ElfMachine.HasTask() {
		task, err = machineCtx.VMService.GetTask(taskRef)
		if err != nil {
			if service.IsTaskNotFound(err) {
				machineCtx.ElfMachine.SetTask("")
				log.Error(err, fmt.Sprintf("task %s of VM %s is missing", taskRef, vmRef))
			} else {
				return false, errors.Wrapf(err, "failed to get task %s for VM %s", taskRef, vmRef)
			}
		}
	}

	if task == nil {
		// VM is performing an operation
		if vm != nil && vm.EntityAsyncStatus != nil {
			log.Info("Waiting for VM task done", "vmRef", vmRef, "taskRef", taskRef)

			return false, nil
		}

		return true, nil
	}

	defer func() {
		if taskDone {
			machineCtx.ElfMachine.SetTask("")
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
		if err := r.reconcileVMFailedTask(ctx, machineCtx, task, taskRef, vmRef); err != nil {
			return true, err
		}
	case models.TaskStatusSUCCESSED:
		log.Info("VM task succeeded", "vmRef", vmRef, "taskRef", taskRef, "taskDescription", service.GetTowerString(task.Description))

		if machineCtx.ElfMachine.RequiresGPUDevices() &&
			(service.IsCloneVMTask(task) || service.IsPowerOnVMTask(task) || service.IsUpdateVMTask(task)) {
			unlockGPUDevicesLockedByVM(machineCtx.ElfCluster.Spec.Cluster, machineCtx.ElfMachine.Name)
		}

		if service.IsPowerOnVMTask(task) &&
			machineCtx.ElfMachine.GetVMFirstBootTimestamp() == nil {
			now := metav1.Now()
			machineCtx.ElfMachine.SetVMFirstBootTimestamp(&now)
		}

		if service.IsCloneVMTask(task) || service.IsPowerOnVMTask(task) || service.IsUpdateVMTask(task) {
			releaseTicketForCreateVM(machineCtx.ElfMachine.Name)
			recordElfClusterStorageInsufficient(machineCtx, false)
			recordElfClusterMemoryInsufficient(machineCtx, false)

			if err := recordPlacementGroupPolicyNotSatisfied(ctx, machineCtx, r.Client, false); err != nil {
				return true, err
			}
		}
	default:
		log.Info("Waiting for VM task done", "vmRef", vmRef, "taskRef", taskRef, "taskStatus", service.GetTowerTaskStatus(task.Status), "taskDescription", service.GetTowerString(task.Description))
	}

	if *task.Status == models.TaskStatusFAILED || *task.Status == models.TaskStatusSUCCESSED {
		return true, nil
	}

	return false, nil
}

// reconcileVMFailedTask handles failed virtual machine tasks.
func (r *ElfMachineReconciler) reconcileVMFailedTask(ctx goctx.Context, machineCtx *context.MachineContext, task *models.Task, taskRef, vmRef string) error {
	log := ctrl.LoggerFrom(ctx)

	errorMessage := service.GetTowerString(task.ErrorMessage)
	if service.IsGPUAssignFailed(errorMessage) {
		errorMessage = service.ParseGPUAssignFailed(errorMessage)
	}
	conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.TaskFailureReason, clusterv1.ConditionSeverityInfo, errorMessage)

	if service.IsCloudInitConfigError(errorMessage) {
		machineCtx.ElfMachine.Status.FailureReason = ptr.To(capeerrors.CloudInitConfigError)
		machineCtx.ElfMachine.Status.FailureMessage = ptr.To("VM cloud-init config error: " + service.FormatCloudInitError(errorMessage))
	}

	log.Error(errors.New("VM task failed"), "", "vmRef", vmRef, "taskRef", taskRef, "taskErrorMessage", errorMessage, "taskErrorCode", service.GetTowerString(task.ErrorCode), "taskDescription", service.GetTowerString(task.Description))

	switch {
	case service.IsCloneVMTask(task):
		releaseTicketForCreateVM(machineCtx.ElfMachine.Name)

		if service.IsVMDuplicateError(errorMessage) {
			setVMDuplicate(machineCtx.ElfMachine.Name)
		}

		if machineCtx.ElfMachine.RequiresGPUDevices() {
			unlockGPUDevicesLockedByVM(machineCtx.ElfCluster.Spec.Cluster, machineCtx.ElfMachine.Name)
		}
	case service.IsUpdateVMDiskTask(task, machineCtx.ElfMachine.Name):
		reason := conditions.GetReason(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition)
		if reason == infrav1.ExpandingVMDiskReason || reason == infrav1.ExpandingVMDiskFailedReason {
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskFailedReason, clusterv1.ConditionSeverityWarning, errorMessage)
		}
	case service.IsUpdateVMTask(task) && conditions.IsFalse(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition):
		reason := conditions.GetReason(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition)
		if reason == infrav1.ExpandingVMComputeResourcesReason || reason == infrav1.ExpandingVMComputeResourcesFailedReason {
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMComputeResourcesFailedReason, clusterv1.ConditionSeverityWarning, errorMessage)
		}
	case service.IsPowerOnVMTask(task) || service.IsUpdateVMTask(task) || service.IsVMColdMigrationTask(task):
		if machineCtx.ElfMachine.RequiresGPUDevices() {
			unlockGPUDevicesLockedByVM(machineCtx.ElfCluster.Spec.Cluster, machineCtx.ElfMachine.Name)
		}
	}

	switch {
	case service.IsVMDuplicateError(errorMessage):
		setVMDuplicate(machineCtx.ElfMachine.Name)
	case service.IsStorageInsufficientError(errorMessage):
		recordElfClusterStorageInsufficient(machineCtx, true)
		message := "Insufficient storage detected for the ELF cluster " + machineCtx.ElfCluster.Spec.Cluster
		log.Info(message)

		return errors.New(message)
	case service.IsMemoryInsufficientError(errorMessage):
		recordElfClusterMemoryInsufficient(machineCtx, true)
		message := "Insufficient memory detected for the ELF cluster " + machineCtx.ElfCluster.Spec.Cluster
		log.Info(message)

		return errors.New(message)
	case service.IsPlacementGroupError(errorMessage):
		if err := recordPlacementGroupPolicyNotSatisfied(ctx, machineCtx, r.Client, true); err != nil {
			return err
		}
		message := "The placement group policy can not be satisfied"
		log.Info(message)

		return errors.New(message)
	}

	return nil
}

// reconcileHostAndZone reconciles the host and zone of the virtual machine.
func (r *ElfMachineReconciler) reconcileHostAndZone(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) error {
	log := ctrl.LoggerFrom(ctx)

	// The host of the virtual machine may change, such as rescheduling caused by HA.
	if vm.Host != nil && machineCtx.ElfMachine.Status.HostServerName != *vm.Host.Name {
		hostName := machineCtx.ElfMachine.Status.HostServerName
		machineCtx.ElfMachine.Status.HostServerRef = *vm.Host.ID
		machineCtx.ElfMachine.Status.HostServerName = *vm.Host.Name
		log.V(1).Info(fmt.Sprintf("Updated VM hostServerName from %s to %s", hostName, *vm.Host.Name))
	}

	if !machineCtx.ElfCluster.IsStretched() {
		return nil
	}

	zones, err := machineCtx.VMService.GetClusterZones(machineCtx.ElfCluster.Spec.Cluster)
	if err != nil {
		return err
	}

	zone := service.GetHostZone(zones, machineCtx.ElfMachine.Status.HostServerRef)
	if zone == nil {
		return errors.New("failed to get zone for the host %s" + machineCtx.ElfMachine.Status.HostServerRef)
	}

	zoneType := infrav1.ElfClusterZoneTypeSecondary
	if *zone.IsPreferred {
		zoneType = infrav1.ElfClusterZoneTypePreferred
	}

	zoneStatus := infrav1.ZoneStatus{
		ZoneID: *zone.ID,
		Type:   zoneType,
	}

	if !zoneStatus.Equal(&machineCtx.ElfMachine.Status.Zone) {
		machineCtx.ElfMachine.Status.Zone = zoneStatus
		log.V(1).Info(fmt.Sprintf("Updated VM zone from %s to %s", machineCtx.ElfMachine.Status.Zone, zoneStatus))
	}

	return nil
}

func (r *ElfMachineReconciler) reconcileProviderID(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) error {
	log := ctrl.LoggerFrom(ctx)

	providerID := machineutil.ConvertUUIDToProviderID(*vm.LocalID)
	if providerID == "" {
		return errors.Errorf("invalid VM UUID %s from %s %s/%s for %s",
			*vm.LocalID,
			machineCtx.ElfCluster.GroupVersionKind(),
			machineCtx.ElfCluster.GetNamespace(),
			machineCtx.ElfCluster.GetName(),
			ctx)
	}

	if machineCtx.ElfMachine.Spec.ProviderID == nil || *machineCtx.ElfMachine.Spec.ProviderID != providerID {
		machineCtx.ElfMachine.Spec.ProviderID = ptr.To(providerID)

		log.Info("updated providerID", "providerID", providerID)
	}

	return nil
}

// reconcileNode sets providerID and host server labels for node.
func (r *ElfMachineReconciler) reconcileNode(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	providerID := machineutil.ConvertUUIDToProviderID(*vm.LocalID)
	if providerID == "" {
		return false, errors.Errorf("invalid VM UUID %s from %s %s/%s for %s",
			*vm.LocalID,
			machineCtx.ElfCluster.GroupVersionKind(),
			machineCtx.ElfCluster.GetNamespace(),
			machineCtx.ElfCluster.GetName(),
			ctx)
	}

	kubeClient, err := util.NewKubeClient(ctx, r.Client, machineCtx.Cluster)
	if err != nil {
		return false, errors.Wrapf(err, "failed to get client for Cluster %s", klog.KObj(machineCtx.Cluster))
	}

	node, err := kubeClient.CoreV1().Nodes().Get(ctx, machineCtx.ElfMachine.Name, metav1.GetOptions{})
	if err != nil {
		return false, errors.Wrapf(err, "failed to get node %s for setting providerID and labels", machineCtx.ElfMachine.Name)
	}

	keys := []string{
		infrav1.HostServerIDLabel,
		infrav1.HostServerNameLabel,
		infrav1.TowerVMIDLabel,
		infrav1.NodeGroupLabel,
		infrav1.ZoneIDLabel,
		infrav1.ZoneTypeLabel,
		labelsutil.ClusterAutoscalerCAPIGPULabel,
	}

	nodeLabels := node.GetLabels()
	if nodeLabels == nil {
		nodeLabels = make(map[string]string)
	}

	actualLabels := map[string]interface{}{}
	for _, key := range keys {
		val, ok := nodeLabels[key]
		if ok {
			actualLabels[key] = val
		} else {
			actualLabels[key] = nil
		}
	}

	expectedLabels := map[string]interface{}{
		infrav1.HostServerIDLabel:   machineCtx.ElfMachine.Status.HostServerRef,
		infrav1.HostServerNameLabel: machineCtx.ElfMachine.Status.HostServerName,
		infrav1.TowerVMIDLabel:      *vm.ID,
		infrav1.NodeGroupLabel:      machineutil.GetNodeGroupName(machineCtx.Machine),
	}

	if machineCtx.ElfMachine.Status.Zone.Type != "" {
		expectedLabels[infrav1.ZoneTypeLabel] = machineCtx.ElfMachine.Status.Zone.Type.ToLower()
	} else {
		expectedLabels[infrav1.ZoneTypeLabel] = nil
	}

	if machineCtx.ElfMachine.Status.Zone.ZoneID != "" {
		expectedLabels[infrav1.ZoneIDLabel] = machineCtx.ElfMachine.Status.Zone.ZoneID
	} else {
		expectedLabels[infrav1.ZoneIDLabel] = nil
	}

	autoscalerCAPIGPULabel := ""
	if len(machineCtx.ElfMachine.Spec.GPUDevices) > 0 {
		autoscalerCAPIGPULabel = labelsutil.ConvertToLabelValue(machineCtx.ElfMachine.Spec.GPUDevices[0].Model)
	} else if len(machineCtx.ElfMachine.Spec.VGPUDevices) > 0 {
		autoscalerCAPIGPULabel = labelsutil.ConvertToLabelValue(machineCtx.ElfMachine.Spec.VGPUDevices[0].Type)
	}
	if autoscalerCAPIGPULabel != "" {
		expectedLabels[labelsutil.ClusterAutoscalerCAPIGPULabel] = autoscalerCAPIGPULabel
	} else {
		expectedLabels[labelsutil.ClusterAutoscalerCAPIGPULabel] = nil
	}

	if node.Spec.ProviderID != "" && reflect.DeepEqual(actualLabels, expectedLabels) {
		return true, nil
	}

	payloads := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": expectedLabels,
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

	log.Info("Setting node providerID and labels succeeded", "providerID", providerID, "labels", expectedLabels)

	return true, nil
}

// Ensure all the VM's NICs that need IP have obtained IP addresses and VM network ready, otherwise requeue.
// 1. If there are DHCP VM NICs, it will ensure that all DHCP VM NICs obtain IP.
// 2. If none of the VM NICs is the DHCP type, at least one IP must be obtained from Tower API or k8s node to ensure that the network is ready.
//
// In the scenario with many virtual machines, it could be slow for SMTX OS to synchronize VM information via vmtools.
// So if not enough IPs can be obtained from Tower API, try to get its IP address from the corresponding K8s Node.
func (r *ElfMachineReconciler) reconcileNetwork(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) (ret bool, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	defer func() {
		if reterr != nil {
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForNetworkAddressesReason, clusterv1.ConditionSeverityWarning, reterr.Error())
		} else if !ret {
			log.V(1).Info("VM network is not ready yet", "nicStatus", machineCtx.ElfMachine.Status.Network)
			conditions.MarkFalse(machineCtx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForNetworkAddressesReason, clusterv1.ConditionSeverityInfo, "")
		}
	}()

	machineCtx.ElfMachine.Status.Network = []infrav1.NetworkStatus{}
	machineCtx.ElfMachine.Status.Addresses = []clusterv1.MachineAddress{}
	// A Map of IP to MachineAddress
	ipToMachineAddressMap := make(map[string]clusterv1.MachineAddress)

	nics, err := machineCtx.VMService.GetVMNics(*vm.ID)
	if err != nil {
		return false, err
	}

	for i := range nics {
		nic := nics[i]
		ip := service.GetTowerString(nic.IPAddress)

		// Add to Status.Network even if IP is empty.
		machineCtx.ElfMachine.Status.Network = append(machineCtx.ElfMachine.Status.Network, infrav1.NetworkStatus{
			IPAddrs:    []string{ip},
			MACAddr:    service.GetTowerString(nic.MacAddress),
			Gateway:    service.GetTowerString(nic.Gateway),
			SubnetMask: service.GetTowerString(nic.SubnetMask),
		})

		if ip == "" {
			continue
		}

		ipToMachineAddressMap[ip] = clusterv1.MachineAddress{
			Type:    clusterv1.MachineInternalIP,
			Address: ip,
		}
	}

	networkDevicesWithIP := machineCtx.ElfMachine.GetNetworkDevicesRequiringIP()
	networkDevicesWithDHCP := machineCtx.ElfMachine.GetNetworkDevicesRequiringDHCP()

	if len(ipToMachineAddressMap) < len(networkDevicesWithIP) {
		// Try to get VM NIC IP address from the K8s Node.
		nodeIP, err := r.getK8sNodeIP(ctx, machineCtx, machineCtx.ElfMachine.Name)
		if err == nil && nodeIP != "" {
			ipToMachineAddressMap[nodeIP] = clusterv1.MachineAddress{
				Address: nodeIP,
				Type:    clusterv1.MachineInternalIP,
			}
		} else if err != nil {
			log.Error(err, "failed to get VM NIC IP address from the K8s node", "Node", machineCtx.ElfMachine.Name)
		}
	}

	if len(networkDevicesWithDHCP) > 0 {
		dhcpIPNum := 0
		for _, ip := range ipToMachineAddressMap {
			if !machineCtx.ElfMachine.IsMachineStaticIP(ip.Address) {
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
		machineCtx.ElfMachine.Status.Addresses = append(machineCtx.ElfMachine.Status.Addresses, machineAddress)
	}

	return true, nil
}

func (r *ElfMachineReconciler) getBootstrapData(ctx goctx.Context, machineCtx *context.MachineContext) (string, error) {
	secret := &corev1.Secret{}
	secretKey := apitypes.NamespacedName{
		Namespace: machineCtx.Machine.Namespace,
		Name:      *machineCtx.Machine.Spec.Bootstrap.DataSecretName,
	}

	if err := r.Client.Get(ctx, secretKey, secret); err != nil {
		return "", errors.Wrapf(err, "failed to get bootstrap data secret %s", secretKey)
	}

	value, ok := secret.Data["value"]
	if !ok {
		return "", errors.New("error retrieving bootstrap data: secret value key is missing")
	}

	return string(value), nil
}

func (r *ElfMachineReconciler) reconcileLabels(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	capeManagedLabelKey := towerresources.GetVMLabelManaged()
	capeManagedLabel := getLabelFromCache(capeManagedLabelKey)
	if capeManagedLabel == nil {
		var err error
		capeManagedLabel, err = machineCtx.VMService.UpsertLabel(capeManagedLabelKey, "true")
		if err != nil {
			return false, errors.Wrapf(err, "%s %s", failedToUpsertLabelMsg, towerresources.GetVMLabelManaged())
		}

		setLabelInCache(capeManagedLabel)
	}

	// If the virtual machine has been labeled with managed label,
	// it is considered that all labels have been labeled.
	for i := range len(vm.Labels) {
		if *vm.Labels[i].ID == *capeManagedLabel.ID {
			return true, nil
		}
	}

	namespaceLabel, err := machineCtx.VMService.UpsertLabel(towerresources.GetVMLabelNamespace(), machineCtx.ElfMachine.Namespace)
	if err != nil {
		return false, errors.Wrapf(err, "%s %s", failedToUpsertLabelMsg, towerresources.GetVMLabelNamespace())
	}
	clusterNameLabel, err := machineCtx.VMService.UpsertLabel(towerresources.GetVMLabelClusterName(), machineCtx.ElfCluster.Name)
	if err != nil {
		return false, errors.Wrapf(err, "%s %s", failedToUpsertLabelMsg, towerresources.GetVMLabelClusterName())
	}

	var vipLabel *models.Label
	if machineutil.IsControlPlaneMachine(machineCtx.ElfMachine) {
		vipLabel, err = machineCtx.VMService.UpsertLabel(towerresources.GetVMLabelVIP(), machineCtx.ElfCluster.Spec.ControlPlaneEndpoint.Host)
		if err != nil {
			return false, errors.Wrapf(err, "%s %s", failedToUpsertLabelMsg, towerresources.GetVMLabelVIP())
		}
	}

	labelIDs := []string{*namespaceLabel.ID, *clusterNameLabel.ID, *capeManagedLabel.ID}
	if machineutil.IsControlPlaneMachine(machineCtx.ElfMachine) {
		labelIDs = append(labelIDs, *vipLabel.ID)
	}
	log.V(3).Info("Upsert labels", "labelIds", labelIDs)
	_, err = machineCtx.VMService.AddLabelsToVM(*vm.ID, labelIDs)
	if err != nil {
		delLabelCache(capeManagedLabelKey)

		return false, err
	}
	return true, nil
}

// isWaitingForStaticIPAllocation checks whether the VM should wait for a static IP
// to be allocated.
func (r *ElfMachineReconciler) isWaitingForStaticIPAllocation(machineCtx *context.MachineContext) bool {
	devices := machineCtx.ElfMachine.Spec.Network.Devices
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
func (r *ElfMachineReconciler) deleteNode(ctx goctx.Context, machineCtx *context.MachineContext, nodeName string) error {
	// When the cluster needs to be deleted, there is no need to delete the k8s node.
	if machineCtx.Cluster.DeletionTimestamp != nil {
		return nil
	}

	// when the control plane is not ready, there is no need to delete the k8s node.
	if !machineCtx.Cluster.Status.ControlPlaneReady {
		return nil
	}

	kubeClient, err := util.NewKubeClient(ctx, r.Client, machineCtx.Cluster)
	if err != nil {
		return errors.Wrapf(err, "failed to get client for Cluster %s", klog.KObj(machineCtx.Cluster))
	}

	// Attempt to delete the corresponding node.
	err = kubeClient.CoreV1().Nodes().Delete(ctx, nodeName, metav1.DeleteOptions{})
	// k8s node is already deleted.
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return errors.Wrapf(err, "failed to delete K8s node %s for Cluster %s/", nodeName, klog.KObj(machineCtx.Cluster))
	}

	return nil
}

// getK8sNodeIP get the default network IP of K8s Node.
func (r *ElfMachineReconciler) getK8sNodeIP(ctx goctx.Context, machineCtx *context.MachineContext, nodeName string) (string, error) {
	kubeClient, err := util.NewKubeClient(ctx, r.Client, machineCtx.Cluster)
	if err != nil {
		return "", errors.Wrapf(err, "failed to get client for Cluster %s", klog.KObj(machineCtx.Cluster))
	}

	k8sNode, err := kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil && apierrors.IsNotFound(err) {
		return "", nil
	}

	if err != nil {
		return "", errors.Wrapf(err, "failed to get K8s Node %s for Cluster %s", nodeName, klog.KObj(machineCtx.Cluster))
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
func (r *ElfMachineReconciler) deleteDuplicateVMs(ctx goctx.Context, machineCtx *context.MachineContext) (reconcile.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Duplicate virtual machines appear in the process of creating virtual machines,
	// only need to check within half an hour after creating virtual machines.
	if machineCtx.ElfMachine.DeletionTimestamp.IsZero() &&
		time.Now().After(machineCtx.ElfMachine.CreationTimestamp.Add(checkDuplicateVMDuration)) {
		return reconcile.Result{}, nil
	}

	vms, err := machineCtx.VMService.FindVMsByName(machineCtx.ElfMachine.Name)
	if err != nil {
		return reconcile.Result{}, err
	}

	if len(vms) <= 1 {
		return reconcile.Result{}, nil
	}

	if machineCtx.ElfMachine.Status.VMRef == "" {
		vmIDs := make([]string, 0, len(vms))
		for i := range vms {
			vmIDs = append(vmIDs, *vms[i].ID)
		}
		log.Info("Waiting for ElfMachine to select one of the duplicate VMs before deleting the other", "vms", vmIDs)
		return reconcile.Result{RequeueAfter: config.Cape.DefaultRequeueTimeout}, nil
	}

	for i := range vms {
		// Do not delete already running virtual machines to avoid deleting already used virtual machines.
		if *vms[i].ID == machineCtx.ElfMachine.Status.VMRef ||
			*vms[i].LocalID == machineCtx.ElfMachine.Status.VMRef ||
			*vms[i].Status != models.VMStatusSTOPPED {
			continue
		}

		// When there are duplicate virtual machines, the service of Tower is unstable.
		// If there is a deletion operation error, just return and try again.
		if err := r.deleteVM(ctx, machineCtx, vms[i]); err != nil {
			return reconcile.Result{}, err
		} else {
			return reconcile.Result{RequeueAfter: config.Cape.DefaultRequeueTimeout}, nil
		}
	}

	return reconcile.Result{}, nil
}

// deleteVM deletes the specified virtual machine.
func (r *ElfMachineReconciler) deleteVM(ctx goctx.Context, machineCtx *context.MachineContext, vm *models.VM) error {
	log := ctrl.LoggerFrom(ctx)

	// VM is performing an operation
	if vm.EntityAsyncStatus != nil {
		log.V(1).Info("Waiting for VM task done before deleting the duplicate VM", "vmID", *vm.ID, "name", *vm.Name)
		return nil
	}

	// Delete the VM.
	// Delete duplicate virtual machines asynchronously,
	// because synchronous deletion will affect the performance of reconcile.
	task, err := machineCtx.VMService.Delete(*vm.ID)
	if err != nil {
		return err
	}

	log.Info(fmt.Sprintf("Destroying duplicate VM %s in task %s", *vm.ID, *task.ID))

	return nil
}
