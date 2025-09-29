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

package service

import (
	goctx "context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	clientcluster "github.com/smartxworks/cloudtower-go-sdk/v2/client/cluster"
	clientvmtemplate "github.com/smartxworks/cloudtower-go-sdk/v2/client/content_library_vm_template"
	clientgpu "github.com/smartxworks/cloudtower-go-sdk/v2/client/gpu_device"
	clienthost "github.com/smartxworks/cloudtower-go-sdk/v2/client/host"
	clientlabel "github.com/smartxworks/cloudtower-go-sdk/v2/client/label"
	clienttask "github.com/smartxworks/cloudtower-go-sdk/v2/client/task"
	clientvlan "github.com/smartxworks/cloudtower-go-sdk/v2/client/vlan"
	clientvm "github.com/smartxworks/cloudtower-go-sdk/v2/client/vm"
	clientvmdisk "github.com/smartxworks/cloudtower-go-sdk/v2/client/vm_disk"
	clientvmnic "github.com/smartxworks/cloudtower-go-sdk/v2/client/vm_nic"
	clientvmplacementgroup "github.com/smartxworks/cloudtower-go-sdk/v2/client/vm_placement_group"
	clientvmvolume "github.com/smartxworks/cloudtower-go-sdk/v2/client/vm_volume"
	clientzone "github.com/smartxworks/cloudtower-go-sdk/v2/client/zone"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	"k8s.io/apimachinery/pkg/util/wait"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/session"
	annotationsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/annotations"
)

var (
	haPriorityMap = map[string]models.VMHaPriority{
		"LOW":    models.VMHaPriorityLEVEL1LOW,
		"MEDIUM": models.VMHaPriorityLEVEL2MEDIUM,
		"HIGH":   models.VMHaPriorityLEVEL3HIGH,
	}
)

type VMService interface {
	Clone(elfCluster *infrav1.ElfCluster, elfMachine *infrav1.ElfMachine, bootstrapData,
		host string, machineGPUDevices []*GPUDeviceInfo) (*models.WithTaskVM, error)
	UpdateVM(vm *models.VM, elfMachine *infrav1.ElfMachine) (*models.WithTaskVM, error)
	Migrate(vmID, hostID string) (*models.WithTaskVM, error)
	Delete(uuid string) (*models.Task, error)
	PowerOff(uuid string) (*models.Task, error)
	PowerOn(id string, hostID string) (*models.Task, error)
	ShutDown(uuid string) (*models.Task, error)
	RemoveGPUDevices(id string, gpus []*models.VMGpuOperationParams) (*models.Task, error)
	AddGPUDevices(id string, gpuDeviceInfo []*GPUDeviceInfo) (*models.Task, error)
	Get(id string) (*models.VM, error)
	GetByName(name string) (*models.VM, error)
	FindByIDs(ids []string) ([]*models.VM, error)
	FindVMsByName(name string) ([]*models.VM, error)
	AddVMNics(vmID string, nics []*models.VMNicParams) (*models.WithTaskVM, error)
	GetVMNics(vmID string) ([]*models.VMNic, error)
	GetVMDisks(vmDiskIDs []string) ([]*models.VMDisk, error)
	GetVMVolume(vmVolumeID string) (*models.VMVolume, error)
	ResizeVMVolume(vmVolumeID string, size int64) (*models.WithTaskVMVolume, error)
	GetVMTemplate(template string) (*models.ContentLibraryVMTemplate, error)
	GetTask(id string) (*models.Task, error)
	WaitTask(ctx goctx.Context, id string, timeout, interval time.Duration) (*models.Task, error)
	GetCluster(id string) (*models.Cluster, error)
	GetClusterZones(clusterID string) ([]*models.Zone, error)
	GetHost(id string) (*models.Host, error)
	GetHostsByCluster(clusterID string) (Hosts, error)
	GetVlan(id string) (*models.Vlan, error)
	UpsertLabel(key, value string) (*models.Label, error)
	DeleteLabel(key, value string, strict bool) (string, error)
	CleanUnusedLabels(keys []string) ([]string, error)
	AddLabelsToVM(vmID string, labels []string) (*models.Task, error)
	CreateVMPlacementGroup(name, clusterID string, vmPolicy models.VMVMPolicy) (*models.WithTaskVMPlacementGroup, error)
	GetVMPlacementGroup(name string) (*models.VMPlacementGroup, error)
	AddVMsToPlacementGroup(placementGroup *models.VMPlacementGroup, vmIDs []string) (*models.Task, error)
	DeleteVMPlacementGroupByID(ctx goctx.Context, id string) (bool, error)
	DeleteVMPlacementGroupsByNamePrefix(ctx goctx.Context, placementGroupName string) ([]string, error)
	GetGPUDevicesAllocationInfoByHostIDs(hostIDs []string, gpuDeviceUsage models.GpuDeviceUsage) (GPUVMInfos, error)
	GetGPUDevicesAllocationInfoByIDs(gpuIDs []string) (GPUVMInfos, error)
	GetVMGPUAllocationInfo(id string) (*models.VMGpuInfo, error)
}

type NewVMServiceFunc func(ctx goctx.Context, auth infrav1.Tower, logger logr.Logger) (VMService, error)

func NewVMService(ctx goctx.Context, auth infrav1.Tower, logger logr.Logger) (VMService, error) {
	authSession, err := session.GetOrCreate(ctx, auth)
	if err != nil {
		return nil, err
	}

	return &TowerVMService{authSession, logger}, nil
}

type TowerVMService struct {
	Session *session.TowerSession `json:"session"`
	Logger  logr.Logger           `json:"logger"`
}

func (svr *TowerVMService) UpdateVM(vm *models.VM, elfMachine *infrav1.ElfMachine) (*models.WithTaskVM, error) {
	vCPU := TowerVCPU(elfMachine.Spec.NumCPUs)
	cpuSocketCores := TowerCPUSocketCores(elfMachine.Spec.NumCoresPerSocket, *vCPU)
	cpuSockets := TowerCPUSockets(*vCPU, *cpuSocketCores)
	memory := TowerMemory(elfMachine.Spec.MemoryMiB)

	updateVMParams := clientvm.NewUpdateVMParams()
	updateVMParams.RequestBody = &models.VMUpdateParams{
		Data: &models.VMUpdateParamsData{
			Vcpu:       vCPU,
			CPUCores:   cpuSocketCores,
			CPUSockets: cpuSockets,
			Memory:     memory,
		},
		Where: &models.VMWhereInput{ID: TowerString(*vm.ID)},
	}

	updateVMResp, err := svr.Session.VM.UpdateVM(updateVMParams)
	if err != nil {
		return nil, err
	}

	return updateVMResp.Payload[0], nil
}

func (svr *TowerVMService) GetVMDisks(vmDiskIDs []string) ([]*models.VMDisk, error) {
	getVMDisksParams := clientvmdisk.NewGetVMDisksParams()
	getVMDisksParams.RequestBody = &models.GetVMDisksRequestBody{
		Where:   &models.VMDiskWhereInput{IDIn: vmDiskIDs},
		OrderBy: models.NewVMDiskOrderByInput(models.VMDiskOrderByInputBootASC),
	}

	getVMDisksResp, err := svr.Session.VMDisk.GetVMDisks(getVMDisksParams)
	if err != nil {
		return nil, err
	}

	return getVMDisksResp.Payload, nil
}

func (svr *TowerVMService) GetVMVolume(volumeID string) (*models.VMVolume, error) {
	getVMVolumesParams := clientvmvolume.NewGetVMVolumesParams()
	getVMVolumesParams.RequestBody = &models.GetVMVolumesRequestBody{
		Where: &models.VMVolumeWhereInput{ID: TowerString(volumeID)},
	}

	getVMVolumesResp, err := svr.Session.VMVolume.GetVMVolumes(getVMVolumesParams)
	if err != nil {
		return nil, err
	}

	if len(getVMVolumesResp.Payload) == 0 {
		return nil, errors.New(VMVolumeNotFound)
	}

	return getVMVolumesResp.Payload[0], nil
}

// ResizeVMVolume resizes the virtual machine volume to the specified size.
// Can only increase the volume size, not reduce it.
func (svr *TowerVMService) ResizeVMVolume(vmVolumeID string, size int64) (*models.WithTaskVMVolume, error) {
	updateVMVolumeParams := clientvmvolume.NewUpdateVMVolumeParams()
	updateVMVolumeParams.RequestBody = &models.UpdateVMVolumeParams{
		Data:  &models.UpdateVMVolumeParamsData{Size: TowerInt64(size)},
		Where: &models.VMVolumeWhereInput{ID: TowerString(vmVolumeID)},
	}

	updateVMVolumeResp, err := svr.Session.VMVolume.UpdateVMVolume(updateVMVolumeParams)
	if err != nil {
		return nil, err
	}

	return updateVMVolumeResp.Payload[0], nil
}

// Clone kicks off a clone operation on Elf to create a new virtual machine using VM template.
func (svr *TowerVMService) Clone(
	elfCluster *infrav1.ElfCluster, elfMachine *infrav1.ElfMachine, bootstrapData,
	host string, gpuDeviceInfos []*GPUDeviceInfo) (*models.WithTaskVM, error) {
	cluster, err := svr.GetCluster(elfCluster.Spec.Cluster)
	if err != nil {
		return nil, err
	}

	template, err := svr.GetVMTemplate(elfMachine.Spec.Template)
	if err != nil {
		return nil, err
	}

	createVMFromTemplateParams, err := svr.createVMFromTemplateParams(elfCluster, elfMachine, bootstrapData, host, gpuDeviceInfos, cluster, template)
	if err != nil {
		return nil, err
	}

	createVMFromContentLibraryTemplateParams := clientvm.NewCreateVMFromContentLibraryTemplateParams()
	createVMFromContentLibraryTemplateParams.RequestBody = []*models.VMCreateVMFromContentLibraryTemplateParams{createVMFromTemplateParams}
	createVMFromTemplateResp, err := svr.Session.VM.CreateVMFromContentLibraryTemplate(createVMFromContentLibraryTemplateParams)
	if err != nil {
		return nil, err
	}

	return createVMFromTemplateResp.Payload[0], nil
}

func (svr *TowerVMService) createVMFromTemplateParams(
	elfCluster *infrav1.ElfCluster, elfMachine *infrav1.ElfMachine, bootstrapData,
	host string, gpuDeviceInfos []*GPUDeviceInfo, cluster *models.Cluster, template *models.ContentLibraryVMTemplate) (*models.VMCreateVMFromContentLibraryTemplateParams, error) {
	vCPU := TowerVCPU(elfMachine.Spec.NumCPUs)
	cpuSocketCores := TowerCPUSocketCores(elfMachine.Spec.NumCoresPerSocket, *vCPU)
	cpuSockets := TowerCPUSockets(*vCPU, *cpuSocketCores)

	gpuDevices := make([]*models.VMGpuOperationParams, len(gpuDeviceInfos))
	for i := range gpuDeviceInfos {
		gpuDevices[i] = &models.VMGpuOperationParams{
			GpuID:  TowerString(gpuDeviceInfos[i].ID),
			Amount: TowerInt32(int(gpuDeviceInfos[i].AllocatedCount)),
		}
	}

	ha := TowerBool(elfMachine.Spec.HA)
	// HA cannot be enabled on a virtual machine with GPU/vGPU devices.
	if len(gpuDevices) > 0 {
		ha = TowerBool(false)
	}

	var haPriority *models.VMHaPriority
	if elfMachine.Spec.HAPriority != "" {
		haPriority = models.NewVMHaPriority(haPriorityMap[elfMachine.Spec.HAPriority])
	} else if elfCluster.IsStretched() {
		haPriority = models.NewVMHaPriority(models.VMHaPriorityLEVEL3HIGH)
	}

	nics := make([]*models.VMNicParams, 0, len(elfMachine.Spec.Network.Devices))
	networks := make([]*models.CloudInitNetWork, 0, len(elfMachine.Spec.Network.Devices))
	defautRouteDeviceIndex := elfMachine.Spec.Network.GetDefaultRouteDeviceIndex()
	for i := range len(elfMachine.Spec.Network.Devices) {
		device := elfMachine.Spec.Network.Devices[i]

		// nics
		vlan, err := svr.GetVlan(device.Vlan)
		if err != nil {
			return nil, err
		}

		nics = append(nics, &models.VMNicParams{
			Model:         models.NewVMNicModel(models.VMNicModelVIRTIO),
			Enabled:       TowerBool(true),
			Mirror:        TowerBool(false),
			ConnectVlanID: vlan.ID,
			MacAddress:    TowerString(device.MACAddr),
		})

		if !device.HasNetworkType() {
			continue
		}

		// networks
		networkType := models.CloudInitNetworkTypeEnumIPV4DHCP
		if string(device.NetworkType) == string(models.CloudInitNetworkTypeEnumIPV4) {
			networkType = models.CloudInitNetworkTypeEnumIPV4
		}

		network := &models.CloudInitNetWork{
			NicIndex: TowerInt32(i),
			Type:     &networkType,
		}

		ipAddress := ""
		if len(device.IPAddrs) > 0 {
			ipAddress = device.IPAddrs[0]
		}
		if ipAddress != "" {
			network.IPAddress = TowerString(ipAddress)
		}

		if device.Netmask != "" {
			network.Netmask = TowerString(device.Netmask)
		}

		if defautRouteDeviceIndex == i {
			routes := make([]*models.CloudInitNetWorkRoute, 0, len(device.Routes))
			for _, route := range device.Routes {
				netmask := route.Netmask
				if netmask == "" {
					netmask = "0.0.0.0"
				}
				network := route.Network
				if network == "" {
					network = "0.0.0.0"
				}

				routes = append(routes, &models.CloudInitNetWorkRoute{
					Gateway: TowerString(route.Gateway),
					Netmask: TowerString(netmask),
					Network: TowerString(network),
				})
			}

			if len(routes) > 0 {
				network.Routes = routes
			}
		}

		networks = append(networks, network)
	}

	cloudInit := &models.TemplateCloudInit{
		Hostname:    TowerString(elfMachine.Name),
		UserData:    TowerString(bootstrapData),
		Networks:    networks,
		Nameservers: elfMachine.Spec.Network.Nameservers,
	}
	if len(elfMachine.Spec.Network.Nameservers) == 0 {
		cloudInit.Nameservers = nil
	}
	if len(networks) == 0 {
		cloudInit.Networks = nil
	}

	isFullCopy := false
	if elfMachine.Spec.CloneMode == infrav1.FullClone {
		isFullCopy = true
	}

	hostID := "AUTO_SCHEDULE"
	if host != "" {
		hostID = host
	} else if elfMachine.Spec.Host != "" {
		host, err := svr.GetHost(elfMachine.Spec.Host)
		if err != nil {
			return nil, err
		}

		hostID = *host.ID
	}

	var owner *models.VMOwnerParams
	if createdBy := annotationsutil.GetCreatedBy(elfCluster); createdBy != "" {
		creator := parseOwnerFromCreatedByAnnotation(createdBy)
		owner = &models.VMOwnerParams{
			SearchFor: TowerString(VMOwnerSearchForUsername),
			Value:     TowerString(creator),
		}
	}

	var diskOperate *models.VMDiskOperate
	if elfCluster.IsStretched() {
		// https://gist.github.com/Sczlog/f89763d27711fb2bbe28182f05b99334
		diskOperate = &models.VMDiskOperate{
			RemoveDisks: &models.VMDiskOperateRemoveDisks{},
			NewDisks:    &models.VMDiskParams{},
		}
		for _, disk := range template.VMDisks {
			if *disk.Type != models.VMDiskTypeDISK {
				continue
			}

			diskOperate.RemoveDisks.DiskIndex = append(diskOperate.RemoveDisks.DiskIndex, *disk.Index)

			diskOperate.NewDisks.MountNewCreateDisks = append(diskOperate.NewDisks.MountNewCreateDisks, &models.MountNewCreateDisksParams{
				Boot:  disk.Boot,
				Bus:   disk.Bus,
				Index: disk.Index,
				VMVolume: &models.MountNewCreateDisksParamsVMVolume{
					Name:             TowerString(fmt.Sprintf("%s-%d", elfMachine.Name, *disk.Index+1)),
					Size:             disk.Size,
					ElfStoragePolicy: models.NewVMVolumeElfStoragePolicyType(models.VMVolumeElfStoragePolicyTypeREPLICA3THINPROVISION),
				},
			})
		}
	}

	return &models.VMCreateVMFromContentLibraryTemplateParams{
		ClusterID:   cluster.ID,
		HostID:      TowerString(hostID),
		Name:        TowerString(elfMachine.Name),
		Description: TowerString(fmt.Sprintf(config.VMDescription, elfCluster.Spec.Tower.Server)),
		Owner:       owner,
		Vcpu:        vCPU,
		CPUCores:    cpuSocketCores,
		CPUSockets:  cpuSockets,
		Memory:      TowerMemory(elfMachine.Spec.MemoryMiB),
		GpuDevices:  gpuDevices,
		Status:      models.NewVMStatus(models.VMStatusSTOPPED),
		Ha:          ha,
		HaPriority:  haPriority,
		IsFullCopy:  TowerBool(isFullCopy),
		TemplateID:  template.ID,
		GuestOsType: models.NewVMGuestsOperationSystem(models.VMGuestsOperationSystem(elfMachine.Spec.OSType)),
		VMNics:      nics,
		DiskOperate: diskOperate,
		CloudInit:   cloudInit,
	}, nil
}

func (svr *TowerVMService) Migrate(vmID, hostID string) (*models.WithTaskVM, error) {
	migrateVMParams := clientvm.NewMigrateVMParams()
	migrateVMParams.RequestBody = &models.VMMigrateParams{
		Data: &models.VMMigrateParamsData{
			HostID: TowerString(hostID),
		},
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: TowerString(vmID)}, {ID: TowerString(vmID)}},
		},
	}

	migrateVMResp, err := svr.Session.VM.MigrateVM(migrateVMParams)
	if err != nil {
		return nil, err
	}

	return migrateVMResp.Payload[0], nil
}

// Delete destroys a virtual machine.
func (svr *TowerVMService) Delete(id string) (*models.Task, error) {
	deleteVMParams := clientvm.NewDeleteVMParams()
	deleteVMParams.RequestBody = &models.VMDeleteParams{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: TowerString(id)}, {ID: TowerString(id)}},
		},
	}

	deleteVMResp, err := svr.Session.VM.DeleteVM(deleteVMParams)
	if err != nil {
		return nil, err
	}

	if len(deleteVMResp.Payload) == 0 {
		return nil, errors.New(VMNotFound)
	}

	return &models.Task{ID: deleteVMResp.Payload[0].TaskID}, nil
}

// PowerOff powers off a virtual machine.
func (svr *TowerVMService) PowerOff(id string) (*models.Task, error) {
	poweroffVMParams := clientvm.NewPoweroffVMParams()
	poweroffVMParams.RequestBody = &models.VMOperateParams{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: TowerString(id)}, {ID: TowerString(id)}},
		},
	}

	poweroffVMResp, err := svr.Session.VM.PoweroffVM(poweroffVMParams)
	if err != nil {
		return nil, err
	}

	if len(poweroffVMResp.Payload) == 0 {
		return nil, errors.New(VMNotFound)
	}

	return &models.Task{ID: poweroffVMResp.Payload[0].TaskID}, nil
}

// PowerOn powers on a virtual machine.
//
// The hostID param:
// Empty string means automatic scheduling.
// Non-empty string means scheduling to the specified host.
func (svr *TowerVMService) PowerOn(id string, hostID string) (*models.Task, error) {
	startVMParams := clientvm.NewStartVMParams()
	startVMParams.RequestBody = &models.VMStartParams{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: TowerString(id)}, {ID: TowerString(id)}},
		},
	}
	if hostID != "" {
		startVMParams.RequestBody.Data = &models.VMStartParamsData{HostID: TowerString(hostID)}
	}

	startVMResp, err := svr.Session.VM.StartVM(startVMParams)
	if err != nil {
		return nil, err
	}

	if len(startVMResp.Payload) == 0 {
		return nil, errors.New(VMNotFound)
	}

	return &models.Task{ID: startVMResp.Payload[0].TaskID}, nil
}

// ShutDown shut down a virtual machine.
func (svr *TowerVMService) ShutDown(id string) (*models.Task, error) {
	shutDownVMParams := clientvm.NewShutDownVMParams()
	shutDownVMParams.RequestBody = &models.VMOperateParams{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: TowerString(id)}, {ID: TowerString(id)}},
		},
	}

	shutDownVMResp, err := svr.Session.VM.ShutDownVM(shutDownVMParams)
	if err != nil {
		return nil, err
	}

	if len(shutDownVMResp.Payload) == 0 {
		return nil, errors.New(VMNotFound)
	}

	return &models.Task{ID: shutDownVMResp.Payload[0].TaskID}, nil
}

func (svr *TowerVMService) RemoveGPUDevices(id string, gpus []*models.VMGpuOperationParams) (*models.Task, error) {
	removeVMGpuDeviceParams := clientvm.NewRemoveVMGpuDeviceParams()
	removeVMGpuDeviceParams.RequestBody = &models.VMRemoveGpuDeviceParams{
		Data: gpus,
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: TowerString(id)}, {ID: TowerString(id)}},
		},
	}

	temoveVMGPUDeviceResp, err := svr.Session.VM.RemoveVMGpuDevice(removeVMGpuDeviceParams)
	if err != nil {
		return nil, err
	}

	if len(temoveVMGPUDeviceResp.Payload) == 0 {
		return nil, errors.New(VMNotFound)
	}

	return &models.Task{ID: temoveVMGPUDeviceResp.Payload[0].TaskID}, nil
}

func (svr *TowerVMService) AddGPUDevices(id string, gpuDeviceInfos []*GPUDeviceInfo) (*models.Task, error) {
	gpus := make([]*models.VMGpuOperationParams, len(gpuDeviceInfos))
	for i := range gpuDeviceInfos {
		gpus[i] = &models.VMGpuOperationParams{
			GpuID:  TowerString(gpuDeviceInfos[i].ID),
			Amount: TowerInt32(int(gpuDeviceInfos[i].AllocatedCount)),
		}
	}

	addVMGpuDeviceParams := clientvm.NewAddVMGpuDeviceParams()
	addVMGpuDeviceParams.RequestBody = &models.VMAddGpuDeviceParams{
		Data: gpus,
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: TowerString(id)}, {ID: TowerString(id)}},
		},
	}

	addVMGpuDeviceResp, err := svr.Session.VM.AddVMGpuDevice(addVMGpuDeviceParams)
	if err != nil {
		return nil, err
	}

	if len(addVMGpuDeviceResp.Payload) == 0 {
		return nil, errors.New(VMNotFound)
	}

	return &models.Task{ID: addVMGpuDeviceResp.Payload[0].TaskID}, nil
}

// Get searches for a virtual machine.
func (svr *TowerVMService) Get(id string) (*models.VM, error) {
	getVmsParams := clientvm.NewGetVmsParams()
	getVmsParams.RequestBody = &models.GetVmsRequestBody{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: TowerString(id)}, {ID: TowerString(id)}},
		},
	}

	getVmsResp, err := svr.Session.VM.GetVms(getVmsParams)
	if err != nil {
		return nil, err
	}

	if len(getVmsResp.Payload) == 0 {
		return nil, errors.New(VMNotFound)
	}

	return getVmsResp.Payload[0], nil
}

// GetByName searches for a virtual machine by name.
func (svr *TowerVMService) GetByName(name string) (*models.VM, error) {
	getVmsParams := clientvm.NewGetVmsParams()
	getVmsParams.RequestBody = &models.GetVmsRequestBody{
		Where: &models.VMWhereInput{
			Name: TowerString(name),
		},
	}

	getVmsResp, err := svr.Session.VM.GetVms(getVmsParams)
	if err != nil {
		return nil, err
	}

	if len(getVmsResp.Payload) == 0 {
		return nil, errors.New(VMNotFound)
	}

	return getVmsResp.Payload[0], nil
}

// FindByIDs searches for virtual machines by ids.
func (svr *TowerVMService) FindByIDs(ids []string) ([]*models.VM, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	getVmsParams := clientvm.NewGetVmsParams()
	getVmsParams.RequestBody = &models.GetVmsRequestBody{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalIDIn: ids}, {IDIn: ids}},
		},
	}

	getVmsResp, err := svr.Session.VM.GetVms(getVmsParams)
	if err != nil {
		return nil, err
	}

	return getVmsResp.Payload, nil
}

// FindVMsByName searches for virtual machines by name.
func (svr *TowerVMService) FindVMsByName(name string) ([]*models.VM, error) {
	if name == "" {
		return nil, nil
	}

	getVmsParams := clientvm.NewGetVmsParams()
	getVmsParams.RequestBody = &models.GetVmsRequestBody{
		Where: &models.VMWhereInput{
			Name: TowerString(name),
		},
	}

	getVmsResp, err := svr.Session.VM.GetVms(getVmsParams)
	if err != nil {
		return nil, err
	}

	return getVmsResp.Payload, nil
}

// GetVMNics searches for nics by virtual machines id.
func (svr *TowerVMService) GetVMNics(vmID string) ([]*models.VMNic, error) {
	getVMNicsParams := clientvmnic.NewGetVMNicsParams()
	getVMNicsParams.RequestBody = &models.GetVMNicsRequestBody{
		Where: &models.VMNicWhereInput{
			VM: &models.VMWhereInput{
				OR: []*models.VMWhereInput{{LocalID: TowerString(vmID)}, {ID: TowerString(vmID)}},
			},
		},
		OrderBy: models.NewVMNicOrderByInput(models.VMNicOrderByInputOrderASC),
	}

	getVMNicsResp, err := svr.Session.VMNic.GetVMNics(getVMNicsParams)
	if err != nil {
		return nil, err
	}

	return getVMNicsResp.Payload, nil
}

func (svr *TowerVMService) AddVMNics(vmID string, nics []*models.VMNicParams) (*models.WithTaskVM, error) {
	addVMNicParams := clientvm.NewAddVMNicParams()
	addVMNicParams.RequestBody = &models.VMAddNicParams{
		Data: &models.VMAddNicParamsData{
			VMNics: nics,
		},
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: TowerString(vmID)}, {ID: TowerString(vmID)}},
		},
	}

	addVMNicResp, err := svr.Session.VM.AddVMNic(addVMNicParams)
	if err != nil {
		return nil, err
	}

	if len(addVMNicResp.Payload) == 0 {
		return nil, errors.New(VMNicFound)
	}

	return addVMNicResp.Payload[0], nil
}

// GetCluster searches for a cluster.
func (svr *TowerVMService) GetCluster(id string) (*models.Cluster, error) {
	getClustersParams := clientcluster.NewGetClustersParams()
	getClustersParams.RequestBody = &models.GetClustersRequestBody{
		Where: &models.ClusterWhereInput{
			OR: []*models.ClusterWhereInput{{LocalID: TowerString(id)}, {ID: TowerString(id)}},
		},
	}

	getClustersResp, err := svr.Session.Cluster.GetClusters(getClustersParams)
	if err != nil {
		return nil, err
	}

	if len(getClustersResp.Payload) == 0 {
		return nil, errors.New(ClusterNotFound)
	}

	return getClustersResp.Payload[0], nil
}

func (svr *TowerVMService) GetClusterZones(clusterID string) ([]*models.Zone, error) {
	getZonesParams := clientzone.NewGetZonesParams()
	getZonesParams.RequestBody = &models.GetZonesRequestBody{
		Where: &models.ZoneWhereInput{
			Cluster: &models.ClusterWhereInput{
				OR: []*models.ClusterWhereInput{{LocalID: TowerString(clusterID)}, {ID: TowerString(clusterID)}},
			},
		},
	}

	getZonesResp, err := svr.Session.Zone.GetZones(getZonesParams)
	if err != nil {
		return nil, err
	}

	return getZonesResp.Payload, nil
}

func (svr *TowerVMService) GetHost(id string) (*models.Host, error) {
	getHostsParams := clienthost.NewGetHostsParams()
	getHostsParams.RequestBody = &models.GetHostsRequestBody{
		Where: &models.HostWhereInput{
			OR: []*models.HostWhereInput{{LocalID: TowerString(id)}, {ID: TowerString(id)}},
		},
	}

	getHostsResp, err := svr.Session.Host.GetHosts(getHostsParams)
	if err != nil {
		return nil, err
	}

	if len(getHostsResp.Payload) == 0 {
		return nil, errors.New(HostNotFound)
	}

	return getHostsResp.Payload[0], nil
}

func (svr *TowerVMService) GetHostsByCluster(clusterID string) (Hosts, error) {
	getHostsParams := clienthost.NewGetHostsParams()
	getHostsParams.RequestBody = &models.GetHostsRequestBody{
		Where: &models.HostWhereInput{
			Cluster: &models.ClusterWhereInput{
				OR: []*models.ClusterWhereInput{{LocalID: TowerString(clusterID)}, {ID: TowerString(clusterID)}},
			},
		},
	}

	getHostsResp, err := svr.Session.Host.GetHosts(getHostsParams)
	if err != nil {
		return nil, err
	}

	if len(getHostsResp.Payload) == 0 {
		return nil, errors.New(HostNotFound)
	}

	return NewHostsFromList(getHostsResp.Payload), nil
}

// GetVlan searches for a vlan.
func (svr *TowerVMService) GetVlan(id string) (*models.Vlan, error) {
	getVlansParams := clientvlan.NewGetVlansParams()
	getVlansParams.RequestBody = &models.GetVlansRequestBody{
		Where: &models.VlanWhereInput{
			OR: []*models.VlanWhereInput{{LocalID: TowerString(id)}, {ID: TowerString(id)}},
		},
	}

	getVlansResp, err := svr.Session.Vlan.GetVlans(getVlansParams)
	if err != nil {
		return nil, err
	}

	if len(getVlansResp.Payload) == 0 {
		return nil, errors.New(VlanNotFound)
	}

	return getVlansResp.Payload[0], nil
}

// GetVMTemplate searches for a virtual machine template.
// 1.0 or earlier versions use the template ID or name to find the virtual machine template,
// and other versions prefer to use SKSVMTemplateUIDLabel to find the virtual machine template.
func (svr *TowerVMService) GetVMTemplate(template string) (*models.ContentLibraryVMTemplate, error) {
	getVMTemplatesParams := clientvmtemplate.NewGetContentLibraryVMTemplatesParams()
	getVMTemplatesParams.RequestBody = &models.GetContentLibraryVMTemplatesRequestBody{
		Where: &models.ContentLibraryVMTemplateWhereInput{
			OR: []*models.ContentLibraryVMTemplateWhereInput{
				{ID: TowerString(template)},
				{Name: TowerString(template)},
				{LabelsSome: &models.LabelWhereInput{
					AND: []*models.LabelWhereInput{
						{Key: TowerString(SKSVMTemplateUIDLabel), Value: TowerString(template)},
					},
				}},
			},
		},
	}

	getVMTemplatesResp, err := svr.Session.ContentLibraryVMTemplate.GetContentLibraryVMTemplates(getVMTemplatesParams)
	if err != nil {
		return nil, err
	}

	vmTemplates := getVMTemplatesResp.Payload
	if len(vmTemplates) == 0 {
		return nil, errors.New(VMTemplateNotFound)
	}

	for i := range vmTemplates {
		// Match SKSVMTemplateUIDLabel.
		if template != *vmTemplates[i].ID && template != *vmTemplates[i].Name {
			return vmTemplates[i], nil
		}
	}

	return vmTemplates[0], nil
}

// GetTask searches for a task.
func (svr *TowerVMService) GetTask(id string) (*models.Task, error) {
	getTasksParams := clienttask.NewGetTasksParams()
	getTasksParams.RequestBody = &models.GetTasksRequestBody{
		Where: &models.TaskWhereInput{
			ID: TowerString(id),
		},
	}

	getTasksResp, err := svr.Session.Task.GetTasks(getTasksParams)
	if err != nil {
		return nil, err
	}

	if len(getTasksResp.Payload) == 0 {
		return nil, errors.New(TaskNotFound)
	}

	return getTasksResp.Payload[0], nil
}

// WaitTask waits for task to complete and returns task.
func (svr *TowerVMService) WaitTask(ctx goctx.Context, id string, timeout, interval time.Duration) (*models.Task, error) {
	var task *models.Task
	err := wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx goctx.Context) (bool, error) {
		var err error
		task, err = svr.GetTask(id)
		if err != nil {
			return false, err
		}

		if *task.Status == models.TaskStatusFAILED || *task.Status == models.TaskStatusSUCCESSED {
			return true, err
		}

		return false, err
	})

	if err != nil {
		return nil, err
	}

	return task, err
}

// UpsertLabel upserts a label.
func (svr *TowerVMService) UpsertLabel(key, value string) (*models.Label, error) {
	getLabelParams := clientlabel.NewGetLabelsParams()
	getLabelParams.RequestBody = &models.GetLabelsRequestBody{
		Where: &models.LabelWhereInput{
			Key:   TowerString(key),
			Value: TowerString(value),
		},
	}
	getLabelResp, err := svr.Session.Label.GetLabels(getLabelParams)
	if err != nil {
		return nil, err
	}
	if len(getLabelResp.Payload) > 0 {
		return getLabelResp.Payload[0], nil
	}

	createLabelParams := clientlabel.NewCreateLabelParams()
	createLabelParams.RequestBody = []*models.LabelCreationParams{
		{Key: &key, Value: &value},
	}
	createLabelResp, err := svr.Session.Label.CreateLabel(createLabelParams)
	if err != nil {
		return nil, err
	}
	if len(createLabelResp.Payload) == 0 {
		return nil, errors.New(LabelCreateFailed)
	}

	return createLabelResp.Payload[0].Data, nil
}

// DeleteLabel deletes a label.
// If strict is false, delete the label directly.
// If strict is true, delete the label only if no virtual machine references the label.
func (svr *TowerVMService) DeleteLabel(key, value string, strict bool) (string, error) {
	deleteLabelParams := clientlabel.NewDeleteLabelParams()
	deleteLabelParams.RequestBody = &models.LabelDeletionParams{
		Where: &models.LabelWhereInput{
			AND: []*models.LabelWhereInput{
				{Key: TowerString(key), Value: TowerString(value)},
			},
		},
	}
	if strict {
		deleteLabelParams.RequestBody.Where.AND = append(
			deleteLabelParams.RequestBody.Where.AND,
			&models.LabelWhereInput{TotalNum: TowerInt32(0)},
		)
	}

	deleteLabelResp, err := svr.Session.Label.DeleteLabel(deleteLabelParams)
	if err != nil {
		return "", err
	}

	if len(deleteLabelResp.Payload) == 0 {
		return "", nil
	}

	return *deleteLabelResp.Payload[0].Data.ID, nil
}

// CleanUnusedLabels deletes specified unused labels.
// CleanUnusedLabels is used to clean unused labels regularly and should not be called frequently.
func (svr *TowerVMService) CleanUnusedLabels(keys []string) ([]string, error) {
	deleteLabelParams := clientlabel.NewDeleteLabelParams()
	deleteLabelParams.RequestBody = &models.LabelDeletionParams{
		Where: &models.LabelWhereInput{
			KeyIn:        keys,
			CreatedAtLte: TowerString(time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)),
			TotalNum:     TowerInt32(0),
		},
	}

	deleteLabelResp, err := svr.Session.Label.DeleteLabel(deleteLabelParams)
	if err != nil {
		return nil, err
	}

	labelIDs := make([]string, len(deleteLabelResp.Payload))
	for i := range len(deleteLabelResp.Payload) {
		labelIDs[i] = *deleteLabelResp.Payload[i].Data.ID
	}

	return labelIDs, nil
}

// AddLabelsToVM adds a label to a VM.
func (svr *TowerVMService) AddLabelsToVM(vmID string, labelIds []string) (*models.Task, error) {
	addLabelsParams := clientlabel.NewAddLabelsToResourcesParams()
	addLabelsParams.RequestBody = &models.AddLabelsToResourcesParams{
		Where: &models.LabelWhereInput{
			IDIn: labelIds,
		},
		Data: &models.AddLabelsToResourcesParamsData{
			Vms: &models.VMWhereInput{
				ID: TowerString(vmID),
			},
		},
	}
	addLabelsResp, err := svr.Session.Label.AddLabelsToResources(addLabelsParams)
	if err != nil {
		return nil, err
	}
	if len(addLabelsResp.Payload) == 0 {
		return nil, errors.New(LabelAddFailed)
	}
	return &models.Task{ID: addLabelsResp.Payload[0].TaskID}, nil
}

// CreateVMPlacementGroup creates a new vm placement group.
func (svr *TowerVMService) CreateVMPlacementGroup(name, clusterID string, vmPolicy models.VMVMPolicy) (*models.WithTaskVMPlacementGroup, error) {
	createVMPlacementGroupParams := clientvmplacementgroup.NewCreateVMPlacementGroupParams()
	createVMPlacementGroupParams.RequestBody = []*models.VMPlacementGroupCreationParams{{
		Name:                TowerString(name),
		ClusterID:           TowerString(clusterID),
		Enabled:             TowerBool(true),
		Description:         TowerString(VMPlacementGroupDescription),
		VMHostMustEnabled:   TowerBool(false),
		VMHostPreferEnabled: TowerBool(false),
		VMVMPolicyEnabled:   TowerBool(true),
		VMVMPolicy:          &vmPolicy,
	}}
	createVMPlacementGroupResp, err := svr.Session.VMPlacementGroup.CreateVMPlacementGroup(createVMPlacementGroupParams)
	if err != nil {
		return nil, err
	}

	return createVMPlacementGroupResp.Payload[0], nil
}

// GetVMPlacementGroup searches for a vm placement group by name.
func (svr *TowerVMService) GetVMPlacementGroup(name string) (*models.VMPlacementGroup, error) {
	getVMPlacementGroupsParams := clientvmplacementgroup.NewGetVMPlacementGroupsParams()
	getVMPlacementGroupsParams.RequestBody = &models.GetVMPlacementGroupsRequestBody{
		Where: &models.VMPlacementGroupWhereInput{
			Name: TowerString(name),
		},
	}

	getVMPlacementGroupsResp, err := svr.Session.VMPlacementGroup.GetVMPlacementGroups(getVMPlacementGroupsParams)
	if err != nil {
		return nil, err
	}

	if len(getVMPlacementGroupsResp.Payload) == 0 {
		return nil, errors.New(VMPlacementGroupNotFound)
	}

	return getVMPlacementGroupsResp.Payload[0], nil
}

func (svr *TowerVMService) AddVMsToPlacementGroup(placementGroup *models.VMPlacementGroup, vmIDs []string) (*models.Task, error) {
	updateVMPlacementGroupParams := clientvmplacementgroup.NewUpdateVMPlacementGroupParams()
	updateVMPlacementGroupParams.RequestBody = &models.VMPlacementGroupUpdationParams{
		Data: &models.VMPlacementGroupUpdationParamsData{
			Enabled:             placementGroup.Enabled,
			Description:         placementGroup.Description,
			VMHostMustEnabled:   placementGroup.VMHostMustEnabled,
			VMHostMustPolicy:    placementGroup.VMHostMustPolicy,
			VMHostPreferEnabled: placementGroup.VMHostPreferEnabled,
			VMHostPreferPolicy:  placementGroup.VMHostPreferPolicy,
			VMVMPolicyEnabled:   placementGroup.VMVMPolicyEnabled,
			VMVMPolicy:          placementGroup.VMVMPolicy,
			Vms:                 &models.VMWhereInput{IDIn: vmIDs},
		},
		Where: &models.VMPlacementGroupWhereInput{
			ID: TowerString(*placementGroup.ID),
		},
	}

	updateVMPlacementGroupResp, err := svr.Session.VMPlacementGroup.UpdateVMPlacementGroup(updateVMPlacementGroupParams)
	if err != nil {
		return nil, err
	}

	return &models.Task{ID: updateVMPlacementGroupResp.Payload[0].TaskID}, nil
}

// DeleteVMPlacementGroupByID deletes placement group by id.
//
// The return value:
// 1. true indicates that the specified placement group have been deleted.
// 2. false indicates that the specified placement group being deleted.
func (svr *TowerVMService) DeleteVMPlacementGroupByID(ctx goctx.Context, id string) (bool, error) {
	getVMPlacementGroupsParams := clientvmplacementgroup.NewGetVMPlacementGroupsParams()
	getVMPlacementGroupsParams.RequestBody = &models.GetVMPlacementGroupsRequestBody{
		Where: &models.VMPlacementGroupWhereInput{
			ID: TowerString(id),
		},
	}

	getVMPlacementGroupsResp, err := svr.Session.VMPlacementGroup.GetVMPlacementGroups(getVMPlacementGroupsParams)
	if err != nil {
		return false, err
	}

	if len(getVMPlacementGroupsResp.Payload) == 0 {
		return true, nil
	} else if getVMPlacementGroupsResp.Payload[0].EntityAsyncStatus != nil {
		return false, nil
	}

	deleteVMPlacementGroupParams := clientvmplacementgroup.NewDeleteVMPlacementGroupParams()
	deleteVMPlacementGroupParams.RequestBody = &models.VMPlacementGroupDeletionParams{
		Where: &models.VMPlacementGroupWhereInput{
			ID: TowerString(id),
		},
	}

	if _, err := svr.Session.VMPlacementGroup.DeleteVMPlacementGroup(deleteVMPlacementGroupParams); err != nil {
		return false, err
	}

	return false, nil
}

// DeleteVMPlacementGroupsByNamePrefix deletes placement groups by name prefix.
//
// The return value:
// 1. Empty string array indicates that all specified placements have been deleted.
// 2. Non-empty string array indicates that the names of the placement groups being deleted.
func (svr *TowerVMService) DeleteVMPlacementGroupsByNamePrefix(ctx goctx.Context, namePrefix string) ([]string, error) {
	// Deleting placement groups in batches, Tower will create a deletion task
	// for each placement group.
	// Some tasks may fail, and failed tasks need to be deleted again.
	// Therefore, need to query and confirm whether all placement groups have been deleted.
	getVMPlacementGroupsParams := clientvmplacementgroup.NewGetVMPlacementGroupsParams()
	getVMPlacementGroupsParams.RequestBody = &models.GetVMPlacementGroupsRequestBody{
		Where: &models.VMPlacementGroupWhereInput{
			NameStartsWith: TowerString(namePrefix),
		},
	}

	getVMPlacementGroupsResp, err := svr.Session.VMPlacementGroup.GetVMPlacementGroups(getVMPlacementGroupsParams)
	if err != nil {
		return nil, err
	} else if len(getVMPlacementGroupsResp.Payload) == 0 {
		return nil, nil
	}

	deleteVMPlacementGroupParams := clientvmplacementgroup.NewDeleteVMPlacementGroupParams()
	deleteVMPlacementGroupParams.RequestBody = &models.VMPlacementGroupDeletionParams{
		Where: &models.VMPlacementGroupWhereInput{
			NameStartsWith:       TowerString(namePrefix),
			EntityAsyncStatusNot: nil,
		},
	}

	deleteVMPlacementGroupResp, err := svr.Session.VMPlacementGroup.DeleteVMPlacementGroup(deleteVMPlacementGroupParams)
	if err != nil {
		return nil, err
	}

	pgNames := make([]string, len(getVMPlacementGroupsResp.Payload))
	for i := range len(getVMPlacementGroupsResp.Payload) {
		pgNames[i] = *getVMPlacementGroupsResp.Payload[i].Name
	}

	if len(deleteVMPlacementGroupResp.Payload) == 0 {
		return pgNames, nil
	}

	// With consecutive calls to DeleteVMPlacementGroupsByNamePrefix (e.g. a short reconcile interval),
	// Tower may create duplicate tasks for deleting placement groups.
	// Wait for the first deletion task to complete or timeout,
	// can increase the interval between calls to DeleteVMPlacementGroupsByNamePrefix
	// to reduce the probability of duplicate deletion tasks.
	taskID := *deleteVMPlacementGroupResp.Payload[0].TaskID
	withLatestStatusTask, err := svr.WaitTask(ctx, taskID, config.WaitTaskTimeoutForPlacementGroupOperation, config.WaitTaskInterval)
	if err != nil {
		return pgNames, errors.Wrapf(err, "failed to wait for placement groups with name prefix %s deleting task to complete in %s: taskID %s", namePrefix, config.WaitTaskTimeoutForPlacementGroupOperation, taskID)
	}

	if *withLatestStatusTask.Status == models.TaskStatusFAILED {
		return pgNames, errors.Errorf("failed to delete placement groups with name prefix %s in task %s", namePrefix, *withLatestStatusTask.ID)
	}

	return pgNames, nil
}

// GetGPUDevicesAllocationInfoByIDs returns the specified GPU devices with VMs and allocation details.
func (svr *TowerVMService) GetGPUDevicesAllocationInfoByIDs(gpuIDs []string) (GPUVMInfos, error) {
	if len(gpuIDs) == 0 {
		return NewGPUVMInfos(), nil
	}

	getDetailVMInfoByGpuDevicesParams := clientgpu.NewGetDetailVMInfoByGpuDevicesParams()
	getDetailVMInfoByGpuDevicesParams.RequestBody = &models.GetGpuDevicesRequestBody{
		Where: &models.GpuDeviceWhereInput{
			IDIn: gpuIDs,
		},
	}

	getDetailVMInfoByGpuDevicesResp, err := svr.Session.GpuDevice.GetDetailVMInfoByGpuDevices(getDetailVMInfoByGpuDevicesParams)
	if err != nil {
		return nil, err
	}

	return NewGPUVMInfosFromList(getDetailVMInfoByGpuDevicesResp.Payload), nil
}

// GetGPUDevicesAllocationInfoByHostIDs returns the GPU devices of specified hosts with VMs and allocation details.
func (svr *TowerVMService) GetGPUDevicesAllocationInfoByHostIDs(hostIDs []string, gpuDeviceUsage models.GpuDeviceUsage) (GPUVMInfos, error) {
	if len(hostIDs) == 0 {
		return NewGPUVMInfos(), nil
	}

	getDetailVMInfoByGpuDevicesParams := clientgpu.NewGetDetailVMInfoByGpuDevicesParams()
	getDetailVMInfoByGpuDevicesParams.RequestBody = &models.GetGpuDevicesRequestBody{
		Where: &models.GpuDeviceWhereInput{
			UserUsage: models.NewGpuDeviceUsage(gpuDeviceUsage),
			Host: &models.HostWhereInput{
				IDIn: hostIDs,
			},
		},
	}

	// Filter GPU devices whose vGPU has been fully used
	if gpuDeviceUsage == models.GpuDeviceUsageVGPU {
		getDetailVMInfoByGpuDevicesParams.RequestBody.Where.AvailableVgpusNumGt = TowerInt32(0)
	}

	getDetailVMInfoByGpuDevicesResp, err := svr.Session.GpuDevice.GetDetailVMInfoByGpuDevices(getDetailVMInfoByGpuDevicesParams)
	if err != nil {
		return nil, err
	}

	return NewGPUVMInfosFromList(getDetailVMInfoByGpuDevicesResp.Payload), nil
}

// GetVMGPUAllocationInfo returns the GPU details allocated to the virtual machine.
func (svr *TowerVMService) GetVMGPUAllocationInfo(id string) (*models.VMGpuInfo, error) {
	getVMGpuDeviceInfoParams := clientvm.NewGetVMGpuDeviceInfoParams()
	getVMGpuDeviceInfoParams.RequestBody = &models.GetVmsRequestBody{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: TowerString(id)}, {ID: TowerString(id)}},
		},
	}

	getVMGpuDeviceInfoResp, err := svr.Session.VM.GetVMGpuDeviceInfo(getVMGpuDeviceInfoParams)
	if err != nil {
		return nil, err
	}

	if len(getVMGpuDeviceInfoResp.Payload) == 0 {
		return nil, errors.New(VMGPUInfoNotFound)
	}

	return getVMGpuDeviceInfoResp.Payload[0], nil
}
