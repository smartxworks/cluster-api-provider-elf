package helpers

import (
	"flag"
	"path"
	"path/filepath"
	goruntime "runtime"

	"github.com/onsi/ginkgo"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"k8s.io/klog/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/log"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha3"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/manager"
)

func init() {
	klog.InitFlags(nil)
	logger := klogr.New()

	// use klog as the internal logger for this envtest environment.
	log.SetLogger(logger)
	// additionally force all of the controllers to use the Ginkgo logger.
	ctrl.SetLogger(logger)
	// add logger for ginkgo
	klog.SetOutput(ginkgo.GinkgoWriter)

	if err := flag.Set("v", "2"); err != nil {
		klog.Fatalf("failed to set log level: %v", err)
	}
}

var (
	scheme = runtime.NewScheme()
	env    *envtest.Environment
)

func init() {
	// Calculate the scheme.
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(admissionv1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(infrav1.AddToScheme(scheme))

	// Get the root of the current file to use in CRD paths.
	_, filename, _, _ := goruntime.Caller(0) //nolint
	root := path.Join(path.Dir(filename), "..", "..")

	crdPaths := []string{
		filepath.Join(root, "config", "crd", "bases"),
	}

	// Create the test environment.
	env = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     crdPaths,
	}
}

// TestEnvironment encapsulates a Kubernetes local test environment.
type TestEnvironment struct {
	manager.Manager
	client.Client
	Config *rest.Config

	doneMgr chan struct{}
}

// NewTestEnvironment creates a new environment spinning up a local api-server.
func NewTestEnvironment() *TestEnvironment {
	if _, err := env.Start(); err != nil {
		err = kerrors.NewAggregate([]error{err, env.Stop()})
		panic(err)
	}

	managerOpts := manager.Options{
		Scheme:     scheme,
		KubeConfig: env.Config,
	}
	managerOpts.AddToManager = func(ctx *context.ControllerManagerContext, mgr ctrlmgr.Manager) error {
		return nil
	}

	mgr, err := manager.New(managerOpts)
	if err != nil {
		klog.Fatalf("failed to create the CAPE controller manager: %v", err)
	}

	return &TestEnvironment{
		Manager: mgr,
		Client:  mgr.GetClient(),
		Config:  mgr.GetConfig(),
		doneMgr: make(chan struct{}),
	}
}

func (t *TestEnvironment) StartManager() error {
	return t.Manager.Start(t.doneMgr)
}

func (t *TestEnvironment) Stop() error {
	t.doneMgr <- struct{}{}
	return env.Stop()
}
