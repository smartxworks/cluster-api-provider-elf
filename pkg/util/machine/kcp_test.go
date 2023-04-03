/*
Copyright 2023.

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

package machine

import (
	"testing"

	"github.com/onsi/gomega"

	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

func TestGetKCPByMachine(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	elfCluster, cluster := fake.NewClusterObjects()
	_, cpMachine := fake.NewMachineObjects(elfCluster, cluster)
	kubeadmCP := fake.NewKCP()
	fake.ToControlPlaneMachine(cpMachine, kubeadmCP)
	ctx := fake.NewControllerManagerContext(kubeadmCP)
	t.Run("should return kcp", func(t *testing.T) {
		kcp, err := GetKCPByMachine(ctx, ctx.Client, cpMachine)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(kcp.Name).To(gomega.Equal(kubeadmCP.Name))
	})

	_, workerMachine := fake.NewMachineObjects(elfCluster, cluster)
	t.Run("should return error", func(t *testing.T) {
		_, err := GetKCPByMachine(ctx, ctx.Client, workerMachine)
		g.Expect(err).To(gomega.HaveOccurred())
	})
}
