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
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	agentv1 "github.com/smartxworks/host-config-agent-api/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/hostagent"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/hostagent/tasks"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service/mock_services"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
	"github.com/smartxworks/cluster-api-provider-elf/test/helpers"
)

var _ = Describe("ElfMachineReconciler", func() {
	var (
		elfCluster       *infrav1.ElfCluster
		cluster          *clusterv1.Cluster
		elfMachine       *infrav1.ElfMachine
		machine          *clusterv1.Machine
		secret           *corev1.Secret
		kubeConfigSecret *corev1.Secret
		logBuffer        *bytes.Buffer
		mockCtrl         *gomock.Controller
		mockVMService    *mock_services.MockVMService
		mockNewVMService service.NewVMServiceFunc
	)

	_, err := testEnv.CreateNamespace(goctx.Background(), "sks-system")
	Expect(err).NotTo(HaveOccurred())

	BeforeEach(func() {
		logBuffer = new(bytes.Buffer)
		klog.SetOutput(logBuffer)

		elfCluster, cluster, elfMachine, machine, secret = fake.NewClusterAndMachineObjects()

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
		It("should reconcile when WaitingForResourcesHotUpdateReason is not empty", func() {
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.WaitingForResourcesHotUpdateReason, clusterv1.ConditionSeverityInfo, "xx")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMResources(ctx, machineContext, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for hot updating resources"))
		})

		It("should mark ResourcesHotUpdatedCondition to true", func() {
			agentJob := newExpandRootPartitionJob(elfMachine)
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
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMResources(ctx, machineContext, vm)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{{conditionType: infrav1.ResourcesHotUpdatedCondition, status: corev1.ConditionTrue}})
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
			mockVMService.EXPECT().ResizeVMVolume(*vmVolume.ID, int64(10)).Return(nil, unexpectedError)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err := reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, infrav1.ResourcesHotUpdatedCondition)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to trigger expand size from"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.ExpandingVMDiskFailedReason}})

			task := fake.NewTowerTask("")
			withTaskVMVolume := fake.NewWithTaskVMVolume(vmVolume, task)
			mockVMService.EXPECT().ResizeVMVolume(*vmVolume.ID, int64(10)).Return(withTaskVMVolume, nil)
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			err = reconciler.resizeVMVolume(ctx, machineContext, vmVolume, 10, infrav1.ResourcesHotUpdatedCondition)
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the vm volume"))
			Expect(elfMachine.Status.TaskRef).To(Equal(*withTaskVMVolume.TaskID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingVMDiskReason}})
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
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for node exists for host agent expand vm root partition"))
		})

		It("should create agent job to expand root partition", func() {
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for expanding root partition"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingRootPartitionReason}})
			var agentJob *agentv1.HostOperationJob
			Eventually(func() error {
				var err error
				agentJob, err = hostagent.GetHostJob(ctx, testEnv.Client, elfMachine.Namespace, hostagent.GetExpandRootPartitionJobName(elfMachine))
				return err
			}, timeout).Should(BeNil())
			Expect(agentJob.Name).To(Equal(hostagent.GetExpandRootPartitionJobName(elfMachine)))
		})

		It("should retry when job failed", func() {
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMDiskReason, clusterv1.ConditionSeverityInfo, "")
			agentJob := newExpandRootPartitionJob(elfMachine)
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for expanding root partition job done"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingRootPartitionReason}})

			logBuffer.Reset()
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseFailed
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.expandVMRootPartition(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Expand root partition failed, will try again"))
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
			agentJob := newExpandRootPartitionJob(elfMachine)
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
			Expect(logBuffer.String()).To(ContainSubstring("Expand root partition to root succeeded"))
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
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMResourcesReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for node exists for host agent expand vm root partition"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.RestartingKubeletReason}})
		})

		It("should create agent job to restart kubelet", func() {
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMResourcesReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for resting kubelet to expanding CPU and memory"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.RestartingKubeletReason}})
			var agentJob *agentv1.HostOperationJob
			Eventually(func() error {
				var err error
				agentJob, err = hostagent.GetHostJob(ctx, testEnv.Client, elfMachine.Namespace, hostagent.GetRestartKubeletJobName(elfMachine))
				return err
			}, timeout).Should(BeNil())
			Expect(agentJob.Name).To(Equal(hostagent.GetRestartKubeletJobName(elfMachine)))
		})

		It("should retry when job failed", func() {
			machine.Status.NodeInfo = &corev1.NodeSystemInfo{}
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMResourcesReason, clusterv1.ConditionSeverityInfo, "")
			agentJob := newRestartKubelet(elfMachine)
			Expect(testEnv.CreateAndWait(ctx, agentJob)).NotTo(HaveOccurred())
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kubeConfigSecret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for expanding CPU and memory job done"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.RestartingKubeletReason}})

			logBuffer.Reset()
			Expect(testEnv.Get(ctx, client.ObjectKey{Namespace: agentJob.Namespace, Name: agentJob.Name}, agentJob)).NotTo(HaveOccurred())
			agentJobPatchSource := agentJob.DeepCopy()
			agentJob.Status.Phase = agentv1.PhaseFailed
			Expect(testEnv.PatchAndWait(ctx, agentJob, agentJobPatchSource)).To(Succeed())
			ok, err = reconciler.restartKubelet(ctx, machineContext)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Expand CPU and memory failed, will try again after three minutes"))
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
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMResourcesReason, clusterv1.ConditionSeverityInfo, "")
			agentJob := newRestartKubelet(elfMachine)
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
			Expect(logBuffer.String()).To(ContainSubstring("Expand CPU and memory succeeded"))
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
			Expect(elfMachine.Status.Resources.Memory.String()).To(Equal(fmt.Sprintf("%dMi", service.ByteToMiB(*vm.Memory))))
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
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingVMResourcesReason}})
		})

		It("should wait task done", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			elfMachine.Spec.MemoryMiB += 1
			conditions.MarkFalse(elfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.ExpandingVMResourcesReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineCtx := newMachineContext(elfCluster, cluster, elfMachine, machine, mockVMService)
			mockVMService.EXPECT().UpdateVM(vm, elfMachine).Return(nil, unexpectedError)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileVMCPUAndMemory(ctx, machineCtx, vm)
			Expect(ok).To(BeFalse())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to trigger update CPU and memory for VM"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.ExpandingVMResourcesFailedReason}})

			logBuffer.Reset()
			inMemoryCache.Flush()
			task := fake.NewTowerTask("")
			withTaskVM := fake.NewWithTaskVM(vm, task)
			mockVMService.EXPECT().UpdateVM(vm, elfMachine).Return(withTaskVM, nil)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx, NewVMService: mockNewVMService}
			ok, err = reconciler.reconcileVMCPUAndMemory(ctx, machineCtx, vm)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for the VM to be updated CPU and memory"))
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.ExpandingVMResourcesReason}})
		})
	})
})

func newExpandRootPartitionJob(elfMachine *infrav1.ElfMachine) *agentv1.HostOperationJob {
	return &agentv1.HostOperationJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostagent.GetExpandRootPartitionJobName(elfMachine),
			Namespace: "sks-system",
		},
		Spec: agentv1.HostOperationJobSpec{
			NodeName: elfMachine.Name,
			Operation: agentv1.Operation{
				Ansible: &agentv1.Ansible{
					LocalPlaybookText: &agentv1.YAMLText{
						Inline: tasks.ExpandRootPartitionTask,
					},
				},
			},
		},
	}
}

func newRestartKubelet(elfMachine *infrav1.ElfMachine) *agentv1.HostOperationJob {
	return &agentv1.HostOperationJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostagent.GetRestartKubeletJobName(elfMachine),
			Namespace: "sks-system",
		},
		Spec: agentv1.HostOperationJobSpec{
			NodeName: elfMachine.Name,
			Operation: agentv1.Operation{
				Ansible: &agentv1.Ansible{
					LocalPlaybookText: &agentv1.YAMLText{
						Inline: tasks.RestartKubeletTask,
					},
				},
			},
		},
	}
}
