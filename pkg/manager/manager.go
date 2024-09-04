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

package manager

import (
	goctx "context"

	"github.com/pkg/errors"
	agentv1 "github.com/smartxworks/host-config-agent-api/api/v1alpha1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cgscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/vendor"
)

// Manager is a CAPE controller manager.
type Manager interface {
	ctrl.Manager

	// GetContext returns the controller manager's context.
	GetControllerManagerContext() *context.ControllerManagerContext
}

// New returns a new CAPE controller manager.
func New(ctx goctx.Context, opts Options) (Manager, error) {
	// Ensure the default options are set.
	opts.defaults()

	utilruntime.Must(cgscheme.AddToScheme(opts.Scheme))
	utilruntime.Must(clusterv1.AddToScheme(opts.Scheme))
	utilruntime.Must(infrav1.AddToScheme(opts.Scheme))
	utilruntime.Must(bootstrapv1.AddToScheme(opts.Scheme))
	utilruntime.Must(controlplanev1.AddToScheme(opts.Scheme))
	vendor.InitHostAgentAPIGroup(&agentv1.GroupVersion, agentv1.SchemeBuilder)
	utilruntime.Must(agentv1.AddToScheme(opts.Scheme))
	// +kubebuilder:scaffold:scheme

	// Build the controller manager.
	mgr, err := ctrl.NewManager(opts.KubeConfig, opts.Options)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create manager")
	}

	// Build the controller manager context.
	ctrlMgrCtx := &context.ControllerManagerContext{
		WatchNamespaces:         opts.Cache.DefaultNamespaces,
		Namespace:               opts.PodNamespace,
		Name:                    opts.PodName,
		LeaderElectionID:        opts.LeaderElectionID,
		LeaderElectionNamespace: opts.LeaderElectionNamespace,
		Client:                  mgr.GetClient(),
		Scheme:                  opts.Scheme,
		WatchFilterValue:        opts.WatchFilterValue,
	}

	// Add the requested items to the manager.
	if err := opts.AddToManager(ctx, ctrlMgrCtx, mgr); err != nil {
		return nil, errors.Wrap(err, "failed to add resources to the manager")
	}

	// +kubebuilder:scaffold:builder

	return &manager{
		Manager:    mgr,
		ctrlMgrCtx: ctrlMgrCtx,
	}, nil
}

type manager struct {
	ctrl.Manager
	ctrlMgrCtx *context.ControllerManagerContext
}

func (m *manager) GetControllerManagerContext() *context.ControllerManagerContext {
	return m.ctrlMgrCtx
}
