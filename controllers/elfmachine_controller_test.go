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
	"strings"
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	capierrors "sigs.k8s.io/cluster-api/errors"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	capeerrors "github.com/smartxworks/cluster-api-provider-elf/pkg/errors"
	towerresources "github.com/smartxworks/cluster-api-provider-elf/pkg/resources"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service/mock_services"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/util"
	machineutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/machine"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
	"github.com/smartxworks/cluster-api-provider-elf/test/helpers"
)

var _ = Describe("ElfMachineReconciler", func() {
	var (
		elfCluster       *infrav1.ElfCluster
		cluster          *clusterv1.Cluster
		elfMachine       *infrav1.ElfMachine
		machine          *clusterv1.Machine
		kcp              *controlplanev1.KubeadmControlPlane
		md               *clusterv1.MachineDeployment
		secret           *corev1.Secret
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

		elfCluster, cluster, elfMachine, machine, secret = fake.NewClusterAndMachineObjects()
		kcp = fake.NewKCP()
		md = fake.NewMD()
		fake.ToWorkerMachine(machine, md)
		fake.ToWorkerMachine(elfMachine, md)

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

	Context("Reconcile an ElfMachine", func() {
		It("should not error and not requeue the request without machine", func() {
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext}

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for Machine Controller to set OwnerRef on ElfMachine"))
		})

		It("should not error and not requeue the request when Cluster is paused", func() {
			cluster.Spec.Paused = true
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("ElfMachine linked to a cluster that is paused"))
		})

		It("should exit immediately on an error state", func() {
			createMachineError := capierrors.CreateMachineError
			elfMachine.Status.FailureReason = &createMachineError
			elfMachine.Status.FailureMessage = pointer.String("Couldn't create machine")
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Error state detected, skipping reconciliation"))
		})

		It("should add our finalizer to the machine", func() {
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Finalizers).To(ContainElement(infrav1.MachineFinalizer))
		})

		It("should exit immediately if cluster infra isn't ready", func() {
			cluster.Status.InfrastructureReady = false
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err).To(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Cluster infrastructure is not ready yet"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForClusterInfrastructureReason}})
		})

		It("should exit immediately if bootstrap data secret reference isn't available", func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err).To(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for bootstrap data to be available"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForBootstrapDataReason}})
		})

		It("should wait cluster ControlPlaneInitialized true when create worker machine", func() {
			cluster.Status.InfrastructureReady = true
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err).To(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the control plane to be initialized"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, clusterv1.WaitingForControlPlaneAvailableReason}})
		})

		It("should not wait cluster ControlPlaneInitialized true when create master machine", func() {
			cluster.Status.InfrastructureReady = true
			elfMachine.Labels[clusterv1.MachineControlPlaneLabel] = ""
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err).To(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for bootstrap data to be available"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForBootstrapDataReason}})
		})
	})

	Context("Reconcile ElfMachine VM", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
		})

		It("should set CloningFailedReason condition when failed to retrieve bootstrap data", func() {
			machine.Spec.Bootstrap.DataSecretName = pointer.String("notfound")
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).Should(HaveOccurred())
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.CloningFailedReason}})
		})

		It("should create a new VM if none exists", func() {
			vm := fake.NewTowerVM()
			vm.Name = &elfMachine.Name
			task := fake.NewTowerTask()
			withTaskVM := fake.NewWithTaskVM(vm, task)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Clone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(withTaskVM, nil)
			mockVMService.EXPECT().Get(*vm.ID).Return(vm, nil)
			mockVMService.EXPECT().GetTask(*task.ID).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).Should(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM task done"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.ID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
		})

		It("should recover from lost task", func() {
			vm := fake.NewTowerVM()
			vm.Name = &elfMachine.Name
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Clone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New(service.VMDuplicate))
			mockVMService.EXPECT().GetByName(elfMachine.Name).Return(vm, nil)
			mockVMService.EXPECT().Get(*vm.ID).Return(vm, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).Should(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM task done"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.ID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
		})

		It("should handle clone error", func() {
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Clone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(err.Error()).To(ContainSubstring("failed to reconcile VM"))
			Expect(elfMachine.Status.VMRef).To(Equal(""))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.CloningFailedReason}})
		})

		It("should allow VM to be temporarily disconnected", func() {
			towerCluster := fake.NewTowerCluster()
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusRUNNING
			vm.Status = &status
			elfMachine.Status.VMRef = *vm.LocalID
			now := metav1.NewTime(time.Now().Add(-infrav1.VMDisconnectionTimeout))
			elfMachine.SetVMDisconnectionTimestamp(&now)
			placementGroup := fake.NewVMPlacementGroup([]string{*vm.ID})
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			mockVMService.EXPECT().UpsertLabel(gomock.Any(), gomock.Any()).Times(3).Return(fake.NewTowerLabel(), nil)
			mockVMService.EXPECT().AddLabelsToVM(gomock.Any(), gomock.Any()).Times(1)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.GetVMDisconnectionTimestamp()).To(BeNil())
		})

		It("should set failure when VM was deleted", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			elfMachine.Status.VMRef = *vm.LocalID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Times(2).Return(nil, errors.New(service.VMNotFound))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).NotTo(HaveOccurred())
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.GetVMDisconnectionTimestamp()).NotTo(BeNil())

			patchHelper, err := patch.NewHelper(elfMachine, reconciler.Client)
			Expect(err).To(BeNil())
			now := metav1.NewTime(time.Now().Add(-infrav1.VMDisconnectionTimeout))
			elfMachine.SetVMDisconnectionTimestamp(&now)
			Expect(patchHelper.Patch(ctx, elfMachine)).To(Succeed())
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).NotTo(HaveOccurred())
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(*elfMachine.Status.FailureReason).To(Equal(capeerrors.RemovedFromInfrastructureError))
			Expect(*elfMachine.Status.FailureMessage).To(Equal(fmt.Sprintf("Unable to find VM by UUID %s. The VM was removed from infrastructure.", elfMachine.Status.VMRef)))
		})

		It("should set ElfMachine to failure when VM was moved to the recycle bin", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			vm.InRecycleBin = pointer.Bool(true)
			elfMachine.Status.VMRef = *vm.LocalID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).Should(BeNil())
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(*elfMachine.Status.FailureReason).To(Equal(capeerrors.MovedToRecycleBinError))
			Expect(*elfMachine.Status.FailureMessage).To(Equal(fmt.Sprintf("The VM %s was moved to the Tower recycle bin by users, so treat it as deleted.", *vm.LocalID)))
			Expect(elfMachine.HasVM()).To(BeFalse())
		})

		It("should retry when create a VM if failed", func() {
			vm := fake.NewTowerVM()
			task := fake.NewTowerTask()
			status := models.TaskStatusFAILED
			task.Status = &status
			elfMachine.Status.VMRef = *vm.ID
			elfMachine.Status.TaskRef = *task.ID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(nil, errors.New(service.VMNotFound))
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("failed to create VM for ElfMachine"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(""))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.TaskFailureReason}})
		})

		It("should set failure when task with cloud-init config error", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			task := fake.NewTowerTask()
			status := models.TaskStatusFAILED
			task.Status = &status
			task.ErrorMessage = util.TowerString("Cannot unwrap Ok value of Result.Err.\r\ncode: CREATE_VM_FORM_TEMPLATE_FAILED\r\nmessage: {\"data\":{},\"ec\":\"VM_CLOUD_INIT_CONFIG_ERROR\",\"error\":{\"msg\":\"[VM_CLOUD_INIT_CONFIG_ERROR]The gateway [192.168.31.215] is unreachable. \"}}")
			elfMachine.Status.VMRef = *vm.ID
			elfMachine.Status.TaskRef = *task.ID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(nil, errors.New(service.VMNotFound))
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).Should(BeNil())
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(""))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			Expect(elfMachine.IsFailed()).To(BeTrue())
			Expect(*elfMachine.Status.FailureReason).To(Equal(capeerrors.CloudInitConfigError))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.TaskFailureReason}})
		})

		It("should power on the VM after it is created", func() {
			towerCluster := fake.NewTowerCluster()
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			task1 := fake.NewTowerTask()
			taskStatus := models.TaskStatusSUCCESSED
			task1.Status = &taskStatus
			task2 := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.ID
			elfMachine.Status.TaskRef = *task1.ID
			placementGroup := fake.NewVMPlacementGroup([]string{*vm.ID})
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task1, nil)
			mockVMService.EXPECT().PowerOn(*vm.LocalID).Return(task2, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).Should(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM to be powered on"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task2.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.PoweringOnReason}})
		})

		It("should wait for the ELF virtual machine to be created", func() {
			vm := fake.NewTowerVM()
			placeholderID := fmt.Sprintf("placeholder-%s", *vm.LocalID)
			vm.LocalID = &placeholderID
			vm.EntityAsyncStatus = nil
			task := fake.NewTowerTask()
			taskStatus := models.TaskStatusFAILED
			task.Status = &taskStatus
			elfMachine.Status.VMRef = *vm.ID
			elfMachine.Status.TaskRef = *task.ID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).Should(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("VM state is not reconciled"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.ID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.TaskFailureReason}})
		})

		It("should handle power on error", func() {
			towerCluster := fake.NewTowerCluster()
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			task1 := fake.NewTowerTask()
			taskStatus := models.TaskStatusSUCCESSED
			task1.Status = &taskStatus
			elfMachine.Status.VMRef = *vm.ID
			elfMachine.Status.TaskRef = *task1.ID
			placementGroup := fake.NewVMPlacementGroup([]string{*vm.ID})
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task1, nil)
			mockVMService.EXPECT().PowerOn(*vm.LocalID).Return(nil, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err.Error()).To(ContainSubstring("failed to trigger power on for VM"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.PoweringOnFailedReason}})
		})

		It(" handle power on task failure", func() {
			towerCluster := fake.NewTowerCluster()
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			task1 := fake.NewTowerTask()
			taskStatus := models.TaskStatusFAILED
			task1.Status = &taskStatus
			task2 := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID
			elfMachine.Status.TaskRef = *task1.ID
			placementGroup := fake.NewVMPlacementGroup([]string{*vm.ID})
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task1, nil)
			mockVMService.EXPECT().PowerOn(*vm.LocalID).Return(task2, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("task failed"))
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM to be powered on"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task2.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.PoweringOnReason}})
		})
	})

	Context("Reconcile Placement Group", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
		})

		It("should create a new placement group and add vm to the placement group", func() {
			towerCluster := fake.NewTowerCluster()
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			task := fake.NewTowerTask()
			taskStatus := models.TaskStatusSUCCESSED
			task.Status = &taskStatus
			withTaskVMPlacementGroup := fake.NewWithTaskVMPlacementGroup(nil, task)
			elfMachine.Status.VMRef = *vm.LocalID
			placementGroup := fake.NewVMPlacementGroup(nil)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(nil, errors.New(service.VMPlacementGroupNotFound))
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			mockVMService.EXPECT().CreateVMPlacementGroup(gomock.Any(), *towerCluster.ID, towerresources.GetVMPlacementGroupPolicy(machine)).Return(withTaskVMPlacementGroup, nil)
			mockVMService.EXPECT().AddVMsToPlacementGroup(placementGroup, []string{*vm.ID}).Return(task, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Times(2).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcilePlacementGroup(machineContext, vm)
			Expect(ok).To(BeTrue())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Creating placement group succeeded"))
			Expect(logBuffer.String()).To(ContainSubstring("Updating placement group succeeded"))
		})

		It("createPlacementGroup", func() {
			towerCluster := fake.NewTowerCluster()
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			task := fake.NewTowerTask()
			taskStatus := models.TaskStatusSUCCESSED
			task.Status = &taskStatus
			withTaskVMPlacementGroup := fake.NewWithTaskVMPlacementGroup(nil, task)
			elfMachine.Status.VMRef = *vm.LocalID
			placementGroup := fake.NewVMPlacementGroup(nil)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().CreateVMPlacementGroup(gomock.Any(), *towerCluster.ID, towerresources.GetVMPlacementGroupPolicy(machine)).Return(withTaskVMPlacementGroup, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			pg, err := reconciler.createPlacementGroup(machineContext, *placementGroup.Name, *towerCluster.ID)
			Expect(err).To(BeZero())
			Expect(*pg.Name).To(Equal(*placementGroup.Name))
			Expect(logBuffer.String()).To(ContainSubstring("Creating placement group succeeded"))

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			taskStatus = models.TaskStatusFAILED
			task.Status = &taskStatus
			mockVMService.EXPECT().CreateVMPlacementGroup(gomock.Any(), *towerCluster.ID, towerresources.GetVMPlacementGroupPolicy(machine)).Return(withTaskVMPlacementGroup, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)

			pg, err = reconciler.createPlacementGroup(machineContext, *placementGroup.Name, *towerCluster.ID)
			Expect(pg).To(BeNil())
			Expect(strings.Contains(err.Error(), "failed to create placement group")).To(BeTrue())

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			mockVMService.EXPECT().CreateVMPlacementGroup(gomock.Any(), *towerCluster.ID, towerresources.GetVMPlacementGroupPolicy(machine)).Return(withTaskVMPlacementGroup, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(nil, errors.New("xxx"))

			pg, err = reconciler.createPlacementGroup(machineContext, *placementGroup.Name, *towerCluster.ID)
			Expect(pg).To(BeNil())
			Expect(err).To(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Wait for placement group creation task done timed out"))
		})

		It("addVMsToPlacementGroup", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			task := fake.NewTowerTask()
			taskStatus := models.TaskStatusSUCCESSED
			task.Status = &taskStatus
			elfMachine.Status.VMRef = *vm.LocalID
			placementGroup := fake.NewVMPlacementGroup(nil)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().AddVMsToPlacementGroup(placementGroup, []string{*vm.ID}).Return(task, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.addVMsToPlacementGroup(machineContext, placementGroup, []string{*vm.ID})
			Expect(ok).To(BeTrue())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Updating placement group succeeded"))

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			taskStatus = models.TaskStatusFAILED
			task.Status = &taskStatus
			mockVMService.EXPECT().AddVMsToPlacementGroup(placementGroup, []string{*vm.ID}).Return(task, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)

			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err = reconciler.addVMsToPlacementGroup(machineContext, placementGroup, []string{*vm.ID})
			Expect(ok).To(BeFalse())
			Expect(strings.Contains(err.Error(), "failed to update placement group")).To(BeTrue())

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			mockVMService.EXPECT().AddVMsToPlacementGroup(placementGroup, []string{*vm.ID}).Return(task, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(nil, errors.New("xxx"))

			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err = reconciler.addVMsToPlacementGroup(machineContext, placementGroup, []string{*vm.ID})
			Expect(ok).To(BeFalse())
			Expect(err).To(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Wait for placement group updation task done timed out"))
		})

		It("should wait for placement group task done", func() {
			towerCluster := fake.NewTowerCluster()
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			elfMachine.Status.VMRef = *vm.LocalID
			placementGroup := fake.NewVMPlacementGroup(nil)
			placementGroup.EntityAsyncStatus = models.EntityAsyncStatusUPDATING.Pointer()
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for placement group task done"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.JoiningPlacementGroupReason}})
		})

		It("should handle placement group error", func() {
			towerCluster := fake.NewTowerCluster()
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			elfMachine.Status.VMRef = *vm.LocalID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(nil, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).To(HaveOccurred())
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.JoiningPlacementGroupFailedReason}})
			Expect(conditions.GetMessage(elfMachine, infrav1.VMProvisionedCondition)).To(Equal("some error"))
		})

		Context("Reconcile Placement Group - Control Plane", func() {
			BeforeEach(func() {
				cluster.Status.InfrastructureReady = true
				conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
				machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
				fake.ToControlPlaneMachine(machine, kcp)
				fake.ToControlPlaneMachine(elfMachine, kcp)
			})

			It("should not be added when placement group is full", func() {
				towerCluster := fake.NewTowerCluster()
				towerCluster.HostNum = util.TowerInt32(1)
				vm := fake.NewTowerVM()
				vm.EntityAsyncStatus = nil
				elfMachine.Status.VMRef = *vm.LocalID
				placementGroup := fake.NewVMPlacementGroup([]string{fake.UUID()})
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kcp)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

				mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err := reconciler.reconcilePlacementGroup(machineContext, vm)
				Expect(ok).To(BeTrue())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring("The placement group is full, skip adding VM to the placement group"))
			})

			It("should add VM to placement group when VM is not in placement group and the host where VM in is not in placement group", func() {
				towerCluster := fake.NewTowerCluster()
				towerCluster.HostNum = util.TowerInt32(2)
				hostID1 := fake.UUID()
				hostID2 := fake.UUID()
				towerCluster.Hosts = []*models.NestedHost{{ID: util.TowerString(hostID1)}, {ID: util.TowerString(hostID2)}}
				vm := fake.NewTowerVM()
				vm.EntityAsyncStatus = nil
				vm.Host = &models.NestedHost{ID: util.TowerString(hostID1)}
				elfMachine.Status.VMRef = *vm.LocalID
				vm2 := fake.NewTowerVM()
				vm2.Host = &models.NestedHost{ID: util.TowerString(hostID2)}
				task := fake.NewTowerTask()
				taskStatus := models.TaskStatusSUCCESSED
				task.Status = &taskStatus
				placementGroup := fake.NewVMPlacementGroup([]string{*vm2.ID})
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kcp)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

				mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().FindByIDs([]string{*vm2.ID}).Return([]*models.VM{vm2}, nil)
				mockVMService.EXPECT().AddVMsToPlacementGroup(placementGroup, gomock.Any()).Return(task, nil)
				mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err := reconciler.reconcilePlacementGroup(machineContext, vm)
				Expect(ok).To(BeTrue())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring("Updating placement group succeeded"))
			})

			It("should not migrate VM when VM is running and kcp.Spec.Replicas != kcp.Status.UpdatedReplicas", func() {
				towerCluster := fake.NewTowerCluster()
				towerCluster.HostNum = util.TowerInt32(2)
				hostID1 := fake.UUID()
				hostID2 := fake.UUID()
				towerCluster.Hosts = []*models.NestedHost{{ID: util.TowerString(hostID1)}, {ID: util.TowerString(hostID2)}}
				vm := fake.NewTowerVM()
				status := models.VMStatusRUNNING
				vm.Status = &status
				vm.EntityAsyncStatus = nil
				vm.Host = &models.NestedHost{ID: util.TowerString(hostID2)}
				elfMachine.Status.VMRef = *vm.LocalID
				vm2 := fake.NewTowerVM()
				vm2.Host = &models.NestedHost{ID: util.TowerString(hostID2)}
				placementGroup := fake.NewVMPlacementGroup([]string{*vm2.ID})
				kcp.Spec.Replicas = pointer.Int32(2)
				kcp.Status.UpdatedReplicas = 1
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kcp)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

				mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().FindByIDs([]string{*vm2.ID}).Return([]*models.VM{vm2}, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err := reconciler.reconcilePlacementGroup(machineContext, vm)
				Expect(ok).To(BeTrue())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring("KCP rolling update in progress, skip migrate VM"))
			})

			It("should migrate VM to another host when the VM is running and the host of VM is not in unused hosts", func() {
				towerCluster := fake.NewTowerCluster()
				towerCluster.HostNum = util.TowerInt32(2)
				hostID1 := fake.UUID()
				hostID2 := fake.UUID()
				towerCluster.Hosts = []*models.NestedHost{{ID: util.TowerString(hostID1)}, {ID: util.TowerString(hostID2)}}
				vm := fake.NewTowerVM()
				status := models.VMStatusRUNNING
				vm.Status = &status
				vm.EntityAsyncStatus = nil
				vm.Host = &models.NestedHost{ID: util.TowerString(hostID2)}
				elfMachine.Status.VMRef = *vm.LocalID
				vm2 := fake.NewTowerVM()
				vm2.Host = &models.NestedHost{ID: util.TowerString(hostID2)}
				task := fake.NewTowerTask()
				withTaskVM := fake.NewWithTaskVM(vm, task)
				placementGroup := fake.NewVMPlacementGroup([]string{*vm2.ID})
				kcp.Spec.Replicas = pointer.Int32(2)
				kcp.Status.UpdatedReplicas = 2
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kcp)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

				mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().FindByIDs([]string{*vm2.ID}).Return([]*models.VM{vm2}, nil)
				mockVMService.EXPECT().Migrate(*vm.ID, hostID1).Return(withTaskVM, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err := reconciler.reconcilePlacementGroup(machineContext, vm)
				Expect(ok).To(BeFalse())
				Expect(err).To(BeZero())
				Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
				Expect(logBuffer.String()).To(ContainSubstring("Waiting for the VM to be migrated from"))
				expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.JoiningPlacementGroupReason}})
			})
		})
	})

	Context("Reconcile VM Host", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
			fake.ToControlPlaneMachine(machine, kcp)
			fake.ToControlPlaneMachine(elfMachine, kcp)
		})

		It("should not set host when kcp.Spec.Replicas > kcp.Status.Replicas", func() {
			kcp.Spec.Replicas = pointer.Int32(1)
			kcp.Status.Replicas = 0
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			host, err := reconciler.getVMHostForRollingUpdate(machineContext)
			Expect(err).To(BeZero())
			Expect(host).To(BeEmpty())
		})

		It("should not set host when placement group does not exist", func() {
			kcp.Spec.Replicas = pointer.Int32(1)
			kcp.Status.Replicas = 1
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(nil, errors.New(service.VMPlacementGroupNotFound))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			host, err := reconciler.getVMHostForRollingUpdate(machineContext)
			Expect(err).To(BeZero())
			Expect(host).To(BeEmpty())
		})

		It("should not set host when placement group has capacity", func() {
			towerCluster := fake.NewTowerCluster()
			towerCluster.HostNum = util.TowerInt32(1)
			placementGroup := fake.NewVMPlacementGroup([]string{})
			kcp.Spec.Replicas = pointer.Int32(1)
			kcp.Status.Replicas = 1
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			host, err := reconciler.getVMHostForRollingUpdate(machineContext)
			Expect(err).To(BeZero())
			Expect(host).To(BeEmpty())
		})

		It("should set host when placement group is full", func() {
			towerCluster := fake.NewTowerCluster()
			towerCluster.HostNum = util.TowerInt32(3)
			elfMachine2, machine2 := fake.NewMachineObjects(elfCluster, cluster)
			elfMachine3, machine3 := fake.NewMachineObjects(elfCluster, cluster)
			elfMachine4, machine4 := fake.NewMachineObjects(elfCluster, cluster)
			machine2.CreationTimestamp = metav1.Now()
			machine3.CreationTimestamp = metav1.NewTime(time.Now().Add(1 * time.Minute))
			machine4.CreationTimestamp = metav1.NewTime(time.Now().Add(2 * time.Minute))
			fake.ToControlPlaneMachine(machine2, kcp)
			fake.ToControlPlaneMachine(elfMachine2, kcp)
			fake.ToControlPlaneMachine(machine3, kcp)
			fake.ToControlPlaneMachine(elfMachine3, kcp)
			fake.ToControlPlaneMachine(machine4, kcp)
			fake.ToControlPlaneMachine(elfMachine4, kcp)
			elfMachine2.Status.VMRef = fake.UUID()
			elfMachine3.Status.VMRef = fake.UUID()
			elfMachine4.Status.VMRef = fake.UUID()
			hostID := fake.UUID()
			vm := fake.NewTowerVM()
			vm.Host = &models.NestedHost{ID: util.TowerString(hostID)}
			placementGroup := fake.NewVMPlacementGroup([]string{})
			placementGroup.Vms = []*models.NestedVM{
				{ID: util.TowerString(fake.UUID()), Name: util.TowerString(elfMachine2.Name)},
				{ID: util.TowerString(fake.UUID()), Name: util.TowerString(elfMachine3.Name)},
				{ID: util.TowerString(*vm.ID), Name: util.TowerString(elfMachine4.Name)},
			}
			kcp.Spec.Replicas = pointer.Int32(3)
			kcp.Status.Replicas = 3
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp, elfMachine2, machine2, elfMachine3, machine3, elfMachine4, machine4)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine2, machine2)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine3, machine3)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine4, machine4)

			mockVMService.EXPECT().Get(*vm.ID).Return(vm, nil)
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			host, err := reconciler.getVMHostForRollingUpdate(machineContext)
			Expect(err).To(BeZero())
			Expect(host).To(Equal(hostID))
		})
	})

	Context("Reconcile ElfMachine providerID", func() {
		BeforeEach(func() {
			mockVMService.EXPECT().UpsertLabel(gomock.Any(), gomock.Any()).Times(3).Return(fake.NewTowerLabel(), nil)
			mockVMService.EXPECT().AddLabelsToVM(gomock.Any(), gomock.Any()).Times(1)
		})

		It("should set providerID to ElfMachine when VM is created", func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
			towerCluster := fake.NewTowerCluster()
			vm := fake.NewTowerVM()
			elfMachine.Status.VMRef = *vm.LocalID
			vm.EntityAsyncStatus = nil
			placementGroup := fake.NewVMPlacementGroup([]string{*vm.ID})
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err).To(BeZero())
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(*elfMachine.Spec.ProviderID).Should(Equal(machineutil.ConvertUUIDToProviderID(*vm.LocalID)))
		})
	})

	Context("Reconcile ElfMachine network", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
			mockVMService.EXPECT().UpsertLabel(gomock.Any(), gomock.Any()).Times(3).Return(fake.NewTowerLabel(), nil)
			mockVMService.EXPECT().AddLabelsToVM(gomock.Any(), gomock.Any()).Times(1)
		})

		It("should wait VM network ready", func() {
			towerCluster := fake.NewTowerCluster()
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			vm.Ips = util.TowerString("")
			elfMachine.Status.VMRef = *vm.LocalID
			placementGroup := fake.NewVMPlacementGroup([]string{*vm.ID})
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).Should(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("network is not reconciled"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForNetworkAddressesReason}})
		})

		It("should set ElfMachine to ready when VM network is ready", func() {
			towerCluster := fake.NewTowerCluster()
			ip := "116.116.116.116"
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			vm.Ips = util.TowerString("116.116.116.116")
			elfMachine.Status.VMRef = *vm.LocalID
			placementGroup := fake.NewVMPlacementGroup([]string{*vm.ID})
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.Network[0].IPAddrs[0]).To(Equal(ip))
			Expect(elfMachine.Status.Addresses[0].Type).To(Equal(clusterv1.MachineInternalIP))
			Expect(elfMachine.Status.Addresses[0].Address).To(Equal(ip))
			Expect(elfMachine.Status.Ready).To(BeTrue())
			expectConditions(elfMachine, []conditionAssertion{{conditionType: infrav1.VMProvisionedCondition, status: corev1.ConditionTrue}})
		})
	})

	Context("Delete a ElfMachine", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
			ctrlutil.AddFinalizer(elfMachine, infrav1.MachineFinalizer)
			elfMachine.DeletionTimestamp = &metav1.Time{Time: time.Now().UTC()}
			elfCluster.Spec.VMGracefulShutdown = true
		})

		It("should delete ElfMachine when tower is out of service and cluster need to force delete", func() {
			mockNewVMService = func(_ goctx.Context, _ infrav1.Tower, _ logr.Logger) (service.VMService, error) {
				return mockVMService, errors.New("get vm service failed")
			}
			elfCluster.Annotations = map[string]string{
				infrav1.ElfClusterForceDeleteAnnotation: "",
			}
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Skip VM deletion due to the force-delete-cluster annotation"))
			elfCluster = &infrav1.ElfCluster{}
			err = reconciler.Client.Get(reconciler, elfMachineKey, elfCluster)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("should delete ElfMachine failed when tower is out of service", func() {
			mockNewVMService = func(_ goctx.Context, _ infrav1.Tower, _ logr.Logger) (service.VMService, error) {
				return mockVMService, errors.New("get vm service failed")
			}
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Finalizers).To(ContainElement(infrav1.MachineFinalizer))
		})

		It("should delete ElfMachine when vmRef is empty and VM not found", func() {
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetByName(elfMachine.Name).Return(nil, errors.New(service.VMNotFound))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("VM already deleted"))
			elfCluster = &infrav1.ElfCluster{}
			err = reconciler.Client.Get(reconciler, elfMachineKey, elfCluster)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("should delete the VM that in creating status and have not been saved to ElfMachine", func() {
			vm := fake.NewTowerVM()
			vm.LocalID = nil
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetByName(elfMachine.Name).Return(vm, nil)
			mockVMService.EXPECT().Get(*vm.ID).Return(vm, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM task done"))
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM to be deleted"))
			elfCluster = &infrav1.ElfCluster{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.ID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
		})

		It("should delete the VM that in created status and have not been saved to ElfMachine", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusRUNNING
			vm.Status = &status
			task := fake.NewTowerTask()
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetByName(elfMachine.Name).Return(vm, nil)
			mockVMService.EXPECT().Get(*vm.LocalID).Return(vm, nil)
			mockVMService.EXPECT().ShutDown(*vm.LocalID).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM shut down"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, clusterv1.DeletingReason}})
		})

		It("should remove vmRef when VM not found", func() {
			vm := fake.NewTowerVM()
			task := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID
			elfMachine.Status.TaskRef = *task.ID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			vmNotFoundError := errors.New(service.VMNotFound)
			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(nil, vmNotFoundError)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("VM already deleted"))
			elfMachine = &infrav1.ElfMachine{}
			err = reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("should handle task - pending", func() {
			vm := fake.NewTowerVM()
			status := models.VMStatusRUNNING
			vm.Status = &status
			vm.EntityAsyncStatus = (*models.EntityAsyncStatus)(util.TowerString("UPDATING"))
			task := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID
			elfMachine.Status.TaskRef = *task.ID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result).NotTo(BeZero())
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM task done"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, clusterv1.DeletingReason}})
		})

		It("should handle task - failed", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			task := fake.NewTowerTask()
			status := models.TaskStatusFAILED
			task.Status = &status
			elfMachine.Status.VMRef = *vm.LocalID
			elfMachine.Status.TaskRef = *task.ID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task, nil)
			mockVMService.EXPECT().ShutDown(elfMachine.Status.VMRef).Return(task, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}

			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(logBuffer.String()).To(ContainSubstring("VM task failed"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, clusterv1.DeletionFailedReason}})
			Expect(conditions.GetMessage(elfMachine, infrav1.VMProvisionedCondition)).To(Equal("some error"))
		})

		It("should power off when VM is powered on and shut down failed", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			task := fake.NewTowerTask()
			status := models.TaskStatusFAILED
			task.Status = &status
			task.ErrorMessage = pointer.String("JOB_VM_SHUTDOWN_TIMEOUT")
			elfMachine.Status.VMRef = *vm.LocalID
			elfMachine.Status.TaskRef = *task.ID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task, nil)
			mockVMService.EXPECT().PowerOff(elfMachine.Status.VMRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("VM task failed"))
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM shut down"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.TaskFailureReason}})
			Expect(conditions.GetMessage(elfMachine, infrav1.VMProvisionedCondition)).To(Equal("JOB_VM_SHUTDOWN_TIMEOUT"))
		})

		It("should handle task - done", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			task := fake.NewTowerTask()
			status := models.TaskStatusSUCCESSED
			task.Status = &status
			elfMachine.Status.VMRef = *vm.LocalID
			elfMachine.Status.TaskRef = *task.ID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task, nil)
			mockVMService.EXPECT().ShutDown(elfMachine.Status.VMRef).Return(nil, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(logBuffer.String()).To(ContainSubstring("VM task succeeded"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, clusterv1.DeletionFailedReason}})
		})

		It("should shut down when VM is powered on", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			task := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().ShutDown(elfMachine.Status.VMRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM shut down"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, clusterv1.DeletingReason}})
		})

		It("should handle delete error", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			elfMachine.Status.VMRef = *vm.LocalID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().Delete(elfMachine.Status.VMRef).Return(nil, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).ToNot(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Destroying VM"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, clusterv1.DeletionFailedReason}})
		})

		It("should delete when VM is not running", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			task := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().Delete(elfMachine.Status.VMRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM to be deleted"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, clusterv1.DeletingReason}})
		})

		It("should power off when VM is running and VMGracefulShutdown is false", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusRUNNING
			vm.Status = &status
			task := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID
			elfCluster.Spec.VMGracefulShutdown = false
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().PowerOff(elfMachine.Status.VMRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM shut down"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, clusterv1.DeletingReason}})
		})
	})

	Context("Reconcile static IP allocation", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
		})

		It("should wait for IP allocation", func() {
			// one IPV4 device without ipAddrs
			elfMachine.Spec.Network.Devices = []infrav1.NetworkDeviceSpec{
				{NetworkType: infrav1.NetworkTypeIPV4},
			}
			waitStaticIPAllocationSpec(mockNewVMService, elfCluster, cluster, elfMachine, machine, secret, md)

			// one IPV4 device without ipAddrs and one with ipAddrs
			elfMachine.Spec.Network.Devices = []infrav1.NetworkDeviceSpec{
				{NetworkType: infrav1.NetworkTypeIPV4},
				{NetworkType: infrav1.NetworkTypeIPV4, IPAddrs: []string{"127.0.0.1"}},
			}
			waitStaticIPAllocationSpec(mockNewVMService, elfCluster, cluster, elfMachine, machine, secret, md)

			// one IPV4 device without ipAddrs and one DHCP device
			elfMachine.Spec.Network.Devices = []infrav1.NetworkDeviceSpec{
				{NetworkType: infrav1.NetworkTypeIPV4},
				{NetworkType: infrav1.NetworkTypeIPV4DHCP},
			}
			waitStaticIPAllocationSpec(mockNewVMService, elfCluster, cluster, elfMachine, machine, secret, md)
		})

		It("should not wait for IP allocation", func() {
			// one IPV4 device with ipAddrs
			elfMachine.Spec.Network.Devices = []infrav1.NetworkDeviceSpec{
				{NetworkType: infrav1.NetworkTypeIPV4, IPAddrs: []string{"127.0.0.1"}},
			}
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().Clone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).ShouldNot(BeNil())
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.CloningFailedReason}})
		})
	})

	Context("Reconcile VM task", func() {
		It("should handle task missing", func() {
			task := fake.NewTowerTask()
			elfMachine.Status.TaskRef = *task.ID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(nil, errors.New(service.TaskNotFound))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMTask(machineContext, nil)
			Expect(ok).Should(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
		})

		It("should handle failed to get task", func() {
			task := fake.NewTowerTask()
			elfMachine.Status.TaskRef = *task.ID
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(nil, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMTask(machineContext, nil)
			Expect(ok).Should(BeFalse())
			Expect(strings.Contains(err.Error(), "failed to get task")).To(BeTrue())
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
		})
	})

	Context("Reconcile Node", func() {
		var node *corev1.Node

		AfterEach(func() {
			Expect(testEnv.Delete(ctx, node)).To(Succeed())
		})

		It("should set providerID and labels for node", func() {
			elfMachine.Status.HostServerRef = fake.UUID()
			elfMachine.Status.HostServerName = fake.UUID()
			vm := fake.NewTowerVM()
			ctrlMgrContext := &context.ControllerManagerContext{
				Context:                 goctx.Background(),
				Client:                  testEnv.Client,
				Logger:                  ctrllog.Log,
				Name:                    fake.ControllerManagerName,
				LeaderElectionNamespace: fake.LeaderElectionNamespace,
				LeaderElectionID:        fake.LeaderElectionID,
			}
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrllog.Log,
			}
			machineContext := &context.MachineContext{
				ControllerContext: ctrlContext,
				Cluster:           cluster,
				Machine:           machine,
				ElfCluster:        elfCluster,
				ElfMachine:        elfMachine,
				Logger:            ctrllog.Log,
			}

			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   elfMachine.Name,
					Labels: map[string]string{},
				},
			}
			Expect(testEnv.CreateAndWait(ctx, node)).To(Succeed())
			Expect(helpers.CreateKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)).To(Succeed())

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileNode(machineContext, vm)
			Expect(ok).Should(BeTrue())
			Expect(err).To(BeNil())
			Eventually(func() bool {
				if err := testEnv.Get(ctx, client.ObjectKey{Namespace: node.Namespace, Name: node.Name}, node); err != nil {
					return false
				}

				return node.Spec.ProviderID == machineutil.ConvertUUIDToProviderID(*vm.LocalID) &&
					node.Labels[infrav1.HostServerIDLabel] == elfMachine.Status.HostServerRef &&
					node.Labels[infrav1.HostServerNameLabel] == elfMachine.Status.HostServerName
			}, timeout).Should(BeTrue())
		})

		It("should update labels but not update providerID", func() {
			elfMachine.Status.HostServerRef = fake.UUID()
			elfMachine.Status.HostServerName = fake.UUID()
			vm := fake.NewTowerVM()
			ctrlMgrContext := &context.ControllerManagerContext{
				Context:                 goctx.Background(),
				Client:                  testEnv.Client,
				Logger:                  ctrllog.Log,
				Name:                    fake.ControllerManagerName,
				LeaderElectionNamespace: fake.LeaderElectionNamespace,
				LeaderElectionID:        fake.LeaderElectionID,
			}
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrllog.Log,
			}
			machineContext := &context.MachineContext{
				ControllerContext: ctrlContext,
				Cluster:           cluster,
				Machine:           machine,
				ElfCluster:        elfCluster,
				ElfMachine:        elfMachine,
				Logger:            ctrllog.Log,
			}

			providerID := machineutil.ConvertUUIDToProviderID(fake.UUID())
			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: elfMachine.Name,
					Labels: map[string]string{
						infrav1.HostServerIDLabel:   "old-id",
						infrav1.HostServerNameLabel: "old-name",
					},
				},
				Spec: corev1.NodeSpec{ProviderID: providerID},
			}
			Expect(testEnv.CreateAndWait(ctx, node)).To(Succeed())
			Expect(helpers.CreateKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)).To(Succeed())

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileNode(machineContext, vm)
			Expect(ok).Should(BeTrue())
			Expect(err).To(BeNil())

			Eventually(func() bool {
				if err := testEnv.Get(ctx, client.ObjectKey{Namespace: node.Namespace, Name: node.Name}, node); err != nil {
					return false
				}

				return node.Spec.ProviderID == providerID &&
					node.Labels[infrav1.HostServerIDLabel] == elfMachine.Status.HostServerRef &&
					node.Labels[infrav1.HostServerNameLabel] == elfMachine.Status.HostServerName
			}, timeout).Should(BeTrue())
		})
	})
})

func waitStaticIPAllocationSpec(mockNewVMService func(ctx goctx.Context, auth infrav1.Tower, logger logr.Logger) (service.VMService, error),
	elfCluster *infrav1.ElfCluster, cluster *clusterv1.Cluster,
	elfMachine *infrav1.ElfMachine, machine *clusterv1.Machine, secret *corev1.Secret, md *clusterv1.MachineDeployment) {
	ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
	fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
	logBuffer := new(bytes.Buffer)
	klog.SetOutput(logBuffer)

	reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
	elfMachineKey := capiutil.ObjectKey(elfMachine)
	result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
	Expect(result.RequeueAfter).To(BeZero())
	Expect(err).Should(BeNil())
	Expect(logBuffer.String()).To(ContainSubstring("VM is waiting for static ip to be available"))
	elfMachine = &infrav1.ElfMachine{}
	Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
	expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForStaticIPAllocationReason}})
}

func newCtrlContexts(objs ...runtime.Object) *context.ControllerContext {
	ctrlMgrContext := fake.NewControllerManagerContext(objs...)
	ctrlContext := &context.ControllerContext{
		ControllerManagerContext: ctrlMgrContext,
		Logger:                   ctrllog.Log,
	}

	return ctrlContext
}

func newMachineContext(ctrlCtx *context.ControllerContext,
	elfCluster *infrav1.ElfCluster, cluster *clusterv1.Cluster,
	elfMachine *infrav1.ElfMachine, machine *clusterv1.Machine,
	vmService service.VMService) *context.MachineContext {
	return &context.MachineContext{
		ControllerContext: ctrlCtx,
		Cluster:           cluster,
		ElfCluster:        elfCluster,
		Machine:           machine,
		ElfMachine:        elfMachine,
		Logger:            ctrlCtx.Logger,
		VMService:         vmService,
	}
}

type conditionAssertion struct {
	conditionType clusterv1.ConditionType
	status        corev1.ConditionStatus
	severity      clusterv1.ConditionSeverity
	reason        string
}

func expectConditions(getter conditions.Getter, expected []conditionAssertion) {
	Expect(len(getter.GetConditions())).To(BeNumerically(">=", len(expected)), "number of conditions")
	for _, c := range expected {
		actual := conditions.Get(getter, c.conditionType)
		Expect(actual).To(Not(BeNil()))
		Expect(actual.Type).To(Equal(c.conditionType))
		Expect(actual.Status).To(Equal(c.status))
		Expect(actual.Severity).To(Equal(c.severity))
		Expect(actual.Reason).To(Equal(c.reason))
	}
}
