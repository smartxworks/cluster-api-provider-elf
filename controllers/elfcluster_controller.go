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
	"fmt"
	"reflect"
	"strings"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	clusterutilv1 "sigs.k8s.io/cluster-api/util"
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
	infrautilv1 "github.com/smartxworks/cluster-api-provider-elf/pkg/util"
)

var (
	clusterControlledType     = &infrav1.ElfCluster{}
	clusterControlledTypeName = reflect.TypeOf(clusterControlledType).Elem().Name()
	clusterControlledTypeGVK  = infrav1.GroupVersion.WithKind(clusterControlledTypeName)
)

//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch

// AddClusterControllerToManager adds the cluster controller to the provided
// manager.
func AddClusterControllerToManager(ctx *context.ControllerManagerContext, mgr manager.Manager) error {
	var (
		controllerNameShort = fmt.Sprintf("%s-controller", strings.ToLower(clusterControlledTypeName))
	)

	// Build the controller context.
	controllerContext := &context.ControllerContext{
		ControllerManagerContext: ctx,
		Name:                     controllerNameShort,
		Logger:                   ctx.Logger.WithName(controllerNameShort),
	}

	reconciler := &ElfClusterReconciler{ControllerContext: controllerContext}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(clusterControlledType).
		// Watch the CAPI resource that owns this infrastructure resource.
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: clusterutilv1.ClusterToInfrastructureMapFunc(clusterControlledTypeGVK),
			},
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctx.MaxConcurrentReconciles}).
		Complete(reconciler)
}

// ElfClusterReconciler reconciles a ElfCluster object
type ElfClusterReconciler struct {
	*context.ControllerContext
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r *ElfClusterReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
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
	cluster, err := clusterutilv1.GetOwnerCluster(r, r.Client, elfCluster.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if cluster == nil {
		r.Logger.Info("Waiting for Cluster Controller to set OwnerRef on ElfCluster",
			"namespace", elfCluster.Namespace,
			"elfCluster", elfCluster.Name)

		return reconcile.Result{}, nil
	}

	if clusterutilv1.IsPaused(cluster, &elfCluster) {
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

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
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

	elfMachines, err := infrautilv1.GetElfMachinesInCluster(ctx, ctx.Client, ctx.ElfCluster.Namespace, ctx.ElfCluster.Name)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err,
			"Unable to list ElfMachines part of ElfCluster %s/%s", ctx.ElfCluster.Namespace, ctx.ElfCluster.Name)
	}

	if len(elfMachines) > 0 {
		ctx.Logger.Info("Waiting for ElfMachines to be deleted", "count", len(elfMachines))

		return reconcile.Result{RequeueAfter: config.DefaultRequeue}, nil
	}

	// Cluster is deleted so remove the finalizer.
	ctrlutil.RemoveFinalizer(ctx.ElfCluster, infrav1.ClusterFinalizer)

	return reconcile.Result{}, nil
}

func (r *ElfClusterReconciler) reconcileNormal(ctx *context.ClusterContext) (reconcile.Result, error) {
	ctx.Logger.Info("Reconciling ElfCluster")

	// If the ElfCluster doesn't have our finalizer, add it.
	ctrlutil.AddFinalizer(ctx.ElfCluster, infrav1.ClusterFinalizer)

	// Reconcile the ElfCluster resource's ready state.
	ctx.ElfCluster.Status.Ready = true

	// If the cluster is deleted, that's mean that the workload cluster is being deleted
	if !ctx.Cluster.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}

	// Update the ElfCluster resource with its API endpoints.
	if err := r.reconcileControlPlaneEndpoint(ctx); err != nil {
		return reconcile.Result{}, errors.Wrapf(err,
			"Failed to reconcile ControlPlaneEndpoint for ElfCluster %s/%s",
			ctx.ElfCluster.Namespace, ctx.ElfCluster.Name)
	}

	// Wait until the API server is online and accessible.
	if !r.isAPIServerOnline(ctx) {
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func (r *ElfClusterReconciler) reconcileControlPlaneEndpoint(ctx *context.ClusterContext) error {
	// If the cluster already has ControlPlaneEndpoint set then there is nothing to do .
	if !ctx.ElfCluster.Spec.ControlPlaneEndpoint.IsZero() {
		ctx.Logger.V(6).Info("ControlPlaneEndpoint already exist of ElfCluster")

		return nil
	}

	return infrautilv1.ErrNoMachineIPAddr
}

func (r *ElfClusterReconciler) isAPIServerOnline(ctx *context.ClusterContext) bool {
	if kubeClient, err := infrautilv1.NewKubeClient(ctx, ctx.Client, ctx.Cluster); err == nil {
		if _, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{}); err == nil {
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
