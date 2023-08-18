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

package controllers

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

var _ = Describe("VMLimiter", func() {
	var vmName string

	BeforeEach(func() {
		vmName = fake.UUID()
		resetVMConcurrentMap()
		resetVMOperationMap()
	})

	It("acquireTicketForCreateVM", func() {
		ok, msg := acquireTicketForCreateVM(vmName, true)
		Expect(ok).To(BeTrue())
		Expect(msg).To(Equal(""))
		Expect(vmOperationMap.Has(getCreationLockKey(vmName))).To(BeFalse())

		setVMDuplicate(vmName)
		Expect(vmOperationMap.Has(getCreationLockKey(vmName))).To(BeTrue())
		ok, msg = acquireTicketForCreateVM(vmName, true)
		Expect(ok).To(BeFalse())
		Expect(msg).To(Equal("Duplicate virtual machine detected"))

		vmOperationMap.Set(getCreationLockKey(vmName), nil, vmSilenceTime)
		vmOperationMap.Values[getCreationLockKey(vmName)].Expiration = time.Now().Add(-vmSilenceTime)
		Expect(vmOperationMap.Has(getCreationLockKey(vmName))).To(BeFalse())
		ok, msg = acquireTicketForCreateVM(vmName, true)
		Expect(ok).To(BeTrue())
		Expect(msg).To(Equal(""))

		ok, msg = acquireTicketForCreateVM(vmName, false)
		Expect(ok).To(BeTrue())
		Expect(msg).To(Equal(""))
		Expect(vmConcurrentMap.Has(vmName)).To(BeTrue())

		for i := 0; i < config.MaxConcurrentVMCreations-1; i++ {
			vmConcurrentMap.Set(fake.UUID(), nil, creationTimeout)
		}
		ok, msg = acquireTicketForCreateVM(vmName, false)
		Expect(ok).To(BeFalse())
		Expect(msg).To(Equal("The number of concurrently created VMs has reached the limit"))
	})

	It("releaseTicketForCreateVM", func() {
		Expect(vmConcurrentMap.Has(vmName)).To(BeFalse())
		ok, _ := acquireTicketForCreateVM(vmName, false)
		Expect(ok).To(BeTrue())
		Expect(vmConcurrentMap.Has(vmName)).To(BeTrue())
		releaseTicketForCreateVM(vmName)
		Expect(vmConcurrentMap.Has(vmName)).To(BeFalse())
	})
})

var _ = Describe("VM Operation Limiter", func() {
	var vmName string

	BeforeEach(func() {
		vmName = fake.UUID()
	})

	It("acquireTicketForUpdatingVM", func() {
		resetVMOperationMap()
		Expect(acquireTicketForUpdatingVM(vmName)).To(BeTrue())
		Expect(vmOperationMap.Has(vmName)).To(BeTrue())
		Expect(acquireTicketForUpdatingVM(vmName)).To(BeFalse())

		resetVMOperationMap()
		vmOperationMap.Set(getCreationLockKey(vmName), nil, vmSilenceTime)
		vmOperationMap.Values[getCreationLockKey(vmName)].Expiration = time.Now().Add(-vmSilenceTime)
		Expect(vmOperationMap.Has(vmName)).To(BeFalse())
		Expect(acquireTicketForUpdatingVM(vmName)).To(BeTrue())
		Expect(vmOperationMap.Has(vmName)).To(BeTrue())
	})
})

var _ = Describe("Placement Group Operation Limiter", func() {
	var groupName string

	BeforeEach(func() {
		groupName = fake.UUID()
	})

	It("acquireTicketForPlacementGroupOperation", func() {
		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeTrue())
		Expect(placementGroupOperationMap.Has(groupName)).To(BeTrue())

		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeFalse())
		releaseTicketForPlacementGroupOperation(groupName)

		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeTrue())
		Expect(placementGroupOperationMap.Has(groupName)).To(BeTrue())
	})

	It("canCreatePlacementGroup", func() {
		key := getCreationLockKey(groupName)

		Expect(placementGroupOperationMap.Has(key)).To(BeFalse())
		Expect(canCreatePlacementGroup(groupName)).To(BeTrue())

		setPlacementGroupDuplicate(groupName)
		Expect(placementGroupOperationMap.Has(key)).To(BeTrue())
		Expect(canCreatePlacementGroup(groupName)).To(BeFalse())

		placementGroupOperationMap.Values[key].Expiration = placementGroupOperationMap.Values[key].Expiration.Add(-placementGroupSilenceTime)
		Expect(placementGroupOperationMap.Has(key)).To(BeFalse())
		Expect(canCreatePlacementGroup(groupName)).To(BeTrue())
		Expect(placementGroupOperationMap.Has(key)).To(BeFalse())

		Expect(canCreatePlacementGroup(groupName)).To(BeTrue())
	})
})

var _ = Describe("ttlMap", func() {
	It("Test ttlMap", func() {
		key := fake.UUID()
		testMap := newTTLMap(gcInterval)
		Expect(testMap.Len()).To(Equal(0))
		Expect(testMap.Has(key)).To(BeFalse())
		_, ok := testMap.Get(key)
		Expect(ok).To(BeFalse())

		testMap.Set(key, 1, 1*time.Minute)
		Expect(testMap.Has(key)).To(BeTrue())
		Expect(testMap.Len()).To(Equal(1))
		val, ok := testMap.Get(key)
		Expect(ok).To(BeTrue())
		ival, valid := val.(int)
		Expect(valid).To(BeTrue())
		Expect(ival).To(Equal(1))

		testMap.Del(key)
		Expect(testMap.Len()).To(Equal(0))
		Expect(testMap.Has(key)).To(BeFalse())

		testMap.Set(key, nil, 1*time.Minute)
		Expect(testMap.Has(key)).To(BeTrue())
		testMap.Values[key].Expiration = time.Now().Add(-1 * time.Minute)
		Expect(testMap.Has(key)).To(BeFalse())
		Expect(testMap.Len()).To(Equal(0))

		testMap.Set(key, nil, 1*time.Minute)
		Expect(testMap.Has(key)).To(BeTrue())
		testMap.Values[key].Expiration = time.Now().Add(-1 * time.Minute)
		testMap.LastGCTime = time.Now().Add(-testMap.GCInterval)
		Expect(testMap.Has(key)).To(BeFalse())
		Expect(testMap.Len()).To(Equal(0))
	})
})

func resetVMConcurrentMap() {
	vmConcurrentMap = newTTLMap(gcInterval)
}

func resetVMOperationMap() {
	vmOperationMap = newTTLMap(gcInterval)
}

func resetPlacementGroupOperationMap() {
	placementGroupOperationMap = newTTLMap(gcInterval)
}
