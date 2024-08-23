/*
Copyright 2021.

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

package main

import (
	goctx "context"
	"flag"
	"fmt"
	"os"
	goruntime "runtime"
	"time"

	"github.com/spf13/pflag"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/feature"
	capiflags "sigs.k8s.io/cluster-api/util/flags"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/smartxworks/cluster-api-provider-elf/controllers"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/manager"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/version"
	"github.com/smartxworks/cluster-api-provider-elf/webhooks"
)

var (
	setupLog       = ctrl.Log.WithName("entrypoint")
	logOptions     = logs.NewOptions()
	controllerName = "cluster-api-elf-manager"

	enableContentionProfiling   bool
	leaderElectionLeaseDuration time.Duration
	leaderElectionRenewDeadline time.Duration
	leaderElectionRetryPeriod   time.Duration
	managerOpts                 manager.Options
	restConfigBurst             int
	restConfigQPS               float32
	syncPeriod                  time.Duration
	webhookOpts                 webhook.Options
	watchNamespace              string

	elfClusterConcurrency         int
	elfMachineConcurrency         int
	elfMachineTemplateConcurrency int

	tlsOptions         = capiflags.TLSOptions{}
	diagnosticsOptions = capiflags.DiagnosticsOptions{}

	defaultProfilerAddr     = os.Getenv("PROFILER_ADDR")
	defaultSyncPeriod       = manager.DefaultSyncPeriod
	defaultLeaderElectionID = manager.DefaultLeaderElectionID
	defaultPodName          = manager.DefaultPodName
	defaultWebhookPort      = manager.DefaultWebhookServiceContainerPort
)

// InitFlags initializes the flags.
func InitFlags(fs *pflag.FlagSet) {
	// Flags specific to CAPE

	fs.StringVar(&managerOpts.LeaderElectionID, "leader-election-id", defaultLeaderElectionID,
		"Name of the config map to use as the locking resource when configuring leader election.")

	flag.StringVar(&managerOpts.LeaderElectionNamespace, "leader-election-namespace", "",
		"Namespace that the controller performs leader election in. If unspecified, the controller will discover which namespace it is running in.",
	)

	fs.IntVar(&elfClusterConcurrency, "max-elfcluster-concurrent-reconciles", 10,
		"Number of ELF clusters to process simultaneously")

	fs.IntVar(&elfMachineConcurrency, "max-elfmachine-concurrent-reconciles", 10,
		"Number of ELF machines to process simultaneously")

	fs.IntVar(&elfMachineTemplateConcurrency, "max-elfmachinetemplate-concurrent-reconciles", 10,
		"Number of ELF machine templates to process simultaneously")

	fs.StringVar(&managerOpts.PodName, "pod-name", defaultPodName,
		"The name of the pod running the controller manager.")

	fs.IntVar(&config.MaxConcurrentVMCreations, "max-concurrent-vm-creations", config.MaxConcurrentVMCreations,
		"The maximum number of concurrent virtual machine creations.")

	// Flags common between CAPI and CAPE

	logsv1.AddFlags(logOptions, fs)

	fs.BoolVar(&managerOpts.LeaderElection, "leader-elect", true,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")

	fs.DurationVar(&leaderElectionLeaseDuration, "leader-elect-lease-duration", 15*time.Second,
		"Interval at which non-leader candidates will wait to force acquire leadership (duration string)")

	fs.DurationVar(&leaderElectionRenewDeadline, "leader-elect-renew-deadline", 10*time.Second,
		"Duration that the leading controller manager will retry refreshing leadership before giving up (duration string)")

	fs.DurationVar(&leaderElectionRetryPeriod, "leader-elect-retry-period", 2*time.Second,
		"Duration the LeaderElector clients should wait between tries of actions (duration string)")

	fs.StringVar(&watchNamespace, "namespace", "",
		"Namespace that the controller watches to reconcile cluster-api objects. If unspecified, the controller watches for cluster-api objects across all namespaces.")

	fs.StringVar(&managerOpts.WatchFilterValue, "watch-filter", "",
		fmt.Sprintf("Label value that the controller watches to reconcile cluster-api objects. Label key is always %s. If unspecified, the controller watches for all cluster-api objects.", clusterv1.WatchLabel))

	fs.StringVar(&managerOpts.PprofBindAddress, "profiler-address", defaultProfilerAddr,
		"Bind address to expose the pprof profiler (e.g. localhost:6060)")

	fs.BoolVar(&enableContentionProfiling, "contention-profiling", false,
		"Enable block profiling, if profiler-address is set.")

	fs.DurationVar(&syncPeriod, "sync-period", defaultSyncPeriod,
		"The minimum interval at which watched resources are reconciled (e.g. 15m)")

	fs.Float32Var(&restConfigQPS, "kube-api-qps", 20,
		"Maximum queries per second from the controller client to the Kubernetes API server. Defaults to 20")

	fs.IntVar(&restConfigBurst, "kube-api-burst", 30,
		"Maximum number of queries that should be allowed in one burst from the controller client to the Kubernetes API server. Default 30")

	fs.IntVar(&webhookOpts.Port, "webhook-port", defaultWebhookPort,
		"Webhook Server port")

	fs.StringVar(&webhookOpts.CertDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs/",
		"Webhook cert dir, only used when webhook-port is specified.")

	fs.StringVar(&managerOpts.HealthProbeBindAddress, "health-addr", ":9440",
		"The address the health endpoint binds to.")

	capiflags.AddTLSOptions(fs, &tlsOptions)
	capiflags.AddDiagnosticsOptions(fs, &diagnosticsOptions)
	feature.MutableGates.AddFlag(fs)
}

// Add RBAC for the authorized diagnostics endpoint.
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

func main() {
	InitFlags(pflag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	// Set log level 2 as default.
	if err := pflag.CommandLine.Set("v", "2"); err != nil {
		setupLog.Error(err, "failed to set log level: %v")
		os.Exit(1)
	}
	pflag.Parse()

	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// klog.Background will automatically use the right logger.
	ctrl.SetLogger(klog.Background())

	managerOpts.KubeConfig = ctrl.GetConfigOrDie()
	managerOpts.KubeConfig.QPS = restConfigQPS
	managerOpts.KubeConfig.Burst = restConfigBurst
	managerOpts.KubeConfig.UserAgent = remote.DefaultClusterAPIUserAgent(controllerName)

	if watchNamespace != "" {
		managerOpts.Cache.DefaultNamespaces = map[string]cache.Config{
			watchNamespace: {},
		}
	}

	if enableContentionProfiling {
		goruntime.SetBlockProfileRate(1)
	}

	setupLog.Info(fmt.Sprintf("Feature gates: %+v\n", feature.Gates))

	managerOpts.Cache.SyncPeriod = &syncPeriod
	managerOpts.LeaseDuration = &leaderElectionLeaseDuration
	managerOpts.RenewDeadline = &leaderElectionRenewDeadline
	managerOpts.RetryPeriod = &leaderElectionRetryPeriod

	// Create a function that adds all of the controllers and webhooks to the manager.
	addToManager := func(ctx goctx.Context, ctrlMgrCtx *context.ControllerManagerContext, mgr ctrlmgr.Manager) error {
		if os.Getenv("ENABLE_WEBHOOKS") != "false" {
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
		}

		if err := controllers.AddClusterControllerToManager(ctx, ctrlMgrCtx, mgr, controller.Options{MaxConcurrentReconciles: elfClusterConcurrency}); err != nil {
			return err
		}

		if err := controllers.AddMachineControllerToManager(ctx, ctrlMgrCtx, mgr, controller.Options{MaxConcurrentReconciles: elfMachineConcurrency}); err != nil {
			return err
		}

		if err := controllers.AddMachineTemplateControllerToManager(ctx, ctrlMgrCtx, mgr, controller.Options{MaxConcurrentReconciles: elfMachineTemplateConcurrency}); err != nil {
			return err
		}

		return nil
	}

	tlsOptionOverrides, err := capiflags.GetTLSOptionOverrideFuncs(tlsOptions)
	if err != nil {
		setupLog.Error(err, "unable to add TLS settings to the webhook server")
		os.Exit(1)
	}
	webhookOpts.TLSOpts = tlsOptionOverrides
	managerOpts.WebhookServer = webhook.NewServer(webhookOpts)
	managerOpts.AddToManager = addToManager
	managerOpts.Metrics = capiflags.GetDiagnosticsOptions(diagnosticsOptions)

	setupLog.Info("Creating controller manager", "capeVersion", version.CAPEVersion(), "version", version.Get().String())
	// Set up the context that's going to be used in controllers and for the manager.
	ctx := ctrl.SetupSignalHandler()
	mgr, err := manager.New(ctx, managerOpts)
	if err != nil {
		setupLog.Error(err, "failed to create controller manager")
		os.Exit(1)
	}

	setupChecks(mgr)

	setupLog.Info("Starting controller manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "failed to run controller manager")
		os.Exit(1)
	}
}

func setupChecks(mgr ctrlmgr.Manager) {
	if err := mgr.AddReadyzCheck("webhook", mgr.GetWebhookServer().StartedChecker()); err != nil {
		setupLog.Error(err, "unable to create ready check")
		os.Exit(1)
	}
	if err := mgr.AddHealthzCheck("webhook", mgr.GetWebhookServer().StartedChecker()); err != nil {
		setupLog.Error(err, "unable to create health check")
		os.Exit(1)
	}
}
