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

	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Hosts is a set of hosts.
type Hosts map[string]*models.Host

// NewHosts creates a Hosts. from a list of values.
func NewHosts(hosts ...*models.Host) Hosts {
	ss := make(Hosts, len(hosts))
	ss.Insert(hosts...)
	return ss
}

// NewHostsFromList creates a Hosts from the given host slice.
func NewHostsFromList(hosts []*models.Host) Hosts {
	ss := make(Hosts, len(hosts))
	for i := range hosts {
		ss.Insert(hosts[i])
	}
	return ss
}

func (s Hosts) Insert(hosts ...*models.Host) {
	for i := range hosts {
		if hosts[i] != nil {
			h := hosts[i]
			s[*h.ID] = h
		}
	}
}

func (s Hosts) Contains(hostID string) bool {
	_, ok := s[hostID]
	return ok
}

// Len returns the size of the set.
func (s Hosts) Len() int {
	return len(s)
}

func (s Hosts) IsEmpty() bool {
	return len(s) == 0
}

func (s Hosts) String() string {
	str := ""

	for _, host := range s {
		state := ""
		if host.HostState != nil {
			state = string(*host.HostState.State)
		}

		str += fmt.Sprintf("{id: %s,name: %s,memory: %d,status: %s,state: %s},", GetTowerString(host.ID), GetTowerString(host.Name), GetTowerInt64(host.AllocatableMemoryBytes), string(*host.Status), state)
	}

	return fmt.Sprintf("[%s]", str)
}

// FilterAvailableHostsWithEnoughMemory returns a Hosts containing the available host which has allocatable memory no less than the specified memory.
func (s Hosts) FilterAvailableHostsWithEnoughMemory(memory int64) Hosts {
	return s.Filter(func(h *models.Host) bool {
		ok, _ := IsAvailableHost(h, memory)
		return ok
	})
}

// FilterUnavailableHostsOrWithoutEnoughMemory returns a Hosts containing the unavailable hosts or available hosts whose available memory is less than the specified memory.
func (s Hosts) FilterUnavailableHostsOrWithoutEnoughMemory(memory int64) Hosts {
	return s.Filter(func(h *models.Host) bool {
		ok, _ := IsAvailableHost(h, memory)
		return !ok
	})
}

// Get returns a Host of the specified host.
func (s Hosts) Get(hostID string) *models.Host {
	if host, ok := s[hostID]; ok {
		return host
	}
	return nil
}

// Find returns a Hosts of the specified hosts.
func (s Hosts) Find(targetHosts sets.Set[string]) Hosts {
	return s.Filter(func(h *models.Host) bool {
		return targetHosts.Has(*h.ID)
	})
}

// UnsortedList returns the slice with contents in random order.
func (s Hosts) UnsortedList() []*models.Host {
	res := make([]*models.Host, 0, len(s))
	for _, value := range s {
		res = append(res, value)
	}
	return res
}

// Difference returns a copy without hosts that are in the given collection.
func (s Hosts) Difference(hosts Hosts) Hosts {
	return s.Filter(func(h *models.Host) bool {
		_, found := hosts[*h.ID]
		return !found
	})
}

// newFilteredHostCollection creates a Hosts from a filtered list of values.
func newFilteredHostCollection(filter Func, hosts ...*models.Host) Hosts {
	ss := make(Hosts, len(hosts))
	for i := range hosts {
		h := hosts[i]
		if filter(h) {
			ss.Insert(h)
		}
	}
	return ss
}

// Filter returns a Hosts containing only the Hosts that match all of the given HostFilters.
func (s Hosts) Filter(filters ...Func) Hosts {
	return newFilteredHostCollection(And(filters...), s.UnsortedList()...)
}

// Func is the functon definition for a filter.
type Func func(host *models.Host) bool

// And returns a filter that returns true if all of the given filters returns true.
func And(filters ...Func) Func {
	return func(host *models.Host) bool {
		for _, f := range filters {
			if !f(host) {
				return false
			}
		}
		return true
	}
}

// IDs returns the IDs of all hosts.
func (s Hosts) IDs() []string {
	res := make([]string, 0, len(s))
	for _, value := range s {
		res = append(res, *value.ID)
	}
	return res
}

// GPUVMInfos is a set of GPUVMInfos.
type GPUVMInfos map[string]*models.GpuVMInfo

// NewGPUVMInfos creates a GPUVMInfos. from a list of values.
func NewGPUVMInfos(gpuVMInfo ...*models.GpuVMInfo) GPUVMInfos {
	ss := make(GPUVMInfos, len(gpuVMInfo))
	ss.Insert(gpuVMInfo...)
	return ss
}

// NewGPUVMInfosFromList creates a Hosts from the given host slice.
func NewGPUVMInfosFromList(gpuVMInfos []*models.GpuVMInfo) GPUVMInfos {
	ss := make(GPUVMInfos, len(gpuVMInfos))
	for i := range gpuVMInfos {
		ss.Insert(gpuVMInfos[i])
	}
	return ss
}

func (s GPUVMInfos) Insert(gpuVMInfos ...*models.GpuVMInfo) {
	for i := range gpuVMInfos {
		if gpuVMInfos[i] != nil {
			g := gpuVMInfos[i]
			s[*g.ID] = g
		}
	}
}

// UnsortedList returns the slice with contents in random order.
func (s GPUVMInfos) UnsortedList() []*models.GpuVMInfo {
	res := make([]*models.GpuVMInfo, 0, len(s))
	for _, value := range s {
		res = append(res, value)
	}
	return res
}

// Get returns a GPUVMInfo of the specified gpuID.
func (s GPUVMInfos) Get(gpuID string) *models.GpuVMInfo {
	if gpuVMInfo, ok := s[gpuID]; ok {
		return gpuVMInfo
	}
	return nil
}

func (s GPUVMInfos) Contains(gpuID string) bool {
	_, ok := s[gpuID]
	return ok
}

func (s GPUVMInfos) Len() int {
	return len(s)
}

func (s GPUVMInfos) Iterate(fn func(*models.GpuVMInfo)) {
	for _, g := range s {
		fn(g)
	}
}

// Filter returns a GPUVMInfos containing only the GPUVMInfos that match all of the given GPUVMInfoFilters.
func (s GPUVMInfos) Filter(filters ...GPUVMInfoFilterFunc) GPUVMInfos {
	return newFilteredGPUVMInfoCollection(GPUVMInfoFilterAnd(filters...), s.UnsortedList()...)
}

// newFilteredGPUVMInfoCollection creates a GPUVMInfos from a filtered list of values.
func newFilteredGPUVMInfoCollection(filter GPUVMInfoFilterFunc, gpuVMInfos ...*models.GpuVMInfo) GPUVMInfos {
	ss := make(GPUVMInfos, len(gpuVMInfos))
	for i := range gpuVMInfos {
		g := gpuVMInfos[i]
		if filter(g) {
			ss.Insert(g)
		}
	}
	return ss
}

// GPUVMInfoFilterFunc is the functon definition for a filter.
type GPUVMInfoFilterFunc func(*models.GpuVMInfo) bool

// GPUVMInfoFilterAnd returns a filter that returns true if all of the given filters returns true.
func GPUVMInfoFilterAnd(filters ...GPUVMInfoFilterFunc) GPUVMInfoFilterFunc {
	return func(g *models.GpuVMInfo) bool {
		for _, f := range filters {
			if !f(g) {
				return false
			}
		}
		return true
	}
}

// FilterAvailableGPUVMInfos returns a GPUVMInfos containing the GPUs
// which can allocatable for virtual machines.
func (s GPUVMInfos) FilterAvailableGPUVMInfos() GPUVMInfos {
	return s.Filter(func(gpuVMInfo *models.GpuVMInfo) bool {
		if (*gpuVMInfo.UserUsage == models.GpuDeviceUsagePASSTHROUGH && len(gpuVMInfo.Vms) > 0) ||
			(*gpuVMInfo.UserUsage == models.GpuDeviceUsageVGPU && *gpuVMInfo.AvailableVgpusNum <= 0) {
			return false
		}

		return true
	})
}
