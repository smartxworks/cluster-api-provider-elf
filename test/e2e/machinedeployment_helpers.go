package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/cluster-api/test/framework"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha4"
)

// UpgradeMachineDeploymentInfrastructureRefAndWaitInput is the input type for UpgradeMachineDeploymentInfrastructureRefAndWait.
type UpgradeMachineDeploymentInfrastructureRefAndWaitInput struct {
	ClusterProxy                framework.ClusterProxy
	Cluster                     *clusterv1.Cluster
	UpgradeVersion              string
	VMTemplateUUID              string
	MachineDeployments          []*clusterv1.MachineDeployment
	WaitForMachinesToBeUpgraded []interface{}
}

// UpgradeMachineDeploymentInfrastructureRefAndWait upgrades a machine deployment kubernetes version
// and infrastructure ref and waits for its machines to be upgraded.
func UpgradeMachineDeploymentInfrastructureRefAndWait(ctx context.Context, input UpgradeMachineDeploymentInfrastructureRefAndWaitInput) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for UpgradeMachineDeploymentInfrastructureRefAndWait")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling UpgradeMachineDeploymentInfrastructureRefAndWait")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling UpgradeMachineDeploymentInfrastructureRefAndWait")
	Expect(input.UpgradeVersion).ToNot(BeNil(), "Invalid argument. input.UpgradeVersion can't be nil when calling UpgradeMachineDeploymentInfrastructureRefAndWait")
	Expect(input.VMTemplateUUID).ToNot(BeNil(), "Invalid argument. input.VMTemplateUUID can't be nil when calling UpgradeMachineDeploymentInfrastructureRefAndWait")
	Expect(input.MachineDeployments).ToNot(BeEmpty(), "Invalid argument. input.MachineDeployments can't be empty when calling UpgradeMachineDeploymentInfrastructureRefAndWait")

	mgmtClient := input.ClusterProxy.GetClient()

	for _, deployment := range input.MachineDeployments {
		Logf("Patching the new kubernetes version and infrastructure ref to Machine Deployment %s/%s", deployment.Namespace, deployment.Name)
		// Retrieve infra object
		infraRef := deployment.Spec.Template.Spec.InfrastructureRef
		var elfMachineTemplate infrav1.ElfMachineTemplate
		elfMachineTemplateKey := client.ObjectKey{
			Namespace: input.Cluster.Namespace,
			Name:      infraRef.Name,
		}
		Expect(mgmtClient.Get(ctx, elfMachineTemplateKey, &elfMachineTemplate)).NotTo(HaveOccurred())
		oldVMTemplateUUID := elfMachineTemplate.Spec.Template.Spec.Template
		elfMachineTemplate.Spec.Template.Spec.Template = input.VMTemplateUUID

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

		infraRef.Name = newInfraObjName
		deployment.Spec.Template.Spec.InfrastructureRef = infraRef
		oldVersion := deployment.Spec.Template.Spec.Version
		deployment.Spec.Template.Spec.Version = &input.UpgradeVersion
		Expect(patchHelper.Patch(ctx, deployment)).To(Succeed())

		Logf("Waiting for Kubernetes versions of machines in MachineDeployment %s/%s to be upgraded from %s to %s, VM template from %s to %s",
			deployment.Namespace, deployment.Name, *oldVersion, input.UpgradeVersion,
			oldVMTemplateUUID, input.VMTemplateUUID)

		Logf("Waiting for Kubernetes versions of machines in MachineDeployment %s/%s to be upgraded from %s to %s",
			deployment.Namespace, deployment.Name, *oldVersion, input.UpgradeVersion)
		framework.WaitForMachineDeploymentMachinesToBeUpgraded(ctx, framework.WaitForMachineDeploymentMachinesToBeUpgradedInput{
			Lister:                   mgmtClient,
			Cluster:                  input.Cluster,
			MachineCount:             int(*deployment.Spec.Replicas),
			KubernetesUpgradeVersion: input.UpgradeVersion,
			MachineDeployment:        *deployment,
		}, input.WaitForMachinesToBeUpgraded...)

		Logf("Waiting for rolling upgrade to complete.")
		framework.WaitForMachineDeploymentRollingUpgradeToComplete(ctx, framework.WaitForMachineDeploymentRollingUpgradeToCompleteInput{
			Getter:            mgmtClient,
			MachineDeployment: deployment,
		}, input.WaitForMachinesToBeUpgraded...)
	}
}
