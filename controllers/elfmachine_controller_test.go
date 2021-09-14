package controllers

import (
	"bytes"
	goctx "context"
	"flag"
	"fmt"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/haijianyang/cloudtower-go-sdk/models"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	capierrors "sigs.k8s.io/cluster-api/errors"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha4"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service/mock_services"
	infrautilv1 "github.com/smartxworks/cluster-api-provider-elf/pkg/util"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

var _ = Describe("ElfMachineReconciler", func() {
	var (
		elfCluster    *infrav1.ElfCluster
		cluster       *clusterv1.Cluster
		elfMachine    *infrav1.ElfMachine
		machine       *clusterv1.Machine
		secret        *corev1.Secret
		mockCtrl      *gomock.Controller
		mockVMService *mock_services.MockVMService
	)

	BeforeEach(func() {
		// set log
		if err := flag.Set("logtostderr", "false"); err != nil {
			_ = fmt.Errorf("Error setting logtostderr flag")
		}
		if err := flag.Set("v", "6"); err != nil {
			_ = fmt.Errorf("Error setting v flag")
		}
		klog.SetOutput(GinkgoWriter)

		elfCluster, cluster, elfMachine, machine, secret = fake.NewClusterAndMachineObjects()

		// mock
		mockCtrl = gomock.NewController(GinkgoT())
		mockVMService = mock_services.NewMockVMService(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("Reconcile an ElfMachine", func() {
		It("should not error and not requeue the request without machine", func() {
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext}

			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: util.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(buf.String()).To(ContainSubstring("Waiting for Machine Controller to set OwnerRef on ElfMachine"))
		})

		It("should not error and not requeue the request when Cluster is paused", func() {
			cluster.Spec.Paused = true

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext}

			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: util.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(buf.String()).To(ContainSubstring("ElfMachine linked to a cluster that is paused"))
		})

		It("should exit immediately on an error state", func() {
			createMachineError := capierrors.CreateMachineError
			elfMachine.Status.FailureReason = &createMachineError
			elfMachine.Status.FailureMessage = pointer.StringPtr("Couldn't create machine")

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: util.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(buf.String()).To(ContainSubstring("Error state detected, skipping reconciliation"))
		})

		It("should add our finalizer to the machine", func() {
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Finalizers).To(ContainElement(infrav1.MachineFinalizer))
		})

		It("should exit immediately if cluster infra isn't ready", func() {
			cluster.Status.InfrastructureReady = false

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err).To(BeNil())
			Expect(buf.String()).To(ContainSubstring("Cluster infrastructure is not ready yet"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForClusterInfrastructureReason}})
		})

		It("should exit immediately if bootstrap data secret reference isn't available", func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err).To(BeNil())
			Expect(buf.String()).To(ContainSubstring("Waiting for bootstrap data to be available"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForBootstrapDataReason}})
		})

		It("should wait cluster ControlPlaneInitialized true when create worker machine", func() {
			cluster.Status.InfrastructureReady = true

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err).To(BeNil())
			Expect(buf.String()).To(ContainSubstring("Waiting for the control plane to be initialized"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, clusterv1.WaitingForControlPlaneAvailableReason}})
		})

		It("should not wait cluster ControlPlaneInitialized true when create master machine", func() {
			cluster.Status.InfrastructureReady = true
			elfMachine.Labels[clusterv1.MachineControlPlaneLabelName] = ""

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err).To(BeNil())
			Expect(buf.String()).To(ContainSubstring("Waiting for bootstrap data to be available"))
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

		It("should create a new VM if none exists", func() {
			vm := fake.NewTowerVM()
			vm.Name = &machine.Name
			task := fake.NewTowerTask()
			withTaskVM := fake.NewWithTaskVM(vm, task)

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Clone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(withTaskVM, nil)
			mockVMService.EXPECT().Get(*vm.ID).Return(vm, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).Should(BeNil())
			Expect(buf.String()).To(ContainSubstring("Waiting for VM task done"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.ID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
		})

		It("should recover from lost task", func() {
			vm := fake.NewTowerVM()
			vm.Name = &machine.Name

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Clone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("VM_DUPLICATE"))
			mockVMService.EXPECT().GetByName(machine.Name).Return(vm, nil)
			mockVMService.EXPECT().Get(*vm.ID).Return(vm, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).Should(BeNil())
			Expect(buf.String()).To(ContainSubstring("Waiting for VM task done"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.ID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
		})

		It("should handle clone error", func() {
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Clone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(err.Error()).To(ContainSubstring("failed to reconcile VM"))
			Expect(elfMachine.Status.VMRef).To(Equal(""))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.CloningFailedReason}})
		})

		It("should set failure when VM was deleted", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			elfMachine.Status.VMRef = *vm.LocalID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(nil, errors.New("VM_NOT_FOUND"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err.Error()).To(ContainSubstring("VM_NOT_FOUND"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(*elfMachine.Status.FailureReason).To(Equal(capierrors.UpdateMachineError))
			Expect(*elfMachine.Status.FailureMessage).To(Equal(fmt.Sprintf("Unable to find VM by UUID %s. The VM was removed from infra", elfMachine.Status.VMRef)))
		})

		It("should retry when create a VM if failed", func() {
			vm := fake.NewTowerVM()
			task := fake.NewTowerTask()
			status := models.TaskStatusFAILED
			task.Status = &status
			elfMachine.Status.VMRef = *vm.ID
			elfMachine.Status.TaskRef = *task.ID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(nil, errors.New("VM_NOT_FOUND"))
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err.Error()).To(ContainSubstring("VM task failed for ElfMachine"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(""))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.TaskFailure}})
		})

		It("should power on the VM after it is created", func() {
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

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task1, nil)
			mockVMService.EXPECT().PowerOn(*vm.LocalID).Return(task2, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).Should(BeNil())
			Expect(buf.String()).To(ContainSubstring("Waiting for VM to be powered on"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task2.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.PoweringOnReason}})
		})

		It("should handle power on error", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			task1 := fake.NewTowerTask()
			taskStatus := models.TaskStatusSUCCESSED
			task1.Status = &taskStatus
			elfMachine.Status.VMRef = *vm.ID
			elfMachine.Status.TaskRef = *task1.ID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task1, nil)
			mockVMService.EXPECT().PowerOn(*vm.LocalID).Return(nil, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err.Error()).To(ContainSubstring("failed to trigger power on for VM"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.PoweringOnFailedReason}})
		})

		It("should handle power on task failed", func() {
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

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task1, nil)
			mockVMService.EXPECT().PowerOn(*vm.LocalID).Return(task2, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(buf.String()).To(ContainSubstring("task failed"))
			Expect(buf.String()).To(ContainSubstring("Waiting for VM to be powered on"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task2.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.PoweringOnReason}})
		})
	})

	Context("Reconcile ElfMachine providerID", func() {
		It("should set providerID to ElfMachine when VM is created", func() {
			elfCluster, cluster, elfMachine, machine, secret := fake.NewClusterAndMachineObjects()
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
			vm := fake.NewTowerVM()
			elfMachine.Status.VMRef = *vm.LocalID
			vm.EntityAsyncStatus = nil

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(*elfMachine.Spec.ProviderID).Should(Equal(infrautilv1.ConvertUUIDToProviderID(*vm.LocalID)))
		})
	})

	Context("Reconcile ElfMachine network", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			conditions.MarkTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
		})

		It("should wait VM network ready", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			vm.Ips = infrautilv1.TowerString("")
			elfMachine.Status.VMRef = *vm.LocalID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).Should(BeNil())
			Expect(buf.String()).To(ContainSubstring("network is not reconciled"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForNetworkAddressesReason}})
		})

		It("should set ElfMachine to ready when VM network is ready", func() {
			ip := "116.116.116.116"

			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			vm.Ips = infrautilv1.TowerString("116.116.116.116")
			elfMachine.Status.VMRef = *vm.LocalID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.Network[0].NetworkIndex).To(Equal(0))
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
		})

		It("should not error and not requeue the request without VM", func() {
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result).To(BeZero())
			Expect(err).Should(BeNil())
			Expect(elfMachine.HasVM()).To(BeFalse())
			Expect(elfMachine.HasTask()).To(BeFalse())
			Expect(buf.String()).To(ContainSubstring("VM has been deleted"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
		})

		It("should remove vmRef when VM not found", func() {
			vm := fake.NewTowerVM()
			task := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID
			elfMachine.Status.TaskRef = *task.ID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			vmNotFoundError := errors.New("VM_NOT_FOUND")
			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(nil, vmNotFoundError)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result).To(BeZero())
			Expect(err).To(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("VM be deleted"))
			elfCluster = &infrav1.ElfCluster{}
			err = reconciler.Client.Get(reconciler, elfMachineKey, elfCluster)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("should handle task - pending", func() {
			vm := fake.NewTowerVM()
			status := models.VMStatusRUNNING
			vm.Status = &status
			vm.EntityAsyncStatus = "UPDATING"
			task := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID
			elfMachine.Status.TaskRef = *task.ID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result).NotTo(BeZero())
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(buf.String()).To(ContainSubstring("Waiting for VM task done"))
		})

		It("should handle task - failed", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			task := fake.NewTowerTask()
			status := models.TaskStatusFAILED
			task.Status = &status
			elfMachine.Status.VMRef = *vm.LocalID
			elfMachine.Status.TaskRef = *task.ID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task, nil)
			mockVMService.EXPECT().PowerOff(elfMachine.Status.VMRef).Return(nil, errors.New("some error"))

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(buf.String()).To(ContainSubstring("VM task failed"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
		})

		It("should handle task - done", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			task := fake.NewTowerTask()
			status := models.TaskStatusSUCCESSED
			task.Status = &status
			elfMachine.Status.VMRef = *vm.LocalID
			elfMachine.Status.TaskRef = *task.ID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().GetTask(elfMachine.Status.TaskRef).Return(task, nil)
			mockVMService.EXPECT().PowerOff(elfMachine.Status.VMRef).Return(nil, errors.New("some error"))

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(buf.String()).To(ContainSubstring("VM task successful"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
		})

		It("should power off when VM is powered on", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			task := fake.NewTowerTask()
			elfMachine.Status.VMRef = *vm.LocalID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().PowerOff(elfMachine.Status.VMRef).Return(task, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(buf.String()).To(ContainSubstring("Waiting for VM power off"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
		})

		It("should handle delete error", func() {
			vm := fake.NewTowerVM()
			vm.EntityAsyncStatus = nil
			status := models.VMStatusSTOPPED
			vm.Status = &status
			elfMachine.Status.VMRef = *vm.LocalID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().Delete(elfMachine.Status.VMRef).Return(nil, errors.New("some error"))

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).ToNot(BeZero())
			Expect(buf.String()).To(ContainSubstring("Destroying VM"))
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

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().Delete(elfMachine.Status.VMRef).Return(task, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(buf.String()).To(ContainSubstring("Waiting for VM to be deleted"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(*vm.LocalID))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
		})
	})
})

func newCtrlContexts(elfCluster *infrav1.ElfCluster, cluster *clusterv1.Cluster,
	elfMachine *infrav1.ElfMachine, machine *clusterv1.Machine, secret *corev1.Secret) *context.ControllerContext {
	ctrlMgrContext := fake.NewControllerManagerContext(cluster, elfCluster, elfMachine, machine, secret)
	ctrlContext := &context.ControllerContext{
		ControllerManagerContext: ctrlMgrContext,
		Logger:                   log.Log,
	}

	return ctrlContext
}

type conditionAssertion struct {
	conditionType clusterv1.ConditionType
	status        corev1.ConditionStatus
	severity      clusterv1.ConditionSeverity
	reason        string
}

func expectConditions(m *infrav1.ElfMachine, expected []conditionAssertion) {
	Expect(len(m.Status.Conditions)).To(BeNumerically(">=", len(expected)), "number of conditions")
	for _, c := range expected {
		actual := conditions.Get(m, c.conditionType)
		Expect(actual).To(Not(BeNil()))
		Expect(actual.Type).To(Equal(c.conditionType))
		Expect(actual.Status).To(Equal(c.status))
		Expect(actual.Severity).To(Equal(c.severity))
		Expect(actual.Reason).To(Equal(c.reason))
	}
}
