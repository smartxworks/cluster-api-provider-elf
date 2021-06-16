package service

import (
	"context"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha3"
)

func NewMachineSrevice(client client.Client) *MachineSrevice {
	return &MachineSrevice{client}
}

type MachineSrevice struct {
	Client client.Client
}

// GetByName gets and return a Machine object using the specified params.
func (svr *MachineSrevice) GetByName(ctx context.Context, namespace, name string) (*clusterv1.Machine, error) {
	machine := &clusterv1.Machine{}
	key := client.ObjectKey{Name: name, Namespace: namespace}

	if err := svr.Client.Get(ctx, key, machine); err != nil {
		return nil, err
	}

	return machine, nil
}

// GetOwnerMachine returns the Machine object owning the current resource.
func (svr *MachineSrevice) GetOwnerMachine(ctx context.Context, obj metav1.ObjectMeta) (*clusterv1.Machine, error) {
	for _, ref := range obj.OwnerReferences {
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			return nil, err
		}

		if ref.Kind == "Machine" && gv.Group == clusterv1.GroupVersion.Group {
			return svr.GetByName(ctx, obj.Namespace, ref.Name)
		}
	}

	return nil, nil
}

// FindMachinesInCluster gets a cluster's Machine resources.
func (svr *MachineSrevice) FindMachinesInCluster(
	ctx context.Context,
	namespace,
	clusterName string) ([]*clusterv1.Machine, error) {

	labels := map[string]string{clusterv1.ClusterLabelName: clusterName}
	machineList := &clusterv1.MachineList{}

	if err := svr.Client.List(
		ctx, machineList,
		client.InNamespace(namespace),
		client.MatchingLabels(labels)); err != nil {
		return nil, errors.Wrapf(
			err, "error getting machines in cluster %s/%s",
			namespace, clusterName)
	}

	machines := make([]*clusterv1.Machine, len(machineList.Items))
	for i := range machineList.Items {
		machines[i] = &machineList.Items[i]
	}

	return machines, nil
}

// FindElfMachinesInCluster gets a cluster's ElfMachine resources.
func (svr *MachineSrevice) FindElfMachinesInCluster(
	ctx context.Context,
	namespace, clusterName string) ([]*infrav1.ElfMachine, error) {

	labels := map[string]string{clusterv1.ClusterLabelName: clusterName}
	machineList := &infrav1.ElfMachineList{}

	if err := svr.Client.List(
		ctx, machineList,
		client.InNamespace(namespace),
		client.MatchingLabels(labels)); err != nil {
		return nil, errors.Wrapf(
			err, "error getting machines in cluster %s/%s",
			namespace, clusterName)
	}

	machines := make([]*infrav1.ElfMachine, len(machineList.Items))
	for i := range machineList.Items {
		machines[i] = &machineList.Items[i]
	}

	return machines, nil
}

// GetElfMachineByName gets a ElfMachine resource for the given CAPE Machine.
func (svr *MachineSrevice) GetElfMachineByName(
	ctx context.Context,
	namespace, name string) (*infrav1.ElfMachine, error) {

	machine := &infrav1.ElfMachine{}
	key := client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}

	if err := svr.Client.Get(ctx, key, machine); err != nil {
		return nil, err
	}

	return machine, nil
}
