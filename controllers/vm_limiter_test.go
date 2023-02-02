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
		Expect(len(vmStatusMap)).To(Equal(1))
	})

	It("releaseTicketForCreateVM", func() {
		Expect(vmStatusMap).NotTo(HaveKey(vmName))
		Expect(acquireTicketForCreateVM(vmName)).To(BeTrue())
		Expect(vmStatusMap).To(HaveKey(vmName))
		releaseTicketForCreateVM(vmName)
		Expect(vmStatusMap).NotTo(HaveKey(vmName))
	})
})

func resetVMStatusMap() {
	vmStatusMap = make(map[string]time.Time)
}
