package session

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	kcfg "sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewKubeClient returns a new client for the target cluster using the KubeConfig
// secret stored in the management cluster.
func NewKubeClient(
	ctx context.Context,
	controllerClient client.Client,
	cluster *clusterv1.Cluster) (kubernetes.Interface, error) {
	clusterKey := client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}
	kubeconfig, err := kcfg.FromSecret(ctx, controllerClient, clusterKey)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve kubeconfig secret for Cluster %q in namespace %q",
			cluster.Name, cluster.Namespace)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create client configuration for Cluster %q in namespace %q",
			cluster.Name, cluster.Namespace)
	}
	// sets the timeout, otherwise this will default to 0 (i.e. no timeout) which might cause tests to hang
	restConfig.Timeout = 10 * time.Second

	return kubernetes.NewForConfig(restConfig)
}
