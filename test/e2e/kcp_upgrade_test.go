package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
)

var _ = Describe("When testing KCP upgrade in a single control plane cluster", func() {
	KCPUpgradeSpec(context.TODO(), func() KCPUpgradeSpecInput {
		return KCPUpgradeSpecInput{
			E2EConfig:                e2eConfig,
			ClusterctlConfigPath:     clusterctlConfigPath,
			BootstrapClusterProxy:    bootstrapClusterProxy,
			ArtifactFolder:           artifactFolder,
			SkipCleanup:              skipCleanup,
			ControlPlaneMachineCount: 1,
			Flavor:                   clusterctl.DefaultFlavor,
		}
	})
})

var _ = Describe("When testing KCP upgrade in a HA cluster", func() {
	KCPUpgradeSpec(context.TODO(), func() KCPUpgradeSpecInput {
		return KCPUpgradeSpecInput{
			E2EConfig:                e2eConfig,
			ClusterctlConfigPath:     clusterctlConfigPath,
			BootstrapClusterProxy:    bootstrapClusterProxy,
			ArtifactFolder:           artifactFolder,
			SkipCleanup:              skipCleanup,
			ControlPlaneMachineCount: 3,
			Flavor:                   clusterctl.DefaultFlavor,
		}
	})
})

var _ = Describe("When testing KCP upgrade in a HA cluster using scale in rollout", func() {
	KCPUpgradeSpec(context.TODO(), func() KCPUpgradeSpecInput {
		return KCPUpgradeSpecInput{
			E2EConfig:                e2eConfig,
			ClusterctlConfigPath:     clusterctlConfigPath,
			BootstrapClusterProxy:    bootstrapClusterProxy,
			ArtifactFolder:           artifactFolder,
			SkipCleanup:              skipCleanup,
			ControlPlaneMachineCount: 3,
			Flavor:                   "kcp-scale-in",
		}
	})
})
