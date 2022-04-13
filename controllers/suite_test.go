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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cgscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/manager"
	"github.com/smartxworks/cluster-api-provider-elf/test/helpers"
)

var (
	testEnv *helpers.TestEnvironment
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

func TestMain(m *testing.M) {
	setup()

	defer func() {
		teardown()
	}()

	code := m.Run()
	os.Exit(code) // nolint:gocritic
}

func setup() {
	utilruntime.Must(infrav1.AddToScheme(cgscheme.Scheme))
	utilruntime.Must(clusterv1.AddToScheme(cgscheme.Scheme))

	testEnv = helpers.NewTestEnvironment()

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
	if err := testEnv.Create(testEnv.GetContext(), ns); err != nil {
		panic("unable to create controller namespace")
	}

	if err := AddClusterControllerToManager(testEnv.GetContext(), testEnv.Manager); err != nil {
		panic(fmt.Sprintf("unable to setup ElfCluster controller: %v", err))
	}
	if err := AddMachineControllerToManager(testEnv.GetContext(), testEnv.Manager); err != nil {
		panic(fmt.Sprintf("unable to setup ElfMachine controller: %v", err))
	}
}

func teardown() {
	if err := testEnv.Stop(); err != nil {
		panic(fmt.Sprintf("Failed to stop envtest: %v", err))
	}
}
