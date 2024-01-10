/*
Copyright 2023.

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

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service/mock_services"
	labelsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/labels"
	machineutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/machine"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
	"github.com/smartxworks/cluster-api-provider-elf/test/helpers"
)

var _ = Describe("ElfMachineReconciler-GPU", func() {
	var (
		elfCluster       *infrav1.ElfCluster
		cluster          *clusterv1.Cluster
		elfMachine       *infrav1.ElfMachine
		machine          *clusterv1.Machine
		md               *clusterv1.MachineDeployment
		secret           *corev1.Secret
		logBuffer        *bytes.Buffer
		mockCtrl         *gomock.Controller
		mockVMService    *mock_services.MockVMService
		mockNewVMService service.NewVMServiceFunc
	)

	ctx := goctx.Background()

	gpuModel := "A16"
	vGPUType := "V100"
	unexpectedError := errors.New("unexpected error")

	BeforeEach(func() {
		logBuffer = new(bytes.Buffer)
		klog.SetOutput(logBuffer)

		elfCluster, cluster, elfMachine, machine, secret = fake.NewClusterAndMachineObjects()
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

	Context("selectHostAndGPUsForVM", func() {

		BeforeEach(func() {
			elfMachine.Spec.GPUDevices = append(elfMachine.Spec.GPUDevices, infrav1.GPUPassthroughDeviceSpec{Model: gpuModel, Count: 1})
		})

		It("should not handle ElfMachine without GPU", func() {
			elfMachine.Spec.GPUDevices = nil
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			host, gpus, err := reconciler.selectHostAndGPUsForVM(machineContext, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(*host).To(BeEmpty())
			Expect(gpus).To(BeEmpty())
		})

		It("should check and use locked GPUs", func() {
			host := fake.NewTowerHost()
			gpuVMInfo := fake.NewTowerGPUVMInfo()
			gpuVMInfo.Host = &models.NestedHost{ID: host.ID}
			gpuVMInfo.Model = service.TowerString(gpuModel)
			gpuIDs := []string{*gpuVMInfo.ID}
			gpuVMInfos := service.NewGPUVMInfos(gpuVMInfo)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
			mockVMService.EXPECT().GetGPUDevicesAllocationInfoByHostIDs([]string{*host.ID}, models.GpuDeviceUsagePASSTHROUGH).Return(gpuVMInfos, nil)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			hostID, gpus, err := reconciler.selectHostAndGPUsForVM(machineContext, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(*hostID).To(Equal(*host.ID))
			Expect(gpus).To(HaveLen(1))
			Expect(gpus[0].ID).To(Equal(*gpuVMInfo.ID))
			Expect(gpus[0].AllocatedCount).To(Equal(int32(1)))

			mockVMService.EXPECT().GetGPUDevicesAllocationInfoByIDs([]string{*gpuVMInfo.ID}).Return(gpuVMInfos, nil)
			hostID, gpus, err = reconciler.selectHostAndGPUsForVM(machineContext, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(*hostID).To(Equal(*host.ID))
			Expect(gpus).To(HaveLen(1))
			Expect(gpus[0].ID).To(Equal(*gpuVMInfo.ID))
			Expect(gpus[0].AllocatedCount).To(Equal(int32(1)))
			Expect(logBuffer.String()).To(ContainSubstring("Found locked VM GPU devices"))

			logBuffer.Reset()
			removeGPUVMInfosCache(gpuIDs)
			gpuVMInfo.Vms = []*models.GpuVMDetail{{ID: service.TowerString("id"), Name: service.TowerString("vm"), Status: models.NewVMStatus(models.VMStatusRUNNING)}}
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(nil, nil)
			mockVMService.EXPECT().GetGPUDevicesAllocationInfoByIDs([]string{*gpuVMInfo.ID}).Return(gpuVMInfos, nil)
			hostID, gpus, err = reconciler.selectHostAndGPUsForVM(machineContext, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(hostID).To(BeNil())
			Expect(gpus).To(BeEmpty())
			Expect(logBuffer.String()).To(ContainSubstring("Locked VM GPU devices are invalid"))
			Expect(logBuffer.String()).To(ContainSubstring("No host with the required GPU devices for the virtual machine"))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForAvailableHostWithEnoughGPUsReason}})
			Expect(getGPUDevicesLockedByVM(elfCluster.Spec.Cluster, elfMachine.Name)).To(BeNil())
		})

		It("should prioritize the preferred host", func() {
			host := fake.NewTowerHost()
			gpuVMInfo := fake.NewTowerGPUVMInfo()
			gpuVMInfo.Host = &models.NestedHost{ID: host.ID}
			preferredHost := fake.NewTowerHost()
			preferredGPUVMInfo := fake.NewTowerGPUVMInfo()
			preferredGPUVMInfo.Host = &models.NestedHost{ID: preferredHost.ID}
			gpuVMInfos := service.NewGPUVMInfos(gpuVMInfo, preferredGPUVMInfo)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host, preferredHost), nil)
			mockVMService.EXPECT().GetGPUDevicesAllocationInfoByHostIDs(gomock.InAnyOrder([]string{*host.ID, *preferredHost.ID}), models.GpuDeviceUsagePASSTHROUGH).Return(gpuVMInfos, nil)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			hostID, gpus, err := reconciler.selectHostAndGPUsForVM(machineContext, *preferredHost.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(*hostID).To(Equal(*preferredHost.ID))
			Expect(gpus).To(HaveLen(1))
			Expect(gpus[0].ID).To(Equal(*preferredGPUVMInfo.ID))
			Expect(gpus[0].AllocatedCount).To(Equal(int32(1)))
		})

		It("select vGPUs", func() {
			host := fake.NewTowerHost()
			gpuVMInfo1 := fake.NewTowerVGPUVMInfo(1)
			gpuVMInfo1.Vms = []*models.GpuVMDetail{}
			gpuVMInfo1.Host = &models.NestedHost{ID: host.ID}
			gpuVMInfo2 := fake.NewTowerVGPUVMInfo(3)
			gpuVMInfo2.AvailableVgpusNum = service.TowerInt32(2)
			gpuVMInfo2.Vms = []*models.GpuVMDetail{{}}
			gpuVMInfo2.Host = &models.NestedHost{ID: host.ID}
			gpuVMInfos := service.NewGPUVMInfos(gpuVMInfo1, gpuVMInfo2)
			requiredVGPUDevice := infrav1.VGPUDeviceSpec{Type: vGPUType, Count: 3}
			elfMachine.Spec.GPUDevices = nil
			elfMachine.Spec.VGPUDevices = []infrav1.VGPUDeviceSpec{requiredVGPUDevice}
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(), nil)
			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			hostID, gpus, err := reconciler.selectHostAndGPUsForVM(machineContext, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(hostID).To(BeNil())
			Expect(gpus).To(BeEmpty())

			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
			mockVMService.EXPECT().GetGPUDevicesAllocationInfoByHostIDs([]string{*host.ID}, models.GpuDeviceUsageVGPU).Return(service.NewGPUVMInfos(), nil)
			hostID, gpus, err = reconciler.selectHostAndGPUsForVM(machineContext, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(hostID).To(BeNil())
			Expect(gpus).To(BeEmpty())

			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
			mockVMService.EXPECT().GetGPUDevicesAllocationInfoByHostIDs([]string{*host.ID}, models.GpuDeviceUsageVGPU).Return(gpuVMInfos, nil)
			hostID, gpus, err = reconciler.selectHostAndGPUsForVM(machineContext, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(hostID).NotTo(BeNil())
			Expect(*hostID).To(Equal(*host.ID))
			Expect(gpus).To(HaveLen(2))
			Expect(gpus).To(ContainElements(&service.GPUDeviceInfo{
				ID:             *gpuVMInfo1.ID,
				AllocatedCount: 1,
				AvailableCount: 1,
			}, &service.GPUDeviceInfo{
				ID:             *gpuVMInfo2.ID,
				AllocatedCount: 2,
				AvailableCount: 2,
			}))
			lockedGPUs := getGPUDevicesLockedByVM(elfCluster.Spec.Cluster, elfMachine.Name)
			Expect(lockedGPUs.GPUDevices).To(HaveLen(2))
			Expect(lockedGPUs.GPUDevices).To(ContainElements(lockedGPUDevice{ID: *gpuVMInfo1.ID, Count: 1}, lockedGPUDevice{ID: *gpuVMInfo2.ID, Count: 2}))

			unlockGPUDevicesLockedByVM(elfCluster.Spec.Cluster, elfMachine.Name)
			lockGPUDevicesForVM(elfCluster.Spec.Cluster, fake.UUID(), *host.ID, []*service.GPUDeviceInfo{{
				ID:             *gpuVMInfo1.ID,
				AllocatedCount: 1,
				AvailableCount: 1,
			}})
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
			mockVMService.EXPECT().GetGPUDevicesAllocationInfoByHostIDs([]string{*host.ID}, models.GpuDeviceUsageVGPU).Return(gpuVMInfos, nil)
			hostID, gpus, err = reconciler.selectHostAndGPUsForVM(machineContext, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(hostID).To(BeNil())
			Expect(gpus).To(BeEmpty())
		})
	})

	Context("reconcileGPUDevices", func() {
		BeforeEach(func() {
			elfMachine.Spec.GPUDevices = append(elfMachine.Spec.GPUDevices, infrav1.GPUPassthroughDeviceSpec{Model: gpuModel, Count: 1})
		})

		It("should not handle ElfMachine without GPU", func() {
			elfMachine.Spec.GPUDevices = nil
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileGPUDevices(machineContext, vm)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
		})

		It("should set .Status.GPUDevices when the virtual machine is not powered off", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.Status = models.NewVMStatus(models.VMStatusRUNNING)
			vm.GpuDevices = []*models.NestedGpuDevice{{ID: service.TowerString(fake.ID()), Name: service.TowerString(fake.ID())}}
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileGPUDevices(machineContext, vm)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(elfMachine.Status.GPUDevices).To(Equal([]infrav1.GPUStatus{{GPUID: *vm.GpuDevices[0].ID, Name: *vm.GpuDevices[0].Name}}))
		})

		It("should add GPU devices to VM when the VM without GPU devices", func() {
			host := fake.NewTowerHost()
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.Host = &models.NestedHost{ID: host.ID}
			vm.Status = models.NewVMStatus(models.VMStatusSTOPPED)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(nil, unexpectedError)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileGPUDevices(machineContext, vm)
			Expect(err).To(HaveOccurred())
			Expect(ok).To(BeFalse())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForAvailableHostWithEnoughGPUsReason}})
		})

		It("should remove GPU devices to VM when detect host are not sufficient", func() {
			host := fake.NewTowerHost()
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.Host = &models.NestedHost{ID: host.ID}
			vm.Status = models.NewVMStatus(models.VMStatusSTOPPED)
			vm.GpuDevices = []*models.NestedGpuDevice{{ID: service.TowerString(fake.ID()), Name: service.TowerString(gpuModel)}}
			conditions.MarkFalse(elfMachine, infrav1.VMProvisionedCondition, infrav1.TaskFailureReason, clusterv1.ConditionSeverityInfo, service.GPUAssignFailed)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().RemoveGPUDevices(elfMachine.Status.VMRef, gomock.Len(1)).Return(nil, unexpectedError)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileGPUDevices(machineContext, vm)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(unexpectedError.Error()))
			Expect(ok).To(BeFalse())
			Expect(logBuffer.String()).To(ContainSubstring("GPU devices of the host are not sufficient and the virtual machine cannot be started"))
		})

		It("should check if GPU devices can be used for VM", func() {
			host := fake.NewTowerHost()
			gpuVMInfo := fake.NewTowerGPUVMInfo()
			gpuVMInfo.Host = &models.NestedHost{ID: host.ID}
			gpuVMInfo.Vms = []*models.GpuVMDetail{{ID: service.TowerString("id"), Name: service.TowerString("vm"), Status: models.NewVMStatus(models.VMStatusRUNNING)}}
			gpuVMInfos := service.NewGPUVMInfos(gpuVMInfo)
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.Host = &models.NestedHost{ID: host.ID}
			vm.Status = models.NewVMStatus(models.VMStatusSTOPPED)
			vm.GpuDevices = []*models.NestedGpuDevice{{ID: gpuVMInfo.ID, Name: gpuVMInfo.Model}}
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetGPUDevicesAllocationInfoByIDs([]string{*gpuVMInfo.ID}).Times(2).Return(gpuVMInfos, nil)
			mockVMService.EXPECT().RemoveGPUDevices(elfMachine.Status.VMRef, gomock.Len(1)).Return(nil, unexpectedError)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileGPUDevices(machineContext, vm)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(unexpectedError.Error()))
			Expect(ok).To(BeFalse())
			Expect(logBuffer.String()).To(ContainSubstring("GPU devices of VM are already in use, so remove and reallocate"))

			removeGPUVMInfosCache([]string{*gpuVMInfo.ID})
			gpuVMInfo.Vms = []*models.GpuVMDetail{{ID: vm.ID, Name: vm.Name, Status: models.NewVMStatus(models.VMStatusRUNNING)}}
			ok, err = reconciler.reconcileGPUDevices(machineContext, vm)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
		})
	})

	Context("addGPUDevicesForVM", func() {
		BeforeEach(func() {
			elfMachine.Spec.GPUDevices = append(elfMachine.Spec.GPUDevices, infrav1.GPUPassthroughDeviceSpec{Model: gpuModel, Count: 1})
		})

		It("should migrate VM when current host does not have enough GPU devices", func() {
			host := fake.NewTowerHost()
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.Host = &models.NestedHost{ID: service.TowerString(fake.ID())}
			elfMachine.Status.VMRef = *vm.LocalID
			gpuVMInfo := fake.NewTowerGPUVMInfo()
			gpuVMInfo.Host = &models.NestedHost{ID: host.ID}
			gpuVMInfos := service.NewGPUVMInfos(gpuVMInfo)
			task := fake.NewTowerTask()
			withTaskVM := fake.NewWithTaskVM(vm, task)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Times(2).Return(service.NewHosts(host), nil)
			mockVMService.EXPECT().GetGPUDevicesAllocationInfoByHostIDs([]string{*host.ID}, models.GpuDeviceUsagePASSTHROUGH).Times(2).Return(gpuVMInfos, nil)
			mockVMService.EXPECT().Migrate(*vm.ID, *host.ID).Return(withTaskVM, nil)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.addGPUDevicesForVM(machineContext, vm)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			Expect(logBuffer.String()).To(ContainSubstring("The current host does not have enough GPU devices"))

			elfMachine.Status.TaskRef = ""
			unlockGPUDevicesLockedByVM(elfCluster.Spec.Cluster, elfMachine.Name)
			mockVMService.EXPECT().Migrate(*vm.ID, *host.ID).Return(nil, unexpectedError)
			ok, err = reconciler.addGPUDevicesForVM(machineContext, vm)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(unexpectedError.Error()))
			Expect(ok).To(BeFalse())
			Expect(elfMachine.Status.TaskRef).To(BeEmpty())
		})

		It("should add GPU devices to VM", func() {
			host := fake.NewTowerHost()
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.Host = &models.NestedHost{ID: host.ID}
			elfMachine.Status.VMRef = *vm.LocalID
			gpuVMInfo := fake.NewTowerGPUVMInfo()
			gpuVMInfo.Host = &models.NestedHost{ID: host.ID}
			gpuVMInfos := service.NewGPUVMInfos(gpuVMInfo)
			task := fake.NewTowerTask()
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Times(2).Return(service.NewHosts(host), nil)
			mockVMService.EXPECT().GetGPUDevicesAllocationInfoByHostIDs([]string{*host.ID}, models.GpuDeviceUsagePASSTHROUGH).Times(2).Return(gpuVMInfos, nil)
			mockVMService.EXPECT().AddGPUDevices(elfMachine.Status.VMRef, gomock.Any()).Return(task, nil)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.addGPUDevicesForVM(machineContext, vm)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.UpdatingReason}})

			elfMachine.Status.TaskRef = ""
			unlockGPUDevicesLockedByVM(elfCluster.Spec.Cluster, elfMachine.Name)
			mockVMService.EXPECT().AddGPUDevices(elfMachine.Status.VMRef, gomock.Any()).Return(task, unexpectedError)
			ok, err = reconciler.addGPUDevicesForVM(machineContext, vm)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(unexpectedError.Error()))
			Expect(ok).To(BeFalse())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.AttachingGPUFailedReason}})
			Expect(elfMachine.Status.TaskRef).To(BeEmpty())
		})
	})

	Context("removeVMGPUDevices", func() {
		It("should remove GPU devices of VM", func() {
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			elfMachine.Status.VMRef = *vm.LocalID
			vm.GpuDevices = []*models.NestedGpuDevice{{ID: service.TowerString(fake.ID()), Name: service.TowerString("A16")}}
			task := fake.NewTowerTask()
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().RemoveGPUDevices(elfMachine.Status.VMRef, gomock.Len(1)).Return(nil, unexpectedError)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			err := reconciler.removeVMGPUDevices(machineContext, vm)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(unexpectedError.Error()))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.DetachingGPUFailedReason}})
			Expect(elfMachine.Status.TaskRef).To(BeEmpty())

			mockVMService.EXPECT().RemoveGPUDevices(elfMachine.Status.VMRef, gomock.Len(1)).Return(task, nil)
			err = reconciler.removeVMGPUDevices(machineContext, vm)
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.UpdatingReason}})
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))

			gpuID := fake.ID()
			vmGPUInfo := &models.VMGpuInfo{GpuDevices: []*models.VMGpuDetail{{ID: service.TowerString(gpuID), VgpuInstanceOnVMNum: service.TowerInt32(2)}}}
			elfMachine.Spec.GPUDevices = nil
			elfMachine.Spec.VGPUDevices = []infrav1.VGPUDeviceSpec{{Type: vGPUType, Count: 2}}
			conditions.MarkFalse(elfMachine, infrav1.VMProvisionedCondition, infrav1.TaskFailureReason, clusterv1.ConditionSeverityInfo, service.VGPUInsufficientError)
			ctrlContext = newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetVMGPUAllocationInfo(*vm.ID).Return(vmGPUInfo, nil)
			mockVMService.EXPECT().RemoveGPUDevices(elfMachine.Status.VMRef, []*models.VMGpuOperationParams{{GpuID: service.TowerString(gpuID), Amount: service.TowerInt32(2)}}).Return(nil, unexpectedError)
			err = reconciler.removeVMGPUDevices(machineContext, vm)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(unexpectedError.Error()))
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.TaskFailureReason}})
		})
	})

	Context("Reconcile GPU Node", func() {
		var node *corev1.Node

		BeforeEach(func() {
			elfMachine.Spec.GPUDevices = append(elfMachine.Spec.GPUDevices, infrav1.GPUPassthroughDeviceSpec{Model: gpuModel, Count: 1})
		})

		AfterEach(func() {
			Expect(testEnv.Delete(ctx, node)).To(Succeed())
		})

		It("should set clusterAutoscaler GPU label for node", func() {
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
					node.Labels[infrav1.NodeGroupLabel] == machineutil.GetNodeGroupName(machine) &&
					node.Labels[labelsutil.ClusterAutoscalerCAPIGPULabel] == labelsutil.ConvertToLabelValue(gpuModel)
			}, timeout).Should(BeTrue())
		})
	})

	It("checkGPUsCanBeUsedForVM", func() {
		host := fake.NewTowerHost()
		gpuVMInfo := fake.NewTowerGPUVMInfo()
		gpuVMInfo.Host = &models.NestedHost{ID: host.ID}
		gpuIDs := []string{*gpuVMInfo.ID}
		elfMachine.Spec.GPUDevices = append(elfMachine.Spec.GPUDevices, infrav1.GPUPassthroughDeviceSpec{Model: "A16", Count: 1})
		ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
		fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

		mockVMService.EXPECT().GetGPUDevicesAllocationInfoByIDs(gpuIDs).Return(service.NewGPUVMInfos(), nil)
		machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
		reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
		ok, err := reconciler.checkGPUsCanBeUsedForVM(machineContext, gpuIDs)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())

		mockVMService.EXPECT().GetGPUDevicesAllocationInfoByIDs(gpuIDs).Return(service.NewGPUVMInfos(gpuVMInfo), nil)
		ok, err = reconciler.checkGPUsCanBeUsedForVM(machineContext, gpuIDs)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		// Use cache
		ok, err = reconciler.checkGPUsCanBeUsedForVM(machineContext, gpuIDs)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		removeGPUVMInfosCache(gpuIDs)
		gpuVMInfo.Vms = []*models.GpuVMDetail{{ID: service.TowerString("vm1"), Name: service.TowerString("vm1"), Status: models.NewVMStatus(models.VMStatusRUNNING)}}
		mockVMService.EXPECT().GetGPUDevicesAllocationInfoByIDs(gpuIDs).Return(service.NewGPUVMInfos(gpuVMInfo), nil)
		ok, err = reconciler.checkGPUsCanBeUsedForVM(machineContext, gpuIDs)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
	})

	It("selectGPUDevicesForVM", func() {
		model := "A16"
		host := &models.NestedHost{ID: service.TowerString("host")}
		gpuVMInfo1 := fake.NewTowerGPUVMInfo()
		gpuVMInfo1.Host = host
		gpuVMInfo2 := fake.NewTowerGPUVMInfo()
		gpuVMInfo2.Host = host

		gpus := selectGPUDevicesForVM(service.NewGPUVMInfos(), []infrav1.GPUPassthroughDeviceSpec{{Model: model, Count: 1}})
		Expect(gpus).To(BeEmpty())

		gpus = selectGPUDevicesForVM(service.NewGPUVMInfos(gpuVMInfo1), []infrav1.GPUPassthroughDeviceSpec{{Model: model, Count: 1}})
		Expect(gpus).To(Equal([]*service.GPUDeviceInfo{
			{ID: *gpuVMInfo1.ID, AllocatedCount: 1, AvailableCount: 1},
		}))

		gpus = selectGPUDevicesForVM(service.NewGPUVMInfos(gpuVMInfo1), []infrav1.GPUPassthroughDeviceSpec{{Model: model, Count: 2}})
		Expect(gpus).To(BeEmpty())

		gpus = selectGPUDevicesForVM(service.NewGPUVMInfos(gpuVMInfo1, gpuVMInfo2), []infrav1.GPUPassthroughDeviceSpec{{Model: model, Count: 2}})
		Expect(gpus).To(ContainElements(&service.GPUDeviceInfo{
			ID: *gpuVMInfo1.ID, AllocatedCount: 1, AvailableCount: 1,
		}, &service.GPUDeviceInfo{
			ID: *gpuVMInfo2.ID, AllocatedCount: 1, AvailableCount: 1,
		}))
	})

	It("selectVGPUDevicesForVM", func() {
		host := &models.NestedHost{ID: service.TowerString("host")}
		vGPUVMInfo1 := fake.NewTowerVGPUVMInfo(1)
		vGPUVMInfo1.Host = host
		vGPUVMInfo2 := fake.NewTowerVGPUVMInfo(2)
		vGPUVMInfo2.Host = host
		requiredVGPUDevice := infrav1.VGPUDeviceSpec{Type: *vGPUVMInfo1.UserVgpuTypeName, Count: 1}
		requiredVGPUDevices := []infrav1.VGPUDeviceSpec{requiredVGPUDevice}
		gpuVMInfos := service.NewGPUVMInfos()
		gpus := selectVGPUDevicesForVM(gpuVMInfos, requiredVGPUDevices)
		Expect(gpus).To(BeEmpty())

		vGPUVMInfo1.AssignedVgpusNum = service.TowerInt32(1)
		vGPUVMInfo1.AvailableVgpusNum = service.TowerInt32(0)
		gpuVMInfos = service.NewGPUVMInfos(vGPUVMInfo1)
		gpus = selectVGPUDevicesForVM(gpuVMInfos, requiredVGPUDevices)
		Expect(gpus).To(BeEmpty())

		vGPUVMInfo1.AvailableVgpusNum = service.TowerInt32(1)
		gpuVMInfos = service.NewGPUVMInfos(vGPUVMInfo1)
		gpus = selectVGPUDevicesForVM(gpuVMInfos, requiredVGPUDevices)
		Expect(gpus).To(Equal([]*service.GPUDeviceInfo{{ID: *vGPUVMInfo1.ID, AllocatedCount: requiredVGPUDevice.Count, AvailableCount: *vGPUVMInfo1.AvailableVgpusNum}}))

		requiredVGPUDevice.Count = 3
		requiredVGPUDevices[0] = requiredVGPUDevice
		gpus = selectVGPUDevicesForVM(gpuVMInfos, requiredVGPUDevices)
		Expect(gpus).To(BeEmpty())

		vGPUVMInfo2.AvailableVgpusNum = service.TowerInt32(2)
		vGPUVMInfo2.AssignedVgpusNum = service.TowerInt32(1)
		gpuVMInfos.Insert(vGPUVMInfo2)
		gpus = selectVGPUDevicesForVM(gpuVMInfos, requiredVGPUDevices)
		Expect(gpus).To(ContainElements(&service.GPUDeviceInfo{
			ID: *vGPUVMInfo1.ID, AllocatedCount: 1, AvailableCount: *vGPUVMInfo1.AvailableVgpusNum,
		}, &service.GPUDeviceInfo{
			ID: *vGPUVMInfo2.ID, AllocatedCount: 2, AvailableCount: *vGPUVMInfo2.AvailableVgpusNum,
		}))
		Expect(gpus[0].AllocatedCount + gpus[1].AllocatedCount).To(Equal(requiredVGPUDevice.Count))
	})
})
