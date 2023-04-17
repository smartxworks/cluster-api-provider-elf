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

package resources

import (
	goctx "context"
	"fmt"

	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	annotationsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/annotations"
	labelsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/labels"
	machineutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/machine"
)

func GetVMPlacementGroupName(ctx goctx.Context, ctrlClient client.Client, machine *clusterv1.Machine, cluster *clusterv1.Cluster) (string, error) {
	groupName := ""
	if machineutil.IsControlPlaneMachine(machine) {
		kcp, err := machineutil.GetKCPByMachine(ctx, ctrlClient, machine)
		if err != nil {
			return "", err
		}

		placementGroupName := annotationsutil.GetPlacementGroupName(kcp)
		if placementGroupName != "" {
			return placementGroupName, nil
		}

		groupName = machineutil.GetKCPNameByMachine(machine)
	} else {
		md, err := machineutil.GetMDByMachine(ctx, ctrlClient, machine)
		if err != nil {
			return "", err
		}

		placementGroupName := annotationsutil.GetPlacementGroupName(md)
		if placementGroupName != "" {
			return placementGroupName, nil
		}

		groupName = labelsutil.GetDeploymentNameLabel(machine)
	}

	if groupName == "" {
		return "", nil
	}

	return fmt.Sprintf("%s-managed-%s-%s-%s", GetResourcePrefix(), cluster.UID, machine.Namespace, groupName), nil
}

func GetVMPlacementGroupPolicy(machine *clusterv1.Machine) models.VMVMPolicy {
	if machineutil.IsControlPlaneMachine(machine) {
		return models.VMVMPolicyMUSTDIFFERENT
	}

	return models.VMVMPolicyPREFERDIFFERENT
}
