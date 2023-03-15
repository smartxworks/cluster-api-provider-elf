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

package helpers

import (
	goctx "context"
	"flag"
	"fmt"
	"go/build"
	"os"
	"path"
	"path/filepath"
	goruntime "runtime"

	"github.com/onsi/ginkgo/v2"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cgscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/log"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
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
	utilruntime.Must(cgscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(admissionv1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1.AddToScheme(scheme))
	utilruntime.Must(infrav1.AddToScheme(scheme))

	// Get the root of the current file to use in CRD paths.
	_, filename, _, _ := goruntime.Caller(0) //nolint
	root := path.Join(path.Dir(filename), "..", "..")

	crdPaths := []string{
		filepath.Join(root, "config", "crd", "bases"),
	}

	// append CAPI CRDs path
	if capiPath := getFilePathToCAPICRDs(root); capiPath != "" {
		crdPaths = append(crdPaths, capiPath)
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

	cancel goctx.CancelFunc
}

// NewTestEnvironment creates a new environment spinning up a local api-server.
func NewTestEnvironment() *TestEnvironment {
	if _, err := env.Start(); err != nil {
		err = kerrors.NewAggregate([]error{err, env.Stop()})
		panic(err)
	}

	managerOpts := manager.Options{
		Options: ctrl.Options{
			Scheme:             scheme,
			Port:               env.WebhookInstallOptions.LocalServingPort,
			CertDir:            env.WebhookInstallOptions.LocalServingCertDir,
			MetricsBindAddress: "0",
		},
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
	}
}

func (t *TestEnvironment) StartManager(ctx goctx.Context) error {
	ctx, cancel := goctx.WithCancel(ctx)
	t.cancel = cancel

	return t.Manager.Start(ctx)
}

func (t *TestEnvironment) Stop() error {
	t.cancel()

	return env.Stop()
}

func (t *TestEnvironment) Cleanup(ctx goctx.Context, objs ...client.Object) error {
	errs := make([]error, 0, len(objs))
	for _, o := range objs {
		err := t.Client.Delete(ctx, o)
		if apierrors.IsNotFound(err) {
			// If the object is not found, it must've been garbage collected
			// already. For example, if we delete namespace first and then
			// objects within it.
			continue
		}

		errs = append(errs, err)
	}

	return kerrors.NewAggregate(errs)
}

func (t *TestEnvironment) CreateNamespace(ctx goctx.Context, generateName string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", generateName),
			Labels: map[string]string{
				"testenv/original-name": generateName,
			},
		},
	}

	if err := t.Client.Create(ctx, ns); err != nil {
		return nil, err
	}

	return ns, nil
}

func (t *TestEnvironment) CreateKubeconfigSecret(ctx goctx.Context, cluster *clusterv1.Cluster) error {
	return t.Create(ctx, kubeconfig.GenerateSecret(cluster, kubeconfig.FromEnvTestConfig(t.Config, cluster)))
}

func getFilePathToCAPICRDs(root string) string {
	mod, err := NewMod(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}

	packageName := "sigs.k8s.io/cluster-api"
	clusterAPIVersion, err := mod.FindDependencyVersion(packageName)
	if err != nil {
		return ""
	}

	gopath := envOr("GOPATH", build.Default.GOPATH)
	return filepath.Join(gopath, "pkg", "mod", "sigs.k8s.io", fmt.Sprintf("cluster-api@%s", clusterAPIVersion), "config", "crd", "bases")
}

func envOr(envKey, defaultValue string) string {
	if value, ok := os.LookupEnv(envKey); ok {
		return value
	}

	return defaultValue
}
