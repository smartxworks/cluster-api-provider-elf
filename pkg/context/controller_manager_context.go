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

package context

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ControllerManagerContext is the context of the controller that owns the
// controllers.
//nolint:containedctx
type ControllerManagerContext struct {
	context.Context

	// Namespace is the namespace in which the resource is located responsible
	// for running the controller manager.
	Namespace string

	// Name is the name of the controller manager.
	Name string

	// LeaderElectionID is the information used to identify the object
	// responsible for synchronizing leader election.
	LeaderElectionID string

	// LeaderElectionNamespace is the namespace in which the LeaderElection
	// object is located.
	LeaderElectionNamespace string

	// WatchNamespace is the namespace the controllers watch for changes. If
	// no value is specified then all namespaces are watched.
	WatchNamespace string

	// Client is the controller manager's client.
	Client client.Client

	// Logger is the controller manager's logger.
	Logger logr.Logger

	// Scheme is the controller manager's API scheme.
	Scheme *runtime.Scheme

	// MaxConcurrentReconciles is the maximum number of recocnile requests this
	// controller will receive concurrently.
	MaxConcurrentReconciles int
}

// String returns ControllerManagerName.
func (r *ControllerManagerContext) String() string {
	return r.Name
}
