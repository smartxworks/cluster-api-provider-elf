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

func TestGetAvailableCountFromGPUVMInfo(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("GPU", func(t *testing.T) {
		gpuVMInfo := &models.GpuVMInfo{
			ID:        TowerString("gpu1"),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsagePASSTHROUGH),
		}
		g.Expect(GetAvailableCountFromGPUVMInfo(gpuVMInfo)).To(gomega.Equal(int32(1)))

		gpuVMInfo.Vms = []*models.GpuVMDetail{{}}
		g.Expect(GetAvailableCountFromGPUVMInfo(gpuVMInfo)).To(gomega.Equal(int32(0)))
	})

	t.Run("vGPU", func(t *testing.T) {
		gpuVMInfo := &models.GpuVMInfo{
			ID:                TowerString("gpu1"),
			AvailableVgpusNum: TowerInt32(3),
			UserUsage:         models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU),
		}
		g.Expect(GetAvailableCountFromGPUVMInfo(gpuVMInfo)).To(gomega.Equal(int32(3)))
	})
}

func TestCalculateAssignedAndAvailableNumForGPUVMInfos(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("GPU", func(t *testing.T) {
		gpuVMInfo := &models.GpuVMInfo{
			ID:                TowerString("gpu1"),
			UserUsage:         models.NewGpuDeviceUsage(models.GpuDeviceUsagePASSTHROUGH),
			AssignedVgpusNum:  TowerInt32(0),
			AvailableVgpusNum: TowerInt32(0),
		}
		CalculateAssignedAndAvailableNumForGPUVMInfos(NewGPUVMInfos(gpuVMInfo))
		g.Expect(*gpuVMInfo.AssignedVgpusNum).To(gomega.Equal(int32(0)))
		g.Expect(*gpuVMInfo.AvailableVgpusNum).To(gomega.Equal(int32(0)))
	})

	t.Run("vGPU", func(t *testing.T) {
		gpuVMInfo := &models.GpuVMInfo{
			ID:                TowerString("gpu1"),
			UserUsage:         models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU),
			AssignedVgpusNum:  TowerInt32(1),
			AvailableVgpusNum: TowerInt32(1),
			VgpuInstanceNum:   TowerInt32(3),
		}
		CalculateAssignedAndAvailableNumForGPUVMInfos(NewGPUVMInfos(gpuVMInfo))
		g.Expect(*gpuVMInfo.AssignedVgpusNum).To(gomega.Equal(int32(0)))
		g.Expect(*gpuVMInfo.AvailableVgpusNum).To(gomega.Equal(int32(3)))
		g.Expect(*gpuVMInfo.VgpuInstanceNum).To(gomega.Equal(int32(3)))

		gpuVMInfo.Vms = []*models.GpuVMDetail{{VgpuInstanceOnVMNum: TowerInt32(1)}}
		CalculateAssignedAndAvailableNumForGPUVMInfos(NewGPUVMInfos(gpuVMInfo))
		g.Expect(*gpuVMInfo.AssignedVgpusNum).To(gomega.Equal(int32(1)))
		g.Expect(*gpuVMInfo.AvailableVgpusNum).To(gomega.Equal(int32(2)))
		g.Expect(*gpuVMInfo.VgpuInstanceNum).To(gomega.Equal(int32(3)))

		gpuVMInfo.Vms = []*models.GpuVMDetail{{VgpuInstanceOnVMNum: TowerInt32(1)}, {VgpuInstanceOnVMNum: TowerInt32(5)}}
		CalculateAssignedAndAvailableNumForGPUVMInfos(NewGPUVMInfos(gpuVMInfo))
		g.Expect(*gpuVMInfo.AssignedVgpusNum).To(gomega.Equal(int32(6)))
		g.Expect(*gpuVMInfo.AvailableVgpusNum).To(gomega.Equal(int32(0)))
		g.Expect(*gpuVMInfo.VgpuInstanceNum).To(gomega.Equal(int32(3)))
	})
}

func TestHasGPUsCanNotBeUsedForVM(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	elfMachine := &infrav1.ElfMachine{}
	elfMachine.Name = "test"

	t.Run("GPU", func(t *testing.T) {
		elfMachine.Spec.GPUDevices = append(elfMachine.Spec.GPUDevices, infrav1.GPUPassthroughDeviceSpec{Model: "A16", Count: 1})

		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(), elfMachine)).To(gomega.BeFalse())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(&models.GpuVMInfo{
			ID:        TowerString("gpu1"),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsagePASSTHROUGH),
			Vms:       []*models.GpuVMDetail{{ID: TowerString("vm1"), Name: TowerString(elfMachine.Name)}},
		}), elfMachine)).To(gomega.BeFalse())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(&models.GpuVMInfo{
			ID:        TowerString("gpu1"),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsagePASSTHROUGH),
			Vms:       []*models.GpuVMDetail{{ID: TowerString("vm1"), Name: TowerString("vm1")}},
		}), elfMachine)).To(gomega.BeTrue())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(&models.GpuVMInfo{
			ID:        TowerString("gpu1"),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsagePASSTHROUGH),
			Vms: []*models.GpuVMDetail{
				{ID: TowerString("vm1"), Name: TowerString("vm1")},
				{ID: TowerString("vm2"), Name: TowerString(elfMachine.Name)},
			},
		}), elfMachine)).To(gomega.BeTrue())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(&models.GpuVMInfo{
			ID:        TowerString("gpu1"),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsagePASSTHROUGH),
			Vms: []*models.GpuVMDetail{
				{ID: TowerString("vm2"), Name: TowerString(elfMachine.Name)},
				{ID: TowerString("vm1"), Name: TowerString("vm1")},
			},
		}), elfMachine)).To(gomega.BeFalse())
	})

	t.Run("vGPU", func(t *testing.T) {
		vGPUType := "V100"
		elfMachine.Spec.GPUDevices = nil
		elfMachine.Spec.VGPUDevices = []infrav1.VGPUDeviceSpec{{Type: vGPUType, Count: 2}}

		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(), elfMachine)).To(gomega.BeFalse())

		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(&models.GpuVMInfo{
			ID:                TowerString("gpu1"),
			UserUsage:         models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU),
			AvailableVgpusNum: TowerInt32(0), UserVgpuTypeName: TowerString(vGPUType),
			Vms: []*models.GpuVMDetail{{ID: TowerString(elfMachine.Name), Name: TowerString(elfMachine.Name)}},
		}), elfMachine)).To(gomega.BeFalse())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(&models.GpuVMInfo{
			ID:                TowerString("gpu1"),
			UserUsage:         models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU),
			AvailableVgpusNum: TowerInt32(0), UserVgpuTypeName: TowerString(vGPUType),
			Vms: []*models.GpuVMDetail{{ID: TowerString(elfMachine.Name), Name: TowerString(elfMachine.Name)}},
		}, &models.GpuVMInfo{
			ID: TowerString("gpu1"), AvailableVgpusNum: TowerInt32(0), UserVgpuTypeName: TowerString(vGPUType),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU),
			Vms:       []*models.GpuVMDetail{},
		}), elfMachine)).To(gomega.BeTrue())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(&models.GpuVMInfo{
			ID: TowerString("gpu1"), AvailableVgpusNum: TowerInt32(0), UserVgpuTypeName: TowerString(vGPUType),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU),
			Vms:       []*models.GpuVMDetail{{ID: TowerString(elfMachine.Name), Name: TowerString(elfMachine.Name)}},
		}, &models.GpuVMInfo{
			ID: TowerString("gpu2"), AvailableVgpusNum: TowerInt32(1), UserVgpuTypeName: TowerString(vGPUType),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU),
			Vms:       []*models.GpuVMDetail{{ID: TowerString(elfMachine.Name), Name: TowerString(elfMachine.Name)}},
		}), elfMachine)).To(gomega.BeFalse())

		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(&models.GpuVMInfo{
			ID: TowerString("gpu1"), AvailableVgpusNum: TowerInt32(1), UserVgpuTypeName: TowerString(vGPUType),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU),
			Vms:       nil,
		}), elfMachine)).To(gomega.BeTrue())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(&models.GpuVMInfo{
			ID: TowerString("gpu1"), AvailableVgpusNum: TowerInt32(2), UserVgpuTypeName: TowerString(vGPUType),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU),
			Vms:       []*models.GpuVMDetail{},
		}), elfMachine)).To(gomega.BeFalse())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(&models.GpuVMInfo{
			ID: TowerString("gpu1"), AvailableVgpusNum: TowerInt32(1), UserVgpuTypeName: TowerString(vGPUType),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU),
			Vms:       []*models.GpuVMDetail{{ID: TowerString("vm1"), Name: TowerString("vm1")}},
		}), elfMachine)).To(gomega.BeTrue())
		g.Expect(HasGPUsCanNotBeUsedForVM(NewGPUVMInfos(&models.GpuVMInfo{
			ID: TowerString("gpu1"), AvailableVgpusNum: TowerInt32(2), UserVgpuTypeName: TowerString(vGPUType),
			UserUsage: models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU),
			Vms:       []*models.GpuVMDetail{{ID: TowerString("vm1"), Name: TowerString("vm1")}},
		}), elfMachine)).To(gomega.BeFalse())
	})
}

func TestParseOwnerFromCreatedByAnnotation(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("parseOwnerFromCreatedByAnnotation", func(t *testing.T) {
		g.Expect(parseOwnerFromCreatedByAnnotation("")).To(gomega.Equal(""))
		g.Expect(parseOwnerFromCreatedByAnnotation("a")).To(gomega.Equal("a"))
		g.Expect(parseOwnerFromCreatedByAnnotation("@")).To(gomega.Equal("@"))
		g.Expect(parseOwnerFromCreatedByAnnotation("a@")).To(gomega.Equal("a@"))
		g.Expect(parseOwnerFromCreatedByAnnotation("@a")).To(gomega.Equal("@a"))
		g.Expect(parseOwnerFromCreatedByAnnotation("@@")).To(gomega.Equal("@@"))
		g.Expect(parseOwnerFromCreatedByAnnotation("root")).To(gomega.Equal("root"))
		g.Expect(parseOwnerFromCreatedByAnnotation("@root")).To(gomega.Equal("@root"))
		g.Expect(parseOwnerFromCreatedByAnnotation("ro@ot")).To(gomega.Equal("ro@ot"))
		g.Expect(parseOwnerFromCreatedByAnnotation("root@")).To(gomega.Equal("root@"))
		g.Expect(parseOwnerFromCreatedByAnnotation("@ro@ot@")).To(gomega.Equal("@ro@ot@"))
		g.Expect(parseOwnerFromCreatedByAnnotation("root@123456")).To(gomega.Equal("root@123456"))
		g.Expect(parseOwnerFromCreatedByAnnotation("root@d8dc20fc-e197-41da-83b6-c903c88663fd")).To(gomega.Equal("root_d8dc20fc-e197-41da-83b6-c903c88663fd"))
		g.Expect(parseOwnerFromCreatedByAnnotation("@root@d8dc20fc-e197-41da-83b6-c903c88663fd")).To(gomega.Equal("@root_d8dc20fc-e197-41da-83b6-c903c88663fd"))
		g.Expect(parseOwnerFromCreatedByAnnotation("root@@d8dc20fc-e197-41da-83b6-c903c88663fd")).To(gomega.Equal("root@_d8dc20fc-e197-41da-83b6-c903c88663fd"))
		g.Expect(parseOwnerFromCreatedByAnnotation("root@d8dc20fc-e197-41da-83b6-c903c88663fd@")).To(gomega.Equal("root@d8dc20fc-e197-41da-83b6-c903c88663fd@"))
	})
}
