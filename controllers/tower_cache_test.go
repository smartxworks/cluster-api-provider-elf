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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

var _ = Describe("TowerCache", func() {
	var clusterID string

	BeforeEach(func() {
		clusterID = fake.UUID()
		resetClusterResourceMap()
	})

	It("should set memoryInsufficient", func() {
		Expect(clusterResourceMap).NotTo(HaveKey(clusterID))
		Expect(clusterResourceMap[getMemoryKey(clusterID)]).To(BeNil())

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(clusterResourceMap[getMemoryKey(clusterID)].IsUnmet).To(BeTrue())
		Expect(clusterResourceMap[getMemoryKey(clusterID)].LastDetected).To(Equal(clusterResourceMap[getMemoryKey(clusterID)].LastRetried))

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(clusterResourceMap[getMemoryKey(clusterID)].IsUnmet).To(BeTrue())
		Expect(clusterResourceMap[getMemoryKey(clusterID)].LastDetected).To(Equal(clusterResourceMap[getMemoryKey(clusterID)].LastRetried))

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(clusterResourceMap[getMemoryKey(clusterID)].IsUnmet).To(BeFalse())
		Expect(clusterResourceMap[getMemoryKey(clusterID)].LastDetected).To(Equal(clusterResourceMap[getMemoryKey(clusterID)].LastRetried))

		resetClusterResourceMap()
		Expect(clusterResourceMap).NotTo(HaveKey(clusterID))
		Expect(clusterResourceMap[getMemoryKey(clusterID)]).To(BeNil())

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(clusterResourceMap[getMemoryKey(clusterID)].IsUnmet).To(BeFalse())
		Expect(clusterResourceMap[getMemoryKey(clusterID)].LastDetected).To(Equal(clusterResourceMap[getMemoryKey(clusterID)].LastRetried))

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(clusterResourceMap[getMemoryKey(clusterID)].IsUnmet).To(BeFalse())
		Expect(clusterResourceMap[getMemoryKey(clusterID)].LastDetected).To(Equal(clusterResourceMap[getMemoryKey(clusterID)].LastRetried))

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(clusterResourceMap[getMemoryKey(clusterID)].IsUnmet).To(BeTrue())
		Expect(clusterResourceMap[getMemoryKey(clusterID)].LastDetected).To(Equal(clusterResourceMap[getMemoryKey(clusterID)].LastRetried))
	})

	It("should return whether need to detect", func() {
		Expect(clusterResourceMap).NotTo(HaveKey(clusterID))
		Expect(clusterResourceMap[getMemoryKey(clusterID)]).To(BeNil())

		Expect(canRetryVMOperation(clusterID, "")).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(canRetryVMOperation(clusterID, "")).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(canRetryVMOperation(clusterID, "")).To(BeFalse())

		clusterResourceMap[getMemoryKey(clusterID)].LastDetected = clusterResourceMap[getMemoryKey(clusterID)].LastDetected.Add(-silenceTime)
		clusterResourceMap[getMemoryKey(clusterID)].LastRetried = clusterResourceMap[getMemoryKey(clusterID)].LastRetried.Add(-silenceTime)
		Expect(canRetryVMOperation(clusterID, "")).To(BeTrue())

		Expect(canRetryVMOperation(clusterID, "")).To(BeFalse())
	})
})

func resetClusterResourceMap() {
	clusterResourceMap = make(map[string]*clusterResource)
}
