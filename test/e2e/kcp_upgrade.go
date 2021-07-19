package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	capi_e2e "sigs.k8s.io/cluster-api/test/e2e"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/cluster-api/util"
)

// KCPUpgradeSpecInput is the input for KCPUpgradeSpec.
type KCPUpgradeSpecInput struct {
	E2EConfig                *clusterctl.E2EConfig
	ClusterctlConfigPath     string
	BootstrapClusterProxy    framework.ClusterProxy
	ArtifactFolder           string
	SkipCleanup              bool
	ControlPlaneMachineCount int64
	Flavor                   string
}

// KCPUpgradeSpec implements a test that verifies KCP to properly upgrade a control plane with 3 machines.
func KCPUpgradeSpec(ctx context.Context, inputGetter func() KCPUpgradeSpecInput) {
	var (
		specName         = "kcp-upgrade"
		input            KCPUpgradeSpecInput
		namespace        *corev1.Namespace
		cancelWatches    context.CancelFunc
		clusterResources *clusterctl.ApplyClusterTemplateAndWaitResult
	)

	BeforeEach(func() {
		Expect(ctx).NotTo(BeNil(), "ctx is required for %s spec", specName)
		input = inputGetter()
		Expect(input.E2EConfig).ToNot(BeNil(), "Invalid argument. input.E2EConfig can't be nil when calling %s spec", specName)
		Expect(input.ClusterctlConfigPath).To(BeAnExistingFile(), "Invalid argument. input.ClusterctlConfigPath must be an existing file when calling %s spec", specName)
		Expect(input.BootstrapClusterProxy).ToNot(BeNil(), "Invalid argument. input.BootstrapClusterProxy can't be nil when calling %s spec", specName)
		Expect(os.MkdirAll(input.ArtifactFolder, 0750)).To(Succeed(), "Invalid argument. input.ArtifactFolder can't be created for %s spec", specName)
		Expect(input.ControlPlaneMachineCount).ToNot(BeZero())
		Expect(input.E2EConfig.Variables).To(HaveKey(capi_e2e.KubernetesVersionUpgradeTo))
		Expect(input.E2EConfig.Variables).To(HaveKey(capi_e2e.KubernetesVersionUpgradeFrom))
		Expect(input.E2EConfig.Variables).To(HaveKey(capi_e2e.EtcdVersionUpgradeTo))
		Expect(input.E2EConfig.Variables).To(HaveKey(capi_e2e.CoreDNSVersionUpgradeTo))
		Expect(input.E2EConfig.Variables).To(HaveKey(ElfTemplateUpgradeTo))

		// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
		namespace, cancelWatches = setupSpecNamespace(ctx, specName, input.BootstrapClusterProxy, input.ArtifactFolder)
		clusterResources = new(clusterctl.ApplyClusterTemplateAndWaitResult)
	})

	It("Should successfully upgrade Kubernetes, DNS, kube-proxy, and etcd", func() {
		By("Creating a workload cluster")
		clusterctl.ApplyClusterTemplateAndWait(ctx, clusterctl.ApplyClusterTemplateAndWaitInput{
			ClusterProxy: input.BootstrapClusterProxy,
			ConfigCluster: clusterctl.ConfigClusterInput{
				LogFolder:                filepath.Join(input.ArtifactFolder, "clusters", input.BootstrapClusterProxy.GetName()),
				ClusterctlConfigPath:     input.ClusterctlConfigPath,
				KubeconfigPath:           input.BootstrapClusterProxy.GetKubeconfigPath(),
				InfrastructureProvider:   clusterctl.DefaultInfrastructureProvider,
				Flavor:                   input.Flavor,
				Namespace:                namespace.Name,
				ClusterName:              fmt.Sprintf("%s-%s", specName, util.RandomString(6)),
				KubernetesVersion:        input.E2EConfig.GetVariable(capi_e2e.KubernetesVersionUpgradeFrom),
				ControlPlaneMachineCount: pointer.Int64Ptr(input.ControlPlaneMachineCount),
				WorkerMachineCount:       pointer.Int64Ptr(1),
			},
			WaitForClusterIntervals:      input.E2EConfig.GetIntervals(specName, "wait-cluster"),
			WaitForControlPlaneIntervals: input.E2EConfig.GetIntervals(specName, "wait-control-plane"),
			WaitForMachineDeployments:    input.E2EConfig.GetIntervals(specName, "wait-worker-nodes"),
		}, clusterResources)

		By("Upgrading Kubernetes, DNS, kube-proxy, and etcd versions")
		UpgradeControlPlaneAndWaitForUpgrade(ctx, UpgradeControlPlaneAndWaitForUpgradeInput{
			ClusterProxy:                input.BootstrapClusterProxy,
			Cluster:                     clusterResources.Cluster,
			ControlPlane:                clusterResources.ControlPlane,
			EtcdImageTag:                input.E2EConfig.GetVariable(capi_e2e.EtcdVersionUpgradeTo),
			DNSImageTag:                 input.E2EConfig.GetVariable(capi_e2e.CoreDNSVersionUpgradeTo),
			KubernetesUpgradeVersion:    input.E2EConfig.GetVariable(capi_e2e.KubernetesVersionUpgradeTo),
			WaitForMachinesToBeUpgraded: input.E2EConfig.GetIntervals(specName, "wait-machine-upgrade"),
			WaitForDNSUpgrade:           input.E2EConfig.GetIntervals(specName, "wait-machine-upgrade"),
			WaitForEtcdUpgrade:          input.E2EConfig.GetIntervals(specName, "wait-machine-upgrade"),
			VMTemplateUUID:              input.E2EConfig.GetVariable(ElfTemplateUpgradeTo),
		})

		By("PASSED!")
	})

	AfterEach(func() {
		// Dumps all the resources in the spec namespace, then cleanups the cluster object and the spec namespace itself.
		dumpSpecResourcesAndCleanup(ctx, specName, input.BootstrapClusterProxy, input.ArtifactFolder, namespace, cancelWatches, clusterResources.Cluster, input.E2EConfig.GetIntervals, input.SkipCleanup)
	})
}
