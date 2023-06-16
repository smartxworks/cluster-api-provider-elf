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
	"fmt"
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
		resetVMStatusMap()
	})

	It("acquireTicketForCreateVM", func() {
		Expect(acquireTicketForCreateVM(vmName)).To(BeTrue())
		Expect(vmStatusMap).To(HaveKey(vmName))

		for i := 0; i < config.MaxConcurrentVMCreations-1; i++ {
			vmStatusMap[fake.UUID()] = time.Now()
		}
		Expect(acquireTicketForCreateVM(vmName)).To(BeFalse())

		resetVMStatusMap()
		for i := 0; i < config.MaxConcurrentVMCreations; i++ {
			vmStatusMap[fake.UUID()] = time.Now().Add(-creationTimeout)
		}
		Expect(acquireTicketForCreateVM(vmName)).To(BeTrue())
		Expect(vmStatusMap).To(HaveKey(vmName))
		Expect(vmStatusMap).To(HaveLen(1))
	})

	It("releaseTicketForCreateVM", func() {
		Expect(vmStatusMap).NotTo(HaveKey(vmName))
		Expect(acquireTicketForCreateVM(vmName)).To(BeTrue())
		Expect(vmStatusMap).To(HaveKey(vmName))
		releaseTicketForCreateVM(vmName)
		Expect(vmStatusMap).NotTo(HaveKey(vmName))
	})
})

var _ = Describe("VM Operation Limiter", func() {
	var vmName string

	BeforeEach(func() {
		vmName = fake.UUID()
	})

	It("acquireTicketForUpdatingVM", func() {
		Expect(acquireTicketForUpdatingVM(vmName)).To(BeTrue())
		Expect(vmOperationMap).To(HaveKey(vmName))

		Expect(acquireTicketForUpdatingVM(vmName)).To(BeFalse())
		acquireTicketForUpdatingVM(vmName)
		resetVMOperationMap()

		vmOperationMap[vmName] = time.Now().Add(-vmOperationRateLimit)
		Expect(acquireTicketForUpdatingVM(vmName)).To(BeTrue())
		Expect(vmOperationMap).To(HaveKey(vmName))
		Expect(vmOperationMap).To(HaveLen(1))
		resetVMOperationMap()
	})
})

var _ = Describe("Placement Group VM Migration Limiter", func() {
	var groupName string

	BeforeEach(func() {
		groupName = fake.UUID()
	})

	It("acquireTicketForPlacementGroupVMMigration", func() {
		Expect(acquireTicketForPlacementGroupVMMigration(groupName)).To(BeTrue())
		Expect(placementGroupVMMigrationMap).To(HaveKey(groupName))

		Expect(acquireTicketForPlacementGroupVMMigration(groupName)).To(BeFalse())
		releaseTicketForPlacementGroupVMMigration(groupName)
		Expect(placementGroupVMMigrationMap).NotTo(HaveKey(groupName))

		placementGroupVMMigrationMap[groupName] = time.Now().Add(-vmMigrationTimeout)
		Expect(acquireTicketForPlacementGroupVMMigration(groupName)).To(BeTrue())
		Expect(placementGroupVMMigrationMap).To(HaveKey(groupName))
	})
})

var _ = Describe("Placement Group Operation Limiter", func() {
	var groupName string

	BeforeEach(func() {
		groupName = fake.UUID()
	})

	It("acquireTicketForPlacementGroupOperation", func() {
		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeTrue())
		Expect(placementGroupOperationMap).To(HaveKey(groupName))

		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeFalse())
		releaseTicketForPlacementGroupOperation(groupName)

		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeTrue())
		Expect(placementGroupOperationMap).To(HaveKey(groupName))
	})
})

var _ = Describe("Placement Group Operation Limiter", func() {
	var groupName string

	BeforeEach(func() {
		groupName = fake.UUID()
	})

	It("acquireTicketForPlacementGroupOperation", func() {
		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeTrue())
		Expect(placementGroupOperationMap).To(HaveKey(groupName))

		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeFalse())
		releaseTicketForPlacementGroupOperation(groupName)

		Expect(acquireTicketForPlacementGroupOperation(groupName)).To(BeTrue())
		Expect(placementGroupOperationMap).To(HaveKey(groupName))
	})

	It("canCreatePlacementGroup", func() {
		key := fmt.Sprintf("%s:creation", groupName)

		Expect(placementGroupOperationMap).NotTo(HaveKey(key))
		Expect(canCreatePlacementGroup(groupName)).To(BeTrue())

		setPlacementGroupDuplicate(groupName)
		Expect(placementGroupOperationMap).To(HaveKey(key))
		Expect(canCreatePlacementGroup(groupName)).To(BeFalse())

		placementGroupOperationMap[key] = placementGroupOperationMap[key].Add(-placementGroupSilenceTime)
		Expect(placementGroupOperationMap).To(HaveKey(key))
		Expect(canCreatePlacementGroup(groupName)).To(BeTrue())
		Expect(placementGroupOperationMap).NotTo(HaveKey(key))

		Expect(canCreatePlacementGroup(groupName)).To(BeTrue())
	})
})

func resetVMStatusMap() {
	vmStatusMap = make(map[string]time.Time)
}

func resetVMOperationMap() {
	vmOperationMap = make(map[string]time.Time)
}
