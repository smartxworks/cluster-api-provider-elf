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
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/constants"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/hostagent"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service/mock_services"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
	"github.com/smartxworks/cluster-api-provider-elf/test/helpers"
)

var _ = Describe("ElfMachineReconciler", func() {
	var (
		elfCluster          *infrav1.ElfCluster
		cluster             *clusterv1.Cluster
		elfMachine          *infrav1.ElfMachine
		machine             *clusterv1.Machine
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
		mockNewVMService = func(_ goctx.Context, _ infrav1.Tower, _ logr.Logger) (service.VMService, error) {
			return mockVMService, nil
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("reconcileVMResources", func() {
		It("should mark ResourcesHotUpdatedCondition to true", func() {
			agentJob := hostagent.GenerateExpandRootPartitionJob(elfMachine, "")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseSucceeded
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			kubeConfigSecret, err := helpers.NewKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)
			Expect(err).ShouldNot(HaveOccurred())
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskReason, clusterv1.ConditionSeverityInfo, "")
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
			expectConditions(elfMachine, []conditionAssertion{{conditionType: infrav1.ResourcesHotUpdatedCondition, status: corev1.ConditionTrue}})
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
			ok, err := reconciler.reconcieVMVolume(ctx, machineContext, vm, infrav1.VMProvisionedCondition)
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
			ok, err := reconciler.reconcieVMVolume(ctx, machineContext, vm, infrav1.VMProvisionedCondition)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})

		It("should expand the disk when size is not up to date", func() {
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskReason, clusterv1.ConditionSeverityInfo, "")
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
			ok, err := reconciler.reconcieVMVolume(ctx, machineContext, vm, infrav1.ResourcesHotUpdatedCondition)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the vm volume"))
			Expect(elfMachine.Status.TaskRef).To(Equal(*withTaskVMVolume.TaskID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingVMDiskReason}})
		})
	})

	Context("resizeVMVolume", func() {
		It("should save the conditionType first", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vmVolume := fake.NewVMVolume(elfMachine)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err := reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, infrav1.VMProvisionedCondition)
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingVMDiskReason}})

			vmVolume.EntityAsyncStatus = models.NewEntityAsyncStatus(models.EntityAsyncStatusUPDATING)
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskFailedReason, clusterv1.ConditionSeverityWarning, "")
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			err = reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, infrav1.ResourcesHotUpdatedCondition)
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for vm volume task done"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.ExpandingVMDiskFailedReason}})
		})

		It("should wait task done", func() {
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vmVolume := fake.NewVMVolume(elfMachine)
			task := fake.NewTowerTask("")
			withTaskVMVolume := fake.NewWithTaskVMVolume(vmVolume, task)
			mockVMService.EXPECT().ResizeVMVolume(*vmVolume.ID, int64(10)).Return(withTaskVMVolume, nil)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err := reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, infrav1.ResourcesHotUpdatedCondition)
			Expect(err).ToNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the vm volume"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingVMDiskReason}})

			mockVMService.EXPECT().ResizeVMVolume(*vmVolume.ID, int64(10)).Return(nil, unexpectedError)
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err = reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, infrav1.ResourcesHotUpdatedCondition)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to trigger expand size from"))
			Expect(elfMachine.Status.TaskRef).To(Equal(*withTaskVMVolume.TaskID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.ExpandingVMDiskFailedReason}})

			task = fake.NewTowerTask("")
			withTaskVMVolume = fake.NewWithTaskVMVolume(vmVolume, task)
			mockVMService.EXPECT().ResizeVMVolume(*vmVolume.ID, int64(10)).Return(withTaskVMVolume, nil)
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err = reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, infrav1.ResourcesHotUpdatedCondition)
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the vm volume"))
			Expect(elfMachine.Status.TaskRef).To(Equal(*withTaskVMVolume.TaskID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.ExpandingVMDiskFailedReason}})
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
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskReason, clusterv1.ConditionSeverityInfo, "")
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
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret, hostAgentJobsConfig)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for job to complete"))
			Expect(logBuffer.String()).To(ContainSubstring("expand-root-partition"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingRootPartitionReason}})
			var agentJob *agentv1.HostOperationJob
			Eventually(func() error {
				var err error
				agentJob, err = hostagent.GetHostJob(ctx, testEnv.Client, elfMachine.Namespace, hostagent.GetExpandRootPartitionJobName(elfMachine))
				return err
			}, timeout).Should(Succeed())
			Expect(agentJob.Name).To(Equal(hostagent.GetExpandRootPartitionJobName(elfMachine)))
		})

		It("should retry when job failed", func() {
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskReason, clusterv1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateExpandRootPartitionJob(elfMachine, "")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for HostJob done"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingRootPartitionReason}})

			logBuffer.Reset()
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseFailed
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("HostJob failed, will try again"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.ExpandingRootPartitionFailedReason}})

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
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskReason, clusterv1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateExpandRootPartitionJob(elfMachine, "")
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
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingRootPartitionReason}})
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
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMComputeResourcesReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for node exists for host agent job"))
			Expect(logBuffer.String()).To(ContainSubstring("restart-kubelet"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.RestartingKubeletReason}})
		})

		It("should create agent job to restart kubelet", func() {
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMComputeResourcesReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret, hostAgentJobsConfig)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for job to complete"))
			Expect(logBuffer.String()).To(ContainSubstring("restart-kubelet"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.RestartingKubeletReason}})
			var agentJob *agentv1.HostOperationJob
			Eventually(func() error {
				var err error
				agentJob, err = hostagent.GetHostJob(ctx, testEnv.Client, elfMachine.Namespace, hostagent.GetRestartKubeletJobName(elfMachine))
				return err
			}, timeout).Should(Succeed())
			Expect(agentJob.Name).To(Equal(hostagent.GetRestartKubeletJobName(elfMachine)))
		})

		It("should retry when job failed", func() {
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMComputeResourcesReason, clusterv1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateRestartKubeletJob(elfMachine, "")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for HostJob done"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.RestartingKubeletReason}})

			logBuffer.Reset()
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseFailed
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("HostJob failed, will try again after three minutes"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.RestartingKubeletFailedReason}})

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
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMComputeResourcesReason, clusterv1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateRestartKubeletJob(elfMachine, "")
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
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.RestartingKubeletReason}})
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
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingVMComputeResourcesReason}})
		})

		It("should wait task done", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			elfMachine.Spec.MemoryMiB += 1
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMComputeResourcesReason, clusterv1.ConditionSeverityInfo, "")
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
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingVMComputeResourcesReason}})

			logBuffer.Reset()
			inMemoryCache.Flush()
			mockVMService.EXPECT().UpdateVM(vm, elfMachine).Return(nil, unexpectedError)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err = reconciler.reconcileVMCPUAndMemory(ctx, machineCtx, vm)
			Expect(ok).To(BeFalse())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to trigger update CPU and memory for VM"))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.ExpandingVMComputeResourcesFailedReason}})

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
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.ExpandingVMComputeResourcesFailedReason}})
		})
	})

	Context("reconcieVMNetworkDevices", func() {
		BeforeEach(func() {
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, "", clusterv1.ConditionSeverityInfo, "")
			var err error
			kubeConfigSecret, err = helpers.NewKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should reconcie vm network devices", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(nil, nil)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcieVMNetworkDevices(ctx, machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.AddingVMNetworkDeviceReason}})

			logBuffer.Reset()
			vmNic := fake.NewVMNic()
			vmNic.IPAddress = ptr.To("")
			vmNics := []*models.VMNic{vmNic}
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(vmNics, nil)
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			agentJob := hostagent.GenerateSetNetworkDeviceConfigJob(elfMachine, "")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).To(Succeed())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseSucceeded
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err = reconciler.reconcieVMNetworkDevices(ctx, machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("waiting for the vm network device"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForNetworkAddressesReason}})

			logBuffer.Reset()
			vmNic = fake.NewVMNic()
			vmNics = []*models.VMNic{vmNic}
			mockVMService.EXPECT().GetVMNics(*vm.ID).Return(vmNics, nil)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err = reconciler.reconcieVMNetworkDevices(ctx, machineContext, vm)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("addVMNetworkDevices", func() {
		It("should add new vm network devices", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err := reconciler.addVMNetworkDevices(ctx, machineContext, vm, nil)
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.AddingVMNetworkDeviceReason}})

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
			err = reconciler.addVMNetworkDevices(ctx, machineContext, vm, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to trigger add new nics to vm"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.AddingVMNetworkDeviceFailedReason}})

			task := fake.NewTowerTask(models.TaskStatusEXECUTING)
			withTaskVM := fake.NewWithTaskVM(vm, task)
			mockVMService.EXPECT().GetVlan(elfMachine.Spec.Network.Devices[0].Vlan).Return(vlan, nil)
			mockVMService.EXPECT().AddVMNics(*vm.ID, []*models.VMNicParams{newNic}).Return(withTaskVM, nil)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err = reconciler.addVMNetworkDevices(ctx, machineContext, vm, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.AddingVMNetworkDeviceReason}})
		})
	})

	Context("setVMNetworkDeviceConfig", func() {
		BeforeEach(func() {
			var err error
			kubeConfigSecret, err = helpers.NewKubeConfigSecret(testEnv, cluster.Namespace, cluster.Name)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should not set network device config", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})

		It("should wait for node exists", func() {
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.AddingVMNetworkDeviceReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for node exists for host agent job"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.SettingVMNetworkDeviceConfigReason}})
		})

		It("should create agent job to set network device config", func() {
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.AddingVMNetworkDeviceReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret, hostAgentJobsConfig)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for job to complete"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.SettingVMNetworkDeviceConfigReason}})
			Eventually(func(g Gomega) {
				agentJob, err := hostagent.GetHostJob(ctx, testEnv.Client, elfMachine.Namespace, hostagent.GetSetNetworkDeviceConfigJobName(elfMachine, len(elfMachine.Spec.Network.Devices)))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(agentJob.Name).To(Equal(hostagent.GetSetNetworkDeviceConfigJobName(elfMachine, len(elfMachine.Spec.Network.Devices))))
			}, timeout).Should(Succeed())
		})

		It("should retry when job failed", func() {
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.AddingVMNetworkDeviceReason, clusterv1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateSetNetworkDeviceConfigJob(elfMachine, "")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for HostJob done"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.SettingVMNetworkDeviceConfigReason}})

			logBuffer.Reset()
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseFailed
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.setVMNetworkDeviceConfig(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("HostJob failed, will try again after three minutes"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.SettingVMNetworkDeviceConfigFailedReason}})

			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource = agentJob.DeepCopy()
			agentJob.Status.LastExecutionTime = &metav1.Time{Time: time.Now().Add(-3 * time.Minute).UTC()}
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.setVMNetworkDeviceConfig(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				err := testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout).Should(Succeed())
		})

		It("should record job succeeded", func() {
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.AddingVMNetworkDeviceReason, clusterv1.ConditionSeverityInfo, "")
			agentJob := hostagent.GenerateSetNetworkDeviceConfigJob(elfMachine, "")
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseSucceeded
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.setVMNetworkDeviceConfig(ctx, machineContext)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("HostJob succeeded"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.SettingVMNetworkDeviceConfigReason}})
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
