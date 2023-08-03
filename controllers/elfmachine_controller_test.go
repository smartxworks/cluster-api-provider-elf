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
	"os"
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
	machineutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/machine"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
	"github.com/smartxworks/cluster-api-provider-elf/test/helpers"
)

var _ = Describe("ElfMachineReconciler", func() {
	var (
		elfCluster       *infrav1.ElfCluster
		cluster          *clusterv1.Cluster
		elfMachine       *infrav1.ElfMachine
		k8sNode          *corev1.Node
		machine          *clusterv1.Machine
		kcp              *controlplanev1.KubeadmControlPlane
		md               *clusterv1.MachineDeployment
		secret           *corev1.Secret
		kubeConfigSecret *corev1.Secret
		logBuffer        *bytes.Buffer
		mockCtrl         *gomock.Controller
		mockVMService    *mock_services.MockVMService
		mockNewVMService service.NewVMServiceFunc
	)

	ctx := goctx.Background()

	BeforeEach(func() {
		var err error

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

		k8sNode = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   elfMachine.Name,
				Labels: map[string]string{},
			},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{
						Address: "127.0.0.1",
						Type:    corev1.NodeInternalIP,
					},
				},
			},
		}

		kubeConfigSecret, err = helpers.NewKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)
		Expect(err).ShouldNot(HaveOccurred())
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
			Expect(err).ToNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for Machine Controller to set OwnerRef on ElfMachine"))
		})

		It("should not error and not requeue the request when Cluster is paused", func() {
			cluster.Spec.Paused = true
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).ToNot(HaveOccurred())
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
			Expect(err).ToNot(HaveOccurred())
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
			Expect(err).ToNot(HaveOccurred())
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
			Expect(err).ToNot(HaveOccurred())
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
			Expect(err).ToNot(HaveOccurred())
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
			Expect(err).ToNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for bootstrap data to be available"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForBootstrapDataReason}})
		})
	})

	Context("Reconcile ElfMachine VM", func() {
		var placementGroup *models.VMPlacementGroup
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}

			placementGroup = fake.NewVMPlacementGroup([]string{fake.ID()})
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
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
			Expect(err).ShouldNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM task done"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.ID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
		})

		It("should recover from lost task", func() {
			vm := fake.NewTowerVM()
			vm.Name = &elfMachine.Name
			vm.LocalID = pointer.String("placeholder-%s" + *vm.LocalID)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Clone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New(service.VMDuplicate))
			mockVMService.EXPECT().GetByName(elfMachine.Name).Return(vm, nil)
			mockVMService.EXPECT().Get(*vm.ID).Return(vm, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
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
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.EntityAsyncStatus = nil
			status := models.VMStatusRUNNING
			vm.Status = &status
			elfMachine.Status.VMRef = *vm.LocalID
			now := metav1.NewTime(time.Now().Add(-infrav1.VMDisconnectionTimeout))
			elfMachine.SetVMDisconnectionTimestamp(&now)
			nic := fake.NewTowerVMNic(0)
			placementGroup := fake.NewVMPlacementGroup([]string{*vm.ID})
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kubeConfigSecret)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			Expect(testEnv.CreateAndWait(ctx, k8sNode)).To(Succeed())

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return([]*models.VMNic{nic}, nil)
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

			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			patchHelper, err := patch.NewHelper(elfMachine, reconciler.Client)
			Expect(err).ToNot(HaveOccurred())
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
			Expect(err).ShouldNot(HaveOccurred())
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
			Expect(err).ToNot(HaveOccurred())
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
			task.ErrorMessage = service.TowerString("Cannot unwrap Ok value of Result.Err.\r\ncode: CREATE_VM_FORM_TEMPLATE_FAILED\r\nmessage: {\"data\":{},\"ec\":\"VM_CLOUD_INIT_CONFIG_ERROR\",\"error\":{\"msg\":\"[VM_CLOUD_INIT_CONFIG_ERROR]The gateway [192.168.31.215] is unreachable. \"}}")
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
			Expect(err).ShouldNot(HaveOccurred())
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(""))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			Expect(elfMachine.IsFailed()).To(BeTrue())
			Expect(*elfMachine.Status.FailureReason).To(Equal(capeerrors.CloudInitConfigError))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.TaskFailureReason}})
		})

		It("should power on the VM after it is created", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
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
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task1, nil)
			mockVMService.EXPECT().PowerOn(*vm.LocalID).Return(task2, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
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
			Expect(err).ShouldNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("The VM is being created"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.ID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.TaskFailureReason}})
		})

		It("should handle power on error", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
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
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
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

		It("should power off the VM when vm is in SUSPENDED status", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSUSPENDED
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
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task1, nil)
			mockVMService.EXPECT().PowerOff(*vm.LocalID).Return(task2, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM to be powered off"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task2.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.PowerOffReason}})
		})

		It("should handle power off error", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSUSPENDED
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
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task1, nil)
			mockVMService.EXPECT().PowerOff(*vm.LocalID).Return(nil, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err.Error()).To(ContainSubstring("failed to trigger powering off for VM"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.PoweringOffFailedReason}})
		})
	})

	Context("Reconcile VM status", func() {
		BeforeEach(func() {
			Expect(os.Setenv(towerresources.AllowCustomVMConfig, "false")).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(os.Unsetenv(towerresources.AllowCustomVMConfig)).NotTo(HaveOccurred())
		})

		It("should return false when VM status in an unexpected status", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.Status = nil
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMStatus(machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("The status of VM is an unexpected value nil"))

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			vm.Status = models.NewVMStatus(models.VMStatusUNKNOWN)
			ok, err = reconciler.reconcileVMStatus(machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("The VM is in an unexpected status"))
		})

		It("should power on the VM when VM is stopped", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.Status = models.NewVMStatus(models.VMStatusSTOPPED)
			task := fake.NewTowerTask()
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			mockVMService.EXPECT().PowerOn(elfMachine.Status.VMRef).Return(task, nil)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMStatus(machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM to be powered on"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.PoweringOnReason}})
		})

		It("should shut down the VM when configuration was modified and VM is running", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			*vm.Vcpu += 1
			vm.Status = models.NewVMStatus(models.VMStatusRUNNING)
			task := fake.NewTowerTask()
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			mockVMService.EXPECT().ShutDown(elfMachine.Status.VMRef).Return(task, nil)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMStatus(machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("The VM configuration has been modified, shut down the VM first and then restore the VM configuration"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ShuttingDownReason}})
		})

		It("should power off the VM when configuration was modified and shut down failed", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			*vm.CPU.Cores += 1
			vm.Status = models.NewVMStatus(models.VMStatusRUNNING)
			task := fake.NewTowerTask()

			conditions.MarkFalse(elfMachine, infrav1.VMProvisionedCondition, infrav1.TaskFailureReason, clusterv1.ConditionSeverityInfo, "JOB_VM_SHUTDOWN_TIMEOUT")
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			mockVMService.EXPECT().PowerOff(elfMachine.Status.VMRef).Return(task, nil)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMStatus(machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("The VM configuration has been modified, power off the VM first and then restore the VM configuration"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.PowerOffReason}})
		})

		It("should restore the VM configuration when configuration was modified", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			*vm.CPU.Sockets += 1
			vm.Status = models.NewVMStatus(models.VMStatusSTOPPED)
			task := fake.NewTowerTask()
			withTaskVM := fake.NewWithTaskVM(vm, task)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			mockVMService.EXPECT().UpdateVM(vm, elfMachine).Return(withTaskVM, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMStatus(machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("The VM configuration has been modified, and the VM is stopped, just restore the VM configuration to expected values"))
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the VM to be updated"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.UpdatingReason}})
		})

		It("should power off the VM configuration when configuration and VM is suspended", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			*vm.Vcpu += 1
			vm.Status = models.NewVMStatus(models.VMStatusSUSPENDED)
			task := fake.NewTowerTask()
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			mockVMService.EXPECT().PowerOff(elfMachine.Status.VMRef).Return(task, nil)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMStatus(machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.PowerOffReason}})
		})
	})

	Context("Reconcile Join Placement Group", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
		})

		It("should skip adding VM to the placement group when capeVersion of ElfMachine is lower than v1.2.0", func() {
			fake.ToControlPlaneMachine(machine, kcp)
			fake.ToControlPlaneMachine(elfMachine, kcp)
			delete(elfMachine.Annotations, infrav1.CAPEVersionAnnotation)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.joinPlacementGroup(machineContext, nil)
			Expect(ok).To(BeTrue())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("The capeVersion of ElfMachine is lower than"))
		})

		It("should add vm to the placement group", func() {
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

			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			mockVMService.EXPECT().AddVMsToPlacementGroup(placementGroup, []string{*vm.ID}).Return(task, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.joinPlacementGroup(machineContext, vm)
			Expect(ok).To(BeTrue())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Updating placement group succeeded"))
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
			err := reconciler.addVMsToPlacementGroup(machineContext, placementGroup, []string{*vm.ID})
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Updating placement group succeeded"))

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			taskStatus = models.TaskStatusFAILED
			task.Status = &taskStatus
			mockVMService.EXPECT().AddVMsToPlacementGroup(placementGroup, []string{*vm.ID}).Return(task, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)

			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			err = reconciler.addVMsToPlacementGroup(machineContext, placementGroup, []string{*vm.ID})
			Expect(strings.Contains(err.Error(), "failed to update placement group")).To(BeTrue())

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			mockVMService.EXPECT().AddVMsToPlacementGroup(placementGroup, []string{*vm.ID}).Return(task, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(nil, errors.New("xxx"))

			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			err = reconciler.addVMsToPlacementGroup(machineContext, placementGroup, []string{*vm.ID})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("failed to wait for placement group updation task done timed out in %s: placementName %s, taskID %s", config.WaitTaskTimeout, *placementGroup.Name, *task.ID)))
		})

		It("should wait for placement group task done", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			elfMachine.Status.VMRef = *vm.LocalID
			placementGroup1 := fake.NewVMPlacementGroup(nil)
			placementGroup2 := fake.NewVMPlacementGroup(nil)
			placementGroup2.EntityAsyncStatus = models.EntityAsyncStatusUPDATING.Pointer()
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup1, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup2, nil)

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
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			elfMachine.Status.VMRef = *vm.LocalID
			placementGroup := fake.NewVMPlacementGroup(nil)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
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

			It("should not check whether the memory of host is sufficient when VM is running and the host where the VM is located is not used", func() {
				host := fake.NewTowerHost()
				host.AllocatableMemoryBytes = service.TowerMemory(0)
				vm := fake.NewTowerVMFromElfMachine(elfMachine)
				vm.Host = &models.NestedHost{ID: host.ID, Name: host.Name}
				placementGroup := fake.NewVMPlacementGroup([]string{})
				task := fake.NewTowerTask()
				task.Status = models.NewTaskStatus(models.TaskStatusSUCCESSED)
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kcp)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
				mockVMService.EXPECT().FindByIDs([]string{}).Return([]*models.VM{}, nil)
				mockVMService.EXPECT().AddVMsToPlacementGroup(placementGroup, []string{*vm.ID}).Return(task, nil)
				mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err := reconciler.joinPlacementGroup(machineContext, vm)
				Expect(ok).To(BeTrue())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring("Updating placement group succeeded"))
			})

			It("should not be added when placement group is full", func() {
				host := fake.NewTowerHost()
				vm := fake.NewTowerVM()
				vm.Status = models.NewVMStatus(models.VMStatusSTOPPED)
				vm.EntityAsyncStatus = nil
				vm.Host = &models.NestedHost{ID: service.TowerString(fake.UUID())}
				elfMachine.Status.VMRef = *vm.LocalID
				vm2 := fake.NewTowerVM()
				vm2.Host = &models.NestedHost{ID: host.ID, Name: host.Name}
				placementGroup := fake.NewVMPlacementGroup([]string{*vm2.ID})
				kcp.Spec.Replicas = pointer.Int32(1)
				kcp.Status.Replicas = 2
				kcp.Status.UpdatedReplicas = 1
				conditions.MarkFalse(kcp, controlplanev1.MachinesSpecUpToDateCondition, controlplanev1.RollingUpdateInProgressReason, clusterv1.ConditionSeverityWarning, "")
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kcp)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
				mockVMService.EXPECT().FindByIDs([]string{*placementGroup.Vms[0].ID}).Return([]*models.VM{vm2}, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err := reconciler.joinPlacementGroup(machineContext, vm)
				Expect(ok).To(BeTrue())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring("KCP is in rolling update, the placement group is full and has no unusable hosts, so skip adding VM to the placement group and power it on"))

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				host.HostState = &models.NestedMaintenanceHostState{State: models.NewMaintenanceModeEnum(models.MaintenanceModeEnumMAINTENANCEMODE)}
				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
				mockVMService.EXPECT().FindByIDs([]string{*placementGroup.Vms[0].ID}).Return([]*models.VM{vm2}, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err = reconciler.joinPlacementGroup(machineContext, vm)
				Expect(ok).To(BeFalse())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring("KCP is in rolling update, the placement group is full and has unusable hosts, so wait for enough available hosts"))

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				vm.Status = models.NewVMStatus(models.VMStatusRUNNING)
				host.HostState = &models.NestedMaintenanceHostState{State: models.NewMaintenanceModeEnum(models.MaintenanceModeEnumMAINTENANCEMODE)}
				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
				mockVMService.EXPECT().FindByIDs([]string{*placementGroup.Vms[0].ID}).Return([]*models.VM{}, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err = reconciler.joinPlacementGroup(machineContext, vm)
				Expect(ok).To(BeTrue())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("The placement group is full and VM is in %s status, skip adding VM to the placement group", *vm.Status)))

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				kcp.Spec.Replicas = pointer.Int32(1)
				kcp.Status.Replicas = 1
				kcp.Status.UpdatedReplicas = 1
				ctrlContext = newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kcp)
				machineContext = newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
				vm.Status = models.NewVMStatus(models.VMStatusSTOPPED)
				host.HostState = &models.NestedMaintenanceHostState{State: models.NewMaintenanceModeEnum(models.MaintenanceModeEnumMAINTENANCEMODE)}
				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
				mockVMService.EXPECT().FindByIDs([]string{*placementGroup.Vms[0].ID}).Return([]*models.VM{}, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err = reconciler.joinPlacementGroup(machineContext, vm)
				Expect(ok).To(BeFalse())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring("KCP is in scaling up or being created, the placement group is full, so wait for enough available hosts"))
			})

			It("should add VM to placement group when VM is not in placement group and the host where VM in is not in placement group", func() {
				host1 := fake.NewTowerHost()
				host2 := fake.NewTowerHost()
				host3 := fake.NewTowerHost()
				host3.Status = models.NewHostStatus(models.HostStatusINITIALIZING)
				vm := fake.NewTowerVM()
				vm.EntityAsyncStatus = nil
				vm.Host = &models.NestedHost{ID: service.TowerString(*host1.ID)}
				elfMachine.Status.VMRef = *vm.LocalID
				vm2 := fake.NewTowerVM()
				vm2.Host = &models.NestedHost{ID: service.TowerString(*host2.ID)}
				task := fake.NewTowerTask()
				taskStatus := models.TaskStatusSUCCESSED
				task.Status = &taskStatus
				placementGroup := fake.NewVMPlacementGroup([]string{*vm2.ID})
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kcp)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host1, host2, host3), nil)
				mockVMService.EXPECT().FindByIDs([]string{*vm2.ID}).Return([]*models.VM{vm2}, nil)
				mockVMService.EXPECT().AddVMsToPlacementGroup(placementGroup, gomock.Any()).Return(task, nil)
				mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err := reconciler.joinPlacementGroup(machineContext, vm)
				Expect(ok).To(BeTrue())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring("Updating placement group succeeded"))
			})

			It("should not migrate VM when VM is running and KCP is in rolling update", func() {
				host1 := fake.NewTowerHost()
				host2 := fake.NewTowerHost()
				host3 := fake.NewTowerHost()
				oldCP3 := fake.NewTowerVM()
				oldCP3.Host = &models.NestedHost{ID: service.TowerString(*host3.ID)}
				newCP1 := fake.NewTowerVM()
				status := models.VMStatusRUNNING
				newCP1.Status = &status
				newCP1.EntityAsyncStatus = nil
				newCP1.Host = &models.NestedHost{ID: service.TowerString(*host3.ID)}

				elfMachine.Status.VMRef = *newCP1.LocalID
				newCP2 := fake.NewTowerVM()
				newCP2.Host = &models.NestedHost{ID: service.TowerString(*host1.ID)}
				placementGroup := fake.NewVMPlacementGroup([]string{*oldCP3.ID, *newCP2.ID})
				kcp.Spec.Replicas = pointer.Int32(3)
				kcp.Status.UpdatedReplicas = 3
				kcp.Status.Replicas = 4
				conditions.MarkFalse(kcp, controlplanev1.MachinesSpecUpToDateCondition, controlplanev1.RollingUpdateInProgressReason, clusterv1.ConditionSeverityWarning, "")
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kcp)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host1, host2, host3), nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*newCP2.ID, *oldCP3.ID})).Return([]*models.VM{newCP2, oldCP3}, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err := reconciler.joinPlacementGroup(machineContext, newCP1)
				Expect(ok).To(BeTrue())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring("KCP rolling update in progress, skip migrating VM"))
			})

			It("should migrate VM to another host when the VM is running and the host of VM is not in unused hosts", func() {
				host0 := fake.NewTowerHost()
				host1 := fake.NewTowerHost()
				host2 := fake.NewTowerHost()
				elfMachine1, machine1 := fake.NewMachineObjects(elfCluster, cluster)
				elfMachine2, machine2 := fake.NewMachineObjects(elfCluster, cluster)
				elfMachine.CreationTimestamp = metav1.Now()
				elfMachine1.CreationTimestamp = metav1.NewTime(time.Now().Add(1 * time.Minute))
				elfMachine2.CreationTimestamp = metav1.NewTime(time.Now().Add(2 * time.Minute))
				fake.ToControlPlaneMachine(machine, kcp)
				fake.ToControlPlaneMachine(elfMachine, kcp)
				fake.ToControlPlaneMachine(machine1, kcp)
				fake.ToControlPlaneMachine(elfMachine1, kcp)
				fake.ToControlPlaneMachine(machine2, kcp)
				fake.ToControlPlaneMachine(elfMachine2, kcp)
				vm0 := fake.NewTowerVMFromElfMachine(elfMachine)
				vm0.Host = &models.NestedHost{ID: service.TowerString(*host2.ID)}
				vm0.Status = models.NewVMStatus(models.VMStatusRUNNING)
				vm1 := fake.NewTowerVMFromElfMachine(elfMachine1)
				vm1.Host = &models.NestedHost{ID: service.TowerString(*host1.ID)}
				vm2 := fake.NewTowerVMFromElfMachine(elfMachine2)
				vm2.Host = &models.NestedHost{ID: service.TowerString(*host2.ID)}
				elfMachine.Status.VMRef = *vm0.LocalID
				elfMachine1.Status.VMRef = *vm1.LocalID
				elfMachine2.Status.VMRef = *vm2.LocalID
				placementGroup := fake.NewVMPlacementGroup([]string{})
				placementGroup.Vms = []*models.NestedVM{
					{ID: vm1.ID, Name: vm1.Name},
					{ID: vm2.ID, Name: vm2.Name},
				}
				elfMachine.Status.PlacementGroupRef = ""
				elfMachine.Status.HostServerRef = ""
				elfMachine1.Status.PlacementGroupRef = *placementGroup.ID
				elfMachine1.Status.HostServerRef = *host1.ID
				elfMachine2.Status.PlacementGroupRef = *placementGroup.ID
				elfMachine2.Status.HostServerRef = *host2.ID
				task := fake.NewTowerTask()
				withTaskVM := fake.NewWithTaskVM(vm1, task)
				kcp.Spec.Replicas = pointer.Int32(3)
				kcp.Status.Replicas = 3
				kcp.Status.UpdatedReplicas = 3

				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp, elfMachine1, machine1, elfMachine2, machine2)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine1, machine1)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine2, machine2)

				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*vm1.ID, *vm2.ID})).Return([]*models.VM{vm1, vm2}, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host0, host1, host2), nil)
				mockVMService.EXPECT().Migrate(*vm0.ID, *host0.ID).Return(withTaskVM, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err := reconciler.joinPlacementGroup(machineContext, vm0)
				Expect(ok).To(BeFalse())
				Expect(err).To(BeZero())
				Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
				Expect(logBuffer.String()).To(ContainSubstring("Start migrateVM since KCP is not in rolling update process"))
				Expect(logBuffer.String()).To(ContainSubstring("Waiting for the VM to be migrated from"))
				expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.JoiningPlacementGroupReason}})

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				elfMachine1.Status.HostServerRef = *host0.ID
				mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*vm1.ID, *vm2.ID})).Return([]*models.VM{vm1, vm2}, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host0, host1, host2), nil)
				ctrlContext = newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp, elfMachine1, machine1, elfMachine2, machine2)
				machineContext = newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine1, machine1)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine2, machine2)
				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err = reconciler.joinPlacementGroup(machineContext, vm0)
				Expect(ok).To(BeTrue())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring("The recommended target host for VM migration is used by the PlacementGroup"))
			})

			It("should not migrate VM to target host when VM is already on the target host or the host is used by placement group", func() {
				host := fake.NewTowerHost()
				vm := fake.NewTowerVMFromElfMachine(elfMachine)
				vm.Host = &models.NestedHost{ID: service.TowerString(*host.ID)}
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				ok, err := reconciler.migrateVM(machineContext, vm, *host.ID)
				Expect(ok).To(BeTrue())
				Expect(err).To(BeZero())
				Expect(logBuffer.String()).To(ContainSubstring("The VM is already on the recommended target host"))

				// vm.Host = &models.NestedHost{ID: service.TowerString(fake.ID())}
				// vm1 := fake.NewTowerVM()
				// vm1.Host = &models.NestedHost{ID: service.TowerString(*host.ID)}
				// placementGroup.Vms = []*models.NestedVM{
				// 	{ID: vm1.ID, Name: vm1.Name},
				// }
				// mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*vm1.ID})).Return([]*models.VM{vm1}, nil)
				// reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				// ok, err = reconciler.migrateVM(machineContext, vm, placementGroup, *host.ID)
				// Expect(ok).To(BeTrue())
				// Expect(err).To(BeZero())
				// Expect(logBuffer.String()).To(ContainSubstring("is already used by placement group"))
			})
		})
	})

	Context("Pre Check Placement Group", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
			fake.ToControlPlaneMachine(machine, kcp)
			fake.ToControlPlaneMachine(elfMachine, kcp)
		})

		Context("Rolling Update", func() {
			It("when placement group is full", func() {
				host := fake.NewTowerHost()
				elfMachine1, machine1 := fake.NewMachineObjects(elfCluster, cluster)
				elfMachine2, machine2 := fake.NewMachineObjects(elfCluster, cluster)
				elfMachine3, machine3 := fake.NewMachineObjects(elfCluster, cluster)
				vm1 := fake.NewTowerVMFromElfMachine(elfMachine1)
				vm1.Host = &models.NestedHost{ID: service.TowerString(*host.ID)}
				vm2 := fake.NewTowerVMFromElfMachine(elfMachine2)
				vm2.Host = &models.NestedHost{ID: service.TowerString(*host.ID)}
				vm3 := fake.NewTowerVMFromElfMachine(elfMachine3)
				vm3.Host = &models.NestedHost{ID: service.TowerString(*host.ID)}
				elfMachine1.Status.VMRef = *vm1.LocalID
				elfMachine2.Status.VMRef = *vm2.LocalID
				elfMachine3.Status.VMRef = *vm3.LocalID
				vm := fake.NewTowerVM()
				elfMachine.Status.VMRef = *vm.LocalID
				placementGroup := fake.NewVMPlacementGroup([]string{})
				placementGroup.Vms = []*models.NestedVM{}
				kcp.Spec.Replicas = pointer.Int32(3)
				kcp.Status.Replicas = 3
				kcp.Status.UpdatedReplicas = 3
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp, elfMachine1, machine1, elfMachine2, machine2, elfMachine3, machine3)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine1, machine1)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine2, machine2)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine3, machine3)
				placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctrlContext.Client, machine, cluster)
				Expect(err).NotTo(HaveOccurred())

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{})).Return([]*models.VM{}, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				hostID, err := reconciler.preCheckPlacementGroup(machineContext)
				Expect(err).To(BeZero())
				Expect(*hostID).To(Equal(""))
				Expect(logBuffer.String()).To(ContainSubstring("The placement group still has capacity"))

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				placementGroup.Vms = []*models.NestedVM{{ID: vm1.ID, Name: vm1.Name}}
				mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*vm1.ID})).Return([]*models.VM{vm1}, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				hostID, err = reconciler.preCheckPlacementGroup(machineContext)
				Expect(err).To(BeZero())
				Expect(hostID).To(BeNil())
				Expect(logBuffer.String()).To(ContainSubstring("KCP is not in rolling update and not in scaling down, the placement group is full, wait for enough available hosts"))
			})

			It("when placement group is full and KCP rolling update in progress", func() {
				host1 := fake.NewTowerHost()
				host2 := fake.NewTowerHost()
				host3 := fake.NewTowerHost()
				elfMachine1, machine1 := fake.NewMachineObjects(elfCluster, cluster)
				elfMachine2, machine2 := fake.NewMachineObjects(elfCluster, cluster)
				elfMachine3, machine3 := fake.NewMachineObjects(elfCluster, cluster)
				machine1.CreationTimestamp = metav1.Now()
				machine2.CreationTimestamp = metav1.NewTime(time.Now().Add(1 * time.Minute))
				machine3.CreationTimestamp = metav1.NewTime(time.Now().Add(2 * time.Minute))
				fake.ToControlPlaneMachine(machine1, kcp)
				fake.ToControlPlaneMachine(elfMachine1, kcp)
				fake.ToControlPlaneMachine(machine2, kcp)
				fake.ToControlPlaneMachine(elfMachine2, kcp)
				fake.ToControlPlaneMachine(machine3, kcp)
				fake.ToControlPlaneMachine(elfMachine3, kcp)
				vm1 := fake.NewTowerVMFromElfMachine(elfMachine1)
				vm1.Host = &models.NestedHost{ID: service.TowerString(*host1.ID)}
				vm2 := fake.NewTowerVMFromElfMachine(elfMachine2)
				vm2.Host = &models.NestedHost{ID: service.TowerString(*host2.ID)}
				vm3 := fake.NewTowerVMFromElfMachine(elfMachine3)
				vm3.Host = &models.NestedHost{ID: service.TowerString(*host3.ID)}
				elfMachine1.Status.VMRef = *vm1.LocalID
				elfMachine2.Status.VMRef = *vm2.LocalID
				elfMachine3.Status.VMRef = *vm3.LocalID
				vm := fake.NewTowerVM()
				elfMachine.Status.VMRef = *vm.LocalID
				placementGroup := fake.NewVMPlacementGroup([]string{})
				placementGroup.Vms = []*models.NestedVM{
					{ID: vm1.ID, Name: vm1.Name},
					{ID: vm2.ID, Name: vm2.Name},
					{ID: vm3.ID, Name: vm3.Name},
				}
				kcp.Spec.Replicas = pointer.Int32(3)
				kcp.Status.Replicas = 4
				kcp.Status.UpdatedReplicas = 1
				conditions.MarkFalse(kcp, controlplanev1.MachinesSpecUpToDateCondition, controlplanev1.RollingUpdateInProgressReason, clusterv1.ConditionSeverityWarning, "")
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp, elfMachine1, machine1, elfMachine2, machine2, elfMachine3, machine3)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine1, machine1)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine2, machine2)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine3, machine3)
				placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctrlContext.Client, machine, cluster)
				Expect(err).NotTo(HaveOccurred())

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				mockVMService.EXPECT().Get(*vm3.ID).Return(vm3, nil)
				mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host1, host2, host3), nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*vm1.ID, *vm2.ID, *vm3.ID})).Return([]*models.VM{vm1, vm2, vm3}, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				host, err := reconciler.preCheckPlacementGroup(machineContext)
				Expect(err).To(BeZero())
				Expect(*host).To(Equal(*vm3.Host.ID))

				// One of the hosts is unavailable.

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				host1.Status = models.NewHostStatus(models.HostStatusCONNECTEDERROR)
				mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host1, host2, host3), nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*vm1.ID, *vm2.ID, *vm3.ID})).Return([]*models.VM{vm1, vm2, vm3}, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				host, err = reconciler.preCheckPlacementGroup(machineContext)
				Expect(err).To(BeZero())
				Expect(host).To(BeNil())
				Expect(logBuffer.String()).To(ContainSubstring("KCP is in rolling update, the placement group is full and has unusable hosts, will wait for enough available hosts"))

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				host1.Status = models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)
				host2.Status = models.NewHostStatus(models.HostStatusCONNECTEDERROR)
				mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host1, host2, host3), nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*vm1.ID, *vm2.ID, *vm3.ID})).Return([]*models.VM{vm1, vm2, vm3}, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				host, err = reconciler.preCheckPlacementGroup(machineContext)
				Expect(err).To(BeZero())
				Expect(host).To(BeNil())
				Expect(logBuffer.String()).To(ContainSubstring("KCP is in rolling update, the placement group is full and has unusable hosts, will wait for enough available hosts"))

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				host2.Status = models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)
				host3.Status = models.NewHostStatus(models.HostStatusCONNECTEDERROR)
				mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host1, host2, host3), nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*vm1.ID, *vm2.ID, *vm3.ID})).Return([]*models.VM{vm1, vm2, vm3}, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				host, err = reconciler.preCheckPlacementGroup(machineContext)
				Expect(err).To(BeZero())
				Expect(host).To(BeNil())
				Expect(logBuffer.String()).To(ContainSubstring("KCP is in rolling update, the placement group is full and has unusable hosts, will wait for enough available hosts"))

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				host3.Status = models.NewHostStatus(models.HostStatusCONNECTEDERROR)
				hosts := []*models.Host{host1, host2, host3}
				mockVMService.EXPECT().Get(*vm3.ID).Return(vm3, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				hostID, err := reconciler.getVMHostForRollingUpdate(machineContext, placementGroup, service.NewHostsFromList(hosts))
				Expect(err).To(BeZero())
				Expect(hostID).To(Equal(""))
				Expect(logBuffer.String()).To(ContainSubstring("Host is unavailable: host is in CONNECTED_ERROR status, skip selecting host for VM"))

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				vm3.Host.ID = service.TowerString(fake.UUID())
				hosts = []*models.Host{host1, host2, host3}
				mockVMService.EXPECT().Get(*vm3.ID).Return(vm3, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				hostID, err = reconciler.getVMHostForRollingUpdate(machineContext, placementGroup, service.NewHostsFromList(hosts))
				Expect(err).To(BeZero())
				Expect(hostID).To(Equal(""))
				Expect(logBuffer.String()).To(ContainSubstring("Host not found, skip selecting host for VM"))

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				vm3.Host = &models.NestedHost{ID: service.TowerString(*host3.ID)}
				host3.Status = models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)
				host4 := fake.NewTowerHost()
				host4.Status = models.NewHostStatus(models.HostStatusCONNECTEDERROR)
				vm4 := fake.NewTowerVMFromElfMachine(elfMachine1)
				vm4.Host = &models.NestedHost{ID: service.TowerString(*host4.ID)}
				hosts = []*models.Host{host1, host2, host3, host4}
				mockVMService.EXPECT().Get(*vm3.ID).Return(vm3, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				hostID, err = reconciler.getVMHostForRollingUpdate(machineContext, placementGroup, service.NewHostsFromList(hosts))
				Expect(err).To(BeZero())
				Expect(hostID).To(Equal(*vm3.Host.ID))
				Expect(logBuffer.String()).To(ContainSubstring("Select a host to power on the VM since the placement group is full"))

				logBuffer = new(bytes.Buffer)
				klog.SetOutput(logBuffer)
				host3.Status = models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)
				hosts = []*models.Host{host1, host2, host3, host4}
				mockVMService.EXPECT().Get(*vm3.ID).Return(vm3, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				hostID, err = reconciler.getVMHostForRollingUpdate(machineContext, placementGroup, service.NewHostsFromList(hosts))
				Expect(err).To(BeZero())
				Expect(hostID).To(Equal(*vm3.Host.ID))
				Expect(logBuffer.String()).To(ContainSubstring("Select a host to power on the VM since the placement group is full"))

				ctrlContext = newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp)
				machineContext = newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				hostID, err = reconciler.getVMHostForRollingUpdate(machineContext, placementGroup, service.NewHostsFromList(hosts))
				Expect(err).To(BeZero())
				Expect(hostID).To(Equal(""))
				Expect(logBuffer.String()).To(ContainSubstring("Newest machine not found, skip selecting host for VM"))
			})
		})

		Context("Scale", func() {
			It("kcp scale up", func() {
				kcp.Spec.Replicas = pointer.Int32(2)
				kcp.Status.Replicas = 1
				host1 := fake.NewTowerHost()
				host2 := fake.NewTowerHost()
				elfMachine1, machine1 := fake.NewMachineObjects(elfCluster, cluster)
				elfMachine2, machine2 := fake.NewMachineObjects(elfCluster, cluster)
				machine1.CreationTimestamp = metav1.Now()
				machine2.CreationTimestamp = metav1.NewTime(time.Now().Add(1 * time.Minute))
				fake.ToControlPlaneMachine(machine1, kcp)
				fake.ToControlPlaneMachine(elfMachine1, kcp)
				fake.ToControlPlaneMachine(machine2, kcp)
				fake.ToControlPlaneMachine(elfMachine2, kcp)
				vm1 := fake.NewTowerVMFromElfMachine(elfMachine1)
				vm1.Host = &models.NestedHost{ID: service.TowerString(*host1.ID)}
				vm2 := fake.NewTowerVMFromElfMachine(elfMachine2)
				vm2.Host = &models.NestedHost{ID: service.TowerString(*host2.ID)}
				elfMachine1.Status.VMRef = *vm1.LocalID
				elfMachine2.Status.VMRef = *vm2.LocalID
				vm := fake.NewTowerVM()
				elfMachine.Status.VMRef = *vm.LocalID
				placementGroup := fake.NewVMPlacementGroup([]string{})
				placementGroup.Vms = []*models.NestedVM{
					{ID: vm1.ID, Name: vm1.Name},
				}
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
				placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctrlContext.Client, machine, cluster)
				Expect(err).NotTo(HaveOccurred())
				mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host1), nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*vm1.ID})).Return([]*models.VM{vm1}, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				hostID, err := reconciler.preCheckPlacementGroup(machineContext)
				Expect(err).To(BeZero())
				Expect(hostID).To(BeNil())
				Expect(logBuffer.String()).To(ContainSubstring("KCP is not in rolling update and not in scaling down, the placement group is full, wait for enough available hosts"))
				expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForAvailableHostRequiredByPlacementGroupReason}})

				elfMachine.Status.Conditions = nil
				mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host1, host2), nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*vm1.ID})).Return([]*models.VM{vm1}, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				hostID, err = reconciler.preCheckPlacementGroup(machineContext)
				Expect(err).To(BeZero())
				Expect(*hostID).To(Equal(""))
				expectConditions(elfMachine, []conditionAssertion{})

				placementGroup.Vms = []*models.NestedVM{}
				mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host1), nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{})).Return([]*models.VM{}, nil)

				reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				hostID, err = reconciler.preCheckPlacementGroup(machineContext)
				Expect(err).To(BeZero())
				Expect(*hostID).To(Equal(""))
				expectConditions(elfMachine, []conditionAssertion{})
			})

			It("kcp scale down", func() {
				kcp.Spec.Replicas = pointer.Int32(1)
				kcp.Status.Replicas = 2
				kcp.Status.UpdatedReplicas = kcp.Status.Replicas
				conditions.MarkFalse(kcp, controlplanev1.ResizedCondition, controlplanev1.ScalingDownReason, clusterv1.ConditionSeverityWarning, "")
				host := fake.NewTowerHost()
				elfMachine1, machine1 := fake.NewMachineObjects(elfCluster, cluster)
				fake.ToControlPlaneMachine(machine1, kcp)
				fake.ToControlPlaneMachine(elfMachine1, kcp)
				vm1 := fake.NewTowerVMFromElfMachine(elfMachine1)
				vm1.Host = &models.NestedHost{ID: service.TowerString(*host.ID)}
				elfMachine.Status.VMRef = *vm1.LocalID
				placementGroup := fake.NewVMPlacementGroup([]string{})
				placementGroup.Vms = []*models.NestedVM{
					{ID: vm1.ID, Name: vm1.Name},
				}
				ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp)
				machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
				fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
				placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctrlContext.Client, machine, cluster)
				Expect(err).NotTo(HaveOccurred())
				mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
				mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
				mockVMService.EXPECT().FindByIDs(gomock.InAnyOrder([]string{*vm1.ID})).Return([]*models.VM{vm1}, nil)

				reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
				hostID, err := reconciler.preCheckPlacementGroup(machineContext)
				Expect(err).To(BeZero())
				Expect(hostID).To(BeNil())
				Expect(logBuffer.String()).To(ContainSubstring("Add the delete machine annotation on KCP Machine in order to delete it"))
				Expect(reconciler.Client.Get(reconciler, capiutil.ObjectKey(machine), machine)).To(Succeed())
				Expect(machine.Annotations).Should(HaveKey(clusterv1.DeleteMachineAnnotation))
			})
		})
	})

	Context("Get Available Hosts For VM", func() {
		It("should return the available hosts", func() {
			host1 := fake.NewTowerHost()
			host1.AllocatableMemoryBytes = service.TowerMemory(0)
			host2 := fake.NewTowerHost()
			host3 := fake.NewTowerHost()

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, kcp)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)

			// virtual machine has not been created

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			availableHosts := reconciler.getAvailableHostsForVM(machineContext, nil, service.NewHosts(), nil)
			Expect(availableHosts).To(BeEmpty())

			availableHosts = reconciler.getAvailableHostsForVM(machineContext, nil, service.NewHosts(host2), nil)
			Expect(availableHosts).To(BeEmpty())

			availableHosts = reconciler.getAvailableHostsForVM(machineContext, service.NewHosts(host1, host2, host3), service.NewHosts(host3), nil)
			Expect(availableHosts).To(ContainElements(host2))

			availableHosts = reconciler.getAvailableHostsForVM(machineContext, service.NewHosts(host1, host2, host3), service.NewHosts(host1, host2, host3), nil)
			Expect(availableHosts).To(BeEmpty())

			// virtual machine is not powered on

			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.Status = models.NewVMStatus(models.VMStatusSTOPPED)

			availableHosts = reconciler.getAvailableHostsForVM(machineContext, nil, service.NewHosts(), vm)
			Expect(availableHosts).To(BeEmpty())

			availableHosts = reconciler.getAvailableHostsForVM(machineContext, nil, service.NewHosts(host2), vm)
			Expect(availableHosts).To(BeEmpty())

			availableHosts = reconciler.getAvailableHostsForVM(machineContext, service.NewHosts(host1, host2, host3), service.NewHosts(host3), vm)
			Expect(availableHosts).To(ContainElements(host2))

			availableHosts = reconciler.getAvailableHostsForVM(machineContext, service.NewHosts(host1, host2, host3), service.NewHosts(host1, host2, host3), vm)
			Expect(availableHosts).To(BeEmpty())

			// virtual machine is powered on
			vm.Status = models.NewVMStatus(models.VMStatusRUNNING)
			vm.Host = &models.NestedHost{ID: host1.ID}

			availableHosts = reconciler.getAvailableHostsForVM(machineContext, service.NewHosts(host1, host2, host3), service.NewHosts(host1, host2, host3), vm)
			Expect(availableHosts).To(BeEmpty())

			availableHosts = reconciler.getAvailableHostsForVM(machineContext, service.NewHosts(host1, host2, host3), service.NewHosts(host2, host3), vm)
			Expect(availableHosts).To(ContainElements(host1))
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
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			elfMachine.Status.VMRef = *vm.LocalID
			vm.EntityAsyncStatus = nil
			placementGroup := fake.NewVMPlacementGroup([]string{*vm.ID})

			// before reconcile, create k8s node for VM.
			Expect(testEnv.CreateAndWait(ctx, k8sNode)).To(Succeed())

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kubeConfigSecret)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			// before reconcile, create kubeconfig secret for cluster.
			Expect(helpers.CreateKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)).To(Succeed())

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(nil, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Times(2).Return(placementGroup, nil)

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
		})

		It("should wait VM network ready", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.EntityAsyncStatus = nil
			elfMachine.Status.VMRef = *vm.LocalID
			placementGroup := fake.NewVMPlacementGroup([]string{*vm.ID})
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kubeConfigSecret)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Times(11).Return(vm, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Times(22).Return(placementGroup, nil)
			mockVMService.EXPECT().UpsertLabel(gomock.Any(), gomock.Any()).Times(33).Return(fake.NewTowerLabel(), nil)
			mockVMService.EXPECT().AddLabelsToVM(gomock.Any(), gomock.Any()).Times(11)

			// k8s node IP is null, VM has no nic info
			k8sNode.Status.Addresses = nil
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(nil, nil)
			Expect(testEnv.CreateAndWait(ctx, k8sNode)).To(Succeed())
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("VM network is not ready yet"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForNetworkAddressesReason}})

			patchHelper, err := patch.NewHelper(k8sNode, testEnv.Client)
			Expect(err).ShouldNot(HaveOccurred())
			k8sNode.Status.Addresses = []corev1.NodeAddress{
				{
					Address: "test",
					Type:    corev1.NodeHostName,
				},
			}
			Expect(patchHelper.Patch(ctx, k8sNode)).To(Succeed())
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(nil, nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("VM network is not ready yet"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForNetworkAddressesReason}})

			k8sNode.Status.Addresses = []corev1.NodeAddress{
				{
					Address: "",
					Type:    corev1.NodeInternalIP,
				},
			}
			Expect(patchHelper.Patch(ctx, k8sNode)).To(Succeed())

			// k8s node IP is null, VM has no nic info
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(nil, nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("VM network is not ready yet"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForNetworkAddressesReason}})

			// k8s node IP is null, VM has nic info
			nic := fake.NewTowerVMNic(0)
			nic.IPAddress = service.TowerString("127.0.0.1")
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return([]*models.VMNic{nic}, nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())

			// k8s node IP is null, VM has error nic info
			nic = fake.NewTowerVMNic(0)
			nic.IPAddress = service.TowerString("")
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return([]*models.VMNic{nic}, nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("VM network is not ready yet"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForNetworkAddressesReason}})

			k8sNode.Status.Addresses = []corev1.NodeAddress{
				{
					Address: "127.0.0.1",
					Type:    corev1.NodeInternalIP,
				},
			}
			Expect(patchHelper.Patch(ctx, k8sNode)).To(Succeed())

			// k8s node has node IP, VM has no nic info
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(nil, nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())

			// k8s node has node IP, VM has error nic info
			nic = fake.NewTowerVMNic(0)
			nic.IPAddress = service.TowerString("")
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return([]*models.VMNic{nic}, nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())

			// k8s node has node IP, VM has one nic info
			nic = fake.NewTowerVMNic(0)
			nic.IPAddress = service.TowerString("127.0.0.1")
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return([]*models.VMNic{nic}, nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())

			// test elfMachine has two network device.
			elfMachine.Spec.Network.Devices = append(elfMachine.Spec.Network.Devices, infrav1.NetworkDeviceSpec{})
			ctrlContext = newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kubeConfigSecret)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}

			// k8s node has node IP, VM has no nic info
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(nil, nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("VM network is not ready yet"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForNetworkAddressesReason}})

			// k8s node does not has node IP, VM has two nic info
			k8sNode.Status.Addresses = nil
			Expect(patchHelper.Patch(ctx, k8sNode)).To(Succeed())
			nic1 := fake.NewTowerVMNic(0)
			nic1.IPAddress = service.TowerString("127.0.0.1")
			nic2 := fake.NewTowerVMNic(1)
			nic2.IPAddress = service.TowerString("127.0.0.2")
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return([]*models.VMNic{nic1, nic2}, nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())

			// test elfMachine has 3 network device, one networkType is None
			elfMachine.Spec.Network.Devices = append(elfMachine.Spec.Network.Devices, infrav1.NetworkDeviceSpec{NetworkType: infrav1.NetworkTypeNone})
			ctrlContext = newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kubeConfigSecret)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}

			// k8s node does not has node IP, VM has 3 nic info, one nic networkType is None
			nic1 = fake.NewTowerVMNic(0)
			nic1.IPAddress = service.TowerString("127.0.0.1")
			nic2 = fake.NewTowerVMNic(1)
			nic2.IPAddress = service.TowerString("127.0.0.2")
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return([]*models.VMNic{nic1, nic2}, nil)
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
		})

		It("should set ElfMachine to ready when VM network is ready", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.EntityAsyncStatus = nil
			elfMachine.Status.VMRef = *vm.LocalID
			nic := fake.NewTowerVMNic(0)
			nic.IPAddress = service.TowerString("127.0.0.1")
			placementGroup := fake.NewVMPlacementGroup([]string{*vm.ID})
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md, kubeConfigSecret)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			Expect(testEnv.CreateAndWait(ctx, k8sNode)).To(Succeed())
			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return([]*models.VMNic{nic}, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Times(2).Return(placementGroup, nil)
			mockVMService.EXPECT().UpsertLabel(gomock.Any(), gomock.Any()).Times(3).Return(fake.NewTowerLabel(), nil)
			mockVMService.EXPECT().AddLabelsToVM(gomock.Any(), gomock.Any()).Times(1)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			elfMachineKey := capiutil.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.Network[0].IPAddrs[0]).To(Equal(*nic.IPAddress))
			Expect(elfMachine.Status.Addresses[0].Type).To(Equal(clusterv1.MachineInternalIP))
			Expect(elfMachine.Status.Addresses[0].Address).To(Equal(*nic.IPAddress))
			Expect(elfMachine.Status.Ready).To(BeTrue())
			expectConditions(elfMachine, []conditionAssertion{{conditionType: infrav1.VMProvisionedCondition, status: corev1.ConditionTrue}})
		})
	})

	Context("Delete an ElfMachine", func() {
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
			vm.LocalID = pointer.String("placeholder-%s" + *vm.LocalID)
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
			vm.EntityAsyncStatus = (*models.EntityAsyncStatus)(service.TowerString("UPDATING"))
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
			vm.Name = &elfMachine.Name
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
			vm.Name = &elfMachine.Name
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

		It("should delete placement group when the deployment is deleted", func() {
			cluster.DeletionTimestamp = &metav1.Time{Time: time.Now().UTC()}
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.deletePlacementGroup(machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			cluster.DeletionTimestamp = nil
			fake.ToControlPlaneMachine(machine, kcp)
			ctrlContext = newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err = reconciler.deletePlacementGroup(machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			fake.ToWorkerMachine(machine, md)
			ctrlContext = newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err = reconciler.deletePlacementGroup(machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			ctrlContext = newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err = reconciler.deletePlacementGroup(machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			md.DeletionTimestamp = &metav1.Time{Time: time.Now().UTC()}
			ctrlContext = newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService
			placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctrlContext.Client, machine, cluster)
			Expect(err).NotTo(HaveOccurred())
			placementGroup := fake.NewVMPlacementGroup([]string{})
			placementGroup.Name = service.TowerString(placementGroupName)
			mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
			mockVMService.EXPECT().DeleteVMPlacementGroupsByName(placementGroupName).Return(nil)

			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err = reconciler.deletePlacementGroup(machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			md.DeletionTimestamp = nil
			md.Spec.Replicas = pointer.Int32(0)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(nil, errors.New(service.VMPlacementGroupNotFound))
			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err = reconciler.deletePlacementGroup(machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(nil, errors.New("error"))
			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err = reconciler.deletePlacementGroup(machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).To(HaveOccurred())

			mockVMService.EXPECT().GetVMPlacementGroup(placementGroupName).Return(placementGroup, nil)
			mockVMService.EXPECT().DeleteVMPlacementGroupsByName(placementGroupName).Return(errors.New("error"))
			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err = reconciler.deletePlacementGroup(machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).To(HaveOccurred())
		})

		It("should delete k8s node before destroying VM.", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			task := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID
			cluster.Status.ControlPlaneReady = true

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			ctrlContext.Client = testEnv.Client
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			// before reconcile, create k8s node for VM.
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   elfMachine.Name,
					Labels: map[string]string{},
				},
			}
			Expect(testEnv.CreateAndWait(ctx, node)).To(Succeed())
			// before reconcile, create kubeconfig secret for cluster.
			Expect(helpers.CreateKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)).To(Succeed())
			defer func() {
				Expect(helpers.DeleteKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)).To(Succeed())
			}()

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().Delete(elfMachine.Status.VMRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			result, err := reconciler.reconcileDelete(machineContext)
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ToNot(HaveOccurred())

			// check k8s node has been deleted.
			Eventually(func() bool {
				err := ctrlContext.Client.Get(ctx, client.ObjectKeyFromObject(node), node)
				return apierrors.IsNotFound(err)
			}, timeout).Should(BeTrue())

			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM to be deleted"))
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, clusterv1.DeletingReason}})
		})

		It("should not delete k8s node when cluster is deleting", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			task := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID
			cluster.Status.ControlPlaneReady = true
			cluster.DeletionTimestamp = &metav1.Time{Time: time.Now().UTC()}

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			ctrlContext.Client = testEnv.Client
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			// before reconcile, create k8s node for VM.
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   elfMachine.Name,
					Labels: map[string]string{},
				},
			}
			Expect(testEnv.CreateAndWait(ctx, node)).To(Succeed())
			// before reconcile, create kubeconfig secret for cluster.
			Expect(helpers.CreateKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)).To(Succeed())
			defer func() {
				Expect(helpers.DeleteKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)).To(Succeed())
			}()

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().Delete(elfMachine.Status.VMRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			result, err := reconciler.reconcileDelete(machineContext)
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ToNot(HaveOccurred())

			// check k8s node still existed.
			err = ctrlContext.Client.Get(ctx, client.ObjectKeyFromObject(node), node)
			Expect(err).ToNot(HaveOccurred())

			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM to be deleted"))
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, clusterv1.DeletingReason}})
		})

		It("should not delete k8s node when control plane is not ready", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			task := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID
			cluster.Status.ControlPlaneReady = false

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			ctrlContext.Client = testEnv.Client
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			// before reconcile, create k8s node for VM.
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   elfMachine.Name,
					Labels: map[string]string{},
				},
			}
			Expect(testEnv.CreateAndWait(ctx, node)).To(Succeed())
			// before reconcile, create kubeconfig secret for cluster.
			Expect(helpers.CreateKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)).To(Succeed())
			defer func() {
				Expect(helpers.DeleteKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)).To(Succeed())
			}()

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().Delete(elfMachine.Status.VMRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			result, err := reconciler.reconcileDelete(machineContext)
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).ToNot(HaveOccurred())

			// check k8s node still existed.
			err = ctrlContext.Client.Get(ctx, client.ObjectKeyFromObject(node), node)
			Expect(err).ToNot(HaveOccurred())

			Expect(logBuffer.String()).To(ContainSubstring("Waiting for VM to be deleted"))
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, clusterv1.DeletingReason}})
		})

		It("should handle error when delete k8s node failed", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			elfMachine.Status.VMRef = *vm.LocalID
			cluster.Status.ControlPlaneReady = true

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			ctrlContext.Client = testEnv.Client
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.VMService = mockVMService

			// before reconcile, create k8s node for VM.
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   elfMachine.Name,
					Labels: map[string]string{},
				},
			}
			Expect(testEnv.CreateAndWait(ctx, node)).To(Succeed())

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			_, err := reconciler.reconcileDelete(machineContext)
			Expect(err).NotTo(BeZero())

			Expect(err.Error()).To(ContainSubstring("failed to get client"))

			// check k8s node still existed.
			err = ctrlContext.Client.Get(ctx, client.ObjectKeyFromObject(node), node)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("Reconcile Placement Group", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
		})

		It("should makes sure that the placement group exist", func() {
			towerCluster := fake.NewTowerCluster()
			placementGroup := fake.NewVMPlacementGroup(nil)
			placementGroup.EntityAsyncStatus = models.NewEntityAsyncStatus(models.EntityAsyncStatusUPDATING)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			placementGroupName, err := towerresources.GetVMPlacementGroupName(ctx, ctrlContext.Client, machine, cluster)
			Expect(err).NotTo(HaveOccurred())

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			result, err := reconciler.reconcilePlacementGroup(machineContext)
			Expect(result.RequeueAfter).To(Equal(config.DefaultRequeueTimeout))
			Expect(err).To(BeZero())

			placementGroup.EntityAsyncStatus = nil
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			result, err = reconciler.reconcilePlacementGroup(machineContext)
			Expect(result).To(BeZero())
			Expect(err).To(BeZero())

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			task := fake.NewTowerTask()
			taskStatus := models.TaskStatusSUCCESSED
			task.Status = &taskStatus
			withTaskVMPlacementGroup := fake.NewWithTaskVMPlacementGroup(nil, task)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(nil, errors.New(service.VMPlacementGroupNotFound))
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().CreateVMPlacementGroup(gomock.Any(), *towerCluster.ID, towerresources.GetVMPlacementGroupPolicy(machine)).Return(withTaskVMPlacementGroup, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)

			reconciler = &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			result, err = reconciler.reconcilePlacementGroup(machineContext)
			Expect(result).To(BeZero())
			Expect(err).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Creating placement group succeeded"))

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			taskStatus = models.TaskStatusFAILED
			task.Status = &taskStatus
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(nil, errors.New(service.VMPlacementGroupNotFound))
			mockVMService.EXPECT().CreateVMPlacementGroup(gomock.Any(), *towerCluster.ID, towerresources.GetVMPlacementGroupPolicy(machine)).Return(withTaskVMPlacementGroup, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)

			result, err = reconciler.reconcilePlacementGroup(machineContext)
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create placement group"))

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(nil, errors.New(service.VMPlacementGroupNotFound))
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().CreateVMPlacementGroup(gomock.Any(), *towerCluster.ID, towerresources.GetVMPlacementGroupPolicy(machine)).Return(withTaskVMPlacementGroup, nil)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(nil, errors.New("xxx"))

			result, err = reconciler.reconcilePlacementGroup(machineContext)
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("failed to wait for placement group creation task done timed out in %s: placementName %s, taskID %s", config.WaitTaskTimeout, placementGroupName, *withTaskVMPlacementGroup.TaskID)))

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(nil, errors.New(service.VMPlacementGroupNotFound))
			mockVMService.EXPECT().GetCluster(elfCluster.Spec.Cluster).Return(towerCluster, nil)
			mockVMService.EXPECT().CreateVMPlacementGroup(gomock.Any(), *towerCluster.ID, towerresources.GetVMPlacementGroupPolicy(machine)).Return(withTaskVMPlacementGroup, nil)
			task.Status = models.NewTaskStatus(models.TaskStatusFAILED)
			task.ErrorMessage = pointer.String(service.VMPlacementGroupDuplicate)
			mockVMService.EXPECT().WaitTask(*task.ID, config.WaitTaskTimeout, config.WaitTaskInterval).Return(task, nil)

			result, err = reconciler.reconcilePlacementGroup(machineContext)
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create placement group"))
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("Duplicate placement group detected, will try again in %s", placementGroupSilenceTime)))

			logBuffer = new(bytes.Buffer)
			klog.SetOutput(logBuffer)
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(nil, errors.New(service.VMPlacementGroupNotFound))

			result, err = reconciler.reconcilePlacementGroup(machineContext)
			Expect(result.RequeueAfter).To(Equal(config.VMPlacementGroupDuplicateTimeout))
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("Tower has duplicate placement group, skip creating placement group %s", placementGroupName)))
		})
	})

	Context("Reconcile static IP allocation", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
		})

		It("should wait for IP allocation", func() {
			placementGroup := fake.NewVMPlacementGroup([]string{fake.ID()})
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Times(3).Return(placementGroup, nil)

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
			placementGroup := fake.NewVMPlacementGroup([]string{fake.ID()})
			mockVMService.EXPECT().GetVMPlacementGroup(gomock.Any()).Return(placementGroup, nil)

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
			Expect(err).Should(HaveOccurred())
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
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool {
				if err := testEnv.Get(ctx, client.ObjectKey{Namespace: node.Namespace, Name: node.Name}, node); err != nil {
					return false
				}

				return node.Spec.ProviderID == machineutil.ConvertUUIDToProviderID(*vm.LocalID) &&
					node.Labels[infrav1.HostServerIDLabel] == elfMachine.Status.HostServerRef &&
					node.Labels[infrav1.HostServerNameLabel] == elfMachine.Status.HostServerName &&
					node.Labels[infrav1.TowerVMIDLabel] == *vm.ID &&
					node.Labels[infrav1.NodeGroupLabel] == machineutil.GetNodeGroupName(machine)
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
						infrav1.HostServerIDLabel:   "old-host-id",
						infrav1.HostServerNameLabel: "old-host-name",
						infrav1.TowerVMIDLabel:      "old-vm-id",
					},
				},
				Spec: corev1.NodeSpec{ProviderID: providerID},
			}
			Expect(testEnv.CreateAndWait(ctx, node)).To(Succeed())
			Expect(helpers.CreateKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)).To(Succeed())

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileNode(machineContext, vm)
			Expect(ok).Should(BeTrue())
			Expect(err).ToNot(HaveOccurred())

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
	Expect(err).ShouldNot(HaveOccurred())
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
