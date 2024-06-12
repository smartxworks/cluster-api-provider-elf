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
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
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

func NewClusterObjects() (*infrav1.ElfCluster, *clusterv1.Cluster) {
	elfCluster := &infrav1.ElfCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("elfcluster-"),
			Namespace: Namespace,
		},
		Spec:   infrav1.ElfClusterSpec{},
		Status: infrav1.ElfClusterStatus{},
	}

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      elfCluster.Name,
			Namespace: Namespace,
		},
		Spec: clusterv1.ClusterSpec{
			InfrastructureRef: &corev1.ObjectReference{
				APIVersion: infrav1.GroupVersion.String(),
				Kind:       ElfClusterKind,
				Name:       elfCluster.Name,
			},
		},
		Status: clusterv1.ClusterStatus{},
	}

	return elfCluster, cluster
}

func NewMachineObjects(elfCluster *infrav1.ElfCluster, cluster *clusterv1.Cluster) (*infrav1.ElfMachine, *clusterv1.Machine) {
	elfMachine := NewElfMachine(elfCluster)

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("machine-"),
			Namespace: Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     pointer.String("1.26.0"),
			InfrastructureRef: corev1.ObjectReference{
				APIVersion: infrav1.GroupVersion.String(),
				Kind:       ElfMachineKind,
				Name:       elfMachine.Name,
			},
		},
	}

	return elfMachine, machine
}

func NewElfMachine(elfCluster *infrav1.ElfCluster) *infrav1.ElfMachine {
	elfMachine := &infrav1.ElfMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:              names.SimpleNameGenerator.GenerateName("elfmachine-"),
			Namespace:         Namespace,
			CreationTimestamp: metav1.Now(),
			Annotations:       map[string]string{infrav1.CAPEVersionAnnotation: "latest"},
		},
		Spec: infrav1.ElfMachineSpec{
			HA:                true,
			NumCPUs:           NumCPUs,
			NumCoresPerSocket: NumCoresPerSocket,
			MemoryMiB:         MemoryMiB,
			DiskGiB:           DiskGiB,
			Network: infrav1.NetworkSpec{
				Devices: []infrav1.NetworkDeviceSpec{
					{
						NetworkType: infrav1.NetworkTypeIPV4DHCP,
					},
				},
			},
		},
	}

	if elfCluster != nil {
		elfMachine.Labels = map[string]string{
			clusterv1.ClusterNameLabel: elfCluster.Name,
		}
	}

	return elfMachine
}

func NewClusterAndMachineObjects() (*infrav1.ElfCluster, *clusterv1.Cluster, *infrav1.ElfMachine, *clusterv1.Machine, *corev1.Secret) {
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

func NewMD() *clusterv1.MachineDeployment {
	return &clusterv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("md-"),
			Namespace: Namespace,
		},
		Spec:   clusterv1.MachineDeploymentSpec{Replicas: pointer.Int32(1)},
		Status: clusterv1.MachineDeploymentStatus{},
	}
}

func InitClusterOwnerReferences(ctx goctx.Context, ctrlMgrCtx *context.ControllerManagerContext,
	elfCluster *infrav1.ElfCluster, cluster *clusterv1.Cluster) {
	By("setting the OwnerRef on the ElfCluster")
	ph, err := patch.NewHelper(elfCluster, ctrlMgrCtx.Client)
	Expect(err).ShouldNot(HaveOccurred())
	elfCluster.OwnerReferences = append(elfCluster.OwnerReferences, metav1.OwnerReference{Kind: ClusterKind, APIVersion: clusterv1.GroupVersion.String(), Name: cluster.Name, UID: "blah"})
	Expect(ph.Patch(ctx, elfCluster, patch.WithStatusObservedGeneration{})).ShouldNot(HaveOccurred())
}

func InitMachineOwnerReferences(ctx goctx.Context, ctrlMgrCtx *context.ControllerManagerContext,
	elfMachine *infrav1.ElfMachine, machine *clusterv1.Machine) {
	By("setting the OwnerRef on the ElfMachine")
	ph, err := patch.NewHelper(elfMachine, ctrlMgrCtx.Client)
	Expect(err).ShouldNot(HaveOccurred())
	elfMachine.OwnerReferences = append(elfMachine.OwnerReferences, metav1.OwnerReference{Kind: MachineKind, APIVersion: clusterv1.GroupVersion.String(), Name: machine.Name, UID: "blah"})
	Expect(ph.Patch(ctx, elfMachine, patch.WithStatusObservedGeneration{})).ShouldNot(HaveOccurred())
}

func InitOwnerReferences(
	ctx goctx.Context, ctrlMgrCtx *context.ControllerManagerContext,
	elfCluster *infrav1.ElfCluster, cluster *clusterv1.Cluster,
	elfMachine *infrav1.ElfMachine, machine *clusterv1.Machine) {
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

	labels[clusterv1.MachineControlPlaneLabel] = ""
	machine.SetLabels(labels)

	machine.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(kcp, controlplanev1.GroupVersion.WithKind("KubeadmControlPlane"))})
}

func ToWorkerMachine(machine metav1.Object, md *clusterv1.MachineDeployment) {
	labels := machine.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	labels[clusterv1.MachineDeploymentNameLabel] = ""
	if md != nil {
		labels[clusterv1.MachineDeploymentNameLabel] = md.Name
	}
	delete(labels, clusterv1.MachineControlPlaneLabel)

	machine.SetLabels(labels)
}

func NewElfMachineTemplate() *infrav1.ElfMachineTemplate {
	return &infrav1.ElfMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("elfMachineTemplate-"),
			Namespace: Namespace,
		},
		Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					DiskGiB: DiskGiB,
				},
			},
		},
	}
}

func SetElfMachineTemplateForElfMachine(elfMachine *infrav1.ElfMachine, emt *infrav1.ElfMachineTemplate) {
	if elfMachine.Annotations == nil {
		elfMachine.Annotations = make(map[string]string)
	}
	elfMachine.Annotations[clusterv1.TemplateClonedFromNameAnnotation] = emt.Name
	elfMachine.Spec = *emt.Spec.Template.Spec.DeepCopy()
}
