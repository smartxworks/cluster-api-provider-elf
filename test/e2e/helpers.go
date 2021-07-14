package e2e

import (
	"context"
	"errors"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/cluster-api/test/framework"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/noderefutil"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha4"
	"sigs.k8s.io/cluster-api/util/patch"
)

// WaitForNodeNotReadyInput is the input for WaitForNodeNotReady.
type WaitForNodeNotReadyInput struct {
	Lister              framework.Lister
	ReadyCount          int
	NotReadyNodeName    string
	WaitForNodeNotReady []interface{}
}

// WaitForNodeNotReady waits until there is a not ready node
// and exactly the given count nodes and they are ready.
func WaitForNodeNotReady(ctx context.Context, input WaitForNodeNotReadyInput) {
	Eventually(func() (bool, error) {
		nodeList := &corev1.NodeList{}
		if err := input.Lister.List(ctx, nodeList); err != nil {
			return false, err
		}

		nodeReadyCount := 0
		notReadyNodeName := ""
		for _, node := range nodeList.Items {
			if noderefutil.IsNodeReady(&node) {
				nodeReadyCount++
			} else {
				notReadyNodeName = node.Name
			}
		}

		return input.ReadyCount == nodeReadyCount && input.NotReadyNodeName == notReadyNodeName, nil
	}, input.WaitForNodeNotReady...).Should(BeTrue())
}

type ScaleAndWaitControlPlaneInput struct {
	ClusterProxy        framework.ClusterProxy
	Cluster             *clusterv1.Cluster
	ControlPlane        *controlplanev1.KubeadmControlPlane
	Replicas            int32
	WaitForControlPlane []interface{}
}

// ScaleAndWaitControlPlane scales KCP and waits until all machines have node ref and equal to Replicas.
func ScaleAndWaitControlPlane(ctx context.Context, input ScaleAndWaitControlPlaneInput) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for ScaleAndWaitControlPlane")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling ScaleAndWaitControlPlane")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling ScaleAndWaitControlPlane")

	patchHelper, err := patch.NewHelper(input.ControlPlane, input.ClusterProxy.GetClient())
	Expect(err).ToNot(HaveOccurred())
	input.ControlPlane.Spec.Replicas = pointer.Int32Ptr(input.Replicas)
	Logf("Scaling controlplane %s/%s from %v to %v replicas", input.ControlPlane.Namespace, input.ControlPlane.Name, *input.ControlPlane.Spec.Replicas, input.Replicas)
	Expect(patchHelper.Patch(ctx, input.ControlPlane)).To(Succeed())

	Logf("Waiting for correct number of replicas to exist")
	Eventually(func() (int, error) {
		machines := framework.GetControlPlaneMachinesByCluster(ctx, framework.GetControlPlaneMachinesByClusterInput{
			Lister:      input.ClusterProxy.GetClient(),
			ClusterName: input.Cluster.Name,
			Namespace:   input.Cluster.Namespace,
		})

		nodeRefCount := 0
		for _, machine := range machines {
			if machine.Status.NodeRef != nil {
				nodeRefCount++
			}
		}

		if len(machines) != nodeRefCount {
			return -1, errors.New("Machine count does not match existing nodes count")
		}

		return nodeRefCount, nil
	}, input.WaitForControlPlane...).Should(Equal(int(input.Replicas)))
}
