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

package e2e

import (
	"flag"
	"os"

	. "github.com/onsi/gomega"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

var (
	elfTemplate          = os.Getenv("ELF_TEMPLATE")
	elfTemplateUpgradeTo = os.Getenv("ELF_TEMPLATE_UPGRADE_TO")
	towerUsername        = os.Getenv("TOWER_USERNAME")
	towerPassword        = os.Getenv("TOWER_PASSWORD")

	towerServer string
	vmService   service.VMService
)

func init() {
	flag.StringVar(&towerServer, "e2e.towerServer", os.Getenv("TOWER_SERVER"), "the tower server used for e2e tests")
}

func initElfSession() {
	var err error
	vmService, err = service.NewVMService(infrav1.Tower{
		Server:   towerServer,
		Username: towerUsername,
		Password: towerPassword}, ctrllog.Log)
	Expect(err).ShouldNot(HaveOccurred())

	template, err := vmService.GetVMTemplate(elfTemplate)
	Expect(err).ShouldNot(HaveOccurred())
	Expect(*template.LocalID).Should(Equal(elfTemplate))

	template, err = vmService.GetVMTemplate(elfTemplateUpgradeTo)
	Expect(err).ShouldNot(HaveOccurred())
	Expect(*template.LocalID).Should(Equal(elfTemplateUpgradeTo))
}
