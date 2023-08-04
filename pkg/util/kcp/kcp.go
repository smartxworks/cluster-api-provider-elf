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
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
)

// IsKCPInRollingUpdate returns whether KCP is in rolling update.
//
// When *kcp.Spec.Replicas > kcp.Status.UpdatedReplicas, it must be in a KCP rolling update process.
// When *kcp.Spec.Replicas == kcp.Status.UpdatedReplicas, it could be in one of the following cases:
//  1. It's not in a KCP rolling update process. So kcp.Spec.Replicas == kcp.Status.Replicas.
//  2. It's at the end of a KCP rolling update process, and the last KCP replica (i.e the last KCP ElfMachine) is created just now.
//     There is still an old KCP ElfMachine, so kcp.Spec.Replicas + 1 == kcp.Status.Replicas.
//
// For more information about KCP replicas, refer to https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/api/v1beta1/kubeadm_control_plane_types.go
// For more information about KCP rollout, refer to https://github.com/kubernetes-sigs/cluster-api/blob/main/docs/proposals/20191017-kubeadm-based-control-plane.md#kubeadmcontrolplane-rollout
func IsKCPInRollingUpdate(kcp *controlplanev1.KubeadmControlPlane) bool {
	if (*kcp.Spec.Replicas > kcp.Status.UpdatedReplicas && *kcp.Spec.Replicas <= kcp.Status.Replicas) ||
		(*kcp.Spec.Replicas == kcp.Status.UpdatedReplicas && *kcp.Spec.Replicas < kcp.Status.Replicas) {
		return true
	}

	return false
}

// IsKCPInScalingDown returns whether KCP is in scaling down.
//
// When KCP is in scaling down/rolling update, KCP controller marks
// ResizedCondition to false and ScalingDownReason as Reason.
//
// For more information about KCP ResizedCondition and ScalingDownReason, refer to https://github.com/kubernetes-sigs/cluster-api/blob/main/api/v1beta1/condition_consts.go
func IsKCPInScalingDown(kcp *controlplanev1.KubeadmControlPlane) bool {
	return conditions.IsFalse(kcp, clusterv1.ResizedCondition) &&
		conditions.GetReason(kcp, controlplanev1.ResizedCondition) == controlplanev1.ScalingDownReason &&
		*kcp.Spec.Replicas < kcp.Status.UpdatedReplicas
}
