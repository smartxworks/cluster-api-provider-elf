/*
Copyright 2026.

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

package v1beta1

import (
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	conditions "sigs.k8s.io/cluster-api/util/conditions"
)

func IsClusterInfrastructureReady(cluster *capiv1.Cluster) bool {
	if cluster == nil {
		return false
	}

	if cluster.Status.Initialization.InfrastructureProvisioned != nil {
		return *cluster.Status.Initialization.InfrastructureProvisioned
	}

	return false
}

func IsClusterControlPlaneInitialized(cluster *capiv1.Cluster) bool {
	if cluster == nil {
		return false
	}

	if cluster.Status.Initialization.ControlPlaneInitialized != nil {
		return *cluster.Status.Initialization.ControlPlaneInitialized
	}

	return conditions.IsTrue(cluster, capiv1.ClusterControlPlaneInitializedCondition)
}

func IsClusterControlPlaneAvailable(cluster *capiv1.Cluster) bool {
	if cluster == nil {
		return false
	}

	return conditions.IsTrue(cluster, capiv1.ClusterControlPlaneAvailableCondition)
}
