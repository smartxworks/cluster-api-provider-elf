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
	"bytes"
	goctx "context"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	agentv1 "github.com/smartxworks/host-config-agent-api/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	capiv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	conditionsv1beta1 "sigs.k8s.io/cluster-api/util/deprecated/v1beta1/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/constants"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/hostagent"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service/mock_services"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
	"github.com/smartxworks/cluster-api-provider-elf/test/helpers"
)

var _ = Describe("ElfMachineReconciler", func() {
	var (
		elfCluster          *capev1.ElfCluster
		cluster             *capiv1.Cluster
		elfMachine          *capev1.ElfMachine
		machine             *capiv1.Machine
		secret              *corev1.Secret
		kubeConfigSecret    *corev1.Secret
		hostAgentJobsConfig *corev1.ConfigMap
		logBuffer           *bytes.Buffer
		mockCtrl            *gomock.Controller
		mockVMService       *mock_services.MockVMService
		mockNewVMService    service.NewVMServiceFunc
	)

	_, err := testEnv.CreateNamespace(goctx.Background(), "sks-system")
	Expect(err).NotTo(HaveOccurred())

	BeforeEach(func() {
		logBuffer = new(bytes.Buffer)
		klog.SetOutput(logBuffer)

		elfCluster, cluster, elfMachine, machine, secret = fake.NewClusterAndMachineObjects()
		hostAgentJobsConfig = newHostAgentJobsConfigMap()

		// mock
		mockCtrl = gomock.NewController(GinkgoT())
		mockVMService = mock_services.NewMockVMService(mockCtrl)
		mockNewVMService = func(_ goctx.Context, _ client.Client, _ capev1.Tower, _ logr.Logger) (service.VMService, error) {
			return mockVMService, nil
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("reconcileVMResources", func() {
		It("should mark ResourcesHotUpdatedCondition to true", func() {
			agentJob := hostagent.GenerateExpandRootPartitionJob(elfMachine, "", "test-node")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseSucceeded
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			kubeConfigSecret, err := helpers.NewKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)
			Expect(err).ShouldNot(HaveOccurred())
			machine.Status.NodeRef = capiv1.MachineNodeReference{Name: "test-node"}
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMDiskReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vmVolume := fake.NewVMVolume(elfMachine)
			vmDisk := fake.NewVMDisk(vmVolume)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.VMDisks = []*models.NestedVMDisk{{ID: vmDisk.ID}}
			mockVMService.EXPECT().GetVMDisks([]string{*vmDisk.ID}).Return([]*models.VMDisk{vmDisk}, nil)
			mockVMService.EXPECT().GetVMVolume(*vmVolume.ID).Return(vmVolume, nil)
			vmNic := fake.NewVMNic()
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return([]*models.VMNic{vmNic}, nil)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMResources(ctx, machineContext, vm)
			expectConditions(elfMachine, []conditionAssertion{{conditionType: capev1.ResourcesHotUpdatedCondition, status: corev1.ConditionTrue}})
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
		})
	})

	Context("reconcieVMVolume", func() {
		It("should not reconcile when disk size is 0", func() {
			elfMachine.Spec.DiskGiB = 0
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vmVolume := fake.NewVMVolume(elfMachine)
			vmDisk := fake.NewVMDisk(vmVolume)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.VMDisks = []*models.NestedVMDisk{{ID: vmDisk.ID}}
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcieVMVolume(ctx, machineContext, vm, capev1.VMProvisionedCondition)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not expand the disk when size is up to date", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vmVolume := fake.NewVMVolume(elfMachine)
			vmDisk := fake.NewVMDisk(vmVolume)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.VMDisks = []*models.NestedVMDisk{{ID: vmDisk.ID}}
			mockVMService.EXPECT().GetVMDisks([]string{*vmDisk.ID}).Return([]*models.VMDisk{vmDisk}, nil)
			mockVMService.EXPECT().GetVMVolume(*vmVolume.ID).Return(vmVolume, nil)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcieVMVolume(ctx, machineContext, vm, capev1.VMProvisionedCondition)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})

		It("should expand the disk when size is not up to date", func() {
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMDiskReason, capiv1beta1.ConditionSeverityInfo, "")
			elfMachine.Spec.DiskGiB = 20
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vmVolume := fake.NewVMVolume(elfMachine)
			vmVolume.Size = service.TowerDisk(10)
			vmDisk := fake.NewVMDisk(vmVolume)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.VMDisks = []*models.NestedVMDisk{{ID: vmDisk.ID}}
			mockVMService.EXPECT().GetVMDisks([]string{*vmDisk.ID}).Return([]*models.VMDisk{vmDisk}, nil)
			mockVMService.EXPECT().GetVMVolume(*vmVolume.ID).Return(vmVolume, nil)
			task := fake.NewTowerTask("")
			withTaskVMVolume := fake.NewWithTaskVMVolume(vmVolume, task)
			mockVMService.EXPECT().ResizeVMVolume(*vmVolume.ID, *service.TowerDisk(20)).Return(withTaskVMVolume, nil)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcieVMVolume(ctx, machineContext, vm, capev1.ResourcesHotUpdatedCondition)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the vm volume"))
			Expect(elfMachine.Status.TaskRef).To(Equal(*withTaskVMVolume.TaskID))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.ExpandingVMDiskReason}})
		})
	})

	Context("resizeVMVolume", func() {
		It("should save the conditionType first", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vmVolume := fake.NewVMVolume(elfMachine)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err := reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, capev1.VMProvisionedCondition)
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{{capev1.VMProvisionedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.ExpandingVMDiskReason}})

			vmVolume.EntityAsyncStatus = models.NewEntityAsyncStatus(models.EntityAsyncStatusUPDATING)
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMDiskFailedReason, capiv1beta1.ConditionSeverityWarning, "")
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			err = reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, capev1.ResourcesHotUpdatedCondition)
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for vm volume task done"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityWarning, capev1.ExpandingVMDiskFailedReason}})
		})

		It("should wait task done", func() {
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMDiskReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vmVolume := fake.NewVMVolume(elfMachine)
			task := fake.NewTowerTask("")
			withTaskVMVolume := fake.NewWithTaskVMVolume(vmVolume, task)
			mockVMService.EXPECT().ResizeVMVolume(*vmVolume.ID, int64(10)).Return(withTaskVMVolume, nil)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err := reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, capev1.ResourcesHotUpdatedCondition)
			Expect(err).ToNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the vm volume"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.ExpandingVMDiskReason}})

			mockVMService.EXPECT().ResizeVMVolume(*vmVolume.ID, int64(10)).Return(nil, unexpectedError)
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err = reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, capev1.ResourcesHotUpdatedCondition)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to trigger expand size from"))
			Expect(elfMachine.Status.TaskRef).To(Equal(*withTaskVMVolume.TaskID))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityWarning, capev1.ExpandingVMDiskFailedReason}})

			task = fake.NewTowerTask("")
			withTaskVMVolume = fake.NewWithTaskVMVolume(vmVolume, task)
			mockVMService.EXPECT().ResizeVMVolume(*vmVolume.ID, int64(10)).Return(withTaskVMVolume, nil)
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err = reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, capev1.ResourcesHotUpdatedCondition)
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the vm volume"))
			Expect(elfMachine.Status.TaskRef).To(Equal(*withTaskVMVolume.TaskID))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityWarning, capev1.ExpandingVMDiskFailedReason}})
		})
	})

	Context("expandVMRootPartition", func() {
		BeforeEach(func() {
			var err error
			kubeConfigSecret, err = helpers.NewKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should not expand root partition without ResourcesHotUpdatedCondition", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{})
		})

		It("should wait for node exists", func() {
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMDiskReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for node exists for host agent job"))
			Expect(logBuffer.String()).To(ContainSubstring("expand-root-partition"))
		})

		It("should create agent job to expand root partition", func() {
			machine.Status.NodeRef = capiv1.MachineNodeReference{Name: "test-node"}
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMDiskReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret, hostAgentJobsConfig)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for job to complete"))
			Expect(logBuffer.String()).To(ContainSubstring("expand-root-partition"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.ExpandingRootPartitionReason}})
			var agentJob *agentv1.HostOperationJob
			Eventually(func() error {
				var err error
				agentJob, err = hostagent.GetHostJob(ctx, testEnv.Client, elfMachine.Namespace, hostagent.GetExpandRootPartitionJobName(elfMachine))
				return err
			}, timeout).Should(Succeed())
			Expect(agentJob.Name).To(Equal(hostagent.GetExpandRootPartitionJobName(elfMachine)))
		})

		It("should retry when job failed", func() {
			machine.Status.NodeRef = capiv1.MachineNodeReference{Name: "test-node"}
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMDiskReason, capiv1beta1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateExpandRootPartitionJob(elfMachine, "", "test-node")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret, hostAgentJobsConfig)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for HostJob done"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.ExpandingRootPartitionReason}})

			logBuffer.Reset()
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseFailed
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("HostJob failed, will try again"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityWarning, capev1.ExpandingRootPartitionFailedReason}})

			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource = agentJob.DeepCopy()
			agentJob.Status.LastExecutionTime = &metav1.Time{Time: time.Now().Add(-3 * time.Minute).UTC()}
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				err := testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)
				return apierrors.IsNotFound(err)
			}, timeout).Should(BeTrue())
		})

		It("should record job succeeded", func() {
			machine.Status.NodeRef = capiv1.MachineNodeReference{Name: "test-node"}
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMDiskReason, capiv1beta1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateExpandRootPartitionJob(elfMachine, "", "test-node")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseSucceeded
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("HostJob succeeded"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.ExpandingRootPartitionReason}})
		})
	})

	Context("restartKubelet", func() {
		BeforeEach(func() {
			var err error
			kubeConfigSecret, err = helpers.NewKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should not restart kubelet without restartKubelet", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{})
		})

		It("should wait for node exists", func() {
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMComputeResourcesReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for node exists for host agent job"))
			Expect(logBuffer.String()).To(ContainSubstring("restart-kubelet"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.RestartingKubeletReason}})
		})

		It("should create agent job to restart kubelet", func() {
			machine.Status.NodeRef = capiv1.MachineNodeReference{Name: "test-node"}
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMComputeResourcesReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret, hostAgentJobsConfig)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for job to complete"))
			Expect(logBuffer.String()).To(ContainSubstring("restart-kubelet"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.RestartingKubeletReason}})
			var agentJob *agentv1.HostOperationJob
			Eventually(func() error {
				var err error
				agentJob, err = hostagent.GetHostJob(ctx, testEnv.Client, elfMachine.Namespace, hostagent.GetRestartKubeletJobName(elfMachine))
				return err
			}, timeout).Should(Succeed())
			Expect(agentJob.Name).To(Equal(hostagent.GetRestartKubeletJobName(elfMachine)))
		})

		It("should retry when job failed", func() {
			machine.Status.NodeRef = capiv1.MachineNodeReference{Name: "test-node"}
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMComputeResourcesReason, capiv1beta1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateRestartKubeletJob(elfMachine, "", "test-node")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for HostJob done"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.RestartingKubeletReason}})

			logBuffer.Reset()
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseFailed
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("HostJob failed, will try again after three minutes"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityWarning, capev1.RestartingKubeletFailedReason}})

			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource = agentJob.DeepCopy()
			agentJob.Status.LastExecutionTime = &metav1.Time{Time: time.Now().Add(-3 * time.Minute).UTC()}
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				err := testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)
				return apierrors.IsNotFound(err)
			}, timeout).Should(BeTrue())
		})

		It("should record job succeeded", func() {
			machine.Status.NodeRef = capiv1.MachineNodeReference{Name: "test-node"}
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMComputeResourcesReason, capiv1beta1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateRestartKubeletJob(elfMachine, "", "test-node")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseSucceeded
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("HostJob succeeded"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.RestartingKubeletReason}})
		})
	})

	Context("reconcileVMCPUAndMemory", func() {
		It("should not reconcile when numCPUs or memory is excepted", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineCtx := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMCPUAndMemory(ctx, machineCtx, vm)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			Expect(elfMachine.Status.Resources.CPUCores).To(Equal(*vm.Vcpu))
			specMemory := *resource.NewQuantity(service.ByteToMiB(*vm.Memory)*1024*1024, resource.BinarySI)
			Expect(elfMachine.Status.Resources.Memory.Equal(specMemory)).To(BeTrue())
		})

		It("should save the conditionType first", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			elfMachine.Spec.NumCPUs += 1
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineCtx := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMCPUAndMemory(ctx, machineCtx, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.ExpandingVMComputeResourcesReason}})
		})

		It("should wait task done", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			elfMachine.Spec.MemoryMiB += 1
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.ExpandingVMComputeResourcesReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineCtx := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			task := fake.NewTowerTask("")
			withTaskVM := fake.NewWithTaskVM(vm, task)
			mockVMService.EXPECT().UpdateVM(vm, elfMachine).Return(withTaskVM, nil)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMCPUAndMemory(ctx, machineCtx, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the VM to be updated CPU and memory"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.ExpandingVMComputeResourcesReason}})

			logBuffer.Reset()
			inMemoryCache.Flush()
			mockVMService.EXPECT().UpdateVM(vm, elfMachine).Return(nil, unexpectedError)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err = reconciler.reconcileVMCPUAndMemory(ctx, machineCtx, vm)
			Expect(ok).To(BeFalse())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to trigger update CPU and memory for VM"))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityWarning, capev1.ExpandingVMComputeResourcesFailedReason}})

			logBuffer.Reset()
			inMemoryCache.Flush()
			task = fake.NewTowerTask("")
			withTaskVM = fake.NewWithTaskVM(vm, task)
			mockVMService.EXPECT().UpdateVM(vm, elfMachine).Return(withTaskVM, nil)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err = reconciler.reconcileVMCPUAndMemory(ctx, machineCtx, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the VM to be updated CPU and memory"))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityWarning, capev1.ExpandingVMComputeResourcesFailedReason}})
		})
	})

	Context("reconcieVMNetworkDevices", func() {
		BeforeEach(func() {
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, "", capiv1beta1.ConditionSeverityInfo, "")
			var err error
			kubeConfigSecret, err = helpers.NewKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should return error when failed to get VM nics", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(nil, unexpectedError)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcieVMNetworkDevices(ctx, machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(unexpectedError.Error()))
		})

		It("should return false and record event when VM has no network devices", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			machineContext.ElfMachine.Status.Network = []capev1.NetworkStatus{{IPAddrs: []string{"127.0.0.1"}}}
			vm := fake.NewTowerVMFromElfMachine(elfMachine)

			// nil vmNics.
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(nil, nil)
			eventRecorder := record.NewFakeRecorder(2)
			reconciler := &ElfMachineReconciler{
				ControllerManagerContext: ctrlMgrCtx,
				NewVMService:             mockNewVMService,
				Recorder:                 eventRecorder,
			}
			ok, err := reconciler.reconcieVMNetworkDevices(ctx, machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("failed to get VM network devices."))
			Expect(logBuffer.String()).To(ContainSubstring("no network devices found for VM " + *vm.Name))
			Expect(<-eventRecorder.Events).To(Equal("Warning VMNetworkDevicesNotFound no network devices found for VM " + *vm.Name))
			// The network status is kept as is, so that the nics can be reconciled after the VM network devices are ready.
			Expect(machineContext.ElfMachine.Status.Network).To(Equal([]capev1.NetworkStatus{{IPAddrs: []string{"127.0.0.1"}}}))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, ""}})

			// empty (non-nil) vmNics.
			logBuffer.Reset()
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return([]*models.VMNic{}, nil)
			eventRecorder = record.NewFakeRecorder(2)
			reconciler = &ElfMachineReconciler{
				ControllerManagerContext: ctrlMgrCtx,
				NewVMService:             mockNewVMService,
				Recorder:                 eventRecorder,
			}
			ok, err = reconciler.reconcieVMNetworkDevices(ctx, machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("failed to get VM network devices."))
			Expect(<-eventRecorder.Events).To(Equal("Warning VMNetworkDevicesNotFound no network devices found for VM " + *vm.Name))
			// No new nics are added, and the network status is kept as is.
			Expect(machineContext.ElfMachine.Status.Network).To(Equal([]capev1.NetworkStatus{{IPAddrs: []string{"127.0.0.1"}}}))
		})

		It("should reconcie vm network devices when vm nic is missing IP address", func() {
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.WaitingForNetworkAddressesReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vmNic := fake.NewVMNic()
			vmNic.IPAddress = ptr.To("")
			vmNics := []*models.VMNic{vmNic}
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(vmNics, nil)
			machine.Status.NodeRef = capiv1.MachineNodeReference{Name: "test-node"}
			agentJob := hostagent.GenerateSetNetworkDeviceConfigJob(elfMachine, "", nil, "test-node")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).To(Succeed())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseSucceeded
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcieVMNetworkDevices(ctx, machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("waiting for the vm network device"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.WaitingForNetworkAddressesReason}})
		})

		It("should reconcie vm network devices when vm nic is ready", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vmNic := fake.NewVMNic()
			vmNics := []*models.VMNic{vmNic}
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(vmNics, nil)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcieVMNetworkDevices(ctx, machineContext, vm)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("addVMNetworkDevices", func() {
		It("should add new vm network devices", func() {
			elfMachine.Status.Network = []capev1.NetworkStatus{{}, {}, {}}
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMNics(ctx, machineContext, vm, nil)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(elfMachine.Status.Network).To(BeEmpty())
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.AddingVMNetworkDeviceReason}})

			vlan := fake.NewVlan()
			newNic := &models.VMNicParams{
				Model:         models.NewVMNicModel(models.VMNicModelVIRTIO),
				Enabled:       service.TowerBool(true),
				Mirror:        service.TowerBool(false),
				ConnectVlanID: vlan.ID,
				MacAddress:    service.TowerString(elfMachine.Spec.Network.Devices[0].MACAddr),
				SubnetMask:    service.TowerString(elfMachine.Spec.Network.Devices[0].Netmask),
			}
			mockVMService.EXPECT().GetVlan(elfMachine.Spec.Network.Devices[0].Vlan).Return(vlan, nil)
			mockVMService.EXPECT().AddVMNics(*vm.ID, []*models.VMNicParams{newNic}).Return(nil, unexpectedError)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err = reconciler.reconcileVMNics(ctx, machineContext, vm, nil)
			Expect(ok).To(BeFalse())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to trigger add new nics to vm"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityWarning, capev1.AddingVMNetworkDeviceFailedReason}})

			task := fake.NewTowerTask(models.TaskStatusEXECUTING)
			withTaskVM := fake.NewWithTaskVM(vm, task)
			mockVMService.EXPECT().GetVlan(elfMachine.Spec.Network.Devices[0].Vlan).Return(vlan, nil)
			mockVMService.EXPECT().AddVMNics(*vm.ID, []*models.VMNicParams{newNic}).Return(withTaskVM, nil)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err = reconciler.reconcileVMNics(ctx, machineContext, vm, nil)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.AddingVMNetworkDeviceReason}})

			vmNic := fake.NewVMNic()
			vmNics := []*models.VMNic{vmNic}
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err = reconciler.reconcileVMNics(ctx, machineContext, vm, vmNics)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("setVMNetworkDeviceConfig", func() {
		BeforeEach(func() {
			var err error
			kubeConfigSecret, err = helpers.NewKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should not set network config when no add new nics", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext, nil)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not set network config when expected network devices is <= the status network devices", func() {
			elfMachine.Status.Network = nil
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.AddingVMNetworkDeviceReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext, nil)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("No vm nics needs to be set network configuration"))
		})

		It("should not set network config when expected network devices is <= the vm nics", func() {
			elfMachine.Spec.Network.Devices = append(elfMachine.Spec.Network.Devices, capev1.NetworkDeviceSpec{NetworkType: capev1.NetworkTypeIPV4})
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.AddingVMNetworkDeviceReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext, nil)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("The expected number of network devices is not equal to the number of vm nics"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityWarning, capev1.SettingVMNetworkDeviceConfigFailedReason}})
		})

		It("should wait for network mac address", func() {
			vmNic1 := fake.NewVMNic()
			vmNic2 := fake.NewVMNic()
			vmNic2.MacAddress = nil
			vmNics := []*models.VMNic{vmNic1, vmNic2}
			elfMachine.Spec.Network.Devices = append(elfMachine.Spec.Network.Devices, capev1.NetworkDeviceSpec{NetworkType: capev1.NetworkTypeIPV4})
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.AddingVMNetworkDeviceReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext, vmNics)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the vm network device 1 mac address"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.WaitingForNetworkMacAddressReason}})
		})

		It("should wait for node exists", func() {
			nic1 := fake.NewVMNic()
			nic2 := fake.NewVMNic()
			elfMachine.Spec.Network.Devices = append(elfMachine.Spec.Network.Devices, capev1.NetworkDeviceSpec{NetworkType: capev1.NetworkTypeIPV4})
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.AddingVMNetworkDeviceReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext, []*models.VMNic{nic1, nic2})
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for node exists for host agent job"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.SettingVMNetworkDeviceConfigReason}})
		})

		It("should create agent job to set network device config", func() {
			nic1 := fake.NewVMNic()
			nic2 := fake.NewVMNic()
			macTypes := []hostagent.MacType{{Mac: *nic1.MacAddress}, {Mac: *nic2.MacAddress}}
			elfMachine.Spec.Network.Devices = append(elfMachine.Spec.Network.Devices, capev1.NetworkDeviceSpec{NetworkType: capev1.NetworkTypeIPV4})
			machine.Status.NodeRef = capiv1.MachineNodeReference{Name: "test-node"}
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.AddingVMNetworkDeviceReason, capiv1beta1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret, hostAgentJobsConfig)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext, []*models.VMNic{nic1, nic2})
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for job to complete"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.SettingVMNetworkDeviceConfigReason}})
			Eventually(func(g Gomega) {
				agentJob, err := hostagent.GetHostJob(ctx, testEnv.Client, elfMachine.Namespace, hostagent.GetSetNetworkDeviceConfigJobName(elfMachine, macTypes))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(agentJob.Name).To(Equal(hostagent.GetSetNetworkDeviceConfigJobName(elfMachine, macTypes)))
			}, timeout).Should(Succeed())
		})

		It("should retry when job failed", func() {
			nic1 := fake.NewVMNic()
			nic2 := fake.NewVMNic()
			macTypes := []hostagent.MacType{{Mac: *nic1.MacAddress}, {Mac: *nic2.MacAddress}}
			elfMachine.Spec.Network.Devices = append(elfMachine.Spec.Network.Devices, capev1.NetworkDeviceSpec{NetworkType: capev1.NetworkTypeIPV4})
			machine.Status.NodeRef = capiv1.MachineNodeReference{Name: "test-node"}
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.AddingVMNetworkDeviceReason, capiv1beta1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateSetNetworkDeviceConfigJob(elfMachine, "", macTypes, "test-node")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret, hostAgentJobsConfig)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext, []*models.VMNic{nic1, nic2})
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for HostJob done"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.SettingVMNetworkDeviceConfigReason}})

			logBuffer.Reset()
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseFailed
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.setVMNetworkDeviceConfig(ctx, machineContext, []*models.VMNic{nic1, nic2})
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("HostJob failed, will try again after three minutes"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityWarning, capev1.SettingVMNetworkDeviceConfigFailedReason}})

			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource = agentJob.DeepCopy()
			agentJob.Status.LastExecutionTime = &metav1.Time{Time: time.Now().Add(-3 * time.Minute).UTC()}
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.setVMNetworkDeviceConfig(ctx, machineContext, []*models.VMNic{nic1, nic2})
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err := testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout).Should(Succeed())
		})

		It("should record job succeeded", func() {
			nic1 := fake.NewVMNic()
			nic2 := fake.NewVMNic()
			macTypes := []hostagent.MacType{{Mac: *nic1.MacAddress}, {Mac: *nic2.MacAddress}}
			elfMachine.Spec.Network.Devices = append(elfMachine.Spec.Network.Devices, capev1.NetworkDeviceSpec{NetworkType: capev1.NetworkTypeIPV4})
			machine.Status.NodeRef = capiv1.MachineNodeReference{Name: "test-node"}
			conditionsv1beta1.MarkFalse(elfMachine, capev1.ResourcesHotUpdatedCondition, capev1.AddingVMNetworkDeviceReason, capiv1beta1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateSetNetworkDeviceConfigJob(elfMachine, "", macTypes, "test-node")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseSucceeded
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext, []*models.VMNic{nic1, nic2})
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("HostJob succeeded"))
			expectConditions(elfMachine, []conditionAssertion{{capev1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, capiv1beta1.ConditionSeverityInfo, capev1.SettingVMNetworkDeviceConfigReason}})
		})
	})
})

func newHostAgentJobsConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostagent.HostAgentJobsConfigName,
			Namespace: constants.NamespaceCape,
		},
		Data: map[string]string{
			string(hostagent.HostAgentJobTypeExpandRootPartition):    "",
			string(hostagent.HostAgentJobTypeRestartKubelet):         "",
			string(hostagent.HostAgentJobTypeSetNetworkDeviceConfig): "",
		},
	}
}
