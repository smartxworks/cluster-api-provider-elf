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
	"time"

	"github.com/onsi/ginkgo/v2"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	cgscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/log"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/manager"
	"github.com/smartxworks/cluster-api-provider-elf/webhooks"
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
	scheme           = runtime.NewScheme()
	crdPaths         []string
	cacheSyncBackoff = wait.Backoff{
		Duration: 100 * time.Millisecond,
		Factor:   1.5,
		Steps:    8,
		Jitter:   0.4,
	}
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

	crdPaths = []string{
		filepath.Join(root, "config", "crd", "bases"),
	}

	// append CAPI CRDs path
	if capiPath := getFilePathToCAPICRDs(root); capiPath != "" {
		crdPaths = append(crdPaths, capiPath)
	}
}

// TestEnvironment encapsulates a Kubernetes local test environment.
type TestEnvironment struct {
	manager.Manager
	client.Client
	Env        *envtest.Environment
	Config     *rest.Config
	Kubeconfig string

	cancel goctx.CancelFunc
}

// NewTestEnvironment creates a new environment spinning up a local api-server.
func NewTestEnvironment() *TestEnvironment {
	// Create the test environment.
	env := &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     crdPaths,
	}

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
		if err := (&webhooks.ElfMachineMutation{
			Client: mgr.GetClient(),
			Logger: mgr.GetLogger().WithName("ElfMachineMutation"),
		}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}

		return nil
	}

	mgr, err := manager.New(managerOpts)
	if err != nil {
		klog.Fatalf("failed to create the CAPE controller manager: %v", err)
	}

	kubeconfig, err := CreateKubeconfig(mgr.GetConfig(), fmt.Sprintf("%s-cluster", capiutil.RandomString(6)))
	if err != nil {
		klog.Fatalf("failed to create kubeconfig: %v", err)
	}

	return &TestEnvironment{
		Manager:    mgr,
		Client:     mgr.GetClient(),
		Config:     mgr.GetConfig(),
		Env:        env,
		Kubeconfig: kubeconfig,
	}
}

func (t *TestEnvironment) StartManager(ctx goctx.Context) error {
	ctx, cancel := goctx.WithCancel(ctx)
	t.cancel = cancel

	return t.Manager.Start(ctx)
}

func (t *TestEnvironment) Stop() error {
	t.cancel()

	if err := t.Env.Stop(); err != nil {
		return err
	}

	return os.Remove(t.Kubeconfig)
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

func (t *TestEnvironment) CreateNamespace(ctx goctx.Context, name string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"testenv/original-name": name,
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

func (t *TestEnvironment) CreateAndWait(ctx goctx.Context, obj client.Object, opts ...client.CreateOption) error {
	if err := t.Create(ctx, obj, opts...); err != nil {
		return err
	}

	// Makes sure the cache is updated with the new object
	key := client.ObjectKeyFromObject(obj)
	return wait.ExponentialBackoff(cacheSyncBackoff, func() (done bool, err error) {
		if err := t.Get(ctx, key, obj); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
}

func (t *TestEnvironment) PatchAndWait(ctx goctx.Context, obj, patchFromObj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	oldObj := obj.DeepCopyObject().(client.Object)
	if err := t.Get(ctx, key, oldObj); err != nil {
		return err
	}
	oldResourceVersion := oldObj.GetResourceVersion()

	patchHelper, err := patch.NewHelper(patchFromObj, t.Client)
	if err != nil {
		return err
	}
	if err := patchHelper.Patch(ctx, obj); err != nil {
		return err
	}

	// Makes sure the cache is updated with the new object
	return wait.ExponentialBackoff(cacheSyncBackoff, func() (done bool, err error) {
		if err := t.Get(ctx, key, obj); err != nil {
			return false, err
		}
		if obj.GetResourceVersion() == oldResourceVersion {
			return false, nil
		}
		return true, nil
	})
}

// CreateKubeconfig returns a new kubeconfig from the envtest config.
func CreateKubeconfig(cfg *rest.Config, clusterName string) (string, error) {
	contextName := fmt.Sprintf("%s@%s", cfg.Username, clusterName)
	c := api.Config{
		Clusters: map[string]*api.Cluster{
			clusterName: {
				Server:                   cfg.Host,
				CertificateAuthorityData: cfg.CAData,
			},
		},
		Contexts: map[string]*api.Context{
			contextName: {
				Cluster:  clusterName,
				AuthInfo: cfg.Username,
			},
		},
		AuthInfos: map[string]*api.AuthInfo{
			cfg.Username: {
				ClientKeyData:         cfg.KeyData,
				ClientCertificateData: cfg.CertData,
			},
		},
		CurrentContext: contextName,
	}

	kubeconfig := "/var/tmp/" + clusterName + ".kubeconfig"
	if err := clientcmd.WriteToFile(c, kubeconfig); err != nil {
		return "", err
	}

	return kubeconfig, nil
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
