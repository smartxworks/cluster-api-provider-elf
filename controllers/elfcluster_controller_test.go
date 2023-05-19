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
	"flag"
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
		// set log
		if err := flag.Set("logtostderr", "false"); err != nil {
			_ = fmt.Errorf("Error setting logtostderr flag")
		}
		if err := flag.Set("v", "6"); err != nil {
			_ = fmt.Errorf("Error setting v flag")
		}
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
			ctrlMgrContext := fake.NewControllerManagerContext()
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrllog.Log,
			}

			reconciler := &ElfClusterReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("ElfCluster not found, won't reconcile"))
		})

		It("should not error and not requeue the request without cluster", func() {
			ctrlMgrContext := fake.NewControllerManagerContext(elfCluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrllog.Log,
			}

			reconciler := &ElfClusterReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for Cluster Controller to set OwnerRef on ElfCluster"))
		})

		It("should not error and not requeue the request when Cluster is paused", func() {
			cluster.Spec.Paused = true

			ctrlMgrContext := fake.NewControllerManagerContext(cluster, elfCluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrlMgrContext.Logger,
			}
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("ElfCluster linked to a cluster that is paused"))
		})

		It("should add finalizer to the elfcluster", func() {
			elfCluster.Spec.ControlPlaneEndpoint.Host = "127.0.0.1"
			elfCluster.Spec.ControlPlaneEndpoint.Port = 6443
			ctrlMgrContext := fake.NewControllerManagerContext(cluster, elfCluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrllog.Log,
			}
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			elfClusterKey := capiutil.ObjectKey(elfCluster)
			reconciler := &ElfClusterReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(reconciler.Client.Get(reconciler, elfClusterKey, elfCluster)).To(Succeed())
			Expect(elfCluster.Status.Ready).To(BeTrue())
			Expect(elfCluster.Finalizers).To(ContainElement(infrav1.ClusterFinalizer))
			expectConditions(elfCluster, []conditionAssertion{
				{conditionType: clusterv1.ReadyCondition, status: corev1.ConditionTrue},
				{conditionType: infrav1.ControlPlaneEndpointReadyCondition, status: corev1.ConditionTrue},
				{conditionType: infrav1.TowerAvailableCondition, status: corev1.ConditionTrue},
			})
		})

		It("should not reconcile if without ControlPlaneEndpoint", func() {
			ctrlMgrContext := fake.NewControllerManagerContext(cluster, elfCluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrllog.Log,
			}
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("The ControlPlaneEndpoint of ElfCluster is not set"))
			Expect(reconciler.Client.Get(reconciler, capiutil.ObjectKey(elfCluster), elfCluster)).To(Succeed())
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
			ctrlMgrContext := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrllog.Log,
			}
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfClusterReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfClusterKey := capiutil.ObjectKey(elfCluster)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for ElfMachines to be deleted"))
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(reconciler.Client.Get(reconciler, elfClusterKey, elfCluster)).To(Succeed())
			Expect(elfCluster.Finalizers).To(ContainElement(infrav1.ClusterFinalizer))
		})

		It("should delete labels and remove elfcluster finalizer", func() {
			task := fake.NewTowerTask()
			ctrlMgrContext := fake.NewControllerManagerContext(cluster, elfCluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrllog.Log,
			}
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfClusterKey := capiutil.ObjectKey(elfCluster)

			mockVMService.EXPECT().SynchDeleteVMPlacementGroupsByName(towerresources.GetVMPlacementGroupNamePrefix(cluster)).Return(task, errors.New("some error"))

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			task.Status = models.NewTaskStatus(models.TaskStatusSUCCESSED)
			mockVMService.EXPECT().SynchDeleteVMPlacementGroupsByName(towerresources.GetVMPlacementGroupNamePrefix(cluster)).Return(task, nil)
			mockVMService.EXPECT().DeleteLabel(towerresources.GetVMLabelClusterName(), elfCluster.Name, true).Return("labelid", nil)
			mockVMService.EXPECT().DeleteLabel(towerresources.GetVMLabelVIP(), elfCluster.Spec.ControlPlaneEndpoint.Host, false).Return("labelid", nil)
			mockVMService.EXPECT().DeleteLabel(towerresources.GetVMLabelNamespace(), elfCluster.Namespace, true).Return("", nil)
			mockVMService.EXPECT().DeleteLabel(towerresources.GetVMLabelManaged(), "true", true).Return("", nil)

			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(result).To(BeZero())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("Placement groups %s deleted", towerresources.GetVMPlacementGroupNamePrefix(cluster))))
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("Label %s:%s deleted", towerresources.GetVMLabelClusterName(), elfCluster.Name)))
			Expect(apierrors.IsNotFound(reconciler.Client.Get(reconciler, elfClusterKey, elfCluster))).To(BeTrue())
		})

		It("should delete failed when tower is out of service", func() {
			mockNewVMService = func(_ goctx.Context, _ infrav1.Tower, _ logr.Logger) (service.VMService, error) {
				return mockVMService, errors.New("get vm service failed")
			}
			ctrlMgrContext := fake.NewControllerManagerContext(elfCluster, cluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrllog.Log,
			}
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfClusterKey := capiutil.ObjectKey(elfCluster)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())
			Expect(reconciler.Client.Get(reconciler, elfClusterKey, elfCluster)).To(Succeed())
			Expect(elfCluster.Finalizers).To(ContainElement(infrav1.ClusterFinalizer))
		})

		It("should force delete when tower is out of service and cluster need to force delete", func() {
			mockNewVMService = func(_ goctx.Context, _ infrav1.Tower, _ logr.Logger) (service.VMService, error) {
				return mockVMService, errors.New("get vm service failed")
			}
			elfCluster.Annotations = map[string]string{
				infrav1.ElfClusterForceDeleteAnnotation: "",
			}
			ctrlMgrContext := fake.NewControllerManagerContext(elfCluster, cluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrllog.Log,
			}
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfClusterKey := capiutil.ObjectKey(elfCluster)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfClusterKey})
			Expect(result).To(BeZero())
			Expect(err).ToNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring(""))
			Expect(apierrors.IsNotFound(reconciler.Client.Get(reconciler, elfClusterKey, elfCluster))).To(BeTrue())
		})
	})
})
