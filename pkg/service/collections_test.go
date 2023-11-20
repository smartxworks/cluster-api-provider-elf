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
		g.Expect(hosts.IDs()).To(gomega.ContainElements(*host1.ID, *host2.ID))
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
		unavailableHosts := hosts.FilterUnavailableHostsOrWithoutEnoughMemory(0)
		g.Expect(unavailableHosts.IsEmpty()).To(gomega.BeTrue())
		g.Expect(unavailableHosts.Len()).To(gomega.Equal(0))
		g.Expect(unavailableHosts.String()).To(gomega.Equal("[]"))

		hosts = NewHostsFromList([]*models.Host{host1, host2})
		unavailableHosts = hosts.FilterUnavailableHostsOrWithoutEnoughMemory(2)
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

func TestGPUVMInfoCollection(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("Find", func(t *testing.T) {
		gpuVMInfo1 := &models.GpuVMInfo{ID: TowerString("gpu1")}
		gpuVMInfo2 := &models.GpuVMInfo{ID: TowerString("gpu2")}

		gpuVMInfos := NewGPUVMInfos()
		g.Expect(gpuVMInfos.Get("404")).To(gomega.BeNil())
		g.Expect(gpuVMInfos.Len()).To(gomega.Equal(0))

		gpuVMInfos.Insert(gpuVMInfo1)
		g.Expect(gpuVMInfos.Contains("gpu1")).To(gomega.BeTrue())
		g.Expect(gpuVMInfos.Get("gpu1")).To(gomega.Equal(gpuVMInfo1))
		g.Expect(gpuVMInfos.UnsortedList()).To(gomega.Equal([]*models.GpuVMInfo{gpuVMInfo1}))
		count := 0
		gpuID := *gpuVMInfo1.ID
		gpuVMInfos.Iterate(func(g *models.GpuVMInfo) {
			count += 1
			gpuID = *g.ID
		})
		g.Expect(count).To(gomega.Equal(1))
		g.Expect(gpuID).To(gomega.Equal(*gpuVMInfo1.ID))

		gpuVMInfos = NewGPUVMInfosFromList([]*models.GpuVMInfo{gpuVMInfo1, gpuVMInfo2})
		filteredGPUVMInfos := gpuVMInfos.Filter(func(g *models.GpuVMInfo) bool {
			return g.ID != gpuVMInfo1.ID
		})
		g.Expect(filteredGPUVMInfos.Len()).To(gomega.Equal(1))
		g.Expect(filteredGPUVMInfos.Contains(*gpuVMInfo2.ID)).To(gomega.BeTrue())

		gpuVMInfo1.UserUsage = models.NewGpuDeviceUsage(models.GpuDeviceUsagePASSTHROUGH)
		g.Expect(NewGPUVMInfos(gpuVMInfo1).FilterAvailableGPUVMInfos().Len()).To(gomega.Equal(1))
		gpuVMInfo1.Vms = []*models.GpuVMDetail{{}}
		g.Expect(NewGPUVMInfos(gpuVMInfo1).FilterAvailableGPUVMInfos().Len()).To(gomega.Equal(0))

		gpuVMInfo2.UserUsage = models.NewGpuDeviceUsage(models.GpuDeviceUsageVGPU)
		gpuVMInfo2.AvailableVgpusNum = TowerInt32(1)
		g.Expect(NewGPUVMInfos(gpuVMInfo2).FilterAvailableGPUVMInfos().Len()).To(gomega.Equal(1))
		gpuVMInfo2.AvailableVgpusNum = TowerInt32(0)
		g.Expect(NewGPUVMInfos(gpuVMInfo2).FilterAvailableGPUVMInfos().Len()).To(gomega.Equal(0))
	})
}
