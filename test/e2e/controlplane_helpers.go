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
	"errors"
	"fmt"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	"sigs.k8s.io/cluster-api/test/framework"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

type ScaleAndWaitControlPlaneInput struct {
	ClusterProxy        framework.ClusterProxy
	Cluster             *clusterv1.Cluster
	ControlPlane        *controlplanev1.KubeadmControlPlane
	Replicas            int32
	WaitForControlPlane []interface{}
}

// ScaleAndWaitControlPlane scales KCP and waits until all machines have node ref and equal to Replicas.
func ScaleAndWaitControlPlane(ctx context.Context, input ScaleAndWaitControlPlaneInput) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for ScaleAndWaitControlPlane")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling ScaleAndWaitControlPlane")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling ScaleAndWaitControlPlane")
	Expect(input.ControlPlane).ToNot(BeNil(), "Invalid argument. input.ControlPlane can't be nil when calling ScaleAndWaitControlPlane")

	patchHelper, err := patch.NewHelper(input.ControlPlane, input.ClusterProxy.GetClient())
	Expect(err).ToNot(HaveOccurred())
	input.ControlPlane.Spec.Replicas = pointer.Int32Ptr(input.Replicas)
	Logf("Scaling controlplane %s/%s from %v to %v replicas", input.ControlPlane.Namespace, input.ControlPlane.Name, *input.ControlPlane.Spec.Replicas, input.Replicas)
	Expect(patchHelper.Patch(ctx, input.ControlPlane)).To(Succeed())

	Logf("Waiting for correct number of replicas to exist")
	Eventually(func() (int, error) {
		machines := framework.GetControlPlaneMachinesByCluster(ctx, framework.GetControlPlaneMachinesByClusterInput{
			Lister:      input.ClusterProxy.GetClient(),
			ClusterName: input.Cluster.Name,
			Namespace:   input.Cluster.Namespace,
		})

		nodeRefCount := 0
		for _, machine := range machines {
			if machine.Status.NodeRef != nil {
				nodeRefCount++
			}
		}

		if len(machines) != nodeRefCount {
			return -1, errors.New("machine count does not match existing nodes count")
		}

		return nodeRefCount, nil
	}, input.WaitForControlPlane...).Should(Equal(int(input.Replicas)))
}

// UpgradeControlPlaneAndWaitForUpgradeInput is the input type for UpgradeControlPlaneAndWaitForUpgrade.
type UpgradeControlPlaneAndWaitForUpgradeInput struct {
	ClusterProxy                framework.ClusterProxy
	Cluster                     *clusterv1.Cluster
	ControlPlane                *controlplanev1.KubeadmControlPlane
	KubernetesUpgradeVersion    string
	EtcdImageTag                string
	DNSImageTag                 string
	VMTemplate                  string
	WaitForMachinesToBeUpgraded []interface{}
	WaitForDNSUpgrade           []interface{}
	WaitForEtcdUpgrade          []interface{}
	WaitForKubeProxyUpgrade     []interface{}
}

// UpgradeControlPlaneAndWaitForUpgrade upgrades a KubeadmControlPlane and waits for it to be upgraded.
func UpgradeControlPlaneAndWaitForUpgrade(ctx context.Context, input UpgradeControlPlaneAndWaitForUpgradeInput) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for UpgradeControlPlaneAndWaitForUpgrade")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling UpgradeControlPlaneAndWaitForUpgrade")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling UpgradeControlPlaneAndWaitForUpgrade")
	Expect(input.ControlPlane).ToNot(BeNil(), "Invalid argument. input.ControlPlane can't be nil when calling UpgradeControlPlaneAndWaitForUpgrade")
	Expect(input.KubernetesUpgradeVersion).ToNot(BeNil(), "Invalid argument. input.KubernetesUpgradeVersion can't be empty when calling UpgradeControlPlaneAndWaitForUpgrade")
	Expect(input.EtcdImageTag).ToNot(BeNil(), "Invalid argument. input.EtcdImageTag can't be empty when calling UpgradeControlPlaneAndWaitForUpgrade")
	Expect(input.DNSImageTag).ToNot(BeNil(), "Invalid argument. input.DNSImageTag can't be empty when calling UpgradeControlPlaneAndWaitForUpgrade")
	Expect(input.VMTemplate).ToNot(BeNil(), "Invalid argument. input.VMTemplate can't be empty when calling UpgradeControlPlaneAndWaitForUpgrade")

	mgmtClient := input.ClusterProxy.GetClient()

	Logf("Patching the new kubernetes version and infrastructure ref to KCP")

	// Retrieve infra object
	infraRef := input.ControlPlane.Spec.MachineTemplate.InfrastructureRef
	var elfMachineTemplate infrav1.ElfMachineTemplate
	elfMachineTemplateKey := client.ObjectKey{
		Namespace: input.Cluster.Namespace,
		Name:      infraRef.Name,
	}
	Expect(mgmtClient.Get(ctx, elfMachineTemplateKey, &elfMachineTemplate)).NotTo(HaveOccurred())
	oldVMTemplate := elfMachineTemplate.Spec.Template.Spec.Template
	elfMachineTemplate.Spec.Template.Spec.Template = input.VMTemplate

	// Convert the ElfMachineTemplate resource to unstructured
	elfMachineTemplateData, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&elfMachineTemplate)
	Expect(err).NotTo(HaveOccurred())
	infraObj := &unstructured.Unstructured{Object: elfMachineTemplateData}
	infraObj.SetGroupVersionKind(infraRef.GroupVersionKind())

	// Creates a new infra object
	newInfraObj := infraObj
	newInfraObjName := fmt.Sprintf("%s-%s", infraRef.Name, capiutil.RandomString(6))
	newInfraObj.SetName(newInfraObjName)
	newInfraObj.SetResourceVersion("")
	Expect(mgmtClient.Create(ctx, newInfraObj)).NotTo(HaveOccurred())

	Logf("Patching the new kubernetes version to KCP")
	// Patch the new infra object's ref to the KubeadmControlPlane
	patchHelper, err := patch.NewHelper(input.ControlPlane, mgmtClient)
	Expect(err).ToNot(HaveOccurred())

	infraRef.Name = newInfraObjName

	oldVersion := input.ControlPlane.Spec.Version
	input.ControlPlane.Spec.Version = input.KubernetesUpgradeVersion
	input.ControlPlane.Spec.MachineTemplate.InfrastructureRef.Name = newInfraObjName

	// If the ClusterConfiguration is not specified, create an empty one.
	if input.ControlPlane.Spec.KubeadmConfigSpec.ClusterConfiguration == nil {
		input.ControlPlane.Spec.KubeadmConfigSpec.ClusterConfiguration = new(bootstrapv1.ClusterConfiguration)
	}

	if input.ControlPlane.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local == nil {
		input.ControlPlane.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local = new(bootstrapv1.LocalEtcd)
	}

	input.ControlPlane.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local.ImageMeta.ImageTag = input.EtcdImageTag
	input.ControlPlane.Spec.KubeadmConfigSpec.ClusterConfiguration.DNS.ImageMeta.ImageTag = input.DNSImageTag

	Expect(patchHelper.Patch(ctx, input.ControlPlane)).To(Succeed())

	Logf("Waiting for Kubernetes versions of machines in KubeadmControlPlane %s/%s to be upgraded from %s to %s, VM template from %s to %s",
		input.ControlPlane.Namespace, input.ControlPlane.Name, oldVersion, input.KubernetesUpgradeVersion,
		oldVMTemplate, input.VMTemplate)

	Logf("Waiting for control-plane machines to have the upgraded kubernetes version")
	framework.WaitForControlPlaneMachinesToBeUpgraded(ctx, framework.WaitForControlPlaneMachinesToBeUpgradedInput{
		Lister:                   mgmtClient,
		Cluster:                  input.Cluster,
		MachineCount:             int(*input.ControlPlane.Spec.Replicas),
		KubernetesUpgradeVersion: input.KubernetesUpgradeVersion,
	}, input.WaitForMachinesToBeUpgraded...)

	Logf("Waiting for kube-proxy to have the upgraded kubernetes version")
	workloadCluster := input.ClusterProxy.GetWorkloadCluster(ctx, input.Cluster.Namespace, input.Cluster.Name)
	workloadClient := workloadCluster.GetClient()
	WaitForKubeProxyUpgrade(ctx, WaitForKubeProxyUpgradeInput{
		Getter:            workloadClient,
		KubernetesVersion: input.KubernetesUpgradeVersion,
	}, input.WaitForDNSUpgrade...)

	Logf("Waiting for CoreDNS to have the upgraded image tag")
	framework.WaitForDNSUpgrade(ctx, framework.WaitForDNSUpgradeInput{
		Getter:     workloadClient,
		DNSVersion: input.DNSImageTag,
	}, input.WaitForDNSUpgrade...)

	Logf("Waiting for etcd to have the upgraded image tag")
	lblSelector, err := labels.Parse("component=etcd")
	Expect(err).ToNot(HaveOccurred())
	framework.WaitForPodListCondition(ctx, framework.WaitForPodListConditionInput{
		Lister:      workloadClient,
		ListOptions: &client.ListOptions{LabelSelector: lblSelector},
		Condition:   framework.EtcdImageTagCondition(input.EtcdImageTag, int(*input.ControlPlane.Spec.Replicas)),
	}, input.WaitForEtcdUpgrade...)
}
