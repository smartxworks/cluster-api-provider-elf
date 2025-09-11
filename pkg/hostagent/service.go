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

package hostagent

import (
	goctx "context"
	"fmt"
	"strings"
	"time"

	agentv1 "github.com/smartxworks/host-config-agent-api/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/constants"
)

type HostAgentJobType string

const (
	defaultTimeout = 1 * time.Minute

	// HostAgentJobsConfigName is the name of the configmap that contains the host agent jobs.
	HostAgentJobsConfigName = "cape-hostagent-jobs"

	// HostAgentJobTypeExpandRootPartition is the job type for expanding the root partition.
	HostAgentJobTypeExpandRootPartition HostAgentJobType = "expand-root-partition"
	// HostAgentJobTypeRestartKubelet is the job type for restarting the kubelet.
	HostAgentJobTypeRestartKubelet HostAgentJobType = "restart-kubelet"
	// HostAgentJobTypeSetNetworkDeviceConfig is the job type for setting the network device configuration.
	HostAgentJobTypeSetNetworkDeviceConfig HostAgentJobType = "set-network-device-config"
)

func GetHostJob(ctx goctx.Context, c client.Client, namespace, name string) (*agentv1.HostOperationJob, error) {
	var restartKubeletJob agentv1.HostOperationJob
	if err := c.Get(ctx, apitypes.NamespacedName{
		Name:      name,
		Namespace: "default",
	}, &restartKubeletJob); err != nil {
		return nil, err
	}

	return &restartKubeletJob, nil
}

// GetExpandRootPartitionJobName return the expand root partition job name.
// The same disk expansion uses the same job name to reduce duplicate jobs.
func GetExpandRootPartitionJobName(elfMachine *infrav1.ElfMachine) string {
	return fmt.Sprintf("cape-expand-root-partition-%s-%d", elfMachine.Name, elfMachine.Spec.DiskGiB)
}

func GetRestartKubeletJobName(elfMachine *infrav1.ElfMachine) string {
	return fmt.Sprintf("cape-restart-kubelet-%s-%d-%d-%d", elfMachine.Name, elfMachine.Spec.NumCPUs, elfMachine.Spec.NumCoresPerSocket, elfMachine.Spec.MemoryMiB)
}

func GetSetNetworkDeviceConfigJobName(elfMachine *infrav1.ElfMachine, macTypes []MacType) string {
	if len(macTypes) == 0 {
		return "cape-set-network-config-" + elfMachine.Name
	}
	mac := strings.ToLower(strings.ReplaceAll(macTypes[len(macTypes)-1].Mac, ":", ""))
	return fmt.Sprintf("cape-set-network-config-%s-%s", elfMachine.Name, mac)
}

func GetJobName(elfMachine *infrav1.ElfMachine, jobType HostAgentJobType, macTypes []MacType) string {
	switch jobType {
	case HostAgentJobTypeExpandRootPartition:
		return GetExpandRootPartitionJobName(elfMachine)
	case HostAgentJobTypeRestartKubelet:
		return GetRestartKubeletJobName(elfMachine)
	case HostAgentJobTypeSetNetworkDeviceConfig:
		return GetSetNetworkDeviceConfigJobName(elfMachine, macTypes)
	default:
		return ""
	}
}

func GenerateExpandRootPartitionJob(elfMachine *infrav1.ElfMachine, playbook string) *agentv1.HostOperationJob {
	return &agentv1.HostOperationJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetExpandRootPartitionJobName(elfMachine),
			Namespace: "default",
		},
		Spec: agentv1.HostOperationJobSpec{
			NodeName: elfMachine.Name,
			Operation: agentv1.Operation{
				Ansible: &agentv1.Ansible{
					LocalPlaybookText: &agentv1.YAMLText{
						Inline: playbook,
					},
				},
				Timeout: metav1.Duration{Duration: defaultTimeout},
			},
		},
	}
}

func GenerateRestartKubeletJob(elfMachine *infrav1.ElfMachine, playbook string) *agentv1.HostOperationJob {
	return &agentv1.HostOperationJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetRestartKubeletJobName(elfMachine),
			Namespace: "default",
		},
		Spec: agentv1.HostOperationJobSpec{
			NodeName: elfMachine.Name,
			Operation: agentv1.Operation{
				Ansible: &agentv1.Ansible{
					LocalPlaybookText: &agentv1.YAMLText{
						Inline: playbook,
					},
				},
				Timeout: metav1.Duration{Duration: defaultTimeout},
			},
		},
	}
}

func GenerateSetNetworkDeviceConfigJob(elfMachine *infrav1.ElfMachine, playbook string, macTypes []MacType) *agentv1.HostOperationJob {
	return &agentv1.HostOperationJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSetNetworkDeviceConfigJobName(elfMachine, macTypes),
			Namespace: "default",
		},
		Spec: agentv1.HostOperationJobSpec{
			NodeName: elfMachine.Name,
			Operation: agentv1.Operation{
				Ansible: &agentv1.Ansible{
					LocalPlaybookText: &agentv1.YAMLText{
						Inline: playbook,
					},
				},
				Timeout: metav1.Duration{Duration: defaultTimeout},
			},
		},
	}
}

type MacType struct {
	Mac  string
	Type string
}

func GenerateJob(ctx goctx.Context, cli client.Client, elfMachine *infrav1.ElfMachine, jobType HostAgentJobType, macTypes []MacType) (*agentv1.HostOperationJob, error) {
	var configmap corev1.ConfigMap
	if err := cli.Get(ctx, client.ObjectKey{Namespace: constants.NamespaceCape, Name: HostAgentJobsConfigName}, &configmap); err != nil {
		return nil, err
	}

	playbook, ok := configmap.Data[string(jobType)]
	if !ok {
		return nil, fmt.Errorf("job playbook not found for job type: %s", jobType)
	}

	switch jobType {
	case HostAgentJobTypeExpandRootPartition:
		return GenerateExpandRootPartitionJob(elfMachine, playbook), nil
	case HostAgentJobTypeRestartKubelet:
		return GenerateRestartKubeletJob(elfMachine, playbook), nil
	case HostAgentJobTypeSetNetworkDeviceConfig:
		if len(macTypes) > 0 {
			var sb strings.Builder
			sb.WriteString("mac_types:\n")
			for i := range macTypes {
				sb.WriteString(fmt.Sprintf("      \"%s\": \"%s\"\n", macTypes[i].Mac, macTypes[i].Type))
			}
			playbook = strings.Replace(playbook, "mac_types: {}", sb.String(), 1)
		}

		return GenerateSetNetworkDeviceConfigJob(elfMachine, playbook, macTypes), nil
	default:
		return nil, fmt.Errorf("unknown job type: %s", jobType)
	}
}
