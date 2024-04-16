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

package machine

import (
	goctx "context"

	apitypes "k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	labelutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/labels"
)

func GetMDByMachine(ctx goctx.Context, ctrlClient client.Client, machine *clusterv1.Machine) (*clusterv1.MachineDeployment, error) {
	var md clusterv1.MachineDeployment
	if err := ctrlClient.Get(ctx, apitypes.NamespacedName{Namespace: machine.Namespace, Name: labelutil.GetDeploymentNameLabel(machine)}, &md); err != nil {
		return nil, err
	}

	return &md, nil
}

func GetMDsForCluster(
	ctx goctx.Context,
	ctrlClient client.Client,
	namespace, clusterName string) ([]*clusterv1.MachineDeployment, error) {
	var mdList clusterv1.MachineDeploymentList
	labels := map[string]string{clusterv1.ClusterNameLabel: clusterName}

	if err := ctrlClient.List(
		ctx, &mdList,
		client.InNamespace(namespace),
		client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	mds := make([]*clusterv1.MachineDeployment, len(mdList.Items))
	for i := range mdList.Items {
		mds[i] = &mdList.Items[i]
	}

	return mds, nil
}
