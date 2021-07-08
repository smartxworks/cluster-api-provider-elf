package manager

import (
	goctx "context"

	"github.com/pkg/errors"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	ctrl "sigs.k8s.io/controller-runtime"

	infrav1a3 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha4"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
)

// Manager is a CAPE controller manager.
type Manager interface {
	ctrl.Manager

	// GetContext returns the controller manager's context.
	GetContext() *context.ControllerManagerContext
}

// New returns a new CAPE controller manager.
func New(opts Options) (Manager, error) {

	// Ensure the default options are set.
	opts.defaults()

	_ = clientgoscheme.AddToScheme(opts.Scheme)
	_ = clusterv1.AddToScheme(opts.Scheme)
	_ = infrav1a3.AddToScheme(opts.Scheme)
	_ = bootstrapv1.AddToScheme(opts.Scheme)
	// +kubebuilder:scaffold:scheme

	// Build the controller manager.
	mgr, err := ctrl.NewManager(opts.KubeConfig, ctrl.Options{
		Scheme:                  opts.Scheme,
		MetricsBindAddress:      opts.MetricsBindAddr,
		LeaderElection:          opts.LeaderElectionEnabled,
		LeaderElectionID:        opts.LeaderElectionID,
		LeaderElectionNamespace: opts.LeaderElectionNamespace,
		SyncPeriod:              &opts.SyncPeriod,
		Namespace:               opts.WatchNamespace,
		HealthProbeBindAddress:  opts.HealthAddr,
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to create manager")
	}

	// Build the controller manager context.
	controllerManagerContext := &context.ControllerManagerContext{
		Context:                 goctx.Background(),
		WatchNamespace:          opts.WatchNamespace,
		Name:                    opts.PodName,
		LeaderElectionID:        opts.LeaderElectionID,
		LeaderElectionNamespace: opts.LeaderElectionNamespace,
		MaxConcurrentReconciles: opts.MaxConcurrentReconciles,
		Client:                  mgr.GetClient(),
		Logger:                  opts.Logger.WithName(opts.PodName),
		Scheme:                  opts.Scheme,
	}

	// Add the requested items to the manager.
	if err := opts.AddToManager(controllerManagerContext, mgr); err != nil {
		return nil, errors.Wrap(err, "failed to add resources to the manager")
	}

	// +kubebuilder:scaffold:builder

	return &manager{
		Manager: mgr,
		ctx:     controllerManagerContext,
	}, nil
}

type manager struct {
	ctrl.Manager
	ctx *context.ControllerManagerContext
}

func (m *manager) GetContext() *context.ControllerManagerContext {
	return m.ctx
}
