package service

import (
	"context"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	clusterutilv1 "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha3"
)

func NewClusterSrevice(client client.Client) *ClusterSrevice {
	return &ClusterSrevice{client}
}

type ClusterSrevice struct {
	Client client.Client
}

// GetByName gets and return a Cluster object using the specified params.
func (svr *ClusterSrevice) GetByName(ctx context.Context, namespace, name string) (*clusterv1.Cluster, error) {
	cluster := &clusterv1.Cluster{}
	key := client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}

	if err := svr.Client.Get(ctx, key, cluster); err != nil {
		return nil, err
	}

	return cluster, nil
}

// GetElfClusterByName gets a ElfCluster resource for the given CAPE Cluster.
func (svr *ClusterSrevice) GetElfClusterByName(ctx context.Context, namespace, name string) (*infrav1.ElfCluster, error) {
	elfCluster := &infrav1.ElfCluster{}
	key := client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}

	if err := svr.Client.Get(ctx, key, elfCluster); err != nil {
		return nil, err
	}

	return elfCluster, nil
}

// GetOwnerCluster returns the Cluster object owning the current resource.
func (svr *ClusterSrevice) GetOwnerCluster(ctx context.Context, obj metav1.ObjectMeta) (*clusterv1.Cluster, error) {
	for _, ref := range obj.OwnerReferences {
		if ref.Kind != "Cluster" {
			continue
		}

		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		if gv.Group == clusterv1.GroupVersion.Group {
			return svr.GetByName(ctx, obj.Namespace, ref.Name)
		}
	}

	return nil, nil
}

// GetClusterFromMetadata returns the Cluster object (if present) using the object metadata.
func (svr *ClusterSrevice) GetClusterFromMetadata(ctx context.Context, obj metav1.ObjectMeta) (*clusterv1.Cluster, error) {
	if obj.Labels[clusterv1.ClusterLabelName] == "" {
		return nil, errors.WithStack(clusterutilv1.ErrNoCluster)
	}

	return svr.GetByName(ctx, obj.Namespace, obj.Labels[clusterv1.ClusterLabelName])
}
