module github.com/smartxworks/cluster-api-provider-elf

go 1.16

require (
	github.com/go-logr/logr v0.4.0
	github.com/go-openapi/runtime v0.19.29
	github.com/go-openapi/strfmt v0.20.1
	github.com/golang/mock v1.6.0
	github.com/google/uuid v1.2.0
	github.com/haijianyang/cloudtower-go-sdk v0.0.0-20210922061435-19d2dbd083b4
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.15.0
	github.com/pkg/errors v0.9.1
	k8s.io/api v0.21.4
	k8s.io/apiextensions-apiserver v0.21.4
	k8s.io/apimachinery v0.21.4
	k8s.io/apiserver v0.21.4
	k8s.io/client-go v0.21.4
	k8s.io/klog/v2 v2.9.0
	k8s.io/utils v0.0.0-20210802155522-efc7438f0176
	sigs.k8s.io/cluster-api v0.4.2
	sigs.k8s.io/cluster-api/test v0.4.2
	sigs.k8s.io/controller-runtime v0.9.7
)

replace sigs.k8s.io/cluster-api => sigs.k8s.io/cluster-api v0.4.2
