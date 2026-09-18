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
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"golang.org/x/tools/go/packages"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	cgscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
	controlplanev1 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta1"
	controlplanev2 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	clusterv2 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	kcpwebhooks "sigs.k8s.io/cluster-api/controlplane/kubeadm/webhooks"
	capiutil "sigs.k8s.io/cluster-api/util"
	capicontract "sigs.k8s.io/cluster-api/util/contract"
	"sigs.k8s.io/cluster-api/util/deprecated/v1beta1/patch"
	capiwebhooks "sigs.k8s.io/cluster-api/webhooks"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"
	"sigs.k8s.io/yaml"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/manager"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/util/capi/kubeconfig"
	"github.com/smartxworks/cluster-api-provider-elf/webhooks"
)

func init() {
	ctrl.SetLogger(klog.Background())
	// add logger for ginkgo
	klog.SetOutput(ginkgo.GinkgoWriter)
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
	utilruntime.Must(clusterv2.AddToScheme(scheme))
	utilruntime.Must(controlplanev1.AddToScheme(scheme))
	utilruntime.Must(controlplanev2.AddToScheme(scheme))
	utilruntime.Must(infrav1.AddToScheme(scheme))

	// Get the root of the current file to use in CRD paths.
	_, filename, _, _ := goruntime.Caller(0) //nolint
	root := path.Join(path.Dir(filename), "..", "..")

	crdPaths = []string{
		filepath.Join(root, "config", "crd", "bases"),
	}

	// append CAPI CRDs path
	if capiPaths := getFilePathToCAPICRDs(); capiPaths != nil {
		crdPaths = append(crdPaths, capiPaths...)
	}

	crdPaths = append(crdPaths, filepath.Join(root, "test", "config", "host-agent"))
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
func NewTestEnvironment(ctx goctx.Context) *TestEnvironment {
	// Create the test environment.
	env := &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     crdPaths,
		Scheme:                scheme,
	}

	if _, err := env.Start(); err != nil {
		err = kerrors.NewAggregate([]error{err, env.Stop()})
		panic(err)
	}

	// Localhost is used on MacOS to avoid Firewall warning popups.
	host := "localhost"
	if strings.EqualFold(os.Getenv("USE_EXISTING_CLUSTER"), "true") {
		// 0.0.0.0 is required on Linux when using kind because otherwise the kube-apiserver running in kind
		// is unable to reach the webhook, because the webhook would be only listening on 127.0.0.1.
		// Somehow that's not an issue on MacOS.
		if goruntime.GOOS == "linux" {
			host = "0.0.0.0"
		}
	}

	managerOpts := manager.Options{
		Options: ctrl.Options{
			Scheme: scheme,
			Metrics: metricsserver.Options{
				BindAddress: "0",
			},
			WebhookServer: webhook.NewServer(
				webhook.Options{
					Port:    env.WebhookInstallOptions.LocalServingPort,
					CertDir: env.WebhookInstallOptions.LocalServingCertDir,
					Host:    host,
				},
			),
		},
		KubeConfig: env.Config,
	}
	managerOpts.AddToManager = func(ctx goctx.Context, ctrlMgrCtx *context.ControllerManagerContext, mgr ctrlmgr.Manager) error {
		mgr.GetWebhookServer().Register("/convert", conversion.NewWebhookHandler(mgr.GetScheme()))

		apiVersionGetter := func(gk schema.GroupKind) (string, error) {
			return getAPIVersionForGroupKind(ctx, mgr.GetClient(), gk)
		}
		clusterv1.SetAPIVersionGetter(apiVersionGetter)
		controlplanev1.SetAPIVersionGetter(apiVersionGetter)

		if err := (&capiwebhooks.Cluster{Client: mgr.GetClient()}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}
		if err := (&capiwebhooks.MachineDeployment{}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}
		if err := (&capiwebhooks.MachineSet{}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}
		if err := (&capiwebhooks.MachineHealthCheck{}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}
		if err := (&capiwebhooks.Machine{}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}
		if err := (&kcpwebhooks.KubeadmControlPlane{}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}
		if err := (&kcpwebhooks.ScaleValidator{Client: mgr.GetClient()}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}
		if err := (&kcpwebhooks.KubeadmControlPlaneTemplate{}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}

		if err := (&webhooks.ElfMachineTemplateValidator{}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}

		if err := (&webhooks.ElfMachineValidator{
			Client: mgr.GetClient(),
		}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}

		if err := (&webhooks.ElfMachineMutation{
			Client: mgr.GetClient(),
			Logger: mgr.GetLogger().WithName("ElfMachineMutation"),
		}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}

		if err := (&webhooks.ElfMachineTemplateMutation{
			Client: mgr.GetClient(),
			Logger: mgr.GetLogger().WithName("ElfMachineTemplateMutation"),
		}).SetupWebhookWithManager(mgr); err != nil {
			return err
		}

		return nil
	}

	mgr, err := manager.New(ctx, managerOpts)
	if err != nil {
		klog.Fatalf("failed to create the CAPE controller manager: %v", err)
	}

	kubeconfig, err := CreateKubeconfig(mgr.GetConfig(), capiutil.RandomString(6)+"-cluster")
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

// WaitForWebhooks waits for the webhook server to be available.
func (t *TestEnvironment) WaitForWebhooks(ctx goctx.Context) {
	port := t.Env.WebhookInstallOptions.LocalServingPort

	klog.V(2).Infof("Waiting for webhook port %d to be open prior to running tests", port)
	timeout := 1 * time.Second
	for {
		time.Sleep(1 * time.Second)
		dialer := &net.Dialer{
			Timeout: timeout,
		}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			klog.V(2).Infof("Webhook port is not ready, will retry in %v: %s", timeout, err)
			continue
		}
		if err := conn.Close(); err != nil {
			klog.V(2).Infof("Closing connection when testing if webhook port is ready failed: %v", err)
		}
		klog.V(2).Info("Webhook port is now open. Continuing with tests...")
		return
	}
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

func getFilePathToCAPICRDs() []string {
	packageName := "sigs.k8s.io/cluster-api"
	packageConfig := &packages.Config{
		Mode: packages.NeedModule,
	}

	pkgs, err := packages.Load(packageConfig, packageName)
	if err != nil {
		return nil
	}

	pkg := pkgs[0]

	paths := []string{
		filepath.Join(pkg.Module.Dir, "config", "crd", "bases"),
		filepath.Join(pkg.Module.Dir, "controlplane", "kubeadm", "config", "crd", "bases"),
	}

	// CAPI CRDs still serve v1alpha3/v1alpha4 for conversion. Those versions are not available in the
	// Go API packages we compile against, which makes the conversion webhook fail decoding requests
	// in envtest. For tests, we only need the supported versions, so we strip v1alpha3/v1alpha4.
	filtered := make([]string, 0, len(paths))
	for _, p := range paths {
		fp, err := filterCRDDirectoryVersions(p, map[string]bool{"v1alpha3": true, "v1alpha4": true})
		if err != nil {
			klog.Errorf("Failed to filter CRD dir %s: %v", p, err)
			filtered = append(filtered, p)
			continue
		}
		filtered = append(filtered, fp)
	}

	return filtered
}

func filterCRDDirectoryVersions(dir string, dropVersions map[string]bool) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp("", "cape-envtest-crds-")
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		srcPath := filepath.Join(dir, name)
		dstPath := filepath.Join(tmpDir, name)

		in, err := os.ReadFile(srcPath) //nolint:gosec
		if err != nil {
			return "", err
		}

		out, err := filterSingleCRDYAML(in, dropVersions)
		if err != nil {
			out = in
		}

		if err := os.WriteFile(dstPath, out, 0o600); err != nil {
			return "", err
		}
	}
	return tmpDir, nil
}

func filterSingleCRDYAML(in []byte, dropVersions map[string]bool) ([]byte, error) {
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(in, &crd); err != nil {
		return nil, err
	}
	if crd.APIVersion == "" || crd.Kind != "CustomResourceDefinition" || len(crd.Spec.Versions) == 0 {
		return nil, io.EOF
	}
	origLen := len(crd.Spec.Versions)
	filtered := crd.Spec.Versions[:0]
	for _, v := range crd.Spec.Versions {
		if dropVersions[v.Name] {
			continue
		}
		filtered = append(filtered, v)
	}
	if len(filtered) == 0 || len(filtered) == origLen {
		return nil, io.EOF
	}
	crd.Spec.Versions = filtered
	return yaml.Marshal(&crd)
}

func getAPIVersionForGroupKind(ctx goctx.Context, c client.Reader, gk schema.GroupKind) (string, error) {
	if gk.Group == "" {
		crdList := &apiextensionsv1.CustomResourceDefinitionList{}
		if err := c.List(ctx, crdList); err != nil {
			return "", err
		}

		var matched *apiextensionsv1.CustomResourceDefinition
		for i := range crdList.Items {
			crd := &crdList.Items[i]
			if crd.Spec.Names.Kind != gk.Kind {
				continue
			}
			if matched != nil && matched.Spec.Group != crd.Spec.Group {
				return "", fmt.Errorf("multiple CRDs found for kind %s without group: %s, %s", gk.Kind, matched.Spec.Group, crd.Spec.Group)
			}
			matched = crd
		}
		if matched == nil {
			return "", fmt.Errorf("crd for kind %s not found", gk.Kind)
		}
		gk.Group = matched.Spec.Group
	}

	crd := &apiextensionsv1.CustomResourceDefinition{}
	crdName := capicontract.CalculateCRDName(gk.Group, gk.Kind)
	if err := c.Get(ctx, client.ObjectKey{Name: crdName}, crd); err != nil {
		return "", err
	}

	for _, version := range crd.Spec.Versions {
		if version.Storage {
			return schema.GroupVersion{Group: gk.Group, Version: version.Name}.String(), nil
		}
	}
	for _, version := range crd.Spec.Versions {
		if version.Served {
			return schema.GroupVersion{Group: gk.Group, Version: version.Name}.String(), nil
		}
	}
	if len(crd.Spec.Versions) == 0 {
		return "", fmt.Errorf("crd %s has no versions", crdName)
	}

	return schema.GroupVersion{Group: gk.Group, Version: crd.Spec.Versions[0].Name}.String(), nil
}
