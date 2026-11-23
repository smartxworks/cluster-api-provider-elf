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

package fake

import (
	agentv1 "github.com/smartxworks/host-config-agent-api/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cgscheme "k8s.io/client-go/kubernetes/scheme"
	controlplanev1beta1 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta1"
	controlplanev1 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta2"
	capiv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
)

const (
	// ControllerManagerName is the name of the fake controller manager.
	ControllerManagerName = "fake-controller-manager"

	// ControllerManagerNamespace is the name of the namespace in which the
	// fake controller manager's resources are located.
	ControllerManagerNamespace = "fake-cape-system"

	// LeaderElectionNamespace is the namespace used to control leader election
	// for the fake controller manager.
	LeaderElectionNamespace = ControllerManagerNamespace

	// LeaderElectionID is the name of the ID used to control leader election
	// for the fake controller manager.
	LeaderElectionID = ControllerManagerName + "-runtime"
)

// NewControllerManagerContext returns a fake ControllerManagerContext for unit
// testing reconcilers and webhooks with a fake client. You can choose to
// initialize it with a slice of runtime.Object.
func NewControllerManagerContext(initObjects ...client.Object) *context.ControllerManagerContext {
	scheme := runtime.NewScheme()
	utilruntime.Must(cgscheme.AddToScheme(scheme))
	utilruntime.Must(capiv1beta1.AddToScheme(scheme))
	utilruntime.Must(capiv1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1beta1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1.AddToScheme(scheme))
	utilruntime.Must(capev1.AddToScheme(scheme))
	utilruntime.Must(agentv1.AddToScheme(scheme))

	clientWithObjects := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(
		&capev1.ElfCluster{},
		&capev1.ElfMachine{},
	).WithObjects(initObjects...).Build()

	return &context.ControllerManagerContext{
		Client:                  clientWithObjects,
		Scheme:                  scheme,
		Namespace:               ControllerManagerNamespace,
		Name:                    ControllerManagerName,
		LeaderElectionNamespace: LeaderElectionNamespace,
		LeaderElectionID:        LeaderElectionID,
	}
}
