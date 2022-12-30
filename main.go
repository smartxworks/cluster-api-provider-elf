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
	"flag"
	"math/rand"
	"os"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlsig "sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/smartxworks/cluster-api-provider-elf/controllers"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/manager"
	"github.com/smartxworks/cluster-api-provider-elf/version"
)

var (
	setupLog = ctrllog.Log.WithName("entrypoint")

	managerOpts manager.Options
	syncPeriod  time.Duration

	defaultSyncPeriod       = manager.DefaultSyncPeriod
	defaultLeaderElectionID = manager.DefaultLeaderElectionID
)

func main() {
	rand.Seed(time.Now().UnixNano())

	klog.InitFlags(nil)
	ctrllog.SetLogger(klogr.New())
	if err := flag.Set("v", "2"); err != nil {
		klog.Fatalf("failed to set log level: %v", err)
	}

	flag.StringVar(
		&managerOpts.MetricsBindAddress,
		"metrics-bind-addr",
		"localhost:8080",
		"The address the metric endpoint binds to.")
	flag.BoolVar(
		&managerOpts.LeaderElection,
		"leader-elect",
		true,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(
		&managerOpts.LeaderElectionID,
		"leader-election-id",
		defaultLeaderElectionID,
		"Name of the config map to use as the locking resource when configuring leader election.")
	flag.StringVar(
		&managerOpts.Namespace,
		"namespace",
		"",
		"Namespace that the controller watches to reconcile cluster-api objects. If unspecified, the controller watches for cluster-api objects across all namespaces.")
	flag.StringVar(
		&managerOpts.LeaderElectionNamespace,
		"leader-election-namespace",
		"",
		"Namespace that the controller performs leader election in. If unspecified, the controller will discover which namespace it is running in.",
	)
	flag.DurationVar(
		&syncPeriod,
		"sync-period",
		defaultSyncPeriod,
		"The interval at which cluster-api objects are synchronized")
	flag.IntVar(
		&managerOpts.MaxConcurrentReconciles,
		"max-concurrent-reconciles",
		10,
		"The maximum number of allowed, concurrent reconciles.")
	flag.StringVar(
		&managerOpts.HealthProbeBindAddress,
		"health-addr",
		":9440",
		"The address the health endpoint binds to.",
	)
	flag.IntVar(
		&config.MaxConcurrentVMCreations,
		"max-concurrent-vm-creations",
		config.MaxConcurrentVMCreations,
		"The maximum number of concurrent virtual machine creations.")

	flag.Parse()

	if managerOpts.Namespace != "" {
		setupLog.Info(
			"Watching objects only in namespace for reconciliation",
			"namespace", managerOpts.Namespace)
	}

	managerOpts.SyncPeriod = &syncPeriod

	// Create a function that adds all of the controllers and webhooks to the manager.
	addToManager := func(ctx *context.ControllerManagerContext, mgr ctrlmgr.Manager) error {
		if err := controllers.AddClusterControllerToManager(ctx, mgr); err != nil {
			return err
		}

		if err := controllers.AddMachineControllerToManager(ctx, mgr); err != nil {
			return err
		}

		return nil
	}

	setupLog.Info("creating controller manager", "version", version.Get().String())
	managerOpts.AddToManager = addToManager
	mgr, err := manager.New(managerOpts)
	if err != nil {
		setupLog.Error(err, "problem creating controller manager")
		os.Exit(1)
	}

	setupChecks(mgr)

	sigHandler := ctrlsig.SetupSignalHandler()
	setupLog.Info("starting controller manager")
	if err := mgr.Start(sigHandler); err != nil {
		setupLog.Error(err, "problem running controller manager")
		os.Exit(1)
	}
}

func setupChecks(mgr ctrlmgr.Manager) {
	if err := mgr.AddReadyzCheck("ping", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to create ready check")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to create health check")
		os.Exit(1)
	}
}
