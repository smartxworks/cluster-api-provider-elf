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

	infrautilv1 "github.com/smartxworks/cluster-api-provider-elf/pkg/util"
)

var _ = Describe("CAPE HA e2e test", func() {
	var (
		specName         = "cape-ha"
		namespace        *corev1.Namespace
		cancelWatches    context.CancelFunc
		clusterResources *clusterctl.ApplyClusterTemplateAndWaitResult
		ctx              = context.TODO()
	)

	BeforeEach(func() {
		Expect(ctx).NotTo(BeNil(), "ctx is required for %s spec", specName)
		Expect(e2eConfig).ToNot(BeNil(), "Invalid argument. e2eConfig can't be nil when calling %s spec", specName)
		Expect(clusterctlConfigPath).To(BeAnExistingFile(), "Invalid argument. clusterctlConfigPath must be an existing file when calling %s spec", specName)
		Expect(bootstrapClusterProxy).ToNot(BeNil(), "Invalid argument. bootstrapClusterProxy can't be nil when calling %s spec", specName)
		Expect(os.MkdirAll(artifactFolder, 0750)).To(Succeed(), "Invalid argument. artifactFolder can't be created for %s spec", specName)
		Expect(e2eConfig.Variables).To(HaveKey(capi_e2e.KubernetesVersion))

		// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
		namespace, cancelWatches = setupSpecNamespace(ctx, specName, bootstrapClusterProxy, artifactFolder)
		clusterResources = new(clusterctl.ApplyClusterTemplateAndWaitResult)
	})

	It("Should work when less than half of the control panel nodes are unavailable", func() {
		Byf("Creating a workload cluster")
		clusterctl.ApplyClusterTemplateAndWait(ctx, clusterctl.ApplyClusterTemplateAndWaitInput{
			ClusterProxy: bootstrapClusterProxy,
			ConfigCluster: clusterctl.ConfigClusterInput{
				LogFolder:                filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
				ClusterctlConfigPath:     clusterctlConfigPath,
				KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
				InfrastructureProvider:   clusterctl.DefaultInfrastructureProvider,
				Flavor:                   "cp-ha",
				Namespace:                namespace.Name,
				ClusterName:              fmt.Sprintf("%s-%s", specName, util.RandomString(6)),
				KubernetesVersion:        e2eConfig.GetVariable(capi_e2e.KubernetesVersion),
				ControlPlaneMachineCount: pointer.Int64Ptr(3),
				WorkerMachineCount:       pointer.Int64Ptr(1),
			},
			WaitForClusterIntervals:      e2eConfig.GetIntervals(specName, "wait-cluster"),
			WaitForControlPlaneIntervals: e2eConfig.GetIntervals(specName, "wait-control-plane"),
			WaitForMachineDeployments:    e2eConfig.GetIntervals(specName, "wait-worker-nodes"),
		}, clusterResources)

		Byf("Setting a unavailable control plane node")
		Logf("Discovering control plane machines")
		machines := framework.GetControlPlaneMachinesByCluster(ctx, framework.GetControlPlaneMachinesByClusterInput{
			Lister:      bootstrapClusterProxy.GetClient(),
			ClusterName: clusterResources.Cluster.Name,
			Namespace:   clusterResources.Cluster.Namespace,
		})
		Expect(len(machines)).Should(Equal(3))

		Logf("Powering off VM")
		PowerOffVM(ctx, PowerOffVMInput{
			UUID:               infrautilv1.ConvertProviderIDToUUID(machines[0].Spec.ProviderID),
			VMService:          vmService,
			WaitVMJobIntervals: e2eConfig.GetIntervals(specName, "wait-vm-job"),
		})

		workloadProxy := bootstrapClusterProxy.GetWorkloadCluster(ctx, namespace.Name, clusterResources.Cluster.Name)
		workloadClient := workloadProxy.GetClient()

		Byf("Wait for the control plane node not ready")
		WaitForNodeNotReady(ctx, WaitForNodeNotReadyInput{
			Lister:              workloadClient,
			ReadyCount:          3,
			NotReadyNodeName:    machines[0].Status.NodeRef.Name,
			WaitForNodeNotReady: e2eConfig.GetIntervals(specName, "wait-nodes-ready"),
		})

		Byf("Wait for control plane nodes ready")
		Logf("Powering on VM")
		PowerOnVM(ctx, PowerOnVMInput{
			UUID:               infrautilv1.ConvertProviderIDToUUID(machines[0].Spec.ProviderID),
			VMService:          vmService,
			WaitVMJobIntervals: e2eConfig.GetIntervals(specName, "wait-vm-job"),
		})

		Logf("Waiting until nodes are ready")
		framework.WaitForNodesReady(ctx, framework.WaitForNodesReadyInput{
			Lister:            workloadClient,
			KubernetesVersion: e2eConfig.GetVariable(capi_e2e.KubernetesVersion),
			Count:             int(4),
			WaitForNodesReady: e2eConfig.GetIntervals(specName, "wait-nodes-ready"),
		})

		Byf("PASSED!")
	})

	AfterEach(func() {
		// Dumps all the resources in the spec namespace, then cleanups the cluster object and the spec namespace itself.
		dumpSpecResourcesAndCleanup(ctx, specName, bootstrapClusterProxy, artifactFolder, namespace, cancelWatches, clusterResources.Cluster, e2eConfig.GetIntervals, skipCleanup)
	})
})
