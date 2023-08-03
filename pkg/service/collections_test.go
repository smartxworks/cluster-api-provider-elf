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
	"fmt"
	"testing"

	"github.com/onsi/gomega"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/pointer"
)

func TestHostCollection(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("Find", func(t *testing.T) {
		host1 := &models.Host{ID: TowerString("1"), Name: TowerString("host1")}
		host2 := &models.Host{ID: TowerString("2"), Name: TowerString("host2")}

		hosts := NewHosts()
		g.Expect(hosts.Find(sets.Set[string]{}.Insert(*host1.ID)).Len()).To(gomega.Equal(0))

		hosts = NewHostsFromList([]*models.Host{host1, host2})
		g.Expect(hosts.Get(*host1.ID)).To(gomega.Equal(host1))
		g.Expect(hosts.Get(*TowerString("404"))).To(gomega.BeNil())
		g.Expect(hosts.Find(sets.Set[string]{}.Insert(*host1.ID)).Contains(*host1.ID)).To(gomega.BeTrue())
		g.Expect(hosts.Find(sets.Set[string]{}.Insert(*host1.ID)).Len()).To(gomega.Equal(1))
	})

	t.Run("Available", func(t *testing.T) {
		host1 := &models.Host{ID: TowerString("1"), Name: TowerString("host1"), AllocatableMemoryBytes: pointer.Int64(1), Status: models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)}
		host2 := &models.Host{ID: TowerString("2"), Name: TowerString("host2"), AllocatableMemoryBytes: pointer.Int64(2), Status: models.NewHostStatus(models.HostStatusCONNECTEDHEALTHY)}

		hosts := NewHosts()
		g.Expect(hosts.FilterAvailableHostsWithEnoughMemory(0).Len()).To(gomega.Equal(0))

		hosts = NewHostsFromList([]*models.Host{host1, host2})
		availableHosts := hosts.FilterAvailableHostsWithEnoughMemory(2)
		g.Expect(availableHosts.Len()).To(gomega.Equal(1))
		g.Expect(availableHosts.Contains(*host2.ID)).To(gomega.BeTrue())

		hosts = NewHosts()
		unavailableHosts := hosts.FilterUnavailableHostsWithoutEnoughMemory(0)
		g.Expect(unavailableHosts.IsEmpty()).To(gomega.BeTrue())
		g.Expect(unavailableHosts.Len()).To(gomega.Equal(0))
		g.Expect(unavailableHosts.String()).To(gomega.Equal("[]"))

		hosts = NewHostsFromList([]*models.Host{host1, host2})
		unavailableHosts = hosts.FilterUnavailableHostsWithoutEnoughMemory(2)
		g.Expect(unavailableHosts.Len()).To(gomega.Equal(1))
		g.Expect(unavailableHosts.Contains(*host1.ID)).To(gomega.BeTrue())
		g.Expect(unavailableHosts.String()).To(gomega.Equal(fmt.Sprintf("[{id: %s,name: %s,memory: %d,status: %s,state: %s},]", *host1.ID, *host1.Name, *host1.AllocatableMemoryBytes, string(*host1.Status), "")))
	})

	t.Run("Difference", func(t *testing.T) {
		host1 := &models.Host{ID: TowerString("1"), Name: TowerString("host1")}
		host2 := &models.Host{ID: TowerString("2"), Name: TowerString("host2")}

		g.Expect(NewHosts().Difference(NewHosts()).Len()).To(gomega.Equal(0))
		g.Expect(NewHosts().Difference(NewHosts(host1)).Len()).To(gomega.Equal(0))
		g.Expect(NewHosts(host1).Difference(NewHosts(host1)).Len()).To(gomega.Equal(0))
		g.Expect(NewHosts(host1).Difference(NewHosts()).Contains(*host1.ID)).To(gomega.BeTrue())
		g.Expect(NewHosts(host1).Difference(NewHosts(host2)).Contains(*host1.ID)).To(gomega.BeTrue())
		g.Expect(NewHosts(host1, host2).Difference(NewHosts(host2)).Contains(*host1.ID)).To(gomega.BeTrue())
	})
}
