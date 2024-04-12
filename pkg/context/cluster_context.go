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

package context

import (
	goctx "context"
	"fmt"

	"github.com/go-logr/logr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

// ClusterContext is a Go context used with a ElfCluster.
type ClusterContext struct {
	Cluster     *clusterv1.Cluster
	ElfCluster  *infrav1.ElfCluster
	PatchHelper *patch.Helper
	Logger      logr.Logger
	VMService   service.VMService
}

// String returns ElfClusterGroupVersionKind ElfClusterNamespace/ElfClusterName.
func (r *ClusterContext) String() string {
	return fmt.Sprintf("%s %s/%s", r.ElfCluster.GroupVersionKind(), r.ElfCluster.Namespace, r.ElfCluster.Name)
}

// Patch updates the object and its status on the API server.
func (r *ClusterContext) Patch(ctx goctx.Context) error {
	return r.PatchHelper.Patch(ctx, r.ElfCluster)
}
