package e2e

import (
	"context"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/cluster-api/test/framework"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/cluster-api/controllers/noderefutil"
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
		for i := range nodeList.Items {
			if noderefutil.IsNodeReady(&nodeList.Items[i]) {
				nodeReadyCount++
			} else {
				notReadyNodeName = nodeList.Items[i].Name
			}
		}

		return input.ReadyCount == nodeReadyCount && input.NotReadyNodeName == notReadyNodeName, nil
	}, input.WaitForNodeNotReady...).Should(BeTrue())
}
