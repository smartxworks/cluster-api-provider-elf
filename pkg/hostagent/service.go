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
	"time"

	agentv1 "github.com/smartxworks/host-config-agent-api/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/hostagent/tasks"
)

const defaultTimeout = 1 * time.Minute

func GetHostJob(ctx goctx.Context, c client.Client, namespace, name string) (*agentv1.HostOperationJob, error) {
	var restartKubeletJob agentv1.HostOperationJob
	if err := c.Get(ctx, apitypes.NamespacedName{
		Name:      name,
		Namespace: "sks-system",
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

func ExpandRootPartition(ctx goctx.Context, c client.Client, elfMachine *infrav1.ElfMachine) (*agentv1.HostOperationJob, error) {
	agentJob := &agentv1.HostOperationJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetExpandRootPartitionJobName(elfMachine),
			Namespace: "sks-system",
		},
		Spec: agentv1.HostOperationJobSpec{
			NodeName: elfMachine.Name,
			Operation: agentv1.Operation{
				Ansible: &agentv1.Ansible{
					LocalPlaybookText: &agentv1.YAMLText{
						Inline: tasks.ExpandRootPartitionTask,
					},
				},
				Timeout: metav1.Duration{Duration: defaultTimeout},
			},
		},
	}

	if err := c.Create(ctx, agentJob); err != nil {
		return nil, err
	}

	return agentJob, nil
}

func RestartMachineKubelet(ctx goctx.Context, c client.Client, elfMachine *infrav1.ElfMachine) (*agentv1.HostOperationJob, error) {
	restartKubeletJob := &agentv1.HostOperationJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetRestartKubeletJobName(elfMachine),
			Namespace: "sks-system",
		},
		Spec: agentv1.HostOperationJobSpec{
			NodeName: elfMachine.Name,
			Operation: agentv1.Operation{
				Ansible: &agentv1.Ansible{
					LocalPlaybookText: &agentv1.YAMLText{
						Inline: tasks.RestartKubeletTask,
					},
				},
				Timeout: metav1.Duration{Duration: defaultTimeout},
			},
		},
	}

	if err := c.Create(ctx, restartKubeletJob); err != nil {
		return nil, err
	}

	return restartKubeletJob, nil
}
