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
	"testing"

	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

func TestElfMachineTemplateValidatorValidateCreate(t *testing.T) {
	g := NewWithT(t)

	var tests []testCaseEMT
	tests = append(tests, testCaseEMT{
		Name: "disk capacity cannot be less than 0",
		EMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					NumCPUs:           1,
					NumCoresPerSocket: 1,
					MemoryMiB:         1,
					DiskGiB:           -1,
				},
			},
		}},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "template", "spec", "diskGiB"), -1, diskCapacityCannotLessThanZeroMsg),
		},
	}, testCaseEMT{
		Name: "disk capacity can be 0",
		EMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					NumCPUs:           1,
					NumCoresPerSocket: 1,
					MemoryMiB:         1,
					DiskGiB:           0,
				},
			},
		}},
		Errs: nil,
	}, testCaseEMT{
		Name: "disk capacity can > 0",
		EMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					NumCPUs:           1,
					NumCoresPerSocket: 1,
					MemoryMiB:         1,
					DiskGiB:           100,
				},
			},
		}},
		Errs: nil,
	}, testCaseEMT{
		Name: "memory cannot be less than 0",
		EMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					NumCPUs:           1,
					NumCoresPerSocket: 1,
					DiskGiB:           100,
					MemoryMiB:         0,
				},
			},
		}},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "template", "spec", "memoryMiB"), 0, memoryCannotLessThanZeroMsg),
		},
	}, testCaseEMT{
		Name: "numCPUs cannot be less than 0",
		EMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					NumCoresPerSocket: 1,
					DiskGiB:           100,
					MemoryMiB:         1,
					NumCPUs:           0,
				},
			},
		}},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "template", "spec", "numCPUs"), 0, numCPUsCannotLessThanZeroMsg),
		},
	}, testCaseEMT{
		Name: "numCoresPerSocket cannot be less than 0",
		EMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					NumCPUs:           1,
					DiskGiB:           100,
					MemoryMiB:         1,
					NumCoresPerSocket: 0,
				},
			},
		}},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "template", "spec", "numCoresPerSocket"), 0, numCoresPerSocketCannotLessThanZeroMsg),
		},
	})

	validator := &ElfMachineTemplateValidator{}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			warnings, err := validator.ValidateCreate(goctx.Background(), tc.EMT)
			g.Expect(warnings).To(BeEmpty())
			expectTestCase(g, tc, err)
		})
	}
}

func TestElfMachineTemplateValidatorValidateUpdate(t *testing.T) {
	g := NewWithT(t)

	var tests []testCaseEMT
	tests = append(tests, testCaseEMT{
		Name: "Cannot reduce disk capacity",
		OldEMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					NumCPUs:           1,
					NumCoresPerSocket: 1,
					MemoryMiB:         1,
					DiskGiB:           2,
				},
			},
		}},
		EMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					NumCPUs:           1,
					NumCoresPerSocket: 1,
					MemoryMiB:         1,
					DiskGiB:           1,
				},
			},
		}},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "template", "spec", "diskGiB"), 1, diskCapacityCanOnlyBeExpandedMsg),
		},
	}, testCaseEMT{
		Name: "memory cannot be less than 0",
		OldEMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{},
			},
		}},
		EMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					NumCPUs:           1,
					NumCoresPerSocket: 1,
					DiskGiB:           1,
					MemoryMiB:         0,
				},
			},
		}},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "template", "spec", "memoryMiB"), 0, memoryCannotLessThanZeroMsg),
		},
	}, testCaseEMT{
		Name: "numCPUs cannot be less than 0",
		OldEMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{},
			},
		}},
		EMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					NumCoresPerSocket: 1,
					DiskGiB:           1,
					MemoryMiB:         1,
					NumCPUs:           0,
				},
			},
		}},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "template", "spec", "numCPUs"), 0, numCPUsCannotLessThanZeroMsg),
		},
	}, testCaseEMT{
		Name: "numCoresPerSocket cannot be less than 0",
		OldEMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{},
			},
		}},
		EMT: &infrav1.ElfMachineTemplate{Spec: infrav1.ElfMachineTemplateSpec{
			Template: infrav1.ElfMachineTemplateResource{
				Spec: infrav1.ElfMachineSpec{
					DiskGiB:           1,
					MemoryMiB:         1,
					NumCPUs:           1,
					NumCoresPerSocket: 0,
				},
			},
		}},
		Errs: field.ErrorList{
			field.Invalid(field.NewPath("spec", "template", "spec", "numCoresPerSocket"), 0, numCoresPerSocketCannotLessThanZeroMsg),
		},
	})

	validator := &ElfMachineTemplateValidator{}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			warnings, err := validator.ValidateUpdate(goctx.Background(), tc.OldEMT, tc.EMT)
			g.Expect(warnings).To(BeEmpty())
			expectTestCase(g, tc, err)
		})
	}
}

func expectTestCase(g Gomega, tc testCaseEMT, err error) {
	if tc.Errs != nil {
		g.Expect(err).To(HaveOccurred())
		statusErr, ok := err.(*apierrors.StatusError)
		g.Expect(ok).To(BeTrue())
		g.Expect(statusErr.ErrStatus.Details.Group).To(Equal(tc.EMT.GroupVersionKind().Group))
		g.Expect(statusErr.ErrStatus.Details.Kind).To(Equal(tc.EMT.GroupVersionKind().Kind))
		g.Expect(statusErr.ErrStatus.Details.Name).To(Equal(tc.EMT.Name))
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

type testCaseEMT struct {
	Name   string
	EMT    *infrav1.ElfMachineTemplate
	OldEMT *infrav1.ElfMachineTemplate
	Errs   field.ErrorList
}
