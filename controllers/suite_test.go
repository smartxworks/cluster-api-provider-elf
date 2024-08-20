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
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cgscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/manager"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/service"
	"github.com/smartxworks/cluster-api-provider-elf/test/helpers"
)

const (
	timeout = time.Second * 10
)

var (
	testEnv         *helpers.TestEnvironment
	ctx             = ctrl.SetupSignalHandler()
	unexpectedError = errors.New("unexpected error")
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
	klog.SetOutput(GinkgoWriter)
	ctrl.SetLogger(klog.Background())
	ctrllog.SetLogger(klog.Background())

	utilruntime.Must(infrav1.AddToScheme(cgscheme.Scheme))
	utilruntime.Must(clusterv1.AddToScheme(cgscheme.Scheme))

	testEnv = helpers.NewTestEnvironment(ctx)

	// Set kubeconfig.
	os.Setenv("KUBECONFIG", testEnv.Kubeconfig)

	go func() {
		fmt.Println("Starting the manager")
		if err := testEnv.StartManager(ctx); err != nil {
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
	if err := testEnv.CreateAndWait(ctx, ns); err != nil {
		panic("unable to create controller namespace")
	}

	controllerOpts := controller.Options{MaxConcurrentReconciles: 10}

	if err := AddClusterControllerToManager(ctx, testEnv.GetControllerManagerContext(), testEnv.Manager, controllerOpts); err != nil {
		panic(fmt.Sprintf("unable to setup ElfCluster controller: %v", err))
	}
	if err := AddMachineControllerToManager(ctx, testEnv.GetControllerManagerContext(), testEnv.Manager, controllerOpts); err != nil {
		panic(fmt.Sprintf("unable to setup ElfMachine controller: %v", err))
	}
}

func teardown() {
	if err := testEnv.Stop(); err != nil {
		panic(fmt.Sprintf("failed to stop envtest: %v", err))
	}
}

func newMachineContext(
	elfCluster *infrav1.ElfCluster, cluster *clusterv1.Cluster,
	elfMachine *infrav1.ElfMachine, machine *clusterv1.Machine,
	vmService service.VMService) *context.MachineContext {
	return &context.MachineContext{
		Cluster:    cluster,
		ElfCluster: elfCluster,
		Machine:    machine,
		ElfMachine: elfMachine,
		VMService:  vmService,
	}
}

func newMachineTemplateContext(
	elfCluster *infrav1.ElfCluster, cluster *clusterv1.Cluster,
	emt *infrav1.ElfMachineTemplate) *context.MachineTemplateContext {
	return &context.MachineTemplateContext{
		Cluster:            cluster,
		ElfCluster:         elfCluster,
		ElfMachineTemplate: emt,
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

func intOrStrPtr(i int32) *intstr.IntOrString {
	res := intstr.FromInt(int(i))
	return &res
}
