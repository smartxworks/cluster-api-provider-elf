/*
Copyright 2024.

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

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

// MachineTemplateContext is a Go context used with an ElfMachineTemplate.
type MachineTemplateContext struct {
	Cluster            *clusterv1.Cluster
	ElfCluster         *infrav1.ElfCluster
	ElfMachineTemplate *infrav1.ElfMachineTemplate
	VMService          service.VMService
}

// String returns ElfMachineTemplateGroupVersionKindElfMachineTemplateNamespace/ElfMachineTemplateName.
func (c *MachineTemplateContext) String() string {
	return fmt.Sprintf("%s %s/%s", c.ElfMachineTemplate.GroupVersionKind(), c.ElfMachineTemplate.Namespace, c.ElfMachineTemplate.Name)
}
