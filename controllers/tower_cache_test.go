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

		Expect(needDetectElfClusterMemoryInsufficient(clusterID)).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, false)
		Expect(needDetectElfClusterMemoryInsufficient(clusterID)).To(BeFalse())

		setElfClusterMemoryInsufficient(clusterID, true)
		Expect(needDetectElfClusterMemoryInsufficient(clusterID)).To(BeFalse())

		clusterStatusMap[clusterID].Resources.LastDetected = clusterStatusMap[clusterID].Resources.LastDetected.Add(-silenceTime)
		clusterStatusMap[clusterID].Resources.LastRetried = clusterStatusMap[clusterID].Resources.LastRetried.Add(-silenceTime)
		Expect(needDetectElfClusterMemoryInsufficient(clusterID)).To(BeTrue())

		Expect(needDetectElfClusterMemoryInsufficient(clusterID)).To(BeFalse())
	})
})

func resetClusterStatusMap() {
	clusterStatusMap = make(map[string]*clusterStatus)
}
