module github.com/smartxworks/cluster-api-provider-elf

go 1.15

require (
	github.com/go-logr/logr v0.1.0
	github.com/google/uuid v1.1.1
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/pkg/errors v0.9.1
	github.smartx.com/smartx/elf-sdk-go v0.0.0-20200629111108-f3fa73369531
	k8s.io/api v0.17.9
	k8s.io/apimachinery v0.17.9
	k8s.io/client-go v0.17.9
	k8s.io/cluster-bootstrap v0.17.9
	k8s.io/klog v1.0.0
	k8s.io/utils v0.0.0-20200619165400-6e3d28b6ed19
	sigs.k8s.io/cluster-api v0.3.14
	sigs.k8s.io/controller-runtime v0.5.14
)
