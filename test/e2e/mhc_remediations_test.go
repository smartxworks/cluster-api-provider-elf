package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	capi_e2e "sigs.k8s.io/cluster-api/test/e2e"
)

var _ = Describe("Machines remediation using CAPI remediation test", func() {
	capi_e2e.MachineRemediationSpec(context.TODO(), func() capi_e2e.MachineRemediationSpecInput {
		return capi_e2e.MachineRemediationSpecInput{
			E2EConfig:             e2eConfig,
			ClusterctlConfigPath:  clusterctlConfigPath,
			BootstrapClusterProxy: bootstrapClusterProxy,
			ArtifactFolder:        artifactFolder,
			SkipCleanup:           skipCleanup,
		}
	})
})
