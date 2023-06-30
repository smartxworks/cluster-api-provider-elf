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

package version

import (
	"testing"

	"github.com/onsi/gomega"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

func TestIsCompatiblePlacementGroup(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("", func(t *testing.T) {
		elfMachine := newElfMachineWithoutCAPEVersion()
		g.Expect(IsCompatiblePlacementGroup(elfMachine)).To(gomega.BeFalse())

		elfMachine = newElfMachineWithoutCAPEVersion()
		SetCAPEVersion(elfMachine, "")
		g.Expect(IsCompatiblePlacementGroup(elfMachine)).To(gomega.BeFalse())

		elfMachine = newElfMachineWithoutCAPEVersion()
		SetCAPEVersion(elfMachine, "a")
		g.Expect(IsCompatiblePlacementGroup(elfMachine)).To(gomega.BeFalse())

		elfMachine = newElfMachineWithoutCAPEVersion()
		SetCAPEVersion(elfMachine, CAPEVersion1_1_0)
		g.Expect(IsCompatiblePlacementGroup(elfMachine)).To(gomega.BeFalse())

		elfMachine = newElfMachineWithoutCAPEVersion()
		SetCAPEVersion(elfMachine, "v1.1.9")
		g.Expect(IsCompatiblePlacementGroup(elfMachine)).To(gomega.BeFalse())

		elfMachine = newElfMachineWithoutCAPEVersion()
		SetCurrentCAPEVersion(elfMachine)
		g.Expect(IsCompatiblePlacementGroup(elfMachine)).To(gomega.BeTrue())

		elfMachine = newElfMachineWithoutCAPEVersion()
		SetCAPEVersion(elfMachine, CAPEVersion1_2_0+"-alpha.0")
		g.Expect(IsCompatiblePlacementGroup(elfMachine)).To(gomega.BeTrue())

		elfMachine = newElfMachineWithoutCAPEVersion()
		SetCAPEVersion(elfMachine, CAPEVersionLatest)
		g.Expect(IsCompatiblePlacementGroup(elfMachine)).To(gomega.BeTrue())
	})
}

func newElfMachineWithoutCAPEVersion() *infrav1.ElfMachine {
	elfMachine := fake.NewElfMachine(nil)
	delete(elfMachine.Annotations, infrav1.CAPEVersionAnnotation)

	return elfMachine
}
