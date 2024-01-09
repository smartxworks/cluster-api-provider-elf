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
	"fmt"
	"reflect"
	"strings"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/util"
	machineutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/machine"
)

//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch

// AddClusterControllerToManager adds the cluster controller to the provided
// manager.
func AddClusterControllerToManager(ctx *context.ControllerManagerContext, mgr ctrlmgr.Manager, options controller.Options) error {
	var (
		clusterControlledType     = &infrav1.ElfCluster{}
		clusterControlledTypeName = reflect.TypeOf(clusterControlledType).Elem().Name()
		clusterControlledTypeGVK  = infrav1.GroupVersion.WithKind(clusterControlledTypeName)
		controllerNameShort       = fmt.Sprintf("%s-controller", strings.ToLower(clusterControlledTypeName))
	)

	// Build the controller context.
	controllerContext := &context.ControllerContext{
		ControllerManagerContext: ctx,
		Name:                     controllerNameShort,
		Logger:                   ctx.Logger.WithName(controllerNameShort),
	}

	reconciler := &ElfClusterReconciler{
		ControllerContext: controllerContext,
		NewVMService:      service.NewVMService,
	}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(clusterControlledType).
		// Watch the CAPI resource that owns this infrastructure resource.
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(capiutil.ClusterToInfrastructureMapFunc(ctx, clusterControlledTypeGVK, mgr.GetClient(), &infrav1.ElfCluster{})),
		).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(ctrl.LoggerFrom(ctx), ctx.WatchFilterValue)).
		WithOptions(options).
		Complete(reconciler)
}

// ElfClusterReconciler reconciles a ElfCluster object.
type ElfClusterReconciler struct {
	*context.ControllerContext
	NewVMService service.NewVMServiceFunc
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r *ElfClusterReconciler) Reconcile(ctx goctx.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	// Get the ElfCluster resource for this request.
	var elfCluster infrav1.ElfCluster
	if err := r.Client.Get(r, req.NamespacedName, &elfCluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("ElfCluster not found, won't reconcile", "key", req.NamespacedName)

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	// Fetch the CAPI Cluster.
	cluster, err := capiutil.GetOwnerCluster(r, r.Client, elfCluster.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if cluster == nil {
		r.Logger.Info("Waiting for Cluster Controller to set OwnerRef on ElfCluster",
			"namespace", elfCluster.Namespace,
			"elfCluster", elfCluster.Name)

		return reconcile.Result{}, nil
	}

	if annotations.IsPaused(cluster, &elfCluster) {
		r.Logger.V(4).Info("ElfCluster linked to a cluster that is paused",
			"namespace", elfCluster.Namespace,
			"elfCluster", elfCluster.Name)

		return reconcile.Result{}, nil
	}

	// Create the patch helper.
	patchHelper, err := patch.NewHelper(&elfCluster, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(
			err,
			"failed to init patch helper for %s %s/%s",
			elfCluster.GroupVersionKind(),
			elfCluster.Namespace,
			elfCluster.Name)
	}

	// Create the cluster context for this request.
	logger := r.Logger.WithValues("namespace", cluster.Namespace, "elfCluster", elfCluster.Name)

	clusterContext := &context.ClusterContext{
		ControllerContext: r.ControllerContext,
		Cluster:           cluster,
		ElfCluster:        &elfCluster,
		Logger:            logger,
		PatchHelper:       patchHelper,
	}

	// If ElfCluster is being deleting and ForceDeleteCluster flag is set, skip creating the VMService object,
	// because Tower server may be out of service. So we can force delete ElfCluster.
	if elfCluster.ObjectMeta.DeletionTimestamp.IsZero() || !elfCluster.HasForceDeleteCluster() {
		vmService, err := r.NewVMService(r.Context, elfCluster.GetTower(), logger)
		if err != nil {
			conditions.MarkFalse(&elfCluster, infrav1.TowerAvailableCondition, infrav1.TowerUnreachableReason, clusterv1.ConditionSeverityError, err.Error())

			return reconcile.Result{}, err
		}
		conditions.MarkTrue(&elfCluster, infrav1.TowerAvailableCondition)

		clusterContext.VMService = vmService
	}

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
		// always update the readyCondition.
		conditions.SetSummary(clusterContext.ElfCluster,
			conditions.WithConditions(
				infrav1.ControlPlaneEndpointReadyCondition,
				infrav1.TowerAvailableCondition,
			),
		)

		if err := clusterContext.Patch(); err != nil {
			if reterr == nil {
				reterr = err
			}

			clusterContext.Logger.Error(err, "patch failed", "elfCluster", clusterContext.String())
		}
	}()

	// Handle deleted clusters
	if !elfCluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(clusterContext)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(clusterContext)
}

func (r *ElfClusterReconciler) reconcileDelete(ctx *context.ClusterContext) (reconcile.Result, error) {
	ctx.Logger.Info("Reconciling ElfCluster delete")

	elfMachines, err := machineutil.GetElfMachinesInCluster(ctx, ctx.Client, ctx.ElfCluster.Namespace, ctx.ElfCluster.Name)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err,
			"Unable to list ElfMachines part of ElfCluster %s/%s", ctx.ElfCluster.Namespace, ctx.ElfCluster.Name)
	}

	if len(elfMachines) > 0 {
		ctx.Logger.Info("Waiting for ElfMachines to be deleted", "count", len(elfMachines))

		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	// if cluster need to force delete, skipping infra resource deletion and remove the finalizer.
	if !ctx.ElfCluster.HasForceDeleteCluster() {
		if ok, err := r.reconcileDeleteVMPlacementGroups(ctx); err != nil {
			return reconcile.Result{}, errors.Wrapf(err, "failed to delete vm placement groups")
		} else if !ok {
			return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
		}

		if err := r.reconcileDeleteLabels(ctx); err != nil {
			return reconcile.Result{}, errors.Wrapf(err, "failed to delete labels")
		}
	}

	// Cluster is deleted so remove the finalizer.
	ctrlutil.RemoveFinalizer(ctx.ElfCluster, infrav1.ClusterFinalizer)

	return reconcile.Result{}, nil
}

func (r *ElfClusterReconciler) reconcileDeleteVMPlacementGroups(ctx *context.ClusterContext) (bool, error) {
	placementGroupPrefix := towerresources.GetVMPlacementGroupNamePrefix(ctx.Cluster)
	if pgNames, err := ctx.VMService.DeleteVMPlacementGroupsByNamePrefix(ctx, placementGroupPrefix); err != nil {
		return false, err
	} else if len(pgNames) > 0 {
		ctx.Logger.Info(fmt.Sprintf("Waiting for the placement groups with name prefix %s to be deleted", placementGroupPrefix), "count", len(pgNames))

		// Delete placement group caches.
		delPGCaches(pgNames)

		return false, nil
	} else {
		ctx.Logger.Info(fmt.Sprintf("The placement groups with name prefix %s are deleted successfully", placementGroupPrefix))
	}

	return true, nil
}

func (r *ElfClusterReconciler) reconcileDeleteLabels(ctx *context.ClusterContext) error {
	if err := r.reconcileDeleteLabel(ctx, towerresources.GetVMLabelClusterName(), ctx.ElfCluster.Name, true); err != nil {
		return err
	}

	if err := r.reconcileDeleteLabel(ctx, towerresources.GetVMLabelVIP(), ctx.ElfCluster.Spec.ControlPlaneEndpoint.Host, false); err != nil {
		return err
	}

	if err := r.reconcileDeleteLabel(ctx, towerresources.GetVMLabelNamespace(), ctx.ElfCluster.Namespace, true); err != nil {
		return err
	}

	if err := r.reconcileDeleteLabel(ctx, towerresources.GetVMLabelManaged(), "true", true); err != nil {
		return err
	}

	return nil
}

func (r *ElfClusterReconciler) reconcileDeleteLabel(ctx *context.ClusterContext, key, value string, strict bool) error {
	labelID, err := ctx.VMService.DeleteLabel(key, value, strict)
	if err != nil {
		return err
	}
	if labelID != "" {
		ctx.Logger.Info(fmt.Sprintf("Label %s:%s deleted", key, value), "labelId", labelID)
	}

	return nil
}

// cleanLabels cleans unused labels for Tower every day.
// If an error is encountered during the cleanup process,
// it will not be retried and will be started again in the next reconcile.
func (r *ElfClusterReconciler) cleanLabels(ctx *context.ClusterContext) {
	// Locking ensures that only one coroutine cleans at the same time
	if ok := acquireTicketForGCTowerLabels(ctx.ElfCluster.Spec.Tower.Server); ok {
		defer releaseTicketForForGCTowerLabels(ctx.ElfCluster.Spec.Tower.Server)
	} else {
		return
	}

	ctx.Logger.V(1).Info(fmt.Sprintf("Cleaning labels for Tower %s", ctx.ElfCluster.Spec.Tower.Server))

	keys := []string{towerresources.GetVMLabelClusterName(), towerresources.GetVMLabelVIP(), towerresources.GetVMLabelNamespace(), towerresources.GetVMLabelManaged()}
	labelIDs, err := ctx.VMService.CleanLabels(keys)
	if err != nil {
		ctx.Logger.Error(err, fmt.Sprintf("failed to clean labels for Tower %s", ctx.ElfCluster.Spec.Tower.Server))

		return
	}

	recordGCTimeForTowerLabels(ctx.ElfCluster.Spec.Tower.Server)

	ctx.Logger.V(1).Info(fmt.Sprintf("Labels of Tower %s are cleaned successfully", ctx.ElfCluster.Spec.Tower.Server), "labelCount", len(labelIDs))
}

func (r *ElfClusterReconciler) reconcileNormal(ctx *context.ClusterContext) (reconcile.Result, error) { //nolint:unparam
	ctx.Logger.Info("Reconciling ElfCluster")

	// If the ElfCluster doesn't have our finalizer, add it.
	ctrlutil.AddFinalizer(ctx.ElfCluster, infrav1.ClusterFinalizer)

	// If the cluster already has ControlPlaneEndpoint set then there is nothing to do.
	if ok := r.reconcileControlPlaneEndpoint(ctx); !ok {
		return reconcile.Result{}, nil
	}

	// Reconcile the ElfCluster resource's ready state.
	ctx.ElfCluster.Status.Ready = true

	// If the cluster is deleted, that's mean that the workload cluster is being deleted
	if !ctx.Cluster.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}

	r.cleanLabels(ctx)

	// Wait until the API server is online and accessible.
	if !r.isAPIServerOnline(ctx) {
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func (r *ElfClusterReconciler) reconcileControlPlaneEndpoint(ctx *context.ClusterContext) bool {
	if !ctx.ElfCluster.Spec.ControlPlaneEndpoint.IsZero() {
		conditions.MarkTrue(ctx.ElfCluster, infrav1.ControlPlaneEndpointReadyCondition)

		return true
	}

	conditions.MarkFalse(ctx.ElfCluster, infrav1.ControlPlaneEndpointReadyCondition, infrav1.WaitingForVIPReason, clusterv1.ConditionSeverityInfo, "")
	ctx.Logger.Info("The ControlPlaneEndpoint of ElfCluster is not set")

	return false
}

func (r *ElfClusterReconciler) isAPIServerOnline(ctx *context.ClusterContext) bool {
	if kubeClient, err := util.NewKubeClient(ctx, ctx.Client, ctx.Cluster); err == nil {
		if _, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
			// The target cluster is online. To make sure the correct control
			// plane endpoint information is logged, it is necessary to fetch
			// an up-to-date Cluster resource. If this fails, then set the
			// control plane endpoint information to the values from the
			// ElfCluster resource, as it must have the correct information
			// if the API server is online.
			cluster := &clusterv1.Cluster{}
			clusterKey := client.ObjectKey{Namespace: ctx.Cluster.Namespace, Name: ctx.Cluster.Name}
			if err := ctx.Client.Get(ctx, clusterKey, cluster); err != nil {
				cluster = ctx.Cluster.DeepCopy()
				cluster.Spec.ControlPlaneEndpoint.Host = ctx.ElfCluster.Spec.ControlPlaneEndpoint.Host
				cluster.Spec.ControlPlaneEndpoint.Port = ctx.ElfCluster.Spec.ControlPlaneEndpoint.Port

				ctx.Logger.Error(err, "failed to get updated cluster object while checking if API server is online")
			}

			ctx.Logger.Info(
				"API server is online",
				"namespace", cluster.Namespace, "cluster", cluster.Name,
				"controlPlaneEndpoint", cluster.Spec.ControlPlaneEndpoint.String())

			return true
		}
	}

	return false
}
