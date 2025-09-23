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

package webhooks

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	. "github.com/onsi/gomega"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/util/annotations"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/version"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

func init() {
	scheme = runtime.NewScheme()
	_ = infrav1.AddToScheme(scheme)
	_ = admissionv1.AddToScheme(scheme)
}

var (
	scheme *runtime.Scheme
)

func TestElfMachineMutation(t *testing.T) {
	g := NewWithT(t)
	scheme := newScheme(g)
	tests := []testCase{}

	elfMachine := fake.NewElfMachine(nil)
	elfMachine.Annotations = nil
	elfMachine.Spec.NumCoresPerSocket = 0
	raw, err := marshal(elfMachine)
	g.Expect(err).NotTo(HaveOccurred())
	tests = append(tests, testCase{
		name: "should set CAPE version and numCoresPerSocket",
		admissionRequest: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Kind:      metav1.GroupVersionKind{Group: infrav1.GroupVersion.Group, Version: infrav1.GroupVersion.Version, Kind: "ElfMachine"},
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
		}},
		expectRespAllowed: true,
		expectPatchs: []jsonpatch.Operation{
			{Operation: "add", Path: "/metadata/annotations", Value: map[string]interface{}{infrav1.CAPEVersionAnnotation: version.CAPEVersion()}},
			{Operation: "add", Path: "/spec/numCoresPerSocket", Value: json.Number(strconv.Itoa(int(elfMachine.Spec.NumCPUs)))},
		},
	})

	elfMachineTemplate := fake.NewElfMachineTemplate()
	elfMachineTemplate.Spec.Template.Spec.NumCoresPerSocket = 2
	elfMachine = fake.NewElfMachine(nil)
	annotations.AddAnnotations(elfMachine, map[string]string{clusterv1.TemplateClonedFromNameAnnotation: elfMachineTemplate.Name})
	elfMachine.Spec.NumCoresPerSocket = 0
	raw, err = marshal(elfMachine)
	g.Expect(err).NotTo(HaveOccurred())
	tests = append(tests, testCase{
		name: "should set NumCoresPerSocket from elfMachineTemplate",
		admissionRequest: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Kind:      metav1.GroupVersionKind{Group: infrav1.GroupVersion.Group, Version: infrav1.GroupVersion.Version, Kind: "ElfMachine"},
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: raw},
		}},
		expectRespAllowed: true,
		expectPatchs: []jsonpatch.Operation{
			{Operation: "add", Path: "/spec/numCoresPerSocket", Value: json.Number(strconv.Itoa(int(elfMachineTemplate.Spec.Template.Spec.NumCoresPerSocket)))},
		},
		client: fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(elfMachineTemplate).Build(),
	})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mutation := ElfMachineMutation{Client: tc.client}
			mutation.InjectDecoder(admission.NewDecoder(scheme))

			resp := mutation.Handle(context.Background(), tc.admissionRequest)
			g.Expect(resp.Allowed).Should(Equal(tc.expectRespAllowed))
			g.Expect(resp.Patches).Should(ContainElements(tc.expectPatchs))
		})
	}
}

func marshal(obj client.Object) ([]byte, error) {
	bs, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return bs, nil
}

type testCase struct {
	name              string
	admissionRequest  admission.Request
	expectRespAllowed bool
	expectPatchs      []jsonpatch.Operation
	client            client.Client
}
