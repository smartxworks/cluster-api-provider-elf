/*
Copyright 2021.

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

package controllers

import (
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cgscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/manager"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
	"github.com/smartxworks/cluster-api-provider-elf/test/helpers"
)

const (
	timeout = time.Second * 10
)

var (
	testEnv *helpers.TestEnvironment
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

func TestMain(m *testing.M) {
	code := 0

	defer func() { os.Exit(code) }()

	setup()

	defer teardown()

	code = m.Run()
}

func setup() {
	// set log
	klog.InitFlags(nil)
	if err := flag.Set("logtostderr", "false"); err != nil {
		_ = fmt.Errorf("Error setting logtostderr flag")
	}
	if err := flag.Set("v", "6"); err != nil {
		_ = fmt.Errorf("Error setting v flag")
	}
	if err := flag.Set("alsologtostderr", "false"); err != nil {
		_ = fmt.Errorf("Error setting alsologtostderr flag")
	}

	utilruntime.Must(infrav1.AddToScheme(cgscheme.Scheme))
	utilruntime.Must(clusterv1.AddToScheme(cgscheme.Scheme))

	testEnv = helpers.NewTestEnvironment()

	// Set kubeconfig.
	os.Setenv("KUBECONFIG", testEnv.Kubeconfig)

	go func() {
		fmt.Println("Starting the manager")
		if err := testEnv.StartManager(testEnv.GetContext()); err != nil {
			panic(fmt.Sprintf("failed to start the envtest manager: %v", err))
		}
	}()
	<-testEnv.Manager.Elected()

	// create manager pod namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: manager.DefaultPodNamespace,
		},
	}
	if err := testEnv.CreateAndWait(testEnv.GetContext(), ns); err != nil {
		panic("unable to create controller namespace")
	}

	controllerOpts := controller.Options{MaxConcurrentReconciles: 10}

	if err := AddClusterControllerToManager(testEnv.GetContext(), testEnv.Manager, controllerOpts); err != nil {
		panic(fmt.Sprintf("unable to setup ElfCluster controller: %v", err))
	}
	if err := AddMachineControllerToManager(testEnv.GetContext(), testEnv.Manager, controllerOpts); err != nil {
		panic(fmt.Sprintf("unable to setup ElfMachine controller: %v", err))
	}
}

func teardown() {
	if err := testEnv.Stop(); err != nil {
		panic(fmt.Sprintf("failed to stop envtest: %v", err))
	}
}

func newCtrlContexts(objs ...client.Object) *context.ControllerContext {
	ctrlMgrContext := fake.NewControllerManagerContext(objs...)
	ctrlContext := &context.ControllerContext{
		ControllerManagerContext: ctrlMgrContext,
		Logger:                   ctrllog.Log,
	}

	return ctrlContext
}

func newMachineContext(ctrlCtx *context.ControllerContext,
	elfCluster *infrav1.ElfCluster, cluster *clusterv1.Cluster,
	elfMachine *infrav1.ElfMachine, machine *clusterv1.Machine,
	vmService service.VMService) *context.MachineContext {
	return &context.MachineContext{
		ControllerContext: ctrlCtx,
		Cluster:           cluster,
		ElfCluster:        elfCluster,
		Machine:           machine,
		ElfMachine:        elfMachine,
		Logger:            ctrlCtx.Logger,
		VMService:         vmService,
	}
}

type conditionAssertion struct {
	conditionType clusterv1.ConditionType
	status        corev1.ConditionStatus
	severity      clusterv1.ConditionSeverity
	reason        string
}

func expectConditions(getter conditions.Getter, expected []conditionAssertion) {
	Expect(len(getter.GetConditions())).To(BeNumerically(">=", len(expected)), "number of conditions")
	for _, c := range expected {
		actual := conditions.Get(getter, c.conditionType)
		Expect(actual).To(Not(BeNil()))
		Expect(actual.Type).To(Equal(c.conditionType))
		Expect(actual.Status).To(Equal(c.status))
		Expect(actual.Severity).To(Equal(c.severity))
		Expect(actual.Reason).To(Equal(c.reason))
	}
}
