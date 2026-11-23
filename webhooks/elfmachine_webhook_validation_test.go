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
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

func TestElfMachineValidatorValidateUpdate(t *testing.T) {
	g := NewWithT(t)

	tests := make([]elfMachineTestCase, 0, 8)
	scheme := newScheme(g)

	elfMachineTemplate := &capev1.ElfMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: capev1.ElfMachineTemplateSpec{
			Template: capev1.ElfMachineTemplateResource{
				Spec: capev1.ElfMachineSpec{
					DiskGiB:   1,
					NumCPUs:   1,
					MemoryMiB: 1,
				},
			},
		},
	}

	tests = append(tests, elfMachineTestCase{
		Name: "Cannot reduce disk capacity",
		OldEM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				DiskGiB: 2,
			},
		},
		EM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				DiskGiB: 1,
			},
		},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "diskGiB"), 1, diskCapacityCanOnlyBeExpandedMsg),
		},
	})
	tests = append(tests, elfMachineTestCase{
		Name: "Cannot reduce vcpu capacity",
		OldEM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				NumCPUs: 2,
			},
		},
		EM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				NumCPUs: 1,
			},
		},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "numCPUs"), 1, vcpuCapacityCanOnlyBeExpandedMsg),
		},
	})
	tests = append(tests, elfMachineTestCase{
		Name: "Cannot reduce memory capacity",
		OldEM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				MemoryMiB: 2,
			},
		},
		EM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				MemoryMiB: 1,
			},
		},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "memoryMiB"), 1, memoryCapacityCanOnlyBeExpandedMsg),
		},
	})
	tests = append(tests, elfMachineTestCase{
		Name: "Can update the default numCoresPerSocket",
		OldEM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				NumCoresPerSocket: 0,
			},
		},
		EM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				NumCoresPerSocket: 1,
			},
		},
		Errs: nil,
	})
	tests = append(tests, elfMachineTestCase{
		Name: "Cannot update numCoresPerSocket",
		OldEM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				NumCoresPerSocket: 1,
			},
		},
		EM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				NumCoresPerSocket: 2,
			},
		},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "numCoresPerSocket"), 2, numCoresPerSocketCannotBeChanged),
		},
	})

	tests = append(tests, elfMachineTestCase{
		Name: "Disk cannot be modified directly",
		OldEM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				DiskGiB:   1,
				NumCPUs:   1,
				MemoryMiB: 1,
			},
		},
		EM: &capev1.ElfMachine{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					capiv1.TemplateClonedFromNameAnnotation: elfMachineTemplate.Name,
				},
			},
			Spec: capev1.ElfMachineSpec{
				DiskGiB:   2,
				NumCPUs:   1,
				MemoryMiB: 1,
			},
		},
		Objs: []client.Object{elfMachineTemplate},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "diskGiB"), 2, fmt.Sprintf(canOnlyModifiedThroughElfMachineTemplate, elfMachineTemplate.Name)),
		},
	})
	tests = append(tests, elfMachineTestCase{
		Name: "vcpu cannot be modified directly",
		OldEM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				DiskGiB:   1,
				NumCPUs:   1,
				MemoryMiB: 1,
			},
		},
		EM: &capev1.ElfMachine{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					capiv1.TemplateClonedFromNameAnnotation: elfMachineTemplate.Name,
				},
			},
			Spec: capev1.ElfMachineSpec{
				DiskGiB:   1,
				NumCPUs:   2,
				MemoryMiB: 1,
			},
		},
		Objs: []client.Object{elfMachineTemplate},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "numCPUs"), 2, fmt.Sprintf(canOnlyModifiedThroughElfMachineTemplate, elfMachineTemplate.Name)),
		},
	})
	tests = append(tests, elfMachineTestCase{
		Name: "memory cannot be modified directly",
		OldEM: &capev1.ElfMachine{
			Spec: capev1.ElfMachineSpec{
				DiskGiB:   1,
				NumCPUs:   1,
				MemoryMiB: 1,
			},
		},
		EM: &capev1.ElfMachine{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					capiv1.TemplateClonedFromNameAnnotation: elfMachineTemplate.Name,
				},
			},
			Spec: capev1.ElfMachineSpec{
				DiskGiB:   1,
				NumCPUs:   1,
				MemoryMiB: 2,
			},
		},
		Objs: []client.Object{elfMachineTemplate},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "memoryMiB"), 2, fmt.Sprintf(canOnlyModifiedThroughElfMachineTemplate, elfMachineTemplate.Name)),
		},
	})

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			validator := &ElfMachineValidator{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.Objs...).Build(),
			}
			warnings, err := validator.ValidateUpdate(t.Context(), tc.OldEM, tc.EM)
			g.Expect(warnings).To(BeEmpty())
			expectElfMachineTestCase(g, tc, err)
		})
	}
}

func newScheme(g Gomega) *runtime.Scheme {
	scheme := runtime.NewScheme()
	g.Expect(capev1.AddToScheme(scheme)).To(Succeed())

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
	EM    *capev1.ElfMachine
	OldEM *capev1.ElfMachine
	Objs  []client.Object
	Errs  field.ErrorList
}
