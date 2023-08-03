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

// FilterUnavailableHostsWithoutEnoughMemory returns a Hosts containing the unavailable host which has allocatable memory less than the specified memory.
func (s Hosts) FilterUnavailableHostsWithoutEnoughMemory(memory int64) Hosts {
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
