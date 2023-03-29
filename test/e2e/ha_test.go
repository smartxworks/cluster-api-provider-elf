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
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
	capie2e "sigs.k8s.io/cluster-api/test/e2e"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"

	machineutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/machine"
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
		Expect(e2eConfig.Variables).To(HaveKey(capie2e.KubernetesVersion))

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
				ClusterName:              fmt.Sprintf("%s-%s", specName, capiutil.RandomString(6)),
				KubernetesVersion:        e2eConfig.GetVariable(capie2e.KubernetesVersion),
				ControlPlaneMachineCount: pointer.Int64(3),
				WorkerMachineCount:       pointer.Int64(1),
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

		Logf("Shut down VM")
		ShutDownVM(ctx, ShutDownVMInput{
			UUID:               machineutil.ConvertProviderIDToUUID(machines[0].Spec.ProviderID),
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
			UUID:               machineutil.ConvertProviderIDToUUID(machines[0].Spec.ProviderID),
			VMService:          vmService,
			WaitVMJobIntervals: e2eConfig.GetIntervals(specName, "wait-vm-job"),
		})

		Logf("Waiting until nodes are ready")
		framework.WaitForNodesReady(ctx, framework.WaitForNodesReadyInput{
			Lister:            workloadClient,
			KubernetesVersion: e2eConfig.GetVariable(capie2e.KubernetesVersion),
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
