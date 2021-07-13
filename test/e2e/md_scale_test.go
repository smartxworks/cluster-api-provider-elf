package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	capi_e2e "sigs.k8s.io/cluster-api/test/e2e"
)

var _ = Describe("Machines scale using CAPI scale test", func() {
	capi_e2e.MachineDeploymentScaleSpec(context.TODO(), func() capi_e2e.MachineDeploymentScaleSpecInput {
		return capi_e2e.MachineDeploymentScaleSpecInput{
			E2EConfig:             e2eConfig,
			ClusterctlConfigPath:  clusterctlConfigPath,
			BootstrapClusterProxy: bootstrapClusterProxy,
			ArtifactFolder:        artifactFolder,
			SkipCleanup:           skipCleanup,
		}
	})
})
