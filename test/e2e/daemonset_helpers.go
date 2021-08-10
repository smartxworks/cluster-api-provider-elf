package e2e

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/cluster-api/test/framework"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WaitForKubeProxyUpgradeInput is the input for WaitForKubeProxyUpgrade.
type WaitForKubeProxyUpgradeInput struct {
	Getter            framework.Getter
	KubernetesVersion string
}

// WaitForKubeProxyUpgrade waits until kube-proxy version matches with the kubernetes version. This is called during KCP upgrade.
func WaitForKubeProxyUpgrade(ctx context.Context, input WaitForKubeProxyUpgradeInput, intervals ...interface{}) {
	By("Ensuring kube-proxy has the correct image")

	Eventually(func() (bool, error) {
		ds := &appsv1.DaemonSet{}

		if err := input.Getter.Get(ctx, client.ObjectKey{Name: "kube-proxy", Namespace: metav1.NamespaceSystem}, ds); err != nil {
			return false, err
		}

		if strings.HasSuffix(ds.Spec.Template.Spec.Containers[0].Image, fmt.Sprintf(":%s", input.KubernetesVersion)) {
			return true, nil
		}

		return false, nil
	}, intervals...).Should(BeTrue())
}
