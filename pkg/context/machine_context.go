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
	"fmt"

	"github.com/go-logr/logr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

// MachineContext is a Go context used with an ElfMachine.
type MachineContext struct {
	*ControllerContext
	Cluster     *clusterv1.Cluster
	Machine     *clusterv1.Machine
	ElfCluster  *infrav1.ElfCluster
	ElfMachine  *infrav1.ElfMachine
	Logger      logr.Logger
	PatchHelper *patch.Helper
	VMService   service.VMService
}

// String returns ElfMachineGroupVersionKindElfMachineNamespace/ElfMachineName.
func (c *MachineContext) String() string {
	return fmt.Sprintf("%s %s/%s", c.ElfMachine.GroupVersionKind(), c.ElfMachine.Namespace, c.ElfMachine.Name)
}

// Patch updates the object and its status on the API server.
func (c *MachineContext) Patch() error {
	return c.PatchHelper.Patch(c, c.ElfMachine)
}
