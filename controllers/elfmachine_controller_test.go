package controllers

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	clustererror "sigs.k8s.io/cluster-api/errors"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha3"
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

			result, err := reconciler.Reconcile(ctrl.Request{NamespacedName: util.ObjectKey(elfMachine)})
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

			result, err := reconciler.Reconcile(ctrl.Request{NamespacedName: util.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(buf.String()).To(ContainSubstring("ElfMachine linked to a cluster that is paused"))
		})

		It("should exit immediately on an error state", func() {
			createMachineError := clustererror.CreateMachineError
			elfMachine.Status.FailureReason = &createMachineError
			elfMachine.Status.FailureMessage = pointer.StringPtr("Couldn't create machine")

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext}

			result, err := reconciler.Reconcile(ctrl.Request{NamespacedName: util.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(buf.String()).To(ContainSubstring("Error state detected, skipping reconciliation"))
		})

		It("should add our finalizer to the machine", func() {
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
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

			reconciler := ElfMachineReconciler{ControllerContext: ctrlContext}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err).To(BeNil())
			Expect(buf.String()).To(ContainSubstring("Cluster infrastructure is not ready yet"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForClusterInfrastructureReason}})
		})

		It("should exit immediately if bootstrap data secret reference isn't available", func() {
			cluster.Status.InfrastructureReady = true
			cluster.Status.ControlPlaneInitialized = true

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := ElfMachineReconciler{ControllerContext: ctrlContext}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
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

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
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

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
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
			cluster.Status.ControlPlaneInitialized = true
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
		})

		It("should create a new VM if none exists", func() {
			vm := fake.NewVM()
			pendingJob := fake.NewVMJob()
			doneJob := fake.NewVMJob()
			doneJob.Id = pendingJob.Id
			doneJob.State = infrav1.VMJobDone
			resource := make(map[string]interface{})
			resource["type"] = "KVM_VM"
			resource["uuid"] = vm.UUID
			resources := make(map[string]interface{})
			resources["vm"] = resource
			doneJob.Resources = resources

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Clone(gomock.Any(), gomock.Any(), gomock.Any()).Return(pendingJob, nil)
			mockVMService.EXPECT().WaitJob(doneJob.Id).Return(doneJob, nil)
			mockVMService.EXPECT().Get(vm.UUID).Return(vm, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(vm.UUID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
		})

		It("should handle clone error", func() {
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Clone(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error"))

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(""))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.CloningFailedReason}})
		})

		It("should retry when create a VM if failed", func() {
			pendingJob := fake.NewVMJob()
			failedJob := fake.NewVMJob()
			failedJob.Id = pendingJob.Id
			failedJob.State = infrav1.VMJobFailed

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Clone(gomock.Any(), gomock.Any(), gomock.Any()).Return(pendingJob, nil)
			mockVMService.EXPECT().WaitJob(failedJob.Id).Return(failedJob, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			Expect(err.Error()).To(ContainSubstring("create VM job failed for ElfMachine"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(""))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.TaskFailure}})
		})

		It("should retry to create a VM from last stop", func() {
			vm := fake.NewVM()
			pendingJob := fake.NewVMJob()
			elfMachine.Status.TaskRef = pendingJob.Id
			doneJob := fake.NewVMJob()
			doneJob.Id = pendingJob.Id
			doneJob.State = infrav1.VMJobDone
			resource := make(map[string]interface{})
			resource["type"] = "KVM_VM"
			resource["uuid"] = vm.UUID
			resources := make(map[string]interface{})
			resources["vm"] = resource
			doneJob.Resources = resources

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().WaitJob(doneJob.Id).Return(doneJob, nil)
			mockVMService.EXPECT().Get(vm.UUID).Return(vm, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})

			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.VMRef).To(Equal(vm.UUID))
			Expect(elfMachine.Status.TaskRef).To(Equal(""))
		})
	})

	Context("Reconcile ElfMachine providerID", func() {
		It("should set providerID to ElfMachine when VM is created", func() {
			elfCluster, cluster, elfMachine, machine, secret := fake.NewClusterAndMachineObjects()
			cluster.Status.InfrastructureReady = true
			cluster.Status.ControlPlaneInitialized = true
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
			vm := fake.NewVM()
			elfMachine.Status.VMRef = vm.UUID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(*elfMachine.Spec.ProviderID).Should(Equal(infrautilv1.ConvertUUIDToProviderID(vm.UUID)))
		})
	})

	Context("Reconcile ElfMachine network", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			cluster.Status.ControlPlaneInitialized = true
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
		})

		It("should wait VM network ready", func() {
			vm := fake.NewVM()
			vm.Network = []infrav1.NetworkStatus{}
			elfMachine.Status.VMRef = vm.UUID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).Should(BeNil())
			Expect(buf.String()).To(ContainSubstring("network is not reconciled"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForNetworkAddressesReason}})
		})

		It("should set ElfMachine to ready when VM network is ready", func() {
			vm := fake.NewVM()
			elfMachine.Status.VMRef = vm.UUID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.Ready).To(BeTrue())
			expectConditions(elfMachine, []conditionAssertion{{conditionType: infrav1.VMProvisionedCondition, status: corev1.ConditionTrue}})
		})
	})

	Context("Delete a ElfMachine", func() {
		BeforeEach(func() {
			cluster.Status.InfrastructureReady = true
			cluster.Status.ControlPlaneInitialized = true
			machine.Spec.Bootstrap = clusterv1.Bootstrap{DataSecretName: &secret.Name}
			elfMachine.DeletionTimestamp = &metav1.Time{Time: time.Now().UTC()}
		})

		It("should not error and not requeue the request without VM", func() {
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result).To(BeZero())
			Expect(err).Should(BeNil())
			Expect(elfMachine.HasVM()).To(BeFalse())
			Expect(elfMachine.HasTask()).To(BeFalse())
			Expect(buf.String()).To(ContainSubstring("VM has been deleted"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
		})

		It("should remove vmRef when VM not found", func() {
			vm := fake.NewVM()
			elfMachine.Status.VMRef = vm.UUID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			vmNotFoundError := errors.New("VM_NOT_FOUND")
			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(nil, vmNotFoundError)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result).To(BeZero())
			Expect(err).Should(BeNil())
			Expect(buf.String()).To(ContainSubstring("VM be deleted"))
		})

		It("should handle task - pending", func() {
			vm := fake.NewVM()
			elfMachine.Status.VMRef = vm.UUID
			job := fake.NewVMJob()
			job.State = infrav1.VMJobPending
			elfMachine.Status.TaskRef = job.Id

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().GetJob(elfMachine.Status.TaskRef).Return(job, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result).To(BeZero())
			Expect(clustererror.IsRequeueAfter(err)).To(BeTrue())
			Expect(buf.String()).To(ContainSubstring("Waiting for delete VM job done"))
		})

		It("should handle task - failed", func() {
			vm := fake.NewVM()
			elfMachine.Status.VMRef = vm.UUID
			job := fake.NewVMJob()
			job.State = infrav1.VMJobFailed
			elfMachine.Status.TaskRef = job.Id

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().GetJob(elfMachine.Status.TaskRef).Return(job, nil)
			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(nil, errors.New("some error"))

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			Expect(buf.String()).To(ContainSubstring("Delete VM job failed"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.HasTask()).To(BeFalse())
		})

		It("should handle task - done", func() {
			vm := fake.NewVM()
			elfMachine.Status.VMRef = vm.UUID
			job := fake.NewVMJob()
			job.State = infrav1.VMJobDone
			elfMachine.Status.TaskRef = job.Id

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().GetJob(elfMachine.Status.TaskRef).Return(job, nil)
			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(nil, errors.New("some error"))

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			_, _ = reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			Expect(buf.String()).To(ContainSubstring("Delete VM job done"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.HasTask()).To(BeFalse())
		})

		It("should power off when VM is powered on", func() {
			vm := fake.NewVM()
			vm.State = infrav1.VirtualMachineStatePoweredOn
			elfMachine.Status.VMRef = vm.UUID
			job := fake.NewVMJob()

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().PowerOff(elfMachine.Status.VMRef).Return(job, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(buf.String()).To(ContainSubstring("Waiting for VM power off"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.TaskRef).To(Equal(job.Id))
		})

		It("should handle delete error", func() {
			vm := fake.NewVM()
			elfMachine.Status.VMRef = vm.UUID

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().Delete(elfMachine.Status.VMRef).Return(nil, errors.New("some error"))

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).To(BeZero())
			Expect(err).ToNot(BeZero())
			Expect(buf.String()).To(ContainSubstring("Destroying VM"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, clusterv1.DeletionFailedReason}})
		})

		It("should delete when VM is not running", func() {
			vm := fake.NewVM()
			elfMachine.Status.VMRef = vm.UUID
			job := fake.NewVMJob()

			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret)

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().Get(elfMachine.Status.VMRef).Return(vm, nil)
			mockVMService.EXPECT().Delete(elfMachine.Status.VMRef).Return(job, nil)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, VMService: mockVMService}

			elfMachineKey := util.ObjectKey(elfMachine)
			result, err := reconciler.Reconcile(ctrl.Request{NamespacedName: elfMachineKey})
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).To(BeZero())
			Expect(buf.String()).To(ContainSubstring("Waiting for VM to be deleted"))
			elfMachine = &infrav1.ElfMachine{}
			Expect(reconciler.Client.Get(reconciler, elfMachineKey, elfMachine)).To(Succeed())
			Expect(elfMachine.Status.TaskRef).To(Equal(job.Id))
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
