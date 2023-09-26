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
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service/mock_services"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
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

	gpuModel := "A16"
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
			elfMachine.Spec.GPUDevices = append(elfMachine.Spec.GPUDevices, infrav1.GPUPassthroughDeviceSpec{GPUModel: gpuModel, Count: 1})
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
			gpu := fake.NewTowerGPU()
			gpu.Host = &models.NestedHost{ID: host.ID}
			gpu.Model = service.TowerString(gpuModel)
			gpuIDs := []string{*gpu.ID}
			gpusDevices := []*models.GpuDevice{gpu}
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
			mockVMService.EXPECT().FindGPUDevicesByHostIDs([]string{*host.ID}).Return(gpusDevices, nil)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			hostID, gpus, err := reconciler.selectHostAndGPUsForVM(machineContext, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(*hostID).To(Equal(*host.ID))
			Expect(gpus).To(Equal(gpusDevices))

			mockVMService.EXPECT().FindGPUDevicesByIDs(gpuIDs).Return(gpusDevices, nil)
			hostID, gpus, err = reconciler.selectHostAndGPUsForVM(machineContext, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(*hostID).To(Equal(*host.ID))
			Expect(gpus).To(Equal(gpusDevices))
			Expect(logBuffer.String()).To(ContainSubstring("Found locked VM GPU devices"))

			logBuffer.Reset()
			gpu.Vms = []*models.NestedVM{{ID: service.TowerString("id"), Name: service.TowerString("vm")}}
			mockVMService.EXPECT().FindGPUDevicesByIDs(gpuIDs).Return(gpusDevices, nil)
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host), nil)
			mockVMService.EXPECT().FindGPUDevicesByHostIDs([]string{*host.ID}).Return(gpusDevices, nil)
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
			gpu := fake.NewTowerGPU()
			gpu.Host = &models.NestedHost{ID: host.ID}
			gpu.Model = service.TowerString(gpuModel)
			preferredHost := fake.NewTowerHost()
			preferredGPU := fake.NewTowerGPU()
			preferredGPU.Host = &models.NestedHost{ID: preferredHost.ID}
			preferredGPU.Model = service.TowerString(gpuModel)
			gpusDevices := []*models.GpuDevice{gpu, preferredGPU}
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Return(service.NewHosts(host, preferredHost), nil)
			mockVMService.EXPECT().FindGPUDevicesByHostIDs([]string{*host.ID, *preferredHost.ID}).Return(gpusDevices, nil)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			hostID, gpus, err := reconciler.selectHostAndGPUsForVM(machineContext, *preferredHost.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(*hostID).To(Equal(*preferredHost.ID))
			Expect(gpus).To(Equal([]*models.GpuDevice{preferredGPU}))
		})
	})

	Context("reconcileGPUDevices", func() {
		BeforeEach(func() {
			elfMachine.Spec.GPUDevices = append(elfMachine.Spec.GPUDevices, infrav1.GPUPassthroughDeviceSpec{GPUModel: gpuModel, Count: 1})
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
			gpu := fake.NewTowerGPU()
			gpu.Host = &models.NestedHost{ID: host.ID}
			gpu.Model = service.TowerString(gpuModel)
			gpu.Vms = []*models.NestedVM{{ID: service.TowerString("id"), Name: service.TowerString("vm")}}
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.Host = &models.NestedHost{ID: host.ID}
			vm.Status = models.NewVMStatus(models.VMStatusSTOPPED)
			vm.GpuDevices = []*models.NestedGpuDevice{{ID: gpu.ID, Name: gpu.Model}}
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().FindGPUDevicesByIDs([]string{*gpu.ID}).Times(2).Return([]*models.GpuDevice{gpu}, nil)
			mockVMService.EXPECT().RemoveGPUDevices(elfMachine.Status.VMRef, gomock.Len(1)).Return(nil, unexpectedError)

			machineContext := newMachineContext(ctrlContext, elfCluster, cluster, elfMachine, machine, mockVMService)
			reconciler := &ElfMachineReconciler{ControllerContext: ctrlContext, NewVMService: mockNewVMService}
			ok, err := reconciler.reconcileGPUDevices(machineContext, vm)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(unexpectedError.Error()))
			Expect(ok).To(BeFalse())
			Expect(logBuffer.String()).To(ContainSubstring("GPU devices of VM are already in use, so remove and reallocate"))

			gpu.Vms = []*models.NestedVM{{ID: vm.ID, Name: vm.Name}}
			ok, err = reconciler.reconcileGPUDevices(machineContext, vm)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
		})
	})

	Context("addGPUDevicesForVM", func() {
		BeforeEach(func() {
			elfMachine.Spec.GPUDevices = append(elfMachine.Spec.GPUDevices, infrav1.GPUPassthroughDeviceSpec{GPUModel: gpuModel, Count: 1})
		})

		It("should migrate VM when current host does not have enough GPU devices", func() {
			host := fake.NewTowerHost()
			vm := fake.NewTowerVMFromElfMachine(elfMachine)
			vm.Host = &models.NestedHost{ID: service.TowerString(fake.ID())}
			elfMachine.Status.VMRef = *vm.LocalID
			gpu := fake.NewTowerGPU()
			gpu.Host = &models.NestedHost{ID: host.ID}
			gpu.Model = service.TowerString(gpuModel)
			task := fake.NewTowerTask()
			withTaskVM := fake.NewWithTaskVM(vm, task)
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Times(2).Return(service.NewHosts(host), nil)
			mockVMService.EXPECT().FindGPUDevicesByHostIDs([]string{*host.ID}).Times(2).Return([]*models.GpuDevice{gpu}, nil)
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
			gpu := fake.NewTowerGPU()
			gpu.Host = &models.NestedHost{ID: host.ID}
			gpu.Model = service.TowerString(gpuModel)
			task := fake.NewTowerTask()
			ctrlContext := newCtrlContexts(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
			mockVMService.EXPECT().GetHostsByCluster(elfCluster.Spec.Cluster).Times(2).Return(service.NewHosts(host), nil)
			mockVMService.EXPECT().FindGPUDevicesByHostIDs([]string{*host.ID}).Times(2).Return([]*models.GpuDevice{gpu}, nil)
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
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.AddingGPUFailedReason}})
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
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityWarning, infrav1.RemovingGPUFailedReason}})
			Expect(elfMachine.Status.TaskRef).To(BeEmpty())

			mockVMService.EXPECT().RemoveGPUDevices(elfMachine.Status.VMRef, gomock.Len(1)).Return(task, nil)
			err = reconciler.removeVMGPUDevices(machineContext, vm)
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.VMProvisionedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.UpdatingReason}})
			Expect(elfMachine.Status.TaskRef).To(Equal(*task.ID))
		})
	})
})
