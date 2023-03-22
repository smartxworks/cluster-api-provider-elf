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
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	clientcluster "github.com/smartxworks/cloudtower-go-sdk/v2/client/cluster"
	clientvmtemplate "github.com/smartxworks/cloudtower-go-sdk/v2/client/content_library_vm_template"
	clienthost "github.com/smartxworks/cloudtower-go-sdk/v2/client/host"
	clientlabel "github.com/smartxworks/cloudtower-go-sdk/v2/client/label"
	clienttask "github.com/smartxworks/cloudtower-go-sdk/v2/client/task"
	clientvlan "github.com/smartxworks/cloudtower-go-sdk/v2/client/vlan"
	clientvm "github.com/smartxworks/cloudtower-go-sdk/v2/client/vm"
	clientvmplacementgroup "github.com/smartxworks/cloudtower-go-sdk/v2/client/vm_placement_group"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	"k8s.io/apimachinery/pkg/util/wait"
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
		bootstrapData, host string) (*models.WithTaskVM, error)
	Migrate(vmID, hostID string) (*models.WithTaskVM, error)
	Delete(uuid string) (*models.Task, error)
	PowerOff(uuid string) (*models.Task, error)
	PowerOn(uuid string) (*models.Task, error)
	ShutDown(uuid string) (*models.Task, error)
	Get(id string) (*models.VM, error)
	GetByName(name string) (*models.VM, error)
	FindByIDs(ids []string) ([]*models.VM, error)
	GetVMTemplate(id string) (*models.ContentLibraryVMTemplate, error)
	GetTask(id string) (*models.Task, error)
	WaitTask(id string, timeout, interval time.Duration) (*models.Task, error)
	GetCluster(id string) (*models.Cluster, error)
	GetHost(id string) (*models.Host, error)
	GetVlan(id string) (*models.Vlan, error)
	UpsertLabel(key, value string) (*models.Label, error)
	DeleteLabel(key, value string, strict bool) (string, error)
	AddLabelsToVM(vmID string, labels []string) (*models.Task, error)
	CreateVMPlacementGroup(name, clusterID string, vmPolicy models.VMVMPolicy) (*models.WithTaskVMPlacementGroup, error)
	GetVMPlacementGroup(name string) (*models.VMPlacementGroup, error)
	AddVMsToPlacementGroup(placementGroup *models.VMPlacementGroup, vmIDs []string) (*models.Task, error)
	DeleteVMPlacementGroupsByName(placementGroupName string) (*models.Task, error)
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

// Clone kicks off a clone operation on Elf to create a new virtual machine using VM template.
func (svr *TowerVMService) Clone(
	elfCluster *infrav1.ElfCluster,
	machine *clusterv1.Machine,
	elfMachine *infrav1.ElfMachine,
	bootstrapData, host string) (*models.WithTaskVM, error) {
	cluster, err := svr.GetCluster(elfCluster.Spec.Cluster)
	if err != nil {
		return nil, err
	}

	template, err := svr.GetVMTemplate(elfMachine.Spec.Template)
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

	var mountDisks []*models.MountNewCreateDisksParams
	if elfMachine.Spec.DiskGiB > 0 {
		storagePolicy := models.VMVolumeElfStoragePolicyTypeREPLICA2THINPROVISION
		bus := models.BusVIRTIO
		mountDisks = append(mountDisks, &models.MountNewCreateDisksParams{
			Boot: util.TowerInt32(0),
			Bus:  &bus,
			VMVolume: &models.MountNewCreateDisksParamsVMVolume{
				ElfStoragePolicy: &storagePolicy,
				Name:             util.TowerString(config.VMDiskName),
				Size:             util.TowerDisk(elfMachine.Spec.DiskGiB),
			},
		})
	}

	nics := make([]*models.VMNicParams, 0, len(elfMachine.Spec.Network.Devices))
	networks := make([]*models.CloudInitNetWork, 0, len(elfMachine.Spec.Network.Devices))
	for i := 0; i < len(elfMachine.Spec.Network.Devices); i++ {
		device := elfMachine.Spec.Network.Devices[i]

		// nics
		vlan, err := svr.GetVlan(device.Vlan)
		if err != nil {
			return nil, err
		}

		nics = append(nics, &models.VMNicParams{
			Model:         models.NewVMNicModel(models.VMNicModelVIRTIO),
			Enabled:       util.TowerBool(true),
			Mirror:        util.TowerBool(false),
			ConnectVlanID: vlan.ID,
			MacAddress:    util.TowerString(device.MACAddr),
		})

		if !device.HasNetworkType() {
			continue
		}

		// networks
		networkType := models.CloudInitNetworkTypeEnumIPV4DHCP
		if string(device.NetworkType) == string(models.CloudInitNetworkTypeEnumIPV4) {
			networkType = models.CloudInitNetworkTypeEnumIPV4
		}

		ipAddress := ""
		if len(device.IPAddrs) > 0 {
			ipAddress = device.IPAddrs[0]
		}

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
				Gateway: util.TowerString(route.Gateway),
				Netmask: util.TowerString(netmask),
				Network: util.TowerString(network),
			})
		}

		networks = append(networks, &models.CloudInitNetWork{
			NicIndex:  util.TowerInt32(i),
			Type:      &networkType,
			IPAddress: util.TowerString(ipAddress),
			Netmask:   util.TowerString(device.Netmask),
			Routes:    routes,
		})
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

	vmCreateVMFromTemplateParams := &models.VMCreateVMFromContentLibraryTemplateParams{
		ClusterID:   cluster.ID,
		HostID:      util.TowerString(hostID),
		Name:        util.TowerString(elfMachine.Name),
		Description: util.TowerString(config.VMDescription),
		Vcpu:        util.TowerCPU(numCPUs),
		CPUCores:    util.TowerCPU(numCoresPerSocket),
		CPUSockets:  util.TowerCPU(numCPUSockets),
		Memory:      util.TowerMemory(memoryMiB),
		Firmware:    models.NewVMFirmware(models.VMFirmwareBIOS),
		Status:      models.NewVMStatus(models.VMStatusSTOPPED),
		Ha:          util.TowerBool(elfMachine.Spec.HA),
		IsFullCopy:  util.TowerBool(isFullCopy),
		TemplateID:  template.ID,
		VMNics:      nics,
		DiskOperate: &models.VMDiskOperate{
			NewDisks: &models.VMDiskParams{
				MountNewCreateDisks: mountDisks,
			},
		},
		CloudInit: &models.TemplateCloudInit{
			Hostname:    util.TowerString(elfMachine.Name),
			UserData:    util.TowerString(bootstrapData),
			Networks:    networks,
			Nameservers: elfMachine.Spec.Network.Nameservers,
		},
	}

	createVMFromTemplateParams := clientvm.NewCreateVMFromContentLibraryTemplateParams()
	createVMFromTemplateParams.RequestBody = []*models.VMCreateVMFromContentLibraryTemplateParams{vmCreateVMFromTemplateParams}
	createVMFromTemplateResp, err := svr.Session.VM.CreateVMFromContentLibraryTemplate(createVMFromTemplateParams)
	if err != nil {
		return nil, err
	}

	return createVMFromTemplateResp.Payload[0], nil
}

func (svr *TowerVMService) Migrate(vmID, hostID string) (*models.WithTaskVM, error) {
	migrateVMParams := clientvm.NewMigrateVMParams()
	migrateVMParams.RequestBody = &models.VMMigrateParams{
		Data: &models.VMMigrateParamsData{
			HostID: util.TowerString(hostID),
		},
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: util.TowerString(vmID)}, {ID: util.TowerString(vmID)}},
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
	deleteVMParams.RequestBody = &models.VMOperateParams{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: util.TowerString(id)}, {ID: util.TowerString(id)}},
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
			OR: []*models.VMWhereInput{{LocalID: util.TowerString(id)}, {ID: util.TowerString(id)}},
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
func (svr *TowerVMService) PowerOn(id string) (*models.Task, error) {
	startVMParams := clientvm.NewStartVMParams()
	startVMParams.RequestBody = &models.VMStartParams{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: util.TowerString(id)}, {ID: util.TowerString(id)}},
		},
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
			OR: []*models.VMWhereInput{{LocalID: util.TowerString(id)}, {ID: util.TowerString(id)}},
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
		return nil, errors.New(VMNotFound)
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
		return nil, errors.New(ClusterNotFound)
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
		return nil, errors.New(HostNotFound)
	}

	return getHostsResp.Payload[0], nil
}

// GetVlan searches for a vlan.
func (svr *TowerVMService) GetVlan(id string) (*models.Vlan, error) {
	getVlansParams := clientvlan.NewGetVlansParams()
	getVlansParams.RequestBody = &models.GetVlansRequestBody{
		Where: &models.VlanWhereInput{
			OR: []*models.VlanWhereInput{{LocalID: util.TowerString(id)}, {ID: util.TowerString(id)}},
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
func (svr *TowerVMService) GetVMTemplate(id string) (*models.ContentLibraryVMTemplate, error) {
	getVMTemplatesParams := clientvmtemplate.NewGetContentLibraryVMTemplatesParams()
	getVMTemplatesParams.RequestBody = &models.GetContentLibraryVMTemplatesRequestBody{
		Where: &models.ContentLibraryVMTemplateWhereInput{
			OR: []*models.ContentLibraryVMTemplateWhereInput{{ID: util.TowerString(id)}, {Name: util.TowerString(id)}},
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
		return nil, errors.New(TaskNotFound)
	}

	return getTasksResp.Payload[0], nil
}

// WaitTask waits for task to complete and returns task.
func (svr *TowerVMService) WaitTask(id string, timeout, interval time.Duration) (*models.Task, error) {
	var task *models.Task
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
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
			Key:   util.TowerString(key),
			Value: util.TowerString(value),
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
				{Key: util.TowerString(key), Value: util.TowerString(value)},
			},
		},
	}
	if strict {
		deleteLabelParams.RequestBody.Where.AND = append(
			deleteLabelParams.RequestBody.Where.AND,
			&models.LabelWhereInput{VMNum: util.TowerInt32(0)},
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

// AddLabelsToVM adds a label to a VM.
func (svr *TowerVMService) AddLabelsToVM(vmID string, labelIds []string) (*models.Task, error) {
	addLabelsParams := clientlabel.NewAddLabelsToResourcesParams()
	addLabelsParams.RequestBody = &models.AddLabelsToResourcesParams{
		Where: &models.LabelWhereInput{
			IDIn: labelIds,
		},
		Data: &models.AddLabelsToResourcesParamsData{
			Vms: &models.VMWhereInput{
				ID: util.TowerString(vmID),
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
		Name:                util.TowerString(name),
		ClusterID:           util.TowerString(clusterID),
		Enabled:             util.TowerBool(true),
		Description:         util.TowerString(VMPlacementGroupDescription),
		VMHostMustEnabled:   util.TowerBool(false),
		VMHostPreferEnabled: util.TowerBool(false),
		VMVMPolicyEnabled:   util.TowerBool(true),
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
			Name: util.TowerString(name),
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
			ID: util.TowerString(*placementGroup.ID),
		},
	}

	updateVMPlacementGroupResp, err := svr.Session.VMPlacementGroup.UpdateVMPlacementGroup(updateVMPlacementGroupParams)
	if err != nil {
		return nil, err
	}

	return &models.Task{ID: updateVMPlacementGroupResp.Payload[0].TaskID}, nil
}

// DeleteVMPlacementGroupsByName deletes placement groups by name.
func (svr *TowerVMService) DeleteVMPlacementGroupsByName(placementGroupName string) (*models.Task, error) {
	deleteVMPlacementGroupParams := clientvmplacementgroup.NewDeleteVMPlacementGroupParams()
	deleteVMPlacementGroupParams.RequestBody = &models.VMPlacementGroupDeletionParams{
		Where: &models.VMPlacementGroupWhereInput{
			NameStartsWith: util.TowerString(placementGroupName),
		},
	}

	deleteVMPlacementGroupResp, err := svr.Session.VMPlacementGroup.DeleteVMPlacementGroup(deleteVMPlacementGroupParams)
	if err != nil {
		return nil, err
	}

	if len(deleteVMPlacementGroupResp.Payload) == 0 {
		return nil, nil
	}

	return &models.Task{ID: deleteVMPlacementGroupResp.Payload[0].TaskID}, nil
}
