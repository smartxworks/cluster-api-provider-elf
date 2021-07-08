package service

import (
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.smartx.com/smartx/elf-sdk-go/client"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha4"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/session"
)

type VMService interface {
	Clone(machine *clusterv1.Machine, elfMachine *infrav1.ElfMachine, bootstrapData string) (*infrav1.VMJob, error)
	Delete(uuid string) (*infrav1.VMJob, error)
	PowerOff(uuid string) (*infrav1.VMJob, error)
	Get(uuid string) (*infrav1.VirtualMachine, error)
	GetVMTemplate(templateUUID string) (*client.VmTemplate, error)
	GetJob(jobId string) (*infrav1.VMJob, error)
	WaitJob(jobId string) (*infrav1.VMJob, error)
}

func NewVMService(auth infrav1.ElfAuth, logger logr.Logger) (VMService, error) {
	authSession, err := session.NewElfSession(auth)
	if err != nil {
		return nil, err
	}

	return &ElfVMService{authSession, logger}, nil
}

type ElfVMService struct {
	Session *session.Session `json:"session"`
	Logger  logr.Logger      `json:"logger"`
}

// Create VM using VM template.
func (svr *ElfVMService) Clone(
	machine *clusterv1.Machine,
	elfMachine *infrav1.ElfMachine,
	bootstrapData string) (*infrav1.VMJob, error) {

	template, err := svr.GetVMTemplate(elfMachine.Spec.Template)
	if err != nil {
		return nil, err
	}

	networkSpec := elfMachine.Spec.Network.Devices[0]
	network := []*client.Network{
		{
			NicIndex:  networkSpec.NetworkIndex,
			Type:      networkSpec.NetworkType,
			IPAddress: networkSpec.IPAddrs,
			Netmask:   networkSpec.Netmask,
		},
	}
	networkInfo := &client.NetworkInfo{
		Nameservers: []string{"114.114.114.114"},
		Gateway:     networkSpec.Gateway,
		Networks:    network,
	}

	cloudInit := &client.CloudInit{
		Hostname:            elfMachine.Name,
		DefaultUserPassword: "elfk8s",
		UserData:            bootstrapData,
		NetworkInfo:         networkInfo,
	}

	vmBody := client.VmTemplateBatchCreateVmBody{
		VmName:               machine.Name,
		Description:          "ELF k8s node.",
		Memory:               int64(template.Memory),
		Vcpu:                 template.Vcpu,
		Ha:                   template.Ha,
		NestedVirtualization: template.NestedVirtualization,
		CpuModel:             template.CpuModel,
		Cpu:                  template.Cpu,
		Nics:                 template.Nics,
		// Disks:                template.Disks,
		CloudInit:    cloudInit,
		AutoSchedule: true,
	}

	job, err := svr.Session.VmTemplateCreateVms(template.Uuid, &client.VmTemplateCreateVmsBody{VMs: []*client.VmTemplateBatchCreateVmBody{&vmBody}})
	if err != nil {
		return nil, errors.Wrapf(err, "error clone for machine")
	}

	return ToVMJob(job), nil
}

// Delete VM.
func (svr *ElfVMService) Delete(uuid string) (*infrav1.VMJob, error) {
	job, err := svr.Session.VmDelete(uuid, true)
	if err != nil {
		return nil, err
	}

	return ToVMJob(job), nil
}

// Power Off VM.
func (svr *ElfVMService) PowerOff(uuid string) (*infrav1.VMJob, error) {
	svr.Logger.Info("Power off VM", "VM", uuid)

	vmStopBody := client.VmStopBody{
		Force: true,
	}
	job, err := svr.Session.VmStop(uuid, &vmStopBody)
	if err != nil {
		return nil, err
	}

	return ToVMJob(job), nil
}

// Get the VM.
func (svr *ElfVMService) Get(uuid string) (*infrav1.VirtualMachine, error) {
	if uuid == "" {
		return nil, nil
	} else {
		elfVM, err := svr.Session.VmGet(uuid)
		if err != nil {
			return nil, err
		}

		return ToVirtualMachine(elfVM), nil
	}
}

// Get the VM template.
func (svr *ElfVMService) GetVMTemplate(templateUUID string) (*client.VmTemplate, error) {
	svr.Logger.Info("Get VM template", "UUID", templateUUID)

	if _, err := uuid.Parse(templateUUID); err != nil {
		return nil, err
	}

	template, err := svr.Session.VmTemplateGet(templateUUID)
	if err != nil {
		return nil, err
	}

	if template != nil {
		return template, nil
	}

	return nil, nil
}

// Get the job by id.
func (svr *ElfVMService) GetJob(jobId string) (*infrav1.VMJob, error) {
	if jobId == "" {
		return nil, nil
	}

	job, err := svr.Session.JobGet(jobId)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}

	VMJob := ToVMJob(job)

	svr.Logger.Info("Get VM job", "jobId", VMJob.Id, "state", job.State)

	return VMJob, nil
}

// Wait job done.
func (svr *ElfVMService) WaitJob(jobId string) (*infrav1.VMJob, error) {
	for {
		svr.Logger.Info("Waiting for VM job done...")
		job, err := svr.Session.JobGet(jobId)
		if err != nil {
			return nil, err
		}

		VMJob := ToVMJob(job)

		if VMJob.IsFinished() {
			return VMJob, nil
		}

		time.Sleep(3 * time.Second)
	}
}

func ToVMJob(job *client.Job) *infrav1.VMJob {
	VMJob := &infrav1.VMJob{
		Id:          job.JobId,
		Description: job.Description,
		State:       job.State,
		Resources:   job.Resources,
	}

	return VMJob
}

func ToVirtualMachine(elfVM *client.Vm) *infrav1.VirtualMachine {
	vm := &infrav1.VirtualMachine{
		Name:    elfVM.VmName,
		UUID:    elfVM.Uuid,
		State:   infrav1.VirtualMachineState(elfVM.Status),
		Network: getNetworkStatus(elfVM),
	}

	return vm
}

func getNetworkStatus(elfVM *client.Vm) []infrav1.NetworkStatus {
	var network []infrav1.NetworkStatus

	if elfVM.GuestInfo != nil {
		for index, nic := range elfVM.GuestInfo.NICs {
			for _, ip := range nic.IPAddresses {
				if ip.IPAddressType != "ipv4" {
					continue
				}

				if ip.IPAddress == "127.0.0.1" || strings.HasPrefix(ip.IPAddress, "169.254.") || strings.HasPrefix(ip.IPAddress, "172.17.0") {
					continue
				}

				if ip.IPAddressType == "ipv4" {
					network = append(network, infrav1.NetworkStatus{
						NetworkIndex: index,
						IPAddrs:      ip.IPAddress,
					})
				}
			}
		}
	}

	if len(network) > 0 {
		return network
	}

	return nil
}
