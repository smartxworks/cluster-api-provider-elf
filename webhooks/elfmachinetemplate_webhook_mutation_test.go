/*
Copyright 2024.

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
	"testing"

	. "github.com/onsi/gomega"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

func TestElfMachineMutationTemplate(t *testing.T) {
	g := NewWithT(t)
	tests := []testCase{}

	elfMachineTemplate := &infrav1.ElfMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "elfmachinetemplate",
		},
		Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{NumCoresPerSocket: 1},
			},
		},
	}
	elfMachineTemplate.Spec.Template.Spec.Network.Devices = []infrav1.NetworkDeviceSpec{
		{AddressesFromPools: []corev1.TypedLocalObjectReference{{Name: "test"}}},
		{AddressesFromPools: []corev1.TypedLocalObjectReference{{Name: "test", APIGroup: pointer.String("")}}},
		{AddressesFromPools: []corev1.TypedLocalObjectReference{{Name: "test", APIGroup: pointer.String("apiGroup")}}},
		{AddressesFromPools: []corev1.TypedLocalObjectReference{{Name: "test", APIGroup: pointer.String("apiGroup"), Kind: "kind"}}},
	}
	raw, err := marshal(elfMachineTemplate)
	g.Expect(err).NotTo(HaveOccurred())
	tests = append(tests, testCase{
		name: "should set default values for network devices",
		admissionRequest: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Kind:      metav1.GroupVersionKind{Group: infrav1.GroupVersion.Group, Version: infrav1.GroupVersion.Version, Kind: "ElfMachine"},
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
		}},
		expectRespAllowed: true,
		expectPatchs: []jsonpatch.Operation{
			{Operation: "replace", Path: "/spec/template/spec/network/devices/0/addressesFromPools/0/apiGroup", Value: defaultIPPoolAPIGroup},
			{Operation: "replace", Path: "/spec/template/spec/network/devices/0/addressesFromPools/0/kind", Value: defaultIPPoolKind},
			{Operation: "replace", Path: "/spec/template/spec/network/devices/1/addressesFromPools/0/apiGroup", Value: defaultIPPoolAPIGroup},
			{Operation: "replace", Path: "/spec/template/spec/network/devices/1/addressesFromPools/0/kind", Value: defaultIPPoolKind},
			{Operation: "replace", Path: "/spec/template/spec/network/devices/2/addressesFromPools/0/kind", Value: defaultIPPoolKind},
		},
	})

	elfMachineTemplate.Spec.Template.Spec.Network.Devices = nil
	elfMachineTemplate.Spec.Template.Spec.NumCPUs = 1
	elfMachineTemplate.Spec.Template.Spec.NumCoresPerSocket = 0
	raw, err = marshal(elfMachineTemplate)
	g.Expect(err).NotTo(HaveOccurred())
	tests = append(tests, testCase{
		name: "should set default values for numCoresPerSocket",
		admissionRequest: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Kind:      metav1.GroupVersionKind{Group: infrav1.GroupVersion.Group, Version: infrav1.GroupVersion.Version, Kind: "ElfMachine"},
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
		}},
		expectRespAllowed: true,
		expectPatchs: []jsonpatch.Operation{
			{Operation: "add", Path: "/spec/template/spec/numCoresPerSocket", Value: float64(elfMachineTemplate.Spec.Template.Spec.NumCPUs)},
		},
	})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mutation := ElfMachineTemplateMutation{}
			mutation.InjectDecoder(admission.NewDecoder(scheme))

			resp := mutation.Handle(context.Background(), tc.admissionRequest)
			g.Expect(resp.Allowed).Should(Equal(tc.expectRespAllowed))
			g.Expect(resp.Patches).Should(HaveLen(len(tc.expectPatchs)))
			g.Expect(resp.Patches).Should(ContainElements(tc.expectPatchs))
		})
	}
}
