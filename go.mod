module github.com/smartxworks/cluster-api-provider-elf

go 1.16

require (
	github.com/go-logr/logr v0.4.0
	github.com/golang/mock v1.6.0
	github.com/google/uuid v1.2.0
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.13.0
	github.com/pkg/errors v0.9.1
	github.smartx.com/smartx/elf-sdk-go v0.0.0-20200629111108-f3fa73369531
	k8s.io/api v0.21.2
	k8s.io/apiextensions-apiserver v0.21.2
	k8s.io/apimachinery v0.21.2
	k8s.io/apiserver v0.21.2
	k8s.io/client-go v0.21.2
	k8s.io/klog/v2 v2.9.0
	k8s.io/utils v0.0.0-20210527160623-6fdb442a123b
	sigs.k8s.io/cluster-api v0.4.0
	sigs.k8s.io/cluster-api/test v0.4.0
	sigs.k8s.io/controller-runtime v0.9.2
)

replace sigs.k8s.io/cluster-api => sigs.k8s.io/cluster-api v0.4.0
