package manager

import (
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"

	// +kubebuilder:scaffold:imports

	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
)

// AddToManagerFunc is a function that can be optionally specified with
// the manager's Options in order to explicitly decide what controllers and
// webhooks to add to the manager.
type AddToManagerFunc func(*context.ControllerManagerContext, ctrlmgr.Manager) error

// Options describes the options used to create a new CAPE manager.
type Options struct {
	// LeaderElectionEnabled is a flag that enables leader election.
	LeaderElectionEnabled bool

	// LeaderElectionID is the name of the config map to use as the
	// locking resource when configuring leader election.
	LeaderElectionID string

	// SyncPeriod is the amount of time to wait between syncing the local
	// object cache with the API server.
	SyncPeriod time.Duration

	// MaxConcurrentReconciles the maximum number of allowed, concurrent
	// reconciles.
	//
	// Defaults to the eponymous constant in this package.
	MaxConcurrentReconciles int

	// MetricsBindAddr is the net.Addr string for the metrics server.
	MetricsBindAddr string

	// HealthAddr is the net.Addr string for the healthcheck server
	HealthAddr string

	// LeaderElectionNamespace is the namespace in which the pod running the
	// controller maintains a leader election lock
	//
	// Defaults to ""
	LeaderElectionNamespace string

	// PodName is the name of the pod running the controller manager.
	//
	// Defaults to the eponymous constant in this package.
	PodName string

	// WatchNamespace is the namespace the controllers watch for changes. If
	// no value is specified then all namespaces are watched.
	//
	// Defaults to the eponymous constant in this package.
	WatchNamespace string

	Logger logr.Logger

	KubeConfig *rest.Config

	Scheme *runtime.Scheme

	// AddToManager is a function that can be optionally specified with
	// the manager's Options in order to explicitly decide what controllers
	// and webhooks to add to the manager.
	AddToManager AddToManagerFunc
}

func (o *Options) defaults() {
	if o.Logger == nil {
		o.Logger = ctrllog.Log
	}

	if o.PodName == "" {
		o.PodName = DefaultPodName
	}

	if o.SyncPeriod == 0 {
		o.SyncPeriod = DefaultSyncPeriod
	}

	if o.KubeConfig == nil {
		o.KubeConfig = config.GetConfigOrDie()
	}

	if o.Scheme == nil {
		o.Scheme = runtime.NewScheme()
	}
}
