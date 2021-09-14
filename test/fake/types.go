package fake

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/storage/names"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha4"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
)

const (
	Namespace = "default"

	ClusterKind = "Cluster"

	MachineKind = "Machine"

	ElfClusterKind = "ElfCluster"

	ElfMachineKind = "ElfMachine"
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
			Name:      names.SimpleNameGenerator.GenerateName("cluster-"),
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
	elfMachine := &infrav1.ElfMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("elfmachine-"),
			Namespace: Namespace,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: elfCluster.Name,
			},
		},
		Spec: infrav1.ElfMachineSpec{
			Network: infrav1.NetworkSpec{
				Devices: []infrav1.NetworkDeviceSpec{
					{},
				},
			},
		},
	}

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("machine-"),
			Namespace: Namespace,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: cluster.Name,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			InfrastructureRef: corev1.ObjectReference{
				APIVersion: infrav1.GroupVersion.String(),
				Kind:       ElfMachineKind,
				Name:       elfMachine.Name,
			},
		},
	}

	return elfMachine, machine
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

func InitClusterOwnerReferences(ctrlContext *context.ControllerContext,
	elfCluster *infrav1.ElfCluster, cluster *clusterv1.Cluster) {
	By("setting the OwnerRef on the ElfCluster")
	ph, err := patch.NewHelper(elfCluster, ctrlContext.Client)
	Expect(err).ShouldNot(HaveOccurred())
	elfCluster.OwnerReferences = append(elfCluster.OwnerReferences, metav1.OwnerReference{Kind: ClusterKind, APIVersion: clusterv1.GroupVersion.String(), Name: cluster.Name, UID: "blah"})
	Expect(ph.Patch(ctrlContext, elfCluster, patch.WithStatusObservedGeneration{})).ShouldNot(HaveOccurred())
}

func InitMachineOwnerReferences(ctrlContext *context.ControllerContext,
	elfMachine *infrav1.ElfMachine, machine *clusterv1.Machine) {
	By("setting the OwnerRef on the ElfMachine")
	ph, err := patch.NewHelper(elfMachine, ctrlContext.Client)
	Expect(err).ShouldNot(HaveOccurred())
	elfMachine.OwnerReferences = append(elfMachine.OwnerReferences, metav1.OwnerReference{Kind: MachineKind, APIVersion: clusterv1.GroupVersion.String(), Name: machine.Name, UID: "blah"})
	Expect(ph.Patch(ctrlContext, elfMachine, patch.WithStatusObservedGeneration{})).ShouldNot(HaveOccurred())
}

func InitOwnerReferences(
	ctrlContext *context.ControllerContext,
	elfCluster *infrav1.ElfCluster, cluster *clusterv1.Cluster,
	elfMachine *infrav1.ElfMachine, machine *clusterv1.Machine) {

	if elfCluster != nil {
		InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)
	}

	if elfMachine != nil {
		InitMachineOwnerReferences(ctrlContext, elfMachine, machine)
	}
}
