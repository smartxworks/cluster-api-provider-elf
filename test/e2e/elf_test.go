package e2e

import (
	"flag"
	"os"

	. "github.com/onsi/gomega"

	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha4"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
)

var (
	elfTemplate       = os.Getenv("ELF_TEMPLATE")
	elfServerUsername = os.Getenv("ELF_SERVER_USERNAME")
	elfServerPassword = os.Getenv("ELF_SERVER_PASSWORD")

	elfServer string
	vmService service.VMService
)

func init() {
	flag.StringVar(&elfServer, "e2e.elfServer", os.Getenv("ELF_SERVER"), "the ELF server used for e2e tests")
}

func initElfSession() {
	var err error
	vmService, err = service.NewVMService(infrav1.ElfAuth{
		Host:     elfServer,
		Username: elfServerUsername,
		Password: elfServerPassword}, ctrllog.Log)
	Expect(err).ShouldNot(HaveOccurred())

	template, err := vmService.GetVMTemplate(elfTemplate)
	Expect(err).ShouldNot(HaveOccurred())
	Expect(template.Uuid).ShouldNot(BeEmpty())
}
