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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apitypes "k8s.io/apimachinery/pkg/types"
	controlplanev1 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetKCPForCluster gets a cluster's ControlPlane resource.
func GetKCPForCluster(
	ctx goctx.Context,
	ctrlClient client.Client,
	clusterKey client.ObjectKey,
) (*controlplanev1.KubeadmControlPlane, error) {
	var kcpList controlplanev1.KubeadmControlPlaneList
	labels := map[string]string{clusterv1.ClusterNameLabel: clusterKey.Name}

	if err := ctrlClient.List(
		ctx, &kcpList,
		client.InNamespace(clusterKey.Namespace),
		client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	if len(kcpList.Items) == 0 {
		return nil, apierrors.NewNotFound(schema.GroupResource{
			Group:    controlplanev1.GroupVersion.Group,
			Resource: "kubeadmcontrolplanes",
		}, clusterKey.Name)
	}

	return &kcpList.Items[0], nil
}

// GetKCPNameByMachine returns the KCP name associated with the Machine.
// Do not use "cluster.x-k8s.io/control-plane-name" label because
// its value will be a hashed string of the KCP name when the KCP name exceeds 63 characters.
func GetKCPNameByMachine(machine *clusterv1.Machine) string {
	for _, o := range machine.OwnerReferences {
		if o.Kind == "KubeadmControlPlane" {
			return o.Name
		}
	}
	panic(fmt.Sprintf("Machine %s is not owned by KubeadmControlPlane", machine.GetName()))
}

func GetKCPByMachine(ctx goctx.Context, ctrlClient client.Client, machine *clusterv1.Machine) (*controlplanev1.KubeadmControlPlane, error) {
	var kcp controlplanev1.KubeadmControlPlane

	kcpName := GetKCPNameByMachine(machine)
	if err := ctrlClient.Get(ctx, apitypes.NamespacedName{Namespace: machine.Namespace, Name: kcpName}, &kcp); err != nil {
		return nil, err
	}

	return &kcp, nil
}
