/*
Copyright 2023.

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

package service

import (
	"testing"

	"github.com/onsi/gomega"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	"k8s.io/utils/pointer"
)

func TestIsAvailableHost(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("should return false when status is CONNECTED_ERROR/SESSION_EXPIRED/INITIALIZING", func(t *testing.T) {
		host := &models.Host{Status: nil}
		ok, message := IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.Equal(""))

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusCONNECTEDERROR)}
		ok, message = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring(string(models.HostStatusCONNECTEDERROR)))

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusSESSIONEXPIRED)}
		ok, message = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring(string(models.HostStatusSESSIONEXPIRED)))

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusINITIALIZING)}
		ok, message = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring(string(models.HostStatusINITIALIZING)))

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusCONNECTING)}
		ok, _ = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusCONNECTEDWARNING)}
		ok, _ = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())

		host = &models.Host{Status: models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)}
		ok, _ = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())
	})

	t.Run("should return false when state is MAINTENANCEMODE/ENTERINGMAINTENANCEMODE", func(t *testing.T) {
		host := &models.Host{HostState: nil, Status: models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)}
		ok, message := IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(message).To(gomega.Equal(""))

		host.HostState = &models.NestedMaintenanceHostState{State: models.NewMaintenanceModeEnum(models.MaintenanceModeEnumMAINTENANCEMODE)}
		ok, message = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring(string(models.MaintenanceModeEnumMAINTENANCEMODE)))

		host.HostState = &models.NestedMaintenanceHostState{State: models.NewMaintenanceModeEnum(models.MaintenanceModeEnumENTERINGMAINTENANCEMODE)}
		ok, message = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring(string(models.MaintenanceModeEnumENTERINGMAINTENANCEMODE)))

		host.HostState = &models.NestedMaintenanceHostState{State: models.NewMaintenanceModeEnum(models.MaintenanceModeEnumINUSE)}
		ok, _ = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())

		host.HostState = &models.NestedMaintenanceHostState{State: models.NewMaintenanceModeEnum(models.MaintenanceModeEnumINUSE)}
		ok, _ = IsAvailableHost(host, 0)
		g.Expect(ok).To(gomega.BeTrue())
	})

	t.Run("should return false when insufficient memory", func(t *testing.T) {
		host := &models.Host{AllocatableMemoryBytes: pointer.Int64(2), Status: models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)}

		ok, _ := IsAvailableHost(host, 1)
		g.Expect(ok).To(gomega.BeTrue())

		ok, _ = IsAvailableHost(host, 2)
		g.Expect(ok).To(gomega.BeTrue())

		ok, message := IsAvailableHost(host, 3)
		g.Expect(ok).To(gomega.BeFalse())
		g.Expect(message).To(gomega.ContainSubstring("3"))
	})
}
