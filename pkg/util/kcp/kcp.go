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

// IsKCPInRollingUpdate returns whether KCP is in rolling update.
// IMPORTANT: This function can only be used when creating a new Machine.
//
// If *kcp.Spec.Replicas > kcp.Status.Replicas, it means KCP is not in rolling update,
// but scaling out or being created.
func IsKCPInRollingUpdate(kcp *controlplanev1.KubeadmControlPlane) bool {
	return *kcp.Spec.Replicas <= kcp.Status.Replicas && kcp.Status.UpdatedReplicas == 1
}

// IsKCPInRollingUpdate returns whether KCP is in scaling down.
func IsKCPInScalingDown(kcp *controlplanev1.KubeadmControlPlane) bool {
	return *kcp.Spec.Replicas < kcp.Status.Replicas && kcp.Status.Replicas == kcp.Status.UpdatedReplicas
}
