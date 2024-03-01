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
	"testing"

	. "github.com/onsi/gomega"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
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
	tests := []testCase{}

	elfMachine := fake.NewElfMachine(nil)
	elfMachine.Annotations = nil
	raw, err := marshal(elfMachine)
	g.Expect(err).NotTo(HaveOccurred())
	tests = append(tests, testCase{
		name: "should set CAPE version",
		admissionRequest: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Kind:      metav1.GroupVersionKind{Group: infrav1.GroupVersion.Group, Version: infrav1.GroupVersion.Version, Kind: "ElfMachine"},
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
		}},
		expectRespAllowed: true,
		expectPatchs: []jsonpatch.Operation{
			{Operation: "add", Path: "/metadata/annotations", Value: map[string]interface{}{infrav1.CAPEVersionAnnotation: version.CAPEVersion()}},
		},
	})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mutation := ElfMachineMutation{}
			mutation.InjectDecoder(admission.NewDecoder(scheme))

			resp := mutation.Handle(context.Background(), tc.admissionRequest)
			g.Expect(resp.Allowed).Should(Equal(tc.expectRespAllowed))
			g.Expect(resp.Patches).Should(Equal(tc.expectPatchs))
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
}
