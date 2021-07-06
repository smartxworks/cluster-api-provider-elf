package context

import (
	"fmt"

	"github.com/go-logr/logr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha3"
)

// ClusterContext is a Go context used with a ElfCluster.
type ClusterContext struct {
	*ControllerContext
	Cluster     *clusterv1.Cluster
	ElfCluster  *infrav1.ElfCluster
	PatchHelper *patch.Helper
	Logger      logr.Logger

	Username string
	Password string
}

// String returns ElfClusterGroupVersionKind ElfClusterNamespace/ElfClusterName.
func (r *ClusterContext) String() string {
	return fmt.Sprintf("%s %s/%s", r.ElfCluster.GroupVersionKind(), r.ElfCluster.Namespace, r.ElfCluster.Name)
}

// Patch updates the object and its status on the API server.
func (r *ClusterContext) Patch() error {
	return r.PatchHelper.Patch(r, r.ElfCluster)
}
