/*
Copyright 2022.

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
	"bytes"
	goctx "context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service/mock_services"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

const (
	Timeout = time.Second * 30
)

var _ = Describe("ElfClusterReconciler", func() {
	var (
		elfCluster       *infrav1.ElfCluster
		cluster          *clusterv1.Cluster
		logBuffer        *bytes.Buffer
		mockCtrl         *gomock.Controller
		mockVMService    *mock_services.MockVMService
		mockNewVMService service.NewVMServiceFunc
	)

	ctx := goctx.Background()

	BeforeEach(func() {
		logBuffer = new(bytes.Buffer)
		klog.SetOutput(logBuffer)

		elfCluster, cluster = fake.NewClusterObjects()
		// mock
		mockCtrl = gomock.NewController(GinkgoT())
		mockVMService = mock_services.NewMockVMService(mockCtrl)
		mockNewVMService = func(_ goctx.Context, _ infrav1.Tower, _ logr.Logger) (service.VMService, error) {
			return mockVMService, nil
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("Reconcile an ElfCluster", func() {
		It("should not reconcile when ElfCluster not found", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext()
			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("ElfCluster not found, won't reconcile"))
		})

		It("should not error and not requeue the request without cluster", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster)
			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for Cluster Controller to set OwnerRef on ElfCluster"))
		})

		It("should not error and not requeue the request when Cluster is paused", func() {
			cluster.Spec.Paused = true

			ctrlMgrCtx := fake.NewControllerManagerContext(cluster, elfCluster)
			fake.InitClusterOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("ElfCluster linked to a cluster that is paused"))
		})

		It("should add finalizer to the elfcluster", func() {
			elfCluster.Spec.ControlPlaneEndpoint.Host = "127.0.0.1"
			elfCluster.Spec.ControlPlaneEndpoint.Port = 6443
			ctrlMgrCtx := fake.NewControllerManagerContext(cluster, elfCluster)
			fake.InitClusterOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster)

			keys := []string{towerresources.GetVMLabelClusterName(), towerresources.GetVMLabelVIP(), towerresources.GetVMLabelNamespace()}
			mockVMService.EXPECT().CleanUnusedLabels(keys).Return(nil, nil)

			elfClusterKey := capiutil.ObjectKey(elfCluster)
			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(reconciler.Client.Get(ctx, elfClusterKey, elfCluster)).To(Succeed())
			Expect(elfCluster.Status.Ready).To(BeTrue())
			Expect(elfCluster.Finalizers).To(ContainElement(infrav1.ClusterFinalizer))
			expectConditions(elfCluster, []conditionAssertion{
				{conditionType: clusterv1.ReadyCondition, status: corev1.ConditionTrue},
				{conditionType: infrav1.ControlPlaneEndpointReadyCondition, status: corev1.ConditionTrue},
				{conditionType: infrav1.TowerAvailableCondition, status: corev1.ConditionTrue},
			})
		})

		It("should not reconcile if without ControlPlaneEndpoint", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(cluster, elfCluster)
			fake.InitClusterOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("The ControlPlaneEndpoint of ElfCluster is not set"))
			Expect(reconciler.Client.Get(ctx, capiutil.ObjectKey(elfCluster), elfCluster)).To(Succeed())
			expectConditions(elfCluster, []conditionAssertion{
				{clusterv1.ReadyCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForVIPReason},
				{infrav1.ControlPlaneEndpointReadyCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForVIPReason},
				{conditionType: infrav1.TowerAvailableCondition, status: corev1.ConditionTrue},
			})
		})
	})

	Context("Delete a ElfCluster", func() {
		BeforeEach(func() {
			ctrlutil.AddFinalizer(elfCluster, infrav1.ClusterFinalizer)
			elfCluster.DeletionTimestamp = &metav1.Time{Time: time.Now().UTC()}
		})

		It("should not remove elfcluster finalizer when has elfmachines", func() {
			elfMachine, machine := fake.NewMachineObjects(elfCluster, cluster)
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			elfClusterKey := capiutil.ObjectKey(elfCluster)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for ElfMachines to be deleted"))
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(reconciler.Client.Get(ctx, elfClusterKey, elfCluster)).To(Succeed())
			Expect(elfCluster.Finalizers).To(ContainElement(infrav1.ClusterFinalizer))
		})

		It("should delete labels and remove elfcluster finalizer", func() {
			task := fake.NewTowerTask("")
			ctrlMgrCtx := fake.NewControllerManagerContext(cluster, elfCluster)
			fake.InitClusterOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			elfClusterKey := capiutil.ObjectKey(elfCluster)

			mockVMService.EXPECT().DeleteVMPlacementGroupsByNamePrefix(gomock.Any(), towerresources.GetVMPlacementGroupNamePrefix(cluster)).Return(nil, errors.New("some error"))

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())

			task.Status = models.NewTaskStatus(models.TaskStatusSUCCESSED)
			logBuffer.Reset()
			pg := fake.NewVMPlacementGroup(nil)
			setPGCache(pg)
			placementGroupPrefix := towerresources.GetVMPlacementGroupNamePrefix(cluster)
			mockVMService.EXPECT().DeleteVMPlacementGroupsByNamePrefix(gomock.Any(), placementGroupPrefix).Return([]string{*pg.Name}, nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(result).NotTo(BeZero())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("Waiting for the placement groups with name prefix %s to be deleted", placementGroupPrefix)))
			Expect(getPGFromCache(*pg.Name)).To(BeNil())

			logBuffer.Reset()
			mockVMService.EXPECT().DeleteVMPlacementGroupsByNamePrefix(gomock.Any(), placementGroupPrefix).Return(nil, nil)
			mockVMService.EXPECT().DeleteLabel(towerresources.GetVMLabelClusterName(), elfCluster.Name, true).Return("labelid", nil)
			mockVMService.EXPECT().DeleteLabel(towerresources.GetVMLabelVIP(), elfCluster.Spec.ControlPlaneEndpoint.Host, false).Return("labelid", nil)
			mockVMService.EXPECT().DeleteLabel(towerresources.GetVMLabelNamespace(), elfCluster.Namespace, true).Return("", nil)
			mockVMService.EXPECT().DeleteLabel(towerresources.GetVMLabelManaged(), "true", true).Return("", nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(result).To(BeZero())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("The placement groups with name prefix %s are deleted successfully", placementGroupPrefix)))
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("Label %s:%s deleted", towerresources.GetVMLabelClusterName(), elfCluster.Name)))
			Expect(apierrors.IsNotFound(reconciler.Client.Get(ctx, elfClusterKey, elfCluster))).To(BeTrue())
		})

		It("should delete failed when tower is out of service", func() {
			mockNewVMService = func(_ goctx.Context, _ infrav1.Tower, _ logr.Logger) (service.VMService, error) {
				return mockVMService, errors.New("get vm service failed")
			}
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster)
			fake.InitClusterOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			elfClusterKey := capiutil.ObjectKey(elfCluster)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())
			Expect(reconciler.Client.Get(ctx, elfClusterKey, elfCluster)).To(Succeed())
			Expect(elfCluster.Finalizers).To(ContainElement(infrav1.ClusterFinalizer))
		})

		It("should force delete when tower is out of service and cluster need to force delete", func() {
			mockNewVMService = func(_ goctx.Context, _ infrav1.Tower, _ logr.Logger) (service.VMService, error) {
				return mockVMService, errors.New("get vm service failed")
			}
			elfCluster.Annotations = map[string]string{
				infrav1.ElfClusterForceDeleteAnnotation: "",
			}
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster)
			fake.InitClusterOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			elfClusterKey := capiutil.ObjectKey(elfCluster)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(result).To(BeZero())
			Expect(err).ToNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring(""))
			Expect(apierrors.IsNotFound(reconciler.Client.Get(ctx, elfClusterKey, elfCluster))).To(BeTrue())
		})
	})

	Context("CleanUnusedLabels", func() {
		BeforeEach(func() {
			resetMemoryCache()
		})

		It("should clean labels for Tower", func() {
			elfCluster.Spec.ControlPlaneEndpoint.Host = "127.0.0.1"
			elfCluster.Spec.ControlPlaneEndpoint.Port = 6443
			ctrlMgrCtx := fake.NewControllerManagerContext(cluster, elfCluster)
			fake.InitClusterOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster)
			clusterCtx := &context.ClusterContext{
				Cluster:    cluster,
				ElfCluster: elfCluster,
				Logger:     ctrllog.Log,
				VMService:  mockVMService,
			}

			logBuffer.Reset()
			keys := []string{towerresources.GetVMLabelClusterName(), towerresources.GetVMLabelVIP(), towerresources.GetVMLabelNamespace()}
			mockVMService.EXPECT().CleanUnusedLabels(keys).Return(nil, unexpectedError)
			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			reconciler.cleanOrphanLabels(ctx, clusterCtx)
			Expect(logBuffer.String()).To(ContainSubstring("Warning: failed to clean orphan labels in Tower " + elfCluster.Spec.Tower.Server))

			logBuffer.Reset()
			mockVMService.EXPECT().CleanUnusedLabels(keys).Return(nil, nil)
			reconciler.cleanOrphanLabels(ctx, clusterCtx)
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("Labels of Tower %s are cleaned successfully", elfCluster.Spec.Tower.Server)))

			logBuffer.Reset()
			reconciler.cleanOrphanLabels(ctx, clusterCtx)
			Expect(logBuffer.String()).NotTo(ContainSubstring(fmt.Sprintf("Cleaning orphan labels in Tower %s created by CAPE", elfCluster.Spec.Tower.Server)))
		})
	})

	Context("reconcileElfCluster", func() {
		It("should return true when clusterType is stretched", func() {
			elfCluster.Spec.ClusterType = infrav1.ElfClusterTypeStretched
			ctrlMgrCtx := fake.NewControllerManagerContext(cluster, elfCluster)
			fake.InitClusterOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster)
			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok := reconciler.reconcileElfCluster(ctx, &context.ClusterContext{
				Cluster:    cluster,
				ElfCluster: elfCluster,
				Logger:     ctrllog.Log,
				VMService:  mockVMService,
			})
			Expect(ok).To(BeTrue())
		})

		It("should return false when clusterType is not set", func() {
			elfCluster.Spec.ClusterType = ""
			ctrlMgrCtx := fake.NewControllerManagerContext(cluster, elfCluster)
			fake.InitClusterOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster)
			reconciler := &ElfClusterReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok := reconciler.reconcileElfCluster(ctx, &context.ClusterContext{
				Cluster:    cluster,
				ElfCluster: elfCluster,
				Logger:     ctrllog.Log,
				VMService:  mockVMService,
			})
			Expect(ok).To(BeFalse())
		})
	})
})
