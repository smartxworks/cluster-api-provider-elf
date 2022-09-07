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

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/test/framework"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

// UpgradeMachineDeploymentsAndWaitInput is the input type for UpgradeMachineDeploymentsAndWait.
type UpgradeMachineDeploymentsAndWaitInput struct {
	ClusterProxy                framework.ClusterProxy
	Cluster                     *clusterv1.Cluster
	UpgradeVersion              string
	VMTemplate                  string
	MachineDeployments          []*clusterv1.MachineDeployment
	WaitForMachinesToBeUpgraded []interface{}
}

// UpgradeMachineDeploymentsAndWait upgrades a machine deployment and waits for its machines to be upgraded.
func UpgradeMachineDeploymentsAndWait(ctx context.Context, input UpgradeMachineDeploymentsAndWaitInput) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for UpgradeMachineDeploymentsAndWait")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling UpgradeMachineDeploymentsAndWait")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling UpgradeMachineDeploymentsAndWait")
	Expect(input.UpgradeVersion).ToNot(BeNil(), "Invalid argument. input.UpgradeVersion can't be nil when calling UpgradeMachineDeploymentsAndWait")
	Expect(input.VMTemplate).ToNot(BeNil(), "Invalid argument. input.VMTemplate can't be nil when calling UpgradeMachineDeploymentsAndWait")
	Expect(input.MachineDeployments).ToNot(BeEmpty(), "Invalid argument. input.MachineDeployments can't be empty when calling UpgradeMachineDeploymentsAndWait")

	mgmtClient := input.ClusterProxy.GetClient()

	for _, deployment := range input.MachineDeployments {
		Logf("Patching the new kubernetes version to Machine Deployment %s/%s", deployment.Namespace, deployment.Name)
		// Retrieve infra object
		infraRef := deployment.Spec.Template.Spec.InfrastructureRef
		var elfMachineTemplate infrav1.ElfMachineTemplate
		Expect(mgmtClient.Get(ctx, client.ObjectKey{
			Namespace: input.Cluster.Namespace,
			Name:      infraRef.Name,
		}, &elfMachineTemplate)).NotTo(HaveOccurred())
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

		// Patch the new infra object's ref to the machine deployment
		patchHelper, err := patch.NewHelper(deployment, mgmtClient)
		Expect(err).ToNot(HaveOccurred())

		oldVersion := deployment.Spec.Template.Spec.Version
		deployment.Spec.Template.Spec.Version = &input.UpgradeVersion
		deployment.Spec.Template.Spec.InfrastructureRef.Name = newInfraObjName

		Expect(patchHelper.Patch(ctx, deployment)).To(Succeed())

		Logf("Waiting for Kubernetes versions of machines in MachineDeployment %s/%s to be upgraded from %s to %s, VM template from %s to %s",
			deployment.Namespace, deployment.Name, *oldVersion, input.UpgradeVersion,
			oldVMTemplate, input.VMTemplate)

		Logf("Waiting for Kubernetes versions of machines in MachineDeployment %s/%s to be upgraded from %s to %s",
			deployment.Namespace, deployment.Name, *oldVersion, input.UpgradeVersion)
		framework.WaitForMachineDeploymentMachinesToBeUpgraded(ctx, framework.WaitForMachineDeploymentMachinesToBeUpgradedInput{
			Lister:                   mgmtClient,
			Cluster:                  input.Cluster,
			MachineCount:             int(*deployment.Spec.Replicas),
			KubernetesUpgradeVersion: input.UpgradeVersion,
			MachineDeployment:        *deployment,
		}, input.WaitForMachinesToBeUpgraded...)
	}
}
