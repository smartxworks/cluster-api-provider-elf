/*
Copyright 2022.

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

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
)

var _ = Describe("When upgrading a workload cluster with a single control plane machine", func() {
	ClusterUpgradeSpec(context.TODO(), func() ClusterUpgradeSpecInput {
		return ClusterUpgradeSpecInput{
			E2EConfig:                e2eConfig,
			ClusterctlConfigPath:     clusterctlConfigPath,
			BootstrapClusterProxy:    bootstrapClusterProxy,
			ArtifactFolder:           artifactFolder,
			SkipCleanup:              skipCleanup,
			ControlPlaneMachineCount: 1,
			WorkerMachineCount:       1,
			Flavor:                   clusterctl.DefaultFlavor,
		}
	})
})

var _ = Describe("When upgrading a workload cluster with a HA control plane", func() {
	ClusterUpgradeSpec(context.TODO(), func() ClusterUpgradeSpecInput {
		return ClusterUpgradeSpecInput{
			E2EConfig:                e2eConfig,
			ClusterctlConfigPath:     clusterctlConfigPath,
			BootstrapClusterProxy:    bootstrapClusterProxy,
			ArtifactFolder:           artifactFolder,
			SkipCleanup:              skipCleanup,
			ControlPlaneMachineCount: 3,
			WorkerMachineCount:       2,
			Flavor:                   clusterctl.DefaultFlavor,
		}
	})
})

var _ = Describe("When upgrading a workload cluster with a HA control plane using scale-in rollout", func() {
	ClusterUpgradeSpec(context.TODO(), func() ClusterUpgradeSpecInput {
		return ClusterUpgradeSpecInput{
			E2EConfig:                e2eConfig,
			ClusterctlConfigPath:     clusterctlConfigPath,
			BootstrapClusterProxy:    bootstrapClusterProxy,
			ArtifactFolder:           artifactFolder,
			SkipCleanup:              skipCleanup,
			ControlPlaneMachineCount: 3,
			WorkerMachineCount:       1,
			Flavor:                   "kcp-scale-in",
		}
	})
})
