package kcp

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

import (
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
)

// IsKCPRollingUpdateFirstMachine returns whether KCP is creating the first rolling update CP machine.
//
// If kcp.Status.Replicas > kcp.Spec.Replicas and kcp.Status.UpdatedReplicas == 1,
// it means KCP is creating the first rolling update CP Machine.
//
// For more information about KCP replicas, refer to https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/api/v1beta1/kubeadm_control_plane_types.go
func IsKCPRollingUpdateFirstMachine(kcp *controlplanev1.KubeadmControlPlane) bool {
	return *kcp.Spec.Replicas < kcp.Status.Replicas && kcp.Status.UpdatedReplicas == 1
}

// IsKCPInScalingDown returns whether KCP is in scaling down.
//
// If kcp.Spec.Replicas < kcp.Status.Replicas and kcp.Status.Replicas == kcp.Status.UpdatedReplicas,
// it means KCP is in scaling down.
//
// For more information about KCP replicas, refer to https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/api/v1beta1/kubeadm_control_plane_types.go
func IsKCPInScalingDown(kcp *controlplanev1.KubeadmControlPlane) bool {
	return *kcp.Spec.Replicas < kcp.Status.Replicas && kcp.Status.Replicas == kcp.Status.UpdatedReplicas
}
