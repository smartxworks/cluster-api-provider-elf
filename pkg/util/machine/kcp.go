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
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	labelutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/labels"
)

func GetKCPByMachine(ctx goctx.Context, ctrlClient client.Client, machine *clusterv1.Machine) (*controlplanev1.KubeadmControlPlane, error) {
	var kcp controlplanev1.KubeadmControlPlane
	if err := ctrlClient.Get(ctx, apitypes.NamespacedName{Namespace: machine.Namespace, Name: labelutil.GetControlPlaneLabel(machine)}, &kcp); err != nil {
		return nil, err
	}

	return &kcp, nil
}
