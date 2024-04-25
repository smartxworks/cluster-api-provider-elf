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
	goctx "context"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

func TestElfMachineValidatorValidateUpdate(t *testing.T) {
	g := NewWithT(t)

	var tests []elfMachineTestCase
	scheme := newScheme(g)

	elfMachineTemplate := &infrav1.ElfMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					DiskGiB: 1,
				},
			},
		},
	}

	tests = append(tests, elfMachineTestCase{
		Name: "Cannot reduce disk capacity",
		OldEM: &infrav1.ElfMachine{
			Spec: infrav1.ElfMachineSpec{
				DiskGiB: 2,
			},
		},
		EM: &infrav1.ElfMachine{
			Spec: infrav1.ElfMachineSpec{
				DiskGiB: 1,
			},
		},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "diskGiB"), 1, diskCapacityCanOnlyBeExpanded),
		},
	})

	tests = append(tests, elfMachineTestCase{
		Name:  "Disk cannot be modified directly",
		OldEM: nil,
		EM: &infrav1.ElfMachine{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					clusterv1.TemplateClonedFromNameAnnotation: elfMachineTemplate.Name,
				},
			},
			Spec: infrav1.ElfMachineSpec{
				DiskGiB: 2,
			},
		},
		Objs: []client.Object{elfMachineTemplate},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "diskGiB"), 2, fmt.Sprintf(canOnlyModifiedThroughElfMachineTemplate, elfMachineTemplate.Name)),
		},
	})

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			validator := &ElfMachineValidator{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.Objs...).Build(),
			}
			warnings, err := validator.ValidateUpdate(goctx.Background(), tc.OldEM, tc.EM)
			g.Expect(warnings).To(BeEmpty())
			expectElfMachineTestCase(g, tc, err)
		})
	}
}

func newScheme(g Gomega) *runtime.Scheme {
	scheme := runtime.NewScheme()
	g.Expect(infrav1.AddToScheme(scheme)).To(Succeed())

	return scheme
}

func expectElfMachineTestCase(g Gomega, tc elfMachineTestCase, err error) {
	if tc.Errs != nil {
		g.Expect(err).To(HaveOccurred())
		statusErr, ok := err.(*apierrors.StatusError)
		g.Expect(ok).To(BeTrue())
		g.Expect(statusErr.ErrStatus.Details.Group).To(Equal(tc.EM.GroupVersionKind().Group))
		g.Expect(statusErr.ErrStatus.Details.Kind).To(Equal(tc.EM.GroupVersionKind().Kind))
		g.Expect(statusErr.ErrStatus.Details.Name).To(Equal(tc.EM.Name))
		causes := make([]metav1.StatusCause, 0, len(tc.Errs))
		for i := range len(tc.Errs) {
			causes = append(causes, metav1.StatusCause{
				Type:    metav1.CauseType(tc.Errs[i].Type),
				Message: tc.Errs[i].ErrorBody(),
				Field:   tc.Errs[i].Field,
			})
		}
		g.Expect(statusErr.ErrStatus.Details.Causes).To(Equal(causes))
	} else {
		g.Expect(err).NotTo(HaveOccurred())
	}
}

type elfMachineTestCase struct {
	Name  string
	EM    *infrav1.ElfMachine
	OldEM *infrav1.ElfMachine
	Objs  []client.Object
	Errs  field.ErrorList
}
