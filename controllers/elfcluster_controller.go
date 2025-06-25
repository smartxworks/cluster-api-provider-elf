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

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
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
func AddClusterControllerToManager(ctx goctx.Context, ctrlMgrCtx *context.ControllerManagerContext, mgr ctrlmgr.Manager, options controller.Options) error {
	var (
		clusterControlledType     = &infrav1.ElfCluster{}
		clusterControlledTypeName = reflect.TypeOf(clusterControlledType).Elem().Name()
		clusterControlledTypeGVK  = infrav1.GroupVersion.WithKind(clusterControlledTypeName)
	)

	reconciler := &ElfClusterReconciler{
		ControllerManagerContext: ctrlMgrCtx,
		NewVMService:             service.NewVMService,
	}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(clusterControlledType).
		// Watch the CAPI resource that owns this infrastructure resource.
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(capiutil.ClusterToInfrastructureMapFunc(ctx, clusterControlledTypeGVK, mgr.GetClient(), &infrav1.ElfCluster{})),
		).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(ctrl.LoggerFrom(ctx), ctrlMgrCtx.WatchFilterValue)).
		WithOptions(options).
		Complete(reconciler)
}

// ElfClusterReconciler reconciles a ElfCluster object.
type ElfClusterReconciler struct {
	*context.ControllerManagerContext
	NewVMService service.NewVMServiceFunc
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r *ElfClusterReconciler) Reconcile(ctx goctx.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	// Get the ElfCluster resource for this request.
	var elfCluster infrav1.ElfCluster
	if err := r.Client.Get(ctx, req.NamespacedName, &elfCluster); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ElfCluster not found, won't reconcile", "key", req.NamespacedName)

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	// Fetch the CAPI Cluster.
	cluster, err := capiutil.GetOwnerCluster(ctx, r.Client, elfCluster.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if cluster == nil {
		log.Info("Waiting for Cluster Controller to set OwnerRef on ElfCluster")

		return reconcile.Result{}, nil
	}
	log = log.WithValues("Cluster", klog.KObj(cluster))
	ctx = ctrl.LoggerInto(ctx, log)

	if annotations.IsPaused(cluster, &elfCluster) {
		log.V(4).Info("ElfCluster linked to a cluster that is paused")

		return reconcile.Result{}, nil
	}

	// Create the patch helper.
	patchHelper, err := patch.NewHelper(&elfCluster, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to init patch helper")
	}

	// Create the cluster context for this request.
	clusterCtx := &context.ClusterContext{
		Cluster:     cluster,
		ElfCluster:  &elfCluster,
		PatchHelper: patchHelper,
	}

	// If ElfCluster is being deleting and ForceDeleteCluster flag is set, skip creating the VMService object,
	// because Tower server may be out of service. So we can force delete ElfCluster.
	if elfCluster.ObjectMeta.DeletionTimestamp.IsZero() || !elfCluster.HasForceDeleteCluster() {
		vmService, err := r.NewVMService(ctx, elfCluster.GetTower(), log)
		if err != nil {
			conditions.MarkFalse(&elfCluster, infrav1.TowerAvailableCondition, infrav1.TowerUnreachableReason, clusterv1.ConditionSeverityError, err.Error())

			return reconcile.Result{}, err
		}
		conditions.MarkTrue(&elfCluster, infrav1.TowerAvailableCondition)

		clusterCtx.VMService = vmService
	}

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
		// always update the readyCondition.
		conditions.SetSummary(clusterCtx.ElfCluster,
			conditions.WithConditions(
				infrav1.ControlPlaneEndpointReadyCondition,
				infrav1.TowerAvailableCondition,
			),
		)

		if err := clusterCtx.Patch(ctx); err != nil {
			if reterr == nil {
				reterr = err
			}

			clusterCtx.Logger.Error(err, "patch failed", "elfCluster", clusterCtx.String())
		}
	}()

	// Handle deleted clusters
	if !elfCluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, clusterCtx)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(ctx, clusterCtx)
}

func (r *ElfClusterReconciler) reconcileDelete(ctx goctx.Context, clusterCtx *context.ClusterContext) (reconcile.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling ElfCluster delete")

	elfMachines, err := machineutil.GetElfMachinesInCluster(ctx, r.Client, clusterCtx.ElfCluster.Namespace, clusterCtx.ElfCluster.Name)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err,
			"Unable to list ElfMachines part of ElfCluster %s", klog.KObj(clusterCtx.ElfCluster))
	}

	if len(elfMachines) > 0 {
		log.Info("Waiting for ElfMachines to be deleted", "count", len(elfMachines))

		return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
	}

	// if cluster need to force delete, skipping infra resource deletion and remove the finalizer.
	if !clusterCtx.ElfCluster.HasForceDeleteCluster() {
		if ok, err := r.reconcileDeleteVMPlacementGroups(ctx, clusterCtx); err != nil {
			return reconcile.Result{}, errors.Wrapf(err, "failed to delete vm placement groups")
		} else if !ok {
			return reconcile.Result{RequeueAfter: config.DefaultRequeueTimeout}, nil
		}

		if err := r.reconcileDeleteLabels(ctx, clusterCtx); err != nil {
			return reconcile.Result{}, errors.Wrapf(err, "failed to delete labels")
		}
	}

	// Cluster is deleted so remove the finalizer.
	ctrlutil.RemoveFinalizer(clusterCtx.ElfCluster, infrav1.ClusterFinalizer)

	return reconcile.Result{}, nil
}

func (r *ElfClusterReconciler) reconcileDeleteVMPlacementGroups(ctx goctx.Context, clusterCtx *context.ClusterContext) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	placementGroupPrefix := towerresources.GetVMPlacementGroupNamePrefix(clusterCtx.Cluster)
	if pgNames, err := clusterCtx.VMService.DeleteVMPlacementGroupsByNamePrefix(ctx, placementGroupPrefix); err != nil {
		return false, err
	} else if len(pgNames) > 0 {
		log.Info(fmt.Sprintf("Waiting for the placement groups with name prefix %s to be deleted", placementGroupPrefix), "count", len(pgNames))

		// Delete placement group caches.
		delPGCaches(pgNames)

		return false, nil
	} else {
		log.Info(fmt.Sprintf("The placement groups with name prefix %s are deleted successfully", placementGroupPrefix))
	}

	return true, nil
}

func (r *ElfClusterReconciler) reconcileDeleteLabels(ctx goctx.Context, clusterCtx *context.ClusterContext) error {
	if err := r.reconcileDeleteLabel(ctx, clusterCtx, towerresources.GetVMLabelClusterName(), clusterCtx.ElfCluster.Name, true); err != nil {
		return err
	}

	if err := r.reconcileDeleteLabel(ctx, clusterCtx, towerresources.GetVMLabelVIP(), clusterCtx.ElfCluster.Spec.ControlPlaneEndpoint.Host, false); err != nil {
		return err
	}

	if err := r.reconcileDeleteLabel(ctx, clusterCtx, towerresources.GetVMLabelNamespace(), clusterCtx.ElfCluster.Namespace, true); err != nil {
		return err
	}

	if err := r.reconcileDeleteLabel(ctx, clusterCtx, towerresources.GetVMLabelManaged(), "true", true); err != nil {
		return err
	}

	return nil
}

func (r *ElfClusterReconciler) reconcileDeleteLabel(ctx goctx.Context, clusterCtx *context.ClusterContext, key, value string, strict bool) error {
	log := ctrl.LoggerFrom(ctx)

	labelID, err := clusterCtx.VMService.DeleteLabel(key, value, strict)
	if err != nil {
		return err
	}
	if labelID != "" {
		log.Info(fmt.Sprintf("Label %s:%s deleted", key, value), "labelId", labelID)
	}

	return nil
}

// cleanOrphanLabels cleans unused labels for Tower every day.
// If an error is encountered during the cleanup process,
// it will not be retried and will be started again in the next reconcile.
func (r *ElfClusterReconciler) cleanOrphanLabels(ctx goctx.Context, clusterCtx *context.ClusterContext) {
	log := ctrl.LoggerFrom(ctx)

	// Locking ensures that only one coroutine cleans at the same time
	if ok := acquireLockForGCTowerLabels(clusterCtx.ElfCluster.Spec.Tower.Server); ok {
		defer releaseLockForForGCTowerLabels(clusterCtx.ElfCluster.Spec.Tower.Server)
	} else {
		return
	}

	log.V(1).Info(fmt.Sprintf("Cleaning orphan labels in Tower %s created by CAPE", clusterCtx.ElfCluster.Spec.Tower.Server))

	keys := []string{towerresources.GetVMLabelClusterName(), towerresources.GetVMLabelVIP(), towerresources.GetVMLabelNamespace()}
	labelIDs, err := clusterCtx.VMService.CleanUnusedLabels(keys)
	if err != nil {
		log.Error(err, "Warning: failed to clean orphan labels in Tower "+clusterCtx.ElfCluster.Spec.Tower.Server)

		return
	}

	recordGCTimeForTowerLabels(clusterCtx.ElfCluster.Spec.Tower.Server)

	log.V(1).Info(fmt.Sprintf("Labels of Tower %s are cleaned successfully", clusterCtx.ElfCluster.Spec.Tower.Server), "labelCount", len(labelIDs))
}

func (r *ElfClusterReconciler) reconcileNormal(ctx goctx.Context, clusterCtx *context.ClusterContext) (reconcile.Result, error) { //nolint:unparam
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling ElfCluster")

	// If the ElfCluster doesn't have our finalizer, add it.
	ctrlutil.AddFinalizer(clusterCtx.ElfCluster, infrav1.ClusterFinalizer)

	if ok := r.reconcileElfCluster(ctx, clusterCtx); !ok {
		return reconcile.Result{}, nil
	}

	// If the cluster already has ControlPlaneEndpoint set then there is nothing to do.
	if ok := r.reconcileControlPlaneEndpoint(ctx, clusterCtx); !ok {
		return reconcile.Result{}, nil
	}

	// Reconcile the ElfCluster resource's ready state.
	clusterCtx.ElfCluster.Status.Ready = true

	// If the cluster is deleted, that's mean that the workload cluster is being deleted
	if !clusterCtx.Cluster.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}

	r.cleanOrphanLabels(ctx, clusterCtx)

	// Wait until the API server is online and accessible.
	if !r.isAPIServerOnline(ctx, clusterCtx) {
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

// reconcileElfCluster reconciles the Elf cluster.
func (r *ElfClusterReconciler) reconcileElfCluster(ctx goctx.Context, clusterCtx *context.ClusterContext) bool {
	if clusterCtx.ElfCluster.Spec.ClusterType != "" {
		return true
	}

	log := ctrl.LoggerFrom(ctx)
	log.Info("Waiting for the clusterType to be set")

	return false
}

func (r *ElfClusterReconciler) reconcileControlPlaneEndpoint(ctx goctx.Context, clusterCtx *context.ClusterContext) bool {
	if !clusterCtx.ElfCluster.Spec.ControlPlaneEndpoint.IsZero() {
		conditions.MarkTrue(clusterCtx.ElfCluster, infrav1.ControlPlaneEndpointReadyCondition)

		return true
	}

	log := ctrl.LoggerFrom(ctx)
	log.Info("The ControlPlaneEndpoint of ElfCluster is not set")
	conditions.MarkFalse(clusterCtx.ElfCluster, infrav1.ControlPlaneEndpointReadyCondition, infrav1.WaitingForVIPReason, clusterv1.ConditionSeverityInfo, "")

	return false
}

func (r *ElfClusterReconciler) isAPIServerOnline(ctx goctx.Context, clusterCtx *context.ClusterContext) bool {
	log := ctrl.LoggerFrom(ctx)

	if kubeClient, err := util.NewKubeClient(ctx, r.Client, clusterCtx.Cluster); err == nil {
		if _, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
			// The target cluster is online. To make sure the correct control
			// plane endpoint information is logged, it is necessary to fetch
			// an up-to-date Cluster resource. If this fails, then set the
			// control plane endpoint information to the values from the
			// ElfCluster resource, as it must have the correct information
			// if the API server is online.
			cluster := &clusterv1.Cluster{}
			clusterKey := client.ObjectKey{Namespace: clusterCtx.Cluster.Namespace, Name: clusterCtx.Cluster.Name}
			if err := r.Client.Get(ctx, clusterKey, cluster); err != nil {
				cluster = clusterCtx.Cluster.DeepCopy()
				cluster.Spec.ControlPlaneEndpoint.Host = clusterCtx.ElfCluster.Spec.ControlPlaneEndpoint.Host
				cluster.Spec.ControlPlaneEndpoint.Port = clusterCtx.ElfCluster.Spec.ControlPlaneEndpoint.Port

				log.Error(err, "failed to get updated cluster object while checking if API server is online")
			}

			log.Info(
				"API server is online",
				"controlPlaneEndpoint", cluster.Spec.ControlPlaneEndpoint.String())

			return true
		}
	}

	return false
}
