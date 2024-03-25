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
	"reflect"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apitypes "k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	kcputil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/kcp"
	machineutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/machine"
	mdutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/md"
)

const (
	anotherMachineHotUpdateInProgressMessage = "another machine resources hot updating is in progress"
)

// ElfMachineTemplateReconciler reconciles a ElfMachineTemplate object.
type ElfMachineTemplateReconciler struct {
	*context.ControllerContext
	NewVMService service.NewVMServiceFunc
}

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachinetemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachinetemplates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachinetemplates/finalizers,verbs=update

// AddMachineTemplateControllerToManager adds the ElfMachineTemplate controller to the provided
// manager.
func AddMachineTemplateControllerToManager(ctx *context.ControllerManagerContext, mgr ctrlmgr.Manager, options controller.Options) error {
	var (
		controlledType     = &infrav1.ElfMachineTemplate{}
		controlledTypeName = reflect.TypeOf(controlledType).Elem().Name()

		controllerNameShort = fmt.Sprintf("%s-controller", strings.ToLower(controlledTypeName))
	)

	// Build the controller context.
	controllerContext := &context.ControllerContext{
		ControllerManagerContext: ctx,
		Name:                     controllerNameShort,
		Logger:                   ctx.Logger.WithName(controllerNameShort),
	}

	reconciler := &ElfMachineTemplateReconciler{
		ControllerContext: controllerContext,
		NewVMService:      service.NewVMService,
	}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(controlledType).
		WithOptions(options).
		// WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(ctrl.LoggerFrom(ctx), ctx.WatchFilterValue)).
		Complete(reconciler)
}

func (r *ElfMachineTemplateReconciler) Reconcile(ctx goctx.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	// Get the ElfMachineTemplate resource for this request.
	var elfMachineTemplate infrav1.ElfMachineTemplate
	if err := r.Client.Get(r, req.NamespacedName, &elfMachineTemplate); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("ElfMachineTemplate not found, won't reconcile", "key", req.NamespacedName)

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	// Fetch the CAPI Cluster.
	cluster, err := capiutil.GetOwnerCluster(r, r.Client, elfMachineTemplate.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if cluster == nil {
		r.Logger.Info("Waiting for Cluster Controller to set OwnerRef on ElfMachineTemplate",
			"namespace", elfMachineTemplate.Namespace, "elfCluster", elfMachineTemplate.Name)

		return reconcile.Result{}, nil
	}

	if annotations.IsPaused(cluster, &elfMachineTemplate) {
		r.Logger.V(4).Info("ElfMachineTemplate linked to a cluster that is paused",
			"namespace", elfMachineTemplate.Namespace, "elfMachineTemplate", elfMachineTemplate.Name)

		return reconcile.Result{}, nil
	}

	// Fetch the ElfCluster
	var elfCluster infrav1.ElfCluster
	if err := r.Client.Get(r, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}, &elfCluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("ElfMachine Waiting for ElfCluster",
				"namespace", elfMachineTemplate.Namespace, "elfMachineTemplate", elfMachineTemplate.Name)
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	logger := r.Logger.WithValues("namespace", elfMachineTemplate.Namespace,
		"elfCluster", elfCluster.Name, "elfMachineTemplate", elfMachineTemplate.Name)

	// Create the machine context for this request.
	machineTemplateContext := &context.MachineTemplateContext{
		ControllerContext:  r.ControllerContext,
		Cluster:            cluster,
		ElfCluster:         &elfCluster,
		ElfMachineTemplate: &elfMachineTemplate,
		Logger:             logger,
	}

	if elfMachineTemplate.ObjectMeta.DeletionTimestamp.IsZero() || !elfCluster.HasForceDeleteCluster() {
		vmService, err := r.NewVMService(r.Context, elfCluster.GetTower(), logger)
		if err != nil {
			return reconcile.Result{}, err
		}

		machineTemplateContext.VMService = vmService
	}

	// Handle deleted machines
	if !elfMachineTemplate.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Handle non-deleted machines
	return r.reconcileNormal(machineTemplateContext)
}

func (r *ElfMachineTemplateReconciler) reconcileNormal(ctx *context.MachineTemplateContext) (reconcile.Result, error) {
	return r.reconcileMachineResources(ctx)
}

// reconcileMachineResources ensures that the resources(disk capacity) of the
// virtual machines are the same as expected by ElfMachine.
// TODO: CPU and memory will be supported in the future.
func (r *ElfMachineTemplateReconciler) reconcileMachineResources(ctx *context.MachineTemplateContext) (reconcile.Result, error) {
	if ok, err := r.reconcileCPResources(ctx); err != nil {
		return reconcile.Result{}, err
	} else if !ok {
		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	if ok, err := r.reconcileWorkerResources(ctx); err != nil {
		return reconcile.Result{}, err
	} else if !ok {
		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	return reconcile.Result{}, nil
}

// reconcileCPResources ensures that the resources(disk capacity) of the
// control plane virtual machines are the same as expected by ElfMachine.
func (r *ElfMachineTemplateReconciler) reconcileCPResources(ctx *context.MachineTemplateContext) (bool, error) {
	var kcp controlplanev1.KubeadmControlPlane
	if err := ctx.Client.Get(ctx, apitypes.NamespacedName{
		Namespace: ctx.Cluster.Spec.ControlPlaneRef.Namespace,
		Name:      ctx.Cluster.Spec.ControlPlaneRef.Name,
	}, &kcp); err != nil {
		return false, err
	}

	if kcp.Spec.MachineTemplate.InfrastructureRef.Namespace != ctx.ElfMachineTemplate.Namespace ||
		kcp.Spec.MachineTemplate.InfrastructureRef.Name != ctx.ElfMachineTemplate.Name {
		return true, nil
	}

	elfMachines, err := machineutil.GetControlPlaneElfMachinesInCluster(ctx, ctx.Client, ctx.Cluster.Namespace, ctx.Cluster.Name)
	if err != nil {
		return false, err
	}

	updatingResourcesElfMachines, needUpdatedResourcesElfMachines, err := selectResourcesNotUpToDateElfMachines(ctx, ctx.ElfMachineTemplate, elfMachines)
	if err != nil {
		return false, err
	} else if len(updatingResourcesElfMachines) == 0 && len(needUpdatedResourcesElfMachines) == 0 {
		return true, nil
	}

	// Only one CP ElfMachine is allowed to update resources at the same time.
	if len(updatingResourcesElfMachines) > 0 {
		ctx.Logger.V(1).Info("Waiting for control plane ElfMachines to be updated resources", "updatingCount", len(updatingResourcesElfMachines), "needUpdatedCount", len(needUpdatedResourcesElfMachines))

		if err := markElfMachinesResourcesNotUpToDate(ctx, ctx.ElfMachineTemplate, needUpdatedResourcesElfMachines); err != nil {
			return false, err
		}

		return false, nil
	}

	checksPassed, err := r.preflightChecksForCP(ctx, &kcp)
	if err != nil {
		return false, err
	}

	var toBeUpdatedElfMachine *infrav1.ElfMachine
	if checksPassed {
		toBeUpdatedElfMachine = needUpdatedResourcesElfMachines[0]
		needUpdatedResourcesElfMachines = needUpdatedResourcesElfMachines[1:]
	}

	if err := markElfMachinesResourcesNotUpToDate(ctx, ctx.ElfMachineTemplate, needUpdatedResourcesElfMachines); err != nil {
		return false, err
	}

	updatingCount := 0
	if toBeUpdatedElfMachine != nil {
		updatingCount = 1
		if err := markElfMachinesToBeUpdatedResources(ctx, ctx.ElfMachineTemplate, []*infrav1.ElfMachine{toBeUpdatedElfMachine}); err != nil {
			return false, err
		}
	}

	ctx.Logger.V(1).Info("Waiting for control plane ElfMachines to be updated resources", "updatingCount", updatingCount, "needUpdatedCount", len(needUpdatedResourcesElfMachines))

	return false, err
}

// preflightChecksForCP checks if the control plane is stable before proceeding with a updating resources operation,
// where stable means that:
// - KCP not in rolling update.
// - There are no machine deletion in progress.
// - All the health conditions on KCP are true.
// - All the health conditions on the control plane machines are true.
// If the control plane is not passing preflight checks, it requeue.
func (r *ElfMachineTemplateReconciler) preflightChecksForCP(ctx *context.MachineTemplateContext, kcp *controlplanev1.KubeadmControlPlane) (bool, error) {
	// During the rolling update process, it is impossible to determine which
	// machines are new and which are old machines. Complete the rolling update
	// first and then update the resources to avoid updating resources for old
	// machines that are about to be deleted.
	if kcputil.IsKCPInRollingUpdate(kcp) {
		ctx.Logger.Info("KCP rolling update in progress, skip updating resources")

		return false, nil
	}

	cpMachines, err := machineutil.GetControlPlaneMachinesForCluster(ctx, ctx.Client, ctx.Cluster)
	if err != nil {
		return false, err
	}

	machines := collections.FromMachines(cpMachines...)
	deletingMachines := machines.Filter(collections.HasDeletionTimestamp)
	if len(deletingMachines) > 0 {
		ctx.Logger.Info("Waiting for machines to be deleted", "machines", deletingMachines.Names())

		return false, nil
	}

	allMachineHealthConditions := []clusterv1.ConditionType{
		controlplanev1.MachineAPIServerPodHealthyCondition,
		controlplanev1.MachineControllerManagerPodHealthyCondition,
		controlplanev1.MachineSchedulerPodHealthyCondition,
		controlplanev1.MachineEtcdPodHealthyCondition,
		controlplanev1.MachineEtcdMemberHealthyCondition,
	}
	machineErrors := []error{}
	for _, machine := range machines {
		if machine.Status.NodeRef == nil {
			// The conditions will only ever be set on a Machine if we're able to correlate a Machine to a Node.
			// Correlating Machines to Nodes requires the nodeRef to be set.
			// Instead of confusing users with errors about that the conditions are not set, let's point them
			// towards the unset nodeRef (which is the root cause of the conditions not being there).
			machineErrors = append(machineErrors, errors.Errorf("Machine %s does not have a corresponding Node yet (Machine.status.nodeRef not set)", machine.Name))
		} else {
			for _, condition := range allMachineHealthConditions {
				if err := preflightCheckCondition("Machine", machine, condition); err != nil {
					machineErrors = append(machineErrors, err)
				}
			}
		}
	}

	if len(machineErrors) > 0 {
		aggregatedError := kerrors.NewAggregate(machineErrors)
		ctx.Logger.Info("Waiting for control plane to pass preflight checks", "failures", aggregatedError.Error())

		return false, nil
	}

	return true, nil
}

func preflightCheckCondition(kind string, obj conditions.Getter, condition clusterv1.ConditionType) error {
	c := conditions.Get(obj, condition)
	if c == nil {
		return errors.Errorf("%s %s does not have %s condition", kind, obj.GetName(), condition)
	}
	if c.Status == corev1.ConditionFalse {
		return errors.Errorf("%s %s reports %s condition is false (%s, %s)", kind, obj.GetName(), condition, c.Severity, c.Message)
	}
	if c.Status == corev1.ConditionUnknown {
		return errors.Errorf("%s %s reports %s condition is unknown (%s)", kind, obj.GetName(), condition, c.Message)
	}
	return nil
}

// reconcileWorkerResources ensures that the resources(disk capacity) of the
// worker virtual machines are the same as expected by ElfMachine.
func (r *ElfMachineTemplateReconciler) reconcileWorkerResources(ctx *context.MachineTemplateContext) (bool, error) {
	mds, err := machineutil.GetMDsForCluster(ctx, ctx.Client, ctx.Cluster.Namespace, ctx.Cluster.Name)
	if err != nil {
		return false, err
	}

	allElfMachinesUpToDate := true
	for i := 0; i < len(mds); i++ {
		if ctx.ElfMachineTemplate.Name != mds[i].Spec.Template.Spec.InfrastructureRef.Name {
			continue
		}

		if ok, err := r.reconcileWorkerResourcesForMD(ctx, mds[i]); err != nil {
			return false, err
		} else if !ok {
			allElfMachinesUpToDate = false
		}
	}

	return allElfMachinesUpToDate, nil
}

// reconcileWorkerResourcesForMD ensures that the resources(disk capacity) of the
// worker virtual machines managed by the md are the same as expected by ElfMachine.
func (r *ElfMachineTemplateReconciler) reconcileWorkerResourcesForMD(ctx *context.MachineTemplateContext, md *clusterv1.MachineDeployment) (bool, error) {
	elfMachines, err := machineutil.GetElfMachinesForMD(ctx, ctx.Client, ctx.Cluster, md)
	if err != nil {
		return false, err
	}

	updatingResourcesElfMachines, needUpdatedResourcesElfMachines, err := selectResourcesNotUpToDateElfMachines(ctx, ctx.ElfMachineTemplate, elfMachines)
	if err != nil {
		return false, err
	} else if len(updatingResourcesElfMachines) == 0 && len(needUpdatedResourcesElfMachines) == 0 {
		return true, nil
	}

	maxSurge := getMaxSurge(md)
	if maxSurge == len(updatingResourcesElfMachines) {
		ctx.Logger.V(1).Info("Waiting for worker ElfMachines to be updated resources", "md", md.Name, "updatingCount", len(updatingResourcesElfMachines), "needUpdatedCount", len(needUpdatedResourcesElfMachines), "maxSurge", maxSurge)

		if err := markElfMachinesResourcesNotUpToDate(ctx, ctx.ElfMachineTemplate, needUpdatedResourcesElfMachines); err != nil {
			return false, err
		}

		return false, nil
	}

	checksPassed := r.preflightChecksForWorker(ctx, md, updatingResourcesElfMachines)

	var toBeUpdatedElfMachines []*infrav1.ElfMachine
	if checksPassed {
		toBeUpdatedCount := maxSurge - len(updatingResourcesElfMachines)
		if toBeUpdatedCount > 0 {
			if toBeUpdatedCount >= len(needUpdatedResourcesElfMachines) {
				toBeUpdatedElfMachines = needUpdatedResourcesElfMachines
				needUpdatedResourcesElfMachines = nil
			} else {
				toBeUpdatedElfMachines = needUpdatedResourcesElfMachines[:toBeUpdatedCount]
				needUpdatedResourcesElfMachines = needUpdatedResourcesElfMachines[toBeUpdatedCount:]
			}
		}
	}

	if err := markElfMachinesResourcesNotUpToDate(ctx, ctx.ElfMachineTemplate, needUpdatedResourcesElfMachines); err != nil {
		return false, err
	}

	if err := markElfMachinesToBeUpdatedResources(ctx, ctx.ElfMachineTemplate, toBeUpdatedElfMachines); err != nil {
		return false, err
	}

	ctx.Logger.V(1).Info("Waiting for worker ElfMachines to be updated resources", "md", md.Name, "updatingCount", len(updatingResourcesElfMachines)+len(toBeUpdatedElfMachines), "needUpdatedCount", len(needUpdatedResourcesElfMachines), "maxSurge", maxSurge)

	return false, nil
}

func getMaxSurge(md *clusterv1.MachineDeployment) int {
	maxSurge := mdutil.MaxSurge(*md)
	if maxSurge <= 0 {
		return 1
	}

	return int(maxSurge)
}

// preflightChecksForWorker checks if the worker is stable before proceeding with a updating resources operation,
// where stable means that:
// - MD not in rolling update.
// - The number of machines updating resources is not greater than maxSurge.
// - The number of unavailable machines is no greater than maxUnavailable.
// If the worker is not passing preflight checks, it requeue.
func (r *ElfMachineTemplateReconciler) preflightChecksForWorker(ctx *context.MachineTemplateContext, md *clusterv1.MachineDeployment, updatingResourcesElfMachines []*infrav1.ElfMachine) bool {
	if mdutil.IsMDInRollingUpdate(md) {
		ctx.Logger.Info("MD rolling update in progress, skip updating resources", "md", md.Name)

		return false
	}

	// Use maxSurge of rolling update to control the maximum number of concurrent
	// update resources to avoid updating too many machines at the same time.
	// If an exception occurs during the resource update process, all machines will
	// not be affected.
	if maxSurge := getMaxSurge(md); len(updatingResourcesElfMachines) >= getMaxSurge(md) {
		ctx.Logger.V(1).Info("Waiting for worker ElfMachines to be updated resources", "md", md.Name, "maxSurge", maxSurge, "updatingCount", len(updatingResourcesElfMachines))

		return false
	}

	maxUnavailable := mdutil.MaxUnavailable(*md)
	if md.Status.UnavailableReplicas > maxUnavailable {
		ctx.Logger.Info(fmt.Sprintf("MD unavailable replicas %d is greater than expected %d, skip updating resources", md.Status.UnavailableReplicas, maxUnavailable), "md", md.Name)

		return false
	}

	return true
}

// selectResourcesNotUpToDateElfMachines returns elfMachines whose resources are
// not as expected.
func selectResourcesNotUpToDateElfMachines(ctx *context.MachineTemplateContext, elfMachineTemplate *infrav1.ElfMachineTemplate, elfMachines []*infrav1.ElfMachine) ([]*infrav1.ElfMachine, []*infrav1.ElfMachine, error) {
	var updatingResourcesElfMachines []*infrav1.ElfMachine
	var needUpdatedResourcesElfMachines []*infrav1.ElfMachine
	for i := 0; i < len(elfMachines); i++ {
		elfMachine := elfMachines[i]

		machine, err := capiutil.GetOwnerMachine(ctx, ctx.Client, elfMachine.ObjectMeta)
		if err != nil {
			return nil, nil, err
		}

		// No need to update the resources of deleted and failed machines.
		if machine == nil ||
			!machine.DeletionTimestamp.IsZero() ||
			clusterv1.MachinePhase(machine.Status.Phase) == clusterv1.MachinePhaseFailed {
			continue
		}

		if machineutil.IsUpdatingElfMachineResources(elfMachine) &&
			machineutil.IsResourcesUpToDate(elfMachineTemplate, elfMachine) {
			updatingResourcesElfMachines = append(updatingResourcesElfMachines, elfMachine)
		} else if machineutil.NeedUpdateElfMachineResources(elfMachineTemplate, elfMachine) {
			needUpdatedResourcesElfMachines = append(needUpdatedResourcesElfMachines, elfMachine)
		}
	}

	return updatingResourcesElfMachines, needUpdatedResourcesElfMachines, nil
}

// markElfMachinesToBeUpdatedResources synchronizes the expected resource values
// from the ElfMachineTemplate and marks the machines to be updated resources.
func markElfMachinesToBeUpdatedResources(ctx *context.MachineTemplateContext, elfMachineTemplate *infrav1.ElfMachineTemplate, elfMachines []*infrav1.ElfMachine) error {
	for i := 0; i < len(elfMachines); i++ {
		elfMachine := elfMachines[i]

		patchHelper, err := patch.NewHelper(elfMachine, ctx.Client)
		if err != nil {
			return err
		}

		// Ensure resources are up to date.
		orignalDiskGiB := elfMachine.Spec.DiskGiB
		elfMachine.Spec.DiskGiB = elfMachineTemplate.Spec.Template.Spec.DiskGiB
		conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.WaitingForResourcesHotUpdateReason, clusterv1.ConditionSeverityInfo, "")

		ctx.Logger.Info(fmt.Sprintf("Resources of ElfMachine is not up to date, marking for updating resources(disk: %d -> %d)", orignalDiskGiB, elfMachine.Spec.DiskGiB), "elfMachine", elfMachine.Name)

		if err := patchHelper.Patch(ctx, elfMachine); err != nil {
			return errors.Wrapf(err, "failed to patch ElfMachine %s to mark for updating resources", elfMachine.Name)
		}
	}

	return nil
}

// markElfMachinesResourcesNotUpToDate synchronizes the expected resource values
// from the ElfMachineTemplate and marks the machines waiting for updated resources.
func markElfMachinesResourcesNotUpToDate(ctx *context.MachineTemplateContext, elfMachineTemplate *infrav1.ElfMachineTemplate, elfMachines []*infrav1.ElfMachine) error {
	for i := 0; i < len(elfMachines); i++ {
		elfMachine := elfMachines[i]
		if machineutil.IsResourcesUpToDate(elfMachineTemplate, elfMachine) {
			continue
		}

		patchHelper, err := patch.NewHelper(elfMachine, ctx.Client)
		if err != nil {
			return err
		}

		// Ensure resources are up to date.
		orignalDiskGiB := elfMachine.Spec.DiskGiB
		elfMachine.Spec.DiskGiB = elfMachineTemplate.Spec.Template.Spec.DiskGiB
		conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.WaitingForResourcesHotUpdateReason, clusterv1.ConditionSeverityInfo, anotherMachineHotUpdateInProgressMessage)

		ctx.Logger.Info(fmt.Sprintf("Resources of ElfMachine is not up to date, marking for resources not up to date and waiting for hot updating resources(disk: %d -> %d)", orignalDiskGiB, elfMachine.Spec.DiskGiB), "elfMachine", elfMachine.Name)

		if err := patchHelper.Patch(ctx, elfMachine); err != nil {
			return errors.Wrapf(err, "failed to patch ElfMachine %s to mark for resources not up to date", elfMachine.Name)
		}
	}

	return nil
}
