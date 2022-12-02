package controllers

import (
	. "github.com/onsi/ginkgo"
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

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(clusterStatusMap[clusterID].Resources.IsMemoryInsufficient).To(BeTrue())

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(clusterStatusMap[clusterID].Resources.IsMemoryInsufficient).To(BeFalse())

		resetClusterStatusMap()
		Expect(clusterStatusMap).NotTo(HaveKey(clusterID))
		Expect(clusterStatusMap[clusterID]).To(BeNil())

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(clusterStatusMap[clusterID].Resources.IsMemoryInsufficient).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(clusterStatusMap[clusterID].Resources.IsMemoryInsufficient).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(clusterStatusMap[clusterID].Resources.IsMemoryInsufficient).To(BeTrue())
	})

	It("should return isMemoryInsufficient", func() {
		Expect(clusterStatusMap).NotTo(HaveKey(clusterID))
		Expect(clusterStatusMap[clusterID]).To(BeNil())

		ok, canRetry := isElfClusterMemoryInsufficient(clusterID)
		Expect(ok).To(BeFalse())
		Expect(canRetry).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, false)
		ok, canRetry = isElfClusterMemoryInsufficient(clusterID)
		Expect(ok).To(BeFalse())
		Expect(canRetry).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, true)
		ok, canRetry = isElfClusterMemoryInsufficient(clusterID)
		Expect(ok).To(BeTrue())
		Expect(canRetry).To(BeFalse())

		clusterStatusMap[clusterID].Resources.LastUpdated = clusterStatusMap[clusterID].Resources.LastUpdated.Add(-silenceTime)
		ok, canRetry = isElfClusterMemoryInsufficient(clusterID)
		Expect(ok).To(BeTrue())
		Expect(canRetry).To(BeTrue())

		ok, canRetry = isElfClusterMemoryInsufficient(clusterID)
		Expect(ok).To(BeTrue())
		Expect(canRetry).To(BeFalse())
	})
})

func resetClusterStatusMap() {
	clusterStatusMap = make(map[string]*clusterStatus)
}
