package context

import (
	"fmt"

	"github.com/go-logr/logr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha3"
)

// MachineContext is a Go context used with a ElfMachine.
type MachineContext struct {
	*ControllerContext
	Cluster     *clusterv1.Cluster
	Machine     *clusterv1.Machine
	ElfCluster  *infrav1.ElfCluster
	ElfMachine  *infrav1.ElfMachine
	Logger      logr.Logger
	PatchHelper *patch.Helper
}

// String returns ElfMachineGroupVersionKindElfMachineNamespace/ElfMachineName.
func (c *MachineContext) String() string {
	return fmt.Sprintf("%s %s/%s", c.ElfMachine.GroupVersionKind(), c.ElfMachine.Namespace, c.ElfMachine.Name)
}

// Patch updates the object and its status on the API server.
func (c *MachineContext) Patch() error {
	return c.PatchHelper.Patch(c, c.ElfMachine)
}
