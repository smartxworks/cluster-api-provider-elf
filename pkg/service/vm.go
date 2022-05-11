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
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	clientcluster "github.com/smartxworks/cloudtower-go-sdk/client/cluster"
	clienthost "github.com/smartxworks/cloudtower-go-sdk/client/host"
	clienttask "github.com/smartxworks/cloudtower-go-sdk/client/task"
	clientvlan "github.com/smartxworks/cloudtower-go-sdk/client/vlan"
	clientvm "github.com/smartxworks/cloudtower-go-sdk/client/vm"
	clientvmtemplate "github.com/smartxworks/cloudtower-go-sdk/client/vm_template"
	"github.com/smartxworks/cloudtower-go-sdk/models"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/session"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/util"
)

type VMService interface {
	Clone(elfCluster *infrav1.ElfCluster,
		machine *clusterv1.Machine,
		elfMachine *infrav1.ElfMachine,
		bootstrapData string) (*models.WithTaskVM, error)
	Delete(uuid string) (*models.Task, error)
	PowerOff(uuid string) (*models.Task, error)
	PowerOn(uuid string) (*models.Task, error)
	Get(id string) (*models.VM, error)
	GetByName(name string) (*models.VM, error)
	GetVMTemplate(templateUUID string) (*models.VMTemplate, error)
	GetTask(id string) (*models.Task, error)
	GetCluster(id string) (*models.Cluster, error)
	GetHost(id string) (*models.Host, error)
	GetVlan(id string) (*models.Vlan, error)
}

func NewVMService(auth infrav1.Tower, logger logr.Logger) (VMService, error) {
	authSession, err := session.NewTowerSession(auth)
	if err != nil {
		return nil, err
	}

	return &TowerVMService{authSession, logger}, nil
}

type TowerVMService struct {
	Session *session.TowerSession `json:"session"`
	Logger  logr.Logger           `json:"logger"`
}

// Clone kicks off a clone operation on Elf to create a new virtual machine using VM template.
func (svr *TowerVMService) Clone(
	elfCluster *infrav1.ElfCluster,
	machine *clusterv1.Machine,
	elfMachine *infrav1.ElfMachine,
	bootstrapData string) (*models.WithTaskVM, error) {
	cluster, err := svr.GetCluster(elfCluster.Spec.Cluster)
	if err != nil {
		return nil, err
	}

	template, err := svr.GetVMTemplate(elfMachine.Spec.Template)
	if err != nil {
		return nil, err
	}

	vlan, err := svr.GetVlan(elfMachine.Spec.Network.Vlan)
	if err != nil {
		return nil, err
	}

	numCPUs := elfMachine.Spec.NumCPUs
	if numCPUs <= 0 {
		numCPUs = config.VMNumCPUs
	}
	numCoresPerSocket := elfMachine.Spec.NumCoresPerSocket
	if numCoresPerSocket <= 0 {
		numCoresPerSocket = numCPUs
	}
	numCPUSockets := numCPUs / numCoresPerSocket

	memoryMiB := elfMachine.Spec.MemoryMiB
	if memoryMiB <= 0 {
		memoryMiB = config.VMMemoryMiB
	}

	diskGiB := elfMachine.Spec.DiskGiB
	if diskGiB <= 0 {
		diskGiB = config.VMDiskGiB
	}

	storagePolicy := models.VMVolumeElfStoragePolicyTypeREPLICA3THICKPROVISION
	bus := models.BusVIRTIO
	mountDisks := []*models.MountNewCreateDisksParams{{
		// Index: util.TowerInt32(0),
		Boot: util.TowerInt32(0),
		Bus:  &bus,
		VMVolume: &models.MountNewCreateDisksParamsVMVolume{
			ElfStoragePolicy: &storagePolicy,
			Name:             util.TowerString(config.VMDiskName),
			Size:             util.TowerDisk(diskGiB),
		},
	}}

	networks := []*models.VMCreateVMFromTemplateParamsCloudInitNetworksItems0{}
	for i := 0; i < len(elfMachine.Spec.Network.Devices); i++ {
		device := elfMachine.Spec.Network.Devices[i]

		networkType := models.CloudInitNetworkTypeEnumIPV4DHCP
		if strings.ToUpper(device.NetworkType) == string(models.CloudInitNetworkTypeEnumIPV4) {
			networkType = models.CloudInitNetworkTypeEnumIPV4
		}

		ipAddress := ""
		if len(device.IPAddrs) > 0 {
			ipAddress = device.IPAddrs[0]
		}

		networks = append(networks, &models.VMCreateVMFromTemplateParamsCloudInitNetworksItems0{
			NicIndex:  util.TowerInt32(device.NetworkIndex),
			Type:      &networkType,
			IPAddress: util.TowerString(ipAddress),
			Netmask:   util.TowerString(device.Netmask),
		})
	}

	isFullCopy := false
	if elfMachine.Spec.CloneMode == infrav1.FullClone {
		isFullCopy = true
	}

	hostID := "AUTO_SCHEDULE"
	if elfMachine.Spec.Host != "" {
		host, err := svr.GetHost(elfMachine.Spec.Host)
		if err != nil {
			return nil, err
		}

		hostID = *host.ID
	}

	vmCreateVMFromTemplateParams := &models.VMCreateVMFromTemplateParams{
		ClusterID:   cluster.ID,
		HostID:      util.TowerString(hostID),
		Name:        util.TowerString(machine.Name),
		Description: util.TowerString(config.VMDescription),
		Vcpu:        util.TowerCPU(numCPUs),
		CPUCores:    util.TowerCPU(numCoresPerSocket),
		CPUSockets:  util.TowerCPU(numCPUSockets),
		Memory:      util.TowerMemory(memoryMiB),
		Firmware:    models.NewVMFirmware(models.VMFirmwareBIOS),
		Status:      models.NewVMStatus(models.VMStatusRUNNING),
		Ha:          util.TowerBool(elfMachine.Spec.HA),
		IsFullCopy:  util.TowerBool(isFullCopy),
		TemplateID:  template.ID,
		VMNics: []*models.VMNicParams{{
			Model:         models.NewVMNicModel(models.VMNicModelVIRTIO),
			Enabled:       util.TowerBool(true),
			Mirror:        util.TowerBool(false),
			ConnectVlanID: vlan.ID,
		}},
		DiskOperate: &models.VMCreateVMFromTemplateParamsDiskOperate{
			NewDisks: &models.VMDiskParams{
				MountNewCreateDisks: mountDisks,
			},
		},
		CloudInit: &models.VMCreateVMFromTemplateParamsCloudInit{
			Hostname:            util.TowerString(elfMachine.Name),
			DefaultUserPassword: util.TowerString(config.VMPassword),
			UserData:            util.TowerString(bootstrapData),
			Networks:            networks,
		},
	}

	createVMFromTemplateParams := clientvm.NewCreateVMFromTemplateParams()
	createVMFromTemplateParams.RequestBody = []*models.VMCreateVMFromTemplateParams{vmCreateVMFromTemplateParams}
	createVMFromTemplateResp, err := svr.Session.VM.CreateVMFromTemplate(createVMFromTemplateParams)
	if err != nil {
		return nil, err
	}

	return createVMFromTemplateResp.Payload[0], nil
}

// Delete destroys a virtual machine.
func (svr *TowerVMService) Delete(uuid string) (*models.Task, error) {
	deleteVMParams := clientvm.NewDeleteVMParams()
	deleteVMParams.RequestBody = &models.VMOperateParams{
		Where: &models.VMWhereInput{LocalID: util.TowerString(uuid)},
	}

	deleteVMResp, err := svr.Session.VM.DeleteVM(deleteVMParams)
	if err != nil {
		return nil, err
	}

	return &models.Task{ID: deleteVMResp.Payload[0].TaskID}, nil
}

// PowerOff powers off a virtual machine.
func (svr *TowerVMService) PowerOff(uuid string) (*models.Task, error) {
	shutDownVMParams := clientvm.NewShutDownVMParams()
	shutDownVMParams.RequestBody = &models.VMOperateParams{
		Where: &models.VMWhereInput{LocalID: util.TowerString(uuid)},
	}

	shutDownVMResp, err := svr.Session.VM.ShutDownVM(shutDownVMParams)
	if err != nil {
		return nil, err
	}

	return &models.Task{ID: shutDownVMResp.Payload[0].TaskID}, nil
}

// PowerOn powers on a virtual machine.
func (svr *TowerVMService) PowerOn(uuid string) (*models.Task, error) {
	startVMParams := clientvm.NewStartVMParams()
	startVMParams.RequestBody = &models.VMStartParams{
		Where: &models.VMWhereInput{LocalID: util.TowerString(uuid)},
	}

	startVMResp, err := svr.Session.VM.StartVM(startVMParams)
	if err != nil {
		return nil, err
	}

	return &models.Task{ID: startVMResp.Payload[0].TaskID}, nil
}

// Get searches for a virtual machine.
func (svr *TowerVMService) Get(id string) (*models.VM, error) {
	getVmsParams := clientvm.NewGetVmsParams()
	getVmsParams.RequestBody = &models.GetVmsRequestBody{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: util.TowerString(id)}, {ID: util.TowerString(id)}},
		},
	}

	getVmsResp, err := svr.Session.VM.GetVms(getVmsParams)
	if err != nil {
		return nil, err
	}

	if len(getVmsResp.Payload) == 0 {
		return nil, errors.New("VM_NOT_FOUND")
	}

	return getVmsResp.Payload[0], nil
}

// GetByName searches for a virtual machine by name.
func (svr *TowerVMService) GetByName(name string) (*models.VM, error) {
	getVmsParams := clientvm.NewGetVmsParams()
	getVmsParams.RequestBody = &models.GetVmsRequestBody{
		Where: &models.VMWhereInput{
			Name: util.TowerString(name),
		},
	}

	getVmsResp, err := svr.Session.VM.GetVms(getVmsParams)
	if err != nil {
		return nil, err
	}

	if len(getVmsResp.Payload) == 0 {
		return nil, errors.New("VM_NOT_FOUND")
	}

	return getVmsResp.Payload[0], nil
}

// GetCluster searches for a cluster.
func (svr *TowerVMService) GetCluster(id string) (*models.Cluster, error) {
	getClustersParams := clientcluster.NewGetClustersParams()
	getClustersParams.RequestBody = &models.GetClustersRequestBody{
		Where: &models.ClusterWhereInput{
			OR: []*models.ClusterWhereInput{{LocalID: util.TowerString(id)}, {ID: util.TowerString(id)}},
		},
	}

	getClustersResp, err := svr.Session.Cluster.GetClusters(getClustersParams)
	if err != nil {
		return nil, err
	}

	if len(getClustersResp.Payload) == 0 {
		return nil, errors.New("CLUSTER_NOT_FOUND")
	}

	return getClustersResp.Payload[0], nil
}

func (svr *TowerVMService) GetHost(id string) (*models.Host, error) {
	getHostsParams := clienthost.NewGetHostsParams()
	getHostsParams.RequestBody = &models.GetHostsRequestBody{
		Where: &models.HostWhereInput{
			OR: []*models.HostWhereInput{{LocalID: util.TowerString(id)}, {ID: util.TowerString(id)}},
		},
	}

	getHostsResp, err := svr.Session.Host.GetHosts(getHostsParams)
	if err != nil {
		return nil, err
	}

	if len(getHostsResp.Payload) == 0 {
		return nil, errors.New("HOST_NOT_FOUND")
	}

	return getHostsResp.Payload[0], nil
}

// GetVlan searches for a vlan.
func (svr *TowerVMService) GetVlan(id string) (*models.Vlan, error) {
	getVlansParams := clientvlan.NewGetVlansParams()
	getVlansParams.RequestBody = &models.GetVlansRequestBody{
		Where: &models.VlanWhereInput{
			LocalID: util.TowerString(id),
		},
	}

	getVlansResp, err := svr.Session.Vlan.GetVlans(getVlansParams)
	if err != nil {
		return nil, err
	}

	if len(getVlansResp.Payload) == 0 {
		return nil, errors.New("VLAN_NOT_FOUND")
	}

	return getVlansResp.Payload[0], nil
}

// GetVMTemplate searches for a virtual machine template.
func (svr *TowerVMService) GetVMTemplate(templateUUID string) (*models.VMTemplate, error) {
	if _, err := uuid.Parse(templateUUID); err != nil {
		return nil, err
	}

	getVMTemplatesParams := clientvmtemplate.NewGetVMTemplatesParams()
	getVMTemplatesParams.RequestBody = &models.GetVMTemplatesRequestBody{
		Where: &models.VMTemplateWhereInput{
			LocalID: util.TowerString(templateUUID),
		},
	}
	getVMTemplatesResp, err := svr.Session.VMTemplate.GetVMTemplates(getVMTemplatesParams)
	if err != nil {
		return nil, err
	}

	vmTemplates := getVMTemplatesResp.Payload
	if len(vmTemplates) == 0 {
		return nil, errors.New("VM_TEMPLATE_NOT_FOUND")
	}

	return vmTemplates[0], nil
}

// GetTask searches for a task.
func (svr *TowerVMService) GetTask(id string) (*models.Task, error) {
	getTasksParams := clienttask.NewGetTasksParams()
	getTasksParams.RequestBody = &models.GetTasksRequestBody{
		Where: &models.TaskWhereInput{
			ID: util.TowerString(id),
		},
	}

	getTasksResp, err := svr.Session.Task.GetTasks(getTasksParams)
	if err != nil {
		return nil, err
	}

	if len(getTasksResp.Payload) == 0 {
		return nil, errors.New("TASK_NOT_FOUND")
	}

	return getTasksResp.Payload[0], nil
}
