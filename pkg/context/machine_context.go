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

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/cluster-api/util/deprecated/v1beta1/patch"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

const (
	cloudInitHostnameRandomLength = 5
)

// MachineContext is a Go context used with an ElfMachine.
type MachineContext struct {
	Cluster     *clusterv1.Cluster
	Machine     *clusterv1.Machine
	ElfCluster  *infrav1.ElfCluster
	ElfMachine  *infrav1.ElfMachine
	PatchHelper *patch.Helper
	VMService   service.VMService
}

// String returns ElfMachineGroupVersionKindElfMachineNamespace/ElfMachineName.
func (c *MachineContext) String() string {
	return fmt.Sprintf("%s %s/%s", c.ElfMachine.GroupVersionKind(), c.ElfMachine.Namespace, c.ElfMachine.Name)
}

// Patch updates the object and its status on the API server.
func (c *MachineContext) Patch(ctx goctx.Context) error {
	return c.PatchHelper.Patch(ctx, c.ElfMachine)
}

func (c *MachineContext) GetFailureDomain() string {
	if c.Machine != nil && c.Machine.Spec.FailureDomain != nil {
		return *c.Machine.Spec.FailureDomain
	}

	return ""
}

func (c *MachineContext) GetCloudFailureDomain() *infrav1.CloudFailureDomain {
	fd := c.GetFailureDomain()
	if fd == "" {
		return nil
	}

	if c.ElfMachine == nil {
		return nil
	}

	failureDomains := c.ElfMachine.Spec.FailureDomains
	for i := range failureDomains {
		if failureDomains[i].ComputeCluster == fd {
			return &failureDomains[i]
		}
	}

	return nil
}

// GetElfClusterID returns the appropriate cluster ID for a machine.
// If FailureDomains is configured and a cluster has been selected, returns the selected cluster.
// Otherwise, returns the default cluster from ElfCluster.
func (c *MachineContext) GetElfClusterID() string {
	fd := c.GetFailureDomain()
	if fd != "" {
		return fd
	} else if c.ElfCluster != nil {
		return c.ElfCluster.Spec.Cluster
	}

	return ""
}

// GenerateHostname generates a hostname for the machine.
// If a HostNamePrefix is specified in the failure domain or machine spec, the hostname will be generated as <prefix>-<random string>.
// Otherwise, the machine name will be used as the hostname.
func (c *MachineContext) GenerateHostname() string {
	if c == nil || c.ElfMachine == nil {
		return ""
	}

	var prefix string
	if failureDomain := c.GetCloudFailureDomain(); failureDomain != nil {
		prefix = failureDomain.HostNamePrefix
	}
	if prefix == "" {
		prefix = c.ElfMachine.Spec.HostNamePrefix
	}
	if prefix != "" {
		return fmt.Sprintf("%s-%s", prefix, capiutil.RandomString(cloudInitHostnameRandomLength))
	}

	return c.ElfMachine.Name
}
