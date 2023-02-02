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
		resetClusterStatusMap()
	})

	It("should set memoryInsufficient", func() {
		Expect(clusterStatusMap).NotTo(HaveKey(clusterID))
		Expect(clusterStatusMap[clusterID]).To(BeNil())

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(clusterStatusMap[clusterID].Resources.IsMemoryInsufficient).To(BeTrue())
		Expect(clusterStatusMap[clusterID].Resources.LastDetected).To(Equal(clusterStatusMap[clusterID].Resources.LastRetried))

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(clusterStatusMap[clusterID].Resources.IsMemoryInsufficient).To(BeTrue())
		Expect(clusterStatusMap[clusterID].Resources.LastDetected).To(Equal(clusterStatusMap[clusterID].Resources.LastRetried))

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(clusterStatusMap[clusterID].Resources.IsMemoryInsufficient).To(BeFalse())
		Expect(clusterStatusMap[clusterID].Resources.LastDetected).To(Equal(clusterStatusMap[clusterID].Resources.LastRetried))

		resetClusterStatusMap()
		Expect(clusterStatusMap).NotTo(HaveKey(clusterID))
		Expect(clusterStatusMap[clusterID]).To(BeNil())

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(clusterStatusMap[clusterID].Resources.IsMemoryInsufficient).To(BeFalse())
		Expect(clusterStatusMap[clusterID].Resources.LastDetected).To(Equal(clusterStatusMap[clusterID].Resources.LastRetried))

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(clusterStatusMap[clusterID].Resources.IsMemoryInsufficient).To(BeFalse())
		Expect(clusterStatusMap[clusterID].Resources.LastDetected).To(Equal(clusterStatusMap[clusterID].Resources.LastRetried))

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(clusterStatusMap[clusterID].Resources.IsMemoryInsufficient).To(BeTrue())
		Expect(clusterStatusMap[clusterID].Resources.LastDetected).To(Equal(clusterStatusMap[clusterID].Resources.LastRetried))
	})

	It("should return whether memory is insufficient", func() {
		Expect(clusterStatusMap).NotTo(HaveKey(clusterID))
		Expect(clusterStatusMap[clusterID]).To(BeNil())

		Expect(isElfClusterMemoryInsufficient(clusterID)).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(isElfClusterMemoryInsufficient(clusterID)).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(isElfClusterMemoryInsufficient(clusterID)).To(BeTrue())
	})

	It("should return whether need to detect", func() {
		Expect(clusterStatusMap).NotTo(HaveKey(clusterID))
		Expect(clusterStatusMap[clusterID]).To(BeNil())

		Expect(canRetryVMOperation(clusterID)).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(canRetryVMOperation(clusterID)).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(canRetryVMOperation(clusterID)).To(BeFalse())

		clusterStatusMap[clusterID].Resources.LastDetected = clusterStatusMap[clusterID].Resources.LastDetected.Add(-silenceTime)
		clusterStatusMap[clusterID].Resources.LastRetried = clusterStatusMap[clusterID].Resources.LastRetried.Add(-silenceTime)
		Expect(canRetryVMOperation(clusterID)).To(BeTrue())

		Expect(canRetryVMOperation(clusterID)).To(BeFalse())
	})
})

func resetClusterStatusMap() {
	clusterStatusMap = make(map[string]*clusterStatus)
}
