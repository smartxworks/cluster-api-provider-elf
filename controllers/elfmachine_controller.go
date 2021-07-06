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
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	clustererror "sigs.k8s.io/cluster-api/errors"
	clusterutilv1 "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha3"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	infrautilv1 "github.com/smartxworks/cluster-api-provider-elf/pkg/util"
)

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch

// ElfMachineReconciler reconciles a ElfMachine object
type ElfMachineReconciler struct {
	*context.ControllerContext
	VMService service.VMService
}

// AddMachineControllerToManager adds the machine controller to the provided
// manager.
func AddMachineControllerToManager(ctx *context.ControllerManagerContext, mgr manager.Manager) error {
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

	reconciler := &ElfMachineReconciler{ControllerContext: controllerContext}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(controlledType).
		// Watch the CAPI resource that owns this infrastructure resource.
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: clusterutilv1.MachineToInfrastructureMapFunc(controlledTypeGVK),
			},
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctx.MaxConcurrentReconciles}).
		Complete(reconciler)
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r *ElfMachineReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
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
	machine, err := clusterutilv1.GetOwnerMachine(r, r.Client, elfMachine.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if machine == nil {
		r.Logger.Info("Waiting for Machine Controller to set OwnerRef on ElfMachine",
			"namespace", elfMachine.Namespace, "elfMachine", elfMachine.Name)

		return reconcile.Result{}, nil
	}

	// Fetch the CAPI Cluster.
	cluster, err := clusterutilv1.GetClusterFromMetadata(r, r.Client, machine.ObjectMeta)
	if err != nil {
		r.Logger.Info("Machine is missing cluster label or cluster does not exist",
			"namespace", machine.Namespace, "machine", machine.Name)

		return reconcile.Result{}, nil
	}
	if clusterutilv1.IsPaused(cluster, &elfMachine) {
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

	// Create the machine context for this request.
	logger := r.Logger.WithValues("namespace", elfMachine.Namespace,
		"elfCluster", elfCluster.Name, "elfMachine", elfMachine.Name)
	machineContext := &context.MachineContext{
		ControllerContext: r.ControllerContext,
		Cluster:           cluster,
		ElfCluster:        &elfCluster,
		Machine:           machine,
		ElfMachine:        &elfMachine,
		Logger:            logger,
		PatchHelper:       patchHelper,
	}

	if r.VMService == nil {
		if r.VMService, err = service.NewVMService(elfCluster.Auth(), logger); err != nil {
			conditions.MarkFalse(machineContext.ElfMachine, infrav1.ElfAvailableCondition, infrav1.ElfUnreachableReason, clusterv1.ConditionSeverityError, err.Error())

			return reconcile.Result{}, err
		}
	}
	conditions.MarkTrue(machineContext.ElfMachine, infrav1.ElfAvailableCondition)

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
		// always update the readyCondition.
		conditions.SetSummary(machineContext.ElfMachine,
			conditions.WithConditions(
				infrav1.VMProvisionedCondition,
				infrav1.ElfAvailableCondition,
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

func (r *ElfMachineReconciler) reconcileDeleteVM(ctx *context.MachineContext) (*infrav1.VirtualMachine, error) {
	if !ctx.ElfMachine.HasVM() || !ctx.ElfMachine.WithVM() {
		ctx.Logger.Info("VM has been deleted")

		return nil, nil
	}

	conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, clusterv1.DeletingReason, clusterv1.ConditionSeverityInfo, "")

	var job *infrav1.VMJob

	// Handle pending task
	if ctx.ElfMachine.HasTask() {
		job, err := r.VMService.GetJob(ctx.ElfMachine.Status.TaskRef)
		if err != nil {
			return nil, err
		}

		if job.IsDone() {
			ctx.ElfMachine.SetTask("")

			ctx.Logger.Info("Delete VM job done",
				"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", job.Id)
		} else if job.IsFailed() {
			ctx.ElfMachine.SetTask("")

			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.TaskFailure, clusterv1.ConditionSeverityInfo, job.Description)

			ctx.Logger.Error(errors.New("Delete VM job failed"),
				"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", job.Id)
		} else {
			ctx.Logger.Info("Waiting for delete VM job done",
				"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

			return nil, &clustererror.RequeueAfterError{RequeueAfter: config.DefaultRequeue}
		}
	}

	vm, err := r.VMService.Get(ctx.ElfMachine.Status.VMRef)
	if err != nil {
		if service.IsVMNotFound(err) {
			ctx.Logger.Info("VM be deleted")

			ctx.ElfMachine.SetVM("")
		}

		return nil, err
	}

	if vm.IsPoweredOn() {
		job, err := r.VMService.PowerOff(ctx.ElfMachine.Status.VMRef)
		if err != nil {
			return nil, err
		}

		ctx.ElfMachine.SetTask(job.Id)

		ctx.Logger.V(6).Info("Waiting for VM power off",
			"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

		return vm, nil
	}

	ctx.Logger.V(6).Info("Destroying VM",
		"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	job, err = r.VMService.Delete(ctx.ElfMachine.Status.VMRef)
	if err != nil {
		return nil, err
	} else {
		ctx.ElfMachine.SetTask(job.Id)
	}

	ctx.Logger.V(6).Info("Waiting for VM to be deleted",
		"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

	return vm, nil
}

func (r *ElfMachineReconciler) reconcileDelete(ctx *context.MachineContext) (reconcile.Result, error) {
	ctx.Logger.Info("Reconciling ElfMachine delete")

	vm, err := r.reconcileDeleteVM(ctx)
	if err != nil {
		if service.IsVMNotFound(err) {
			// The VM is deleted so remove the finalizer.
			ctrlutil.RemoveFinalizer(ctx.ElfMachine, infrav1.MachineFinalizer)

			return reconcile.Result{}, nil
		}

		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, clusterv1.DeletionFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		return reconcile.Result{}, err
	}

	if vm != nil {
		ctx.Logger.Info("Waiting for VM to be deleted",
			"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

		return reconcile.Result{RequeueAfter: config.DefaultRequeue}, nil
	}

	return reconcile.Result{}, nil
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
		if !infrautilv1.IsControlPlaneMachine(ctx.ElfMachine) && !ctx.Cluster.Status.ControlPlaneInitialized {
			ctx.Logger.Info("Waiting for the control plane to be initialized")

			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, clusterv1.WaitingForControlPlaneAvailableReason, clusterv1.ConditionSeverityInfo, "")

			return ctrl.Result{}, nil
		}

		ctx.Logger.Info("Waiting for bootstrap data to be available")

		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForBootstrapDataReason, clusterv1.ConditionSeverityInfo, "")

		return reconcile.Result{}, nil
	}

	vm, err := r.reconcileVM(ctx)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to reconcile VM")
	}
	if vm == nil {
		ctx.Logger.Info("Waiting for VM to be created")

		return reconcile.Result{RequeueAfter: config.DefaultRequeue}, nil
	}

	// Reconcile the ElfMachine's providerID using the VM's UUID.
	if ok, err := r.reconcileProviderID(ctx, vm); !ok {
		if err != nil {
			return reconcile.Result{}, errors.Wrapf(err,
				"unexpected error while reconciling providerID for %s", ctx)
		}

		ctx.Logger.Info("providerID is not reconciled",
			"namespace", ctx.ElfMachine.Namespace, "elfMachine", ctx.ElfMachine.Name)

		return reconcile.Result{RequeueAfter: config.DefaultRequeue}, nil
	}

	// Reconcile the ElfMachine's node addresses from the VM's IP addresses.
	if ok, err := r.reconcileNetwork(ctx, vm); !ok {
		if err != nil {
			return reconcile.Result{}, errors.Wrapf(err,
				"unexpected error while reconciling network for %s", ctx)
		}

		ctx.Logger.Info("network is not reconciled",
			"namespace", ctx.ElfMachine.Namespace, "elfMachine", ctx.ElfMachine.Name)

		conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.WaitingForNetworkAddressesReason, clusterv1.ConditionSeverityInfo, "")

		return reconcile.Result{RequeueAfter: config.DefaultRequeue}, nil
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

		return reconcile.Result{RequeueAfter: config.DefaultRequeue}, nil
	}

	return reconcile.Result{}, nil
}

// Reconcile ELF VM, create or get VM.
func (r *ElfMachineReconciler) reconcileVM(ctx *context.MachineContext) (*infrav1.VirtualMachine, error) {
	// If there is no pending taskRef or no vmRef then no VM exists, create one
	if !ctx.ElfMachine.WithVM() {
		ctx.Logger.Info("vmRef and taskRef not exist")

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

		ctx.Logger.V(6).Info("Create VM for ElfMachine")
		job, err := r.VMService.Clone(ctx.Machine, ctx.ElfMachine, bootstrapData)
		if err != nil {
			ctx.Logger.Error(err, "failed to create VM",
				"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", ctx.ElfMachine.Status.TaskRef)

			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.CloningFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

			return nil, err
		}

		ctx.ElfMachine.SetTask(job.Id)
	}

	// Handle pending task
	if ctx.ElfMachine.HasTask() {
		job, err := r.VMService.WaitJob(ctx.ElfMachine.Status.TaskRef)
		if err != nil {
			return nil, err
		}

		if job.IsDone() {
			ctx.ElfMachine.SetVM(job.GetVMUUID())

			ctx.Logger.Info("Create VM job done",
				"vmRef", ctx.ElfMachine.Status.VMRef,
				"taskRef", job.Id)
		} else if job.IsFailed() {
			ctx.ElfMachine.SetTask("")

			conditions.MarkFalse(ctx.ElfMachine, infrav1.VMProvisionedCondition, infrav1.TaskFailure, clusterv1.ConditionSeverityInfo, job.Description)

			ctx.Logger.Error(errors.New("Create VM job failed"),
				"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", job.Id)

			// todo record job error
			return nil, errors.Errorf("create VM job failed for ElfMachine %s/%s", ctx.ElfMachine.Namespace, ctx.ElfMachine.Name)
		} else {
			ctx.Logger.Info("Waiting for Create VM job done",
				"vmRef", ctx.ElfMachine.Status.VMRef, "taskRef", job.Id)

			return nil, nil
		}
	}

	vm, err := r.VMService.Get(ctx.ElfMachine.Status.VMRef)
	if err != nil {
		return nil, err
	}

	return vm, nil
}

func (r *ElfMachineReconciler) reconcileProviderID(ctx *context.MachineContext, vm *infrav1.VirtualMachine) (bool, error) {
	providerID := infrautilv1.ConvertUUIDToProviderID(vm.UUID)
	if providerID == "" {
		return false, errors.Errorf("invalid VM UUID %s from %s %s/%s for %s",
			vm.UUID,
			ctx.ElfCluster.GroupVersionKind(),
			ctx.ElfCluster.GetNamespace(),
			ctx.ElfCluster.GetName(),
			ctx)
	}

	if ctx.ElfMachine.Spec.ProviderID == nil || *ctx.ElfMachine.Spec.ProviderID != providerID {
		ctx.ElfMachine.Spec.ProviderID = pointer.StringPtr(providerID)

		ctx.Logger.Info("updated providerID", "providerID", providerID)
	}

	return true, nil
}

// ELF without cloud provider.
func (r *ElfMachineReconciler) reconcileNodeProviderID(ctx *context.MachineContext, vm *infrav1.VirtualMachine) (bool, error) {
	kubeClient, err := infrautilv1.NewKubeClient(ctx, ctx.Client, ctx.Cluster)
	if err != nil {
		return false, errors.Wrapf(err,
			"failed to get client for Cluster %s/%s",
			ctx.Cluster.Namespace, ctx.Cluster.Name,
		)
	}

	nodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return false, err
	}

	for _, node := range nodeList.Items {
		if node.Name != ctx.ElfMachine.Name {
			continue
		}

		providerID := node.Spec.ProviderID
		if providerID != "" {
			return true, nil
		}

		providerID = *ctx.ElfMachine.Spec.ProviderID
		node.Spec.ProviderID = providerID
		var payloads []interface{}
		payloads = append(payloads,
			infrav1.PatchStringValue{
				Op:    "add",
				Path:  "/spec/providerID",
				Value: providerID,
			})
		payloadBytes, _ := json.Marshal(payloads)
		_, err := kubeClient.CoreV1().Nodes().Patch(node.Name, apitypes.JSONPatchType, payloadBytes)
		if err != nil {
			return false, err
		}

		ctx.Logger.Info("Set node providerID success",
			"cluster", ctx.Cluster.Name,
			"node", node.Name,
			"providerID", providerID)

		return true, nil
	}

	return false, errors.Wrapf(err,
		"Waiting for node add providerID k8s cluster %s/%s/%s",
		ctx.Cluster.Namespace, ctx.Cluster.Name, ctx.ElfMachine.Name,
	)
}

// If the VM is powered on then issue requeues until all of the VM's
// networks have IP addresses.
func (r *ElfMachineReconciler) reconcileNetwork(ctx *context.MachineContext, vm *infrav1.VirtualMachine) (bool, error) {
	ctx.ElfMachine.Status.Network = vm.Network

	var ipAddrs []clusterv1.MachineAddress
	for _, netStatus := range ctx.ElfMachine.Status.Network {
		ipAddrs = append(ipAddrs, clusterv1.MachineAddress{
			Type:    clusterv1.MachineInternalIP,
			Address: netStatus.IPAddrs,
		})
	}

	ctx.ElfMachine.Status.Addresses = ipAddrs
	if len(ctx.ElfMachine.Status.Addresses) == 0 {
		return false, nil
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
