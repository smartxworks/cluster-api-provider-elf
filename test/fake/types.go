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

package fake

import (
	goctx "context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/utils/ptr"
	controlplanev1 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta2"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/deprecated/v1beta1/patch"

	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
)

const (
	// Namespace is the fake namespace.
	Namespace = "default"

	// ClusterKind is the fake cluster kind.
	ClusterKind = "Cluster"

	// MachineKind is the fake machine kind.
	MachineKind = "Machine"

	// ElfClusterKind is the fake elf cluster kind.
	ElfClusterKind = "ElfCluster"

	// ElfMachineKind is the fake elf machine kind.
	ElfMachineKind = "ElfMachine"

	// NumCPUs is the default CPU number.
	NumCPUs = 6
	// NumCoresPerSocket is the default CPU number of socket.
	NumCoresPerSocket = 6
	// DiskGiB is the default disk size.
	DiskGiB = 60
	// MemoryMiB is the default memory size.
	MemoryMiB = 1024 * 7
)

func NewClusterObjects() (*capev1.ElfCluster, *capiv1.Cluster) {
	elfCluster := &capev1.ElfCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("elfcluster-"),
			Namespace: Namespace,
		},
		Spec: capev1.ElfClusterSpec{
			ClusterType: capev1.ElfClusterTypeStandard,
		},
		Status: capev1.ElfClusterStatus{},
	}

	cluster := &capiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      elfCluster.Name,
			Namespace: Namespace,
		},
		Spec: capiv1.ClusterSpec{
			InfrastructureRef: capiv1.ContractVersionedObjectReference{
				APIGroup: capev1.GroupVersion.Group,
				Kind:     ElfClusterKind,
				Name:     elfCluster.Name,
			},
		},
		Status: capiv1.ClusterStatus{},
	}

	return elfCluster, cluster
}

func NewMachineObjects(elfCluster *capev1.ElfCluster, cluster *capiv1.Cluster) (*capev1.ElfMachine, *capiv1.Machine) {
	elfMachine := NewElfMachine(elfCluster)

	machine := &capiv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("machine-"),
			Namespace: Namespace,
			Labels: map[string]string{
				capiv1.ClusterNameLabel: cluster.Name,
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: capiv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "1.26.0",
			InfrastructureRef: capiv1.ContractVersionedObjectReference{
				APIGroup: capev1.GroupVersion.Group,
				Kind:     ElfMachineKind,
				Name:     elfMachine.Name,
			},
		},
	}

	return elfMachine, machine
}

func NewElfMachine(elfCluster *capev1.ElfCluster) *capev1.ElfMachine {
	elfMachine := &capev1.ElfMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:              names.SimpleNameGenerator.GenerateName("elfmachine-"),
			Namespace:         Namespace,
			CreationTimestamp: metav1.Now(),
			Annotations:       map[string]string{capev1.CAPEVersionAnnotation: "latest"},
		},
		Spec: capev1.ElfMachineSpec{
			HA:                true,
			NumCPUs:           NumCPUs,
			NumCoresPerSocket: NumCoresPerSocket,
			MemoryMiB:         MemoryMiB,
			DiskGiB:           DiskGiB,
			Network: capev1.NetworkSpec{
				Devices: []capev1.NetworkDeviceSpec{
					{
						NetworkType: capev1.NetworkTypeIPV4DHCP,
					},
				},
			},
		},
	}

	if elfCluster != nil {
		elfMachine.Labels = map[string]string{
			capiv1.ClusterNameLabel: elfCluster.Name,
		}
	}

	return elfMachine
}

func NewClusterAndMachineObjects() (*capev1.ElfCluster, *capiv1.Cluster, *capev1.ElfMachine, *capiv1.Machine, *corev1.Secret) {
	elfCluster, cluster := NewClusterObjects()
	elfMachine, machine := NewMachineObjects(elfCluster, cluster)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: elfCluster.Namespace,
			Name:      names.SimpleNameGenerator.GenerateName("bootstrap-data-"),
		},
		Data: map[string][]byte{
			"value": []byte("shell-script"),
		},
	}

	return elfCluster, cluster, elfMachine, machine, secret
}

func NewKCP() *controlplanev1.KubeadmControlPlane {
	return &controlplanev1.KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("kcp-"),
			Namespace: Namespace,
		},
		Spec:   controlplanev1.KubeadmControlPlaneSpec{},
		Status: controlplanev1.KubeadmControlPlaneStatus{},
	}
}

func NewMD() *capiv1.MachineDeployment {
	return &capiv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("md-"),
			Namespace: Namespace,
		},
		Spec:   capiv1.MachineDeploymentSpec{Replicas: ptr.To[int32](1)},
		Status: capiv1.MachineDeploymentStatus{},
	}
}

func InitClusterOwnerReferences(ctx goctx.Context, ctrlMgrCtx *context.ControllerManagerContext,
	elfCluster *capev1.ElfCluster, cluster *capiv1.Cluster) {
	By("setting the OwnerRef on the ElfCluster")
	ph, err := patch.NewHelper(elfCluster, ctrlMgrCtx.Client)
	Expect(err).ShouldNot(HaveOccurred())
	elfCluster.OwnerReferences = append(elfCluster.OwnerReferences, metav1.OwnerReference{Kind: ClusterKind, APIVersion: capiv1.GroupVersion.String(), Name: cluster.Name, UID: "blah"})
	Expect(ph.Patch(ctx, elfCluster, patch.WithStatusObservedGeneration{})).ShouldNot(HaveOccurred())
}

func InitMachineOwnerReferences(ctx goctx.Context, ctrlMgrCtx *context.ControllerManagerContext,
	elfMachine *capev1.ElfMachine, machine *capiv1.Machine) {
	By("setting the OwnerRef on the ElfMachine")
	ph, err := patch.NewHelper(elfMachine, ctrlMgrCtx.Client)
	Expect(err).ShouldNot(HaveOccurred())
	elfMachine.OwnerReferences = append(elfMachine.OwnerReferences, metav1.OwnerReference{Kind: MachineKind, APIVersion: capiv1.GroupVersion.String(), Name: machine.Name, UID: "blah"})
	Expect(ph.Patch(ctx, elfMachine, patch.WithStatusObservedGeneration{})).ShouldNot(HaveOccurred())
}

func InitOwnerReferences(
	ctx goctx.Context, ctrlMgrCtx *context.ControllerManagerContext,
	elfCluster *capev1.ElfCluster, cluster *capiv1.Cluster,
	elfMachine *capev1.ElfMachine, machine *capiv1.Machine) {
	if elfCluster != nil {
		InitClusterOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster)
	}

	if elfMachine != nil {
		InitMachineOwnerReferences(ctx, ctrlMgrCtx, elfMachine, machine)
	}
}

func ToCPMachine(machine metav1.Object, kcp *controlplanev1.KubeadmControlPlane) {
	labels := machine.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	labels[capiv1.MachineControlPlaneLabel] = ""
	machine.SetLabels(labels)

	machine.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(kcp, controlplanev1.GroupVersion.WithKind("KubeadmControlPlane"))})
}

func ToWorkerMachine(machine metav1.Object, md *capiv1.MachineDeployment) {
	labels := machine.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	labels[capiv1.MachineDeploymentNameLabel] = ""
	if md != nil {
		labels[capiv1.MachineDeploymentNameLabel] = md.Name
	}
	delete(labels, capiv1.MachineControlPlaneLabel)

	machine.SetLabels(labels)
}

func NewElfMachineTemplate() *capev1.ElfMachineTemplate {
	return &capev1.ElfMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("elfMachineTemplate-"),
			Namespace: Namespace,
		},
		Spec: capev1.ElfMachineTemplateSpec{
			Template: capev1.ElfMachineTemplateResource{
				Spec: capev1.ElfMachineSpec{
					DiskGiB: DiskGiB,
					Network: capev1.NetworkSpec{
						Devices: []capev1.NetworkDeviceSpec{
							{
								NetworkType: capev1.NetworkTypeIPV4DHCP,
							},
						},
					},
				},
			},
		},
	}
}

func SetElfMachineTemplateForElfMachine(elfMachine *capev1.ElfMachine, emt *capev1.ElfMachineTemplate) {
	if elfMachine.Annotations == nil {
		elfMachine.Annotations = make(map[string]string)
	}
	elfMachine.Annotations[capiv1.TemplateClonedFromNameAnnotation] = emt.Name
	elfMachine.Spec = *emt.Spec.Template.Spec.DeepCopy()
}
