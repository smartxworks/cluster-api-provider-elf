package fake

import (
	goctx "context"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha3"
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
func NewControllerManagerContext(initObjects ...runtime.Object) *context.ControllerManagerContext {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = clusterv1.AddToScheme(scheme)
	_ = infrav1.AddToScheme(scheme)

	return &context.ControllerManagerContext{
		Context:                 goctx.Background(),
		Client:                  fake.NewFakeClientWithScheme(scheme, initObjects...),
		Logger:                  ctrllog.Log.WithName(ControllerManagerName),
		Scheme:                  scheme,
		Name:                    ControllerManagerName,
		LeaderElectionNamespace: LeaderElectionNamespace,
		LeaderElectionID:        LeaderElectionID,
	}
}
