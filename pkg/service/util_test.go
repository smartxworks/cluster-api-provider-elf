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

package service

import (
	"testing"

	"github.com/onsi/gomega"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	"k8s.io/utils/pointer"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

func TestIsAvailableHost(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("should return false when status is CONNECTED_ERROR/SESSION_EXPIRED/INITIALIZING", func(t *testing.T) {
		host := &models.Host{Status: nil}
		ok, message := IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.Equal(""))

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusCONNECTEDERROR)}
		ok, message = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring(string(models.HostStatusCONNECTEDERROR)))

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusSESSIONEXPIRED)}
		ok, message = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring(string(models.HostStatusSESSIONEXPIRED)))

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusINITIALIZING)}
		ok, message = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring(string(models.HostStatusINITIALIZING)))

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusCONNECTING)}
		ok, _ = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusCONNECTEDWARNING)}
		ok, _ = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)}
		ok, _ = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())
	})

	t.Run("should return false when state is MAINTENANCEMODE/ENTERINGMAINTENANCEMODE", func(t *testing.T) {
		host := &models.Host{HostState: nil, Status: models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)}
		ok, message := IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(message).To(gomega.Equal(""))

		host.HostState = &models.NestedMaintenanceHostState{State: models.NewMaintenanceModeEnum(models.MaintenanceModeEnumMAINTENANCEMODE)}
		ok, message = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring(string(models.MaintenanceModeEnumMAINTENANCEMODE)))

		host.HostState = &models.NestedMaintenanceHostState{State: models.NewMaintenanceModeEnum(models.MaintenanceModeEnumENTERINGMAINTENANCEMODE)}
		ok, message = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring(string(models.MaintenanceModeEnumENTERINGMAINTENANCEMODE)))

		host.HostState = &models.NestedMaintenanceHostState{State: models.NewMaintenanceModeEnum(models.MaintenanceModeEnumINUSE)}
		ok, _ = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())

		host.HostState = &models.NestedMaintenanceHostState{State: models.NewMaintenanceModeEnum(models.MaintenanceModeEnumINUSE)}
		ok, _ = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())
	})

	t.Run("should return false when insufficient memory", func(t *testing.T) {
		host := &models.Host{AllocatableMemoryBytes: pointer.Int64(2), Status: models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)}

		ok, _ := IsAvailableHost(host, 1)
		g.Expect(ok).To(gomega.BeTrue())

		ok, _ = IsAvailableHost(host, 2)
		g.Expect(ok).To(gomega.BeTrue())

		ok, message := IsAvailableHost(host, 3)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring("3"))
	})
}

func TestHasGPUsCanNotBeUsedForVM(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	elfMachine := &infrav1.ElfMachine{}
	elfMachine.Name = "test"

	t.Run("GPU", func(t *testing.T) {
		elfMachine.Spec.GPUDevices = append(elfMachine.Spec.GPUDevices, infrav1.GPUPassthroughDeviceSpec{Model: "A16", Count: 1})

		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(), elfMachine)).To(gomega.BeFalse())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID:  "gpu1",
			VMs: []GPUDeviceVM{{ID: "vm1", Name: elfMachine.Name}},
		}), elfMachine)).To(gomega.BeFalse())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID:  "gpu1",
			VMs: []GPUDeviceVM{{ID: "vm1", Name: "vm1"}},
		}), elfMachine)).To(gomega.BeTrue())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID: "gpu1",
			VMs: []GPUDeviceVM{
				{ID: "vm1", Name: "vm1"},
				{ID: "vm2", Name: elfMachine.Name},
			},
		}), elfMachine)).To(gomega.BeTrue())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID: "gpu1",
			VMs: []GPUDeviceVM{
				{ID: "vm2", Name: elfMachine.Name},
				{ID: "vm1", Name: "vm1"},
			},
		}), elfMachine)).To(gomega.BeFalse())
	})

	t.Run("vGPU", func(t *testing.T) {
		vGPUType := "V100"
		elfMachine.Spec.GPUDevices = nil
		elfMachine.Spec.VGPUDevices = []infrav1.VGPUDeviceSpec{{Type: vGPUType, Count: 2}}

		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(), elfMachine)).To(gomega.BeFalse())

		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID:             "gpu1",
			AvailableCount: 0, VGPUType: vGPUType,
			VMs: []GPUDeviceVM{{ID: elfMachine.Name, Name: elfMachine.Name}},
		}), elfMachine)).To(gomega.BeFalse())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID:             "gpu1",
			AvailableCount: 0, VGPUType: vGPUType,
			VMs: []GPUDeviceVM{{ID: elfMachine.Name, Name: elfMachine.Name}},
		}, &GPUDeviceInfo{
			ID: "gpu1", AvailableCount: 0, VGPUType: vGPUType,
			VMs: []GPUDeviceVM{},
		}), elfMachine)).To(gomega.BeTrue())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID: "gpu1", AvailableCount: 0, VGPUType: vGPUType,
			VMs: []GPUDeviceVM{{ID: elfMachine.Name, Name: elfMachine.Name}},
		}, &GPUDeviceInfo{
			ID: "gpu2", AvailableCount: 1, VGPUType: vGPUType,
			VMs: []GPUDeviceVM{{ID: elfMachine.Name, Name: elfMachine.Name}},
		}), elfMachine)).To(gomega.BeFalse())

		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID: "gpu1", AvailableCount: 1, VGPUType: vGPUType,
			VMs: []GPUDeviceVM{},
		}), elfMachine)).To(gomega.BeTrue())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID: "gpu1", AvailableCount: 2, VGPUType: vGPUType,
			VMs: []GPUDeviceVM{},
		}), elfMachine)).To(gomega.BeFalse())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID: "gpu1", AvailableCount: 1, VGPUType: vGPUType,
			VMs: []GPUDeviceVM{{ID: "vm1", Name: "vm1"}},
		}), elfMachine)).To(gomega.BeTrue())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID: "gpu1", AvailableCount: 2, VGPUType: vGPUType,
			VMs: []GPUDeviceVM{{ID: "vm1", Name: "vm1"}},
		}), elfMachine)).To(gomega.BeFalse())
	})
}

func TestAggregateUnusedGPUDevicesToGPUDeviceInfos(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	host := &models.NestedHost{ID: TowerString("host1")}

	t.Run("GPU", func(t *testing.T) {
		gpuDevice := &models.GpuDevice{ID: TowerString("gpu1"), Host: host, Model: TowerString("A16"), UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsagePASSTHROUGH), UserVgpuTypeName: TowerString("")}
		gpuDevices := []*models.GpuDevice{gpuDevice}
		gpuDeviceInfos := NewGPUDeviceInfos()

		AggregateUnusedGPUDevicesToGPUDeviceInfos(gpuDeviceInfos, gpuDevices)
		g.Expect(gpuDeviceInfos.Len()).To(gomega.Equal(1))
		g.Expect(*gpuDeviceInfos.Get(*gpuDevice.ID)).To(gomega.Equal(GPUDeviceInfo{
			ID:             *gpuDevice.ID,
			HostID:         *gpuDevice.Host.ID,
			Model:          *gpuDevice.Model,
			VGPUType:       *gpuDevice.UserVgpuTypeName,
			AllocatedCount: 0,
			AvailableCount: 1,
		}))

		AggregateUnusedGPUDevicesToGPUDeviceInfos(gpuDeviceInfos, gpuDevices)
		g.Expect(gpuDeviceInfos.Len()).To(gomega.Equal(1))
		g.Expect(gpuDeviceInfos.Contains(*gpuDevice.ID)).To(gomega.BeTrue())
	})

	t.Run("vGPU", func(t *testing.T) {
		gpuDevice := &models.GpuDevice{ID: TowerString("gpu1"), Host: host, Model: TowerString("V100"), UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU), UserVgpuTypeName: TowerString(""), AvailableVgpusNum: TowerInt32(6)}
		gpuDevices := []*models.GpuDevice{gpuDevice}
		gpuDeviceInfos := NewGPUDeviceInfos()

		AggregateUnusedGPUDevicesToGPUDeviceInfos(gpuDeviceInfos, gpuDevices)
		g.Expect(gpuDeviceInfos.Len()).To(gomega.Equal(1))
		g.Expect(*gpuDeviceInfos.Get(*gpuDevice.ID)).To(gomega.Equal(GPUDeviceInfo{
			ID:             *gpuDevice.ID,
			HostID:         *gpuDevice.Host.ID,
			Model:          *gpuDevice.Model,
			VGPUType:       *gpuDevice.UserVgpuTypeName,
			AllocatedCount: 0,
			AvailableCount: *gpuDevice.AvailableVgpusNum,
		}))

		AggregateUnusedGPUDevicesToGPUDeviceInfos(gpuDeviceInfos, gpuDevices)
		g.Expect(gpuDeviceInfos.Len()).To(gomega.Equal(1))
		g.Expect(gpuDeviceInfos.Contains(*gpuDevice.ID)).To(gomega.BeTrue())
	})
}

func TestConvertVMGpuInfosToGPUDeviceInfos(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	host := &models.NestedHost{ID: TowerString("host1")}

	t.Run("GPU", func(t *testing.T) {
		vmGpuDetail := &models.VMGpuDetail{ID: TowerString("gpu1"), Host: host, Model: TowerString("A16"), UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsagePASSTHROUGH), UserVgpuTypeName: TowerString("")}
		vmGpuInfo1 := &models.VMGpuInfo{ID: TowerString("1"), Name: TowerString("vm1"), GpuDevices: []*models.VMGpuDetail{vmGpuDetail}}
		vmGpuInfo2 := &models.VMGpuInfo{ID: TowerString("2"), Name: TowerString("vm2"), GpuDevices: []*models.VMGpuDetail{vmGpuDetail}}

		g.Expect(ConvertVMGpuInfosToGPUDeviceInfos(
			[]*models.VMGpuInfo{},
		)).To(gomega.BeEmpty())

		g.Expect(ConvertVMGpuInfosToGPUDeviceInfos(
			[]*models.VMGpuInfo{vmGpuInfo1},
		)).To(gomega.Equal(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID:             *vmGpuDetail.ID,
			HostID:         *vmGpuDetail.Host.ID,
			Model:          *vmGpuDetail.Model,
			VGPUType:       *vmGpuDetail.UserVgpuTypeName,
			AllocatedCount: 1,
			AvailableCount: 0,
			VMs:            []GPUDeviceVM{{ID: *vmGpuInfo1.ID, Name: *vmGpuInfo1.Name, AllocatedCount: 1}},
		})))

		g.Expect(ConvertVMGpuInfosToGPUDeviceInfos(
			[]*models.VMGpuInfo{vmGpuInfo1, vmGpuInfo2},
		)).To(gomega.Equal(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID:             *vmGpuDetail.ID,
			HostID:         *vmGpuDetail.Host.ID,
			Model:          *vmGpuDetail.Model,
			VGPUType:       *vmGpuDetail.UserVgpuTypeName,
			AllocatedCount: 2,
			AvailableCount: 0,
			VMs: []GPUDeviceVM{
				{ID: *vmGpuInfo1.ID, Name: *vmGpuInfo1.Name, AllocatedCount: 1},
				{ID: *vmGpuInfo2.ID, Name: *vmGpuInfo2.Name, AllocatedCount: 1},
			},
		})))
	})

	t.Run("vGPU", func(t *testing.T) {
		vmGpuDetail1 := &models.VMGpuDetail{ID: TowerString("gpu1"), Host: host, Model: TowerString("V100"), UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU), UserVgpuTypeName: TowerString("GRID V100-4C"), VgpuInstanceNum: TowerInt32(3), VgpuInstanceOnVMNum: TowerInt32(1)}
		vmGpuDetail2 := &models.VMGpuDetail{ID: TowerString("gpu1"), Host: host, Model: TowerString("V100"), UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU), UserVgpuTypeName: TowerString("GRID V100-4C"), VgpuInstanceNum: TowerInt32(3), VgpuInstanceOnVMNum: TowerInt32(2)}
		vmGpuDetail3 := &models.VMGpuDetail{ID: TowerString("gpu1"), Host: host, Model: TowerString("V100"), UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU), UserVgpuTypeName: TowerString("GRID V100-4C"), VgpuInstanceNum: TowerInt32(3), VgpuInstanceOnVMNum: TowerInt32(1)}
		vmGpuInfo1 := &models.VMGpuInfo{ID: TowerString("1"), Name: TowerString("vm1"), GpuDevices: []*models.VMGpuDetail{vmGpuDetail1}}
		vmGpuInfo2 := &models.VMGpuInfo{ID: TowerString("2"), Name: TowerString("vm2"), GpuDevices: []*models.VMGpuDetail{vmGpuDetail2}}
		vmGpuInfo3 := &models.VMGpuInfo{ID: TowerString("1"), Name: TowerString("vm2"), GpuDevices: []*models.VMGpuDetail{vmGpuDetail3}}

		g.Expect(ConvertVMGpuInfosToGPUDeviceInfos(
			[]*models.VMGpuInfo{vmGpuInfo1},
		)).To(gomega.Equal(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID:             *vmGpuDetail1.ID,
			HostID:         *vmGpuDetail1.Host.ID,
			Model:          *vmGpuDetail1.Model,
			VGPUType:       *vmGpuDetail1.UserVgpuTypeName,
			AllocatedCount: 1,
			AvailableCount: 2,
			VMs:            []GPUDeviceVM{{ID: *vmGpuInfo1.ID, Name: *vmGpuInfo1.Name, AllocatedCount: 1}},
		})))

		g.Expect(ConvertVMGpuInfosToGPUDeviceInfos(
			[]*models.VMGpuInfo{vmGpuInfo1, vmGpuInfo2},
		)).To(gomega.Equal(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID:             *vmGpuDetail1.ID,
			HostID:         *vmGpuDetail1.Host.ID,
			Model:          *vmGpuDetail1.Model,
			VGPUType:       *vmGpuDetail1.UserVgpuTypeName,
			AllocatedCount: 3,
			AvailableCount: 0,
			VMs: []GPUDeviceVM{
				{ID: *vmGpuInfo1.ID, Name: *vmGpuInfo1.Name, AllocatedCount: 1},
				{ID: *vmGpuInfo2.ID, Name: *vmGpuInfo2.Name, AllocatedCount: 2},
			},
		})))

		g.Expect(ConvertVMGpuInfosToGPUDeviceInfos(
			[]*models.VMGpuInfo{vmGpuInfo1, vmGpuInfo2, vmGpuInfo3},
		)).To(gomega.Equal(NewGPUDeviceInfos(&GPUDeviceInfo{
			ID:             *vmGpuDetail1.ID,
			HostID:         *vmGpuDetail1.Host.ID,
			Model:          *vmGpuDetail1.Model,
			VGPUType:       *vmGpuDetail1.UserVgpuTypeName,
			AllocatedCount: 4,
			AvailableCount: 0,
			VMs: []GPUDeviceVM{
				{ID: *vmGpuInfo1.ID, Name: *vmGpuInfo1.Name, AllocatedCount: 1},
				{ID: *vmGpuInfo2.ID, Name: *vmGpuInfo2.Name, AllocatedCount: 2},
				{ID: *vmGpuInfo3.ID, Name: *vmGpuInfo3.Name, AllocatedCount: 1},
			},
		})))
	})
}
