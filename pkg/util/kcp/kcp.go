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

// IsKCPRollingUpdateFirstMachine returns true if KCP is in rolling update and creating the first CP Machine.
//
// KCP rollout algorithm is as follows:
// Find Machines that have an outdated spec, If there is a machine requiring rollout
// 1.Scale up control plane creating a machine with the new spec
// 2.Scale down control plane by removing one of the machine that needs rollout (the oldest out-of date machine in the failure domain that has the most control-plane machines on it)
//
// kcp.Status.UpdatedReplicas is the total number of machines that are up to date with the control
// plane's configuration and therefore do not require rollout.
//
// So when KCP is in rolling update and creating the first CP Machine,
// kcp.Status.Replicas is greater than kcp.Spec.Replicas and kcp.Status.UpdatedReplicas equals 1.
//
// For more information about KCP replicas, refer to https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/api/v1beta1/kubeadm_control_plane_types.go
// For more information about KCP rollout, refer to https://github.com/kubernetes-sigs/cluster-api/blob/main/docs/proposals/20191017-kubeadm-based-control-plane.md#kubeadmcontrolplane-rollout
func IsKCPRollingUpdateFirstMachine(kcp *controlplanev1.KubeadmControlPlane) bool {
	return *kcp.Spec.Replicas < kcp.Status.Replicas && kcp.Status.UpdatedReplicas == 1
}

// IsKCPInScalingDown returns whether KCP is in scaling down.
//
// When KCP is in scaling down, machines managed by KCP is greater than kcp.Spec.Replicas,
// and these machines are up to date with the control plane's configuration.
//
// For more information about KCP replicas, refer to https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/api/v1beta1/kubeadm_control_plane_types.go
func IsKCPInScalingDown(kcp *controlplanev1.KubeadmControlPlane) bool {
	return *kcp.Spec.Replicas < kcp.Status.Replicas && kcp.Status.Replicas == kcp.Status.UpdatedReplicas
}
