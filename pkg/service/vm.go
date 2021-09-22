package service

import (
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/haijianyang/cloudtower-go-sdk/client/operations"
	"github.com/haijianyang/cloudtower-go-sdk/models"
	"github.com/pkg/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha4"
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

// Create VM using VM template.
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
	if elfMachine.Spec.MemoryMiB <= 0 {
		memoryMiB = config.VMMemoryMiB
	}

	diskGiB := elfMachine.Spec.DiskGiB
	if diskGiB <= 0 {
		diskGiB = config.VMDiskGiB
	}

	storagePolicy := models.VMVolumeElfStoragePolicyTypeREPLICA3THICKPROVISION
	bus := models.BusVIRTIO
	mountDisks := []*models.MountNewCreateDisksParamsItems0{{
		Index: float64(0),
		Boot:  util.TowerFloat64(0),
		Bus:   &bus,
		VMVolume: &models.MountNewCreateDisksParamsItems0VMVolume{
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
			NicIndex:  util.TowerFloat64(device.NetworkIndex),
			Type:      &networkType,
			IPAddress: ipAddress,
			Netmask:   device.Netmask,
		})
	}

	vmCreateVMFromTemplateParams := &models.VMCreateVMFromTemplateParams{
		ClusterID:   cluster.ID,
		Name:        util.TowerString(machine.Name),
		Description: config.VMDescription,
		Vcpu:        util.TowerCPU(numCPUs),
		CPUCores:    util.TowerCPU(numCoresPerSocket),
		CPUSockets:  util.TowerCPU(numCPUSockets),
		Memory:      util.TowerMemory(memoryMiB),
		Firmware:    models.VMFirmwareBIOS,
		Status:      models.VMStatusRUNNING,
		Ha:          elfMachine.Spec.HA,
		TemplateID:  template.ID,
		VMNics: []*models.VMNicParamsItems0{{
			Model:         models.VMNicModelVIRTIO,
			Enabled:       true,
			Mirror:        false,
			ConnectVlanID: vlan.ID,
		}},
		DiskOperate: &models.VMCreateVMFromTemplateParamsDiskOperate{
			NewDisks: &models.VMDiskParams{
				MountNewCreateDisks: mountDisks,
			},
		},
		CloudInit: &models.VMCreateVMFromTemplateParamsCloudInit{
			Hostname: elfMachine.Name,
			UserData: bootstrapData,
			Networks: networks,
		},
	}
	if elfMachine.Spec.AutoSchedule {
		vmCreateVMFromTemplateParams.HostID = "AUTO_SCHEDULE"
	}

	createVMFromTemplateParams := operations.NewCreateVMFromTemplateParams()
	createVMFromTemplateParams.RequestBody = []*models.VMCreateVMFromTemplateParams{vmCreateVMFromTemplateParams}
	createVMFromTemplateResp, err := svr.Session.CreateVMFromTemplate(createVMFromTemplateParams)
	if err != nil {
		return nil, err
	}

	return createVMFromTemplateResp.Payload[0], nil
}

// Delete VM.
func (svr *TowerVMService) Delete(uuid string) (*models.Task, error) {
	deleteVMParams := operations.NewDeleteVMParams()
	deleteVMParams.RequestBody = &models.VMOperateParams{
		Where: &models.VMWhereInput{LocalID: util.TowerString(uuid)},
	}

	deleteVMResp, err := svr.Session.DeleteVM(deleteVMParams)
	if err != nil {
		return nil, err
	}

	return &models.Task{ID: deleteVMResp.Payload[0].TaskID}, nil
}

// Power Off VM.
func (svr *TowerVMService) PowerOff(uuid string) (*models.Task, error) {
	shutDownVMParams := operations.NewShutDownVMParams()
	shutDownVMParams.RequestBody = &models.VMOperateParams{
		Where: &models.VMWhereInput{LocalID: util.TowerString(uuid)},
	}

	shutDownVMResp, err := svr.Session.ShutDownVM(shutDownVMParams)
	if err != nil {
		return nil, err
	}

	return &models.Task{ID: shutDownVMResp.Payload[0].TaskID}, nil
}

// Power On VM.
func (svr *TowerVMService) PowerOn(uuid string) (*models.Task, error) {
	startVMParams := operations.NewStartVMParams()
	startVMParams.RequestBody = &models.VMStartParams{
		Where: &models.VMWhereInput{LocalID: util.TowerString(uuid)},
	}

	startVMResp, err := svr.Session.StartVM(startVMParams)
	if err != nil {
		return nil, err
	}

	return &models.Task{ID: startVMResp.Payload[0].TaskID}, nil
}

// Get the VM.
func (svr *TowerVMService) Get(id string) (*models.VM, error) {
	getVmsParams := operations.NewGetVmsParams()
	getVmsParams.RequestBody = &models.GetVmsRequestBody{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: util.TowerString(id)}, {ID: util.TowerString(id)}},
		},
	}

	getVmsResp, err := svr.Session.GetVms(getVmsParams)
	if err != nil {
		return nil, err
	}

	if len(getVmsResp.Payload) == 0 {
		return nil, errors.New("VM_NOT_FOUND")
	}

	return getVmsResp.Payload[0], nil
}

// Get the VM by name.
func (svr *TowerVMService) GetByName(name string) (*models.VM, error) {
	getVmsParams := operations.NewGetVmsParams()
	getVmsParams.RequestBody = &models.GetVmsRequestBody{
		Where: &models.VMWhereInput{
			Name: util.TowerString(name),
		},
	}

	getVmsResp, err := svr.Session.GetVms(getVmsParams)
	if err != nil {
		return nil, err
	}

	if len(getVmsResp.Payload) == 0 {
		return nil, errors.New("VM_NOT_FOUND")
	}

	return getVmsResp.Payload[0], nil
}

// Get the cluster.
func (svr *TowerVMService) GetCluster(id string) (*models.Cluster, error) {
	getClustersParams := operations.NewGetClustersParams()
	getClustersParams.RequestBody = &models.GetClustersRequestBody{
		Where: &models.VMWhereInput{
			OR: []*models.VMWhereInput{{LocalID: util.TowerString(id)}, {ID: util.TowerString(id)}},
		},
	}

	getClustersResp, err := svr.Session.GetClusters(getClustersParams)
	if err != nil {
		return nil, err
	}

	if len(getClustersResp.Payload) == 0 {
		return nil, errors.New("CLUSTER_NOT_FOUND")
	}

	return getClustersResp.Payload[0], nil
}

// Get the vlan.
func (svr *TowerVMService) GetVlan(id string) (*models.Vlan, error) {
	getVlansParams := operations.NewGetVlansParams()
	getVlansParams.RequestBody = &models.GetVlansRequestBody{
		Where: &models.VMWhereInput{
			LocalID: util.TowerString(id),
		},
	}

	getVlansResp, err := svr.Session.GetVlans(getVlansParams)
	if err != nil {
		return nil, err
	}

	if len(getVlansResp.Payload) == 0 {
		return nil, errors.New("VLAN_NOT_FOUND")
	}

	return getVlansResp.Payload[0], nil
}

// Get the VM template.
func (svr *TowerVMService) GetVMTemplate(templateUUID string) (*models.VMTemplate, error) {
	if _, err := uuid.Parse(templateUUID); err != nil {
		return nil, err
	}

	getVMTemplatesParams := operations.NewGetVMTemplatesParams()
	getVMTemplatesParams.RequestBody = &models.GetVMTemplatesRequestBody{
		Where: &models.VMWhereInput{
			LocalID: util.TowerString(templateUUID),
		},
	}
	getVMTemplatesResp, err := svr.Session.GetVMTemplates(getVMTemplatesParams)
	if err != nil {
		return nil, err
	}

	vmTemplates := getVMTemplatesResp.Payload
	if len(vmTemplates) == 0 {
		return nil, errors.New("VM_TEMPLATE_NOT_FOUND")
	}

	return vmTemplates[0], nil
}

// Get the task by id.
func (svr *TowerVMService) GetTask(id string) (*models.Task, error) {
	getTasksParams := operations.NewGetTasksParams()
	getTasksParams.RequestBody = &models.GetTasksRequestBody{
		Where: &models.VMWhereInput{
			ID: util.TowerString(id),
		},
	}

	getTasksResp, err := svr.Session.GetTasks(getTasksParams)
	if err != nil {
		return nil, err
	}

	if len(getTasksResp.Payload) == 0 {
		return nil, errors.New("TASK_NOT_FOUND")
	}

	return getTasksResp.Payload[0], nil
}
