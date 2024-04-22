package md

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

import (
	intstrutil "k8s.io/apimachinery/pkg/util/intstr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// IsMDInRollingUpdate returns whether MD is in rolling update.
//
// When *md.Spec.Replicas > md.Status.UpdatedReplicas, it must be in a MD rolling update process.
// When *md.Spec.Replicas == md.Status.UpdatedReplicas, it could be in one of the following cases:
//  1. It's not in a MD rolling update process. So md.Spec.Replicas == md.Status.Replicas.
//  2. It's at the end of a MD rolling update process, and the last MD replica (i.e the last MD ElfMachine) is created just now.
//     There is still an old MD ElfMachine, so md.Spec.Replicas + 1 == md.Status.Replicas.
func IsMDInRollingUpdate(md *clusterv1.MachineDeployment) bool {
	if (*md.Spec.Replicas > md.Status.UpdatedReplicas && *md.Spec.Replicas <= md.Status.Replicas) ||
		(*md.Spec.Replicas == md.Status.UpdatedReplicas && *md.Spec.Replicas < md.Status.Replicas) {
		return true
	}

	return false
}

/*
Copy from CAPI: https://github.com/kubernetes-sigs/cluster-api/blob/release-1.5/internal/controllers/machinedeployment/mdutil/util.go
*/

// MaxUnavailable returns the maximum unavailable machines a rolling deployment can take.
func MaxUnavailable(deployment clusterv1.MachineDeployment) int32 {
	if deployment.Spec.Strategy.Type != clusterv1.RollingUpdateMachineDeploymentStrategyType || *(deployment.Spec.Replicas) == 0 {
		return int32(0)
	}
	// Error caught by validation
	_, maxUnavailable, _ := ResolveFenceposts(deployment.Spec.Strategy.RollingUpdate.MaxSurge, deployment.Spec.Strategy.RollingUpdate.MaxUnavailable, *(deployment.Spec.Replicas))
	if maxUnavailable > *deployment.Spec.Replicas {
		return *deployment.Spec.Replicas
	}
	return maxUnavailable
}

// MaxSurge returns the maximum surge machines a rolling deployment can take.
func MaxSurge(deployment clusterv1.MachineDeployment) int32 {
	if deployment.Spec.Strategy.Type != clusterv1.RollingUpdateMachineDeploymentStrategyType {
		return int32(0)
	}
	// Error caught by validation
	maxSurge, _, _ := ResolveFenceposts(deployment.Spec.Strategy.RollingUpdate.MaxSurge, deployment.Spec.Strategy.RollingUpdate.MaxUnavailable, *(deployment.Spec.Replicas))
	return maxSurge
}

// ResolveFenceposts resolves both maxSurge and maxUnavailable. This needs to happen in one
// step. For example:
//
// 2 desired, max unavailable 1%, surge 0% - should scale old(-1), then new(+1), then old(-1), then new(+1)
// 1 desired, max unavailable 1%, surge 0% - should scale old(-1), then new(+1)
// 2 desired, max unavailable 25%, surge 1% - should scale new(+1), then old(-1), then new(+1), then old(-1)
// 1 desired, max unavailable 25%, surge 1% - should scale new(+1), then old(-1)
// 2 desired, max unavailable 0%, surge 1% - should scale new(+1), then old(-1), then new(+1), then old(-1)
// 1 desired, max unavailable 0%, surge 1% - should scale new(+1), then old(-1).
func ResolveFenceposts(maxSurge, maxUnavailable *intstrutil.IntOrString, desired int32) (int32, int32, error) {
	surge, err := intstrutil.GetScaledValueFromIntOrPercent(maxSurge, int(desired), true)
	if err != nil {
		return 0, 0, err
	}
	unavailable, err := intstrutil.GetScaledValueFromIntOrPercent(maxUnavailable, int(desired), false)
	if err != nil {
		return 0, 0, err
	}

	if surge == 0 && unavailable == 0 {
		// Validation should never allow the user to explicitly use zero values for both maxSurge
		// maxUnavailable. Due to rounding down maxUnavailable though, it may resolve to zero.
		// If both fenceposts resolve to zero, then we should set maxUnavailable to 1 on the
		// theory that surge might not work due to quota.
		unavailable = 1
	}

	return int32(surge), int32(unavailable), nil
}
