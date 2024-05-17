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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

// Error messages.
const (
	diskCapacityCannotLessThanZeroMsg = "the disk capacity can only greater than or equal to 0"
	diskCapacityCanOnlyBeExpandedMsg  = "the disk capacity can only be expanded"

	memoryCannotLessThanZeroMsg = "the memory can only greater than 0"

	numCPUsCannotLessThanZeroMsg           = "the umCPUs can only greater than 0"
	numCoresPerSocketCannotLessThanZeroMsg = "the numCoresPerSocket can only greater than 0"
)

func (v *ElfMachineTemplateValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&infrav1.ElfMachineTemplate{}).
		WithValidator(v).
		Complete()
}

//+kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-elfmachinetemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=elfmachinetemplates,verbs=create;update,versions=v1beta1,name=validation.elfmachinetemplate.infrastructure.x-k8s.io,admissionReviewVersions=v1

// ElfMachineTemplateValidator implements a validation webhook for ElfMachineTemplate.
type ElfMachineTemplateValidator struct{}

var _ webhook.CustomValidator = &ElfMachineTemplateValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (v *ElfMachineTemplateValidator) ValidateCreate(ctx goctx.Context, obj runtime.Object) (admission.Warnings, error) {
	elfMachineTemplate, ok := obj.(*infrav1.ElfMachineTemplate)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected an ElfMachineTemplate but got a %T", obj))
	}

	var allErrs field.ErrorList
	if elfMachineTemplate.Spec.Template.Spec.DiskGiB < 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec", "diskGiB"), elfMachineTemplate.Spec.Template.Spec.DiskGiB, diskCapacityCannotLessThanZeroMsg))
	}

	if elfMachineTemplate.Spec.Template.Spec.MemoryMiB <= 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec", "memoryMiB"), elfMachineTemplate.Spec.Template.Spec.MemoryMiB, memoryCannotLessThanZeroMsg))
	}

	if elfMachineTemplate.Spec.Template.Spec.NumCPUs <= 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec", "numCPUs"), elfMachineTemplate.Spec.Template.Spec.NumCPUs, numCPUsCannotLessThanZeroMsg))
	}

	if elfMachineTemplate.Spec.Template.Spec.NumCoresPerSocket <= 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec", "numCoresPerSocket"), elfMachineTemplate.Spec.Template.Spec.NumCoresPerSocket, numCoresPerSocketCannotLessThanZeroMsg))
	}

	return nil, aggregateObjErrors(elfMachineTemplate.GroupVersionKind().GroupKind(), elfMachineTemplate.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (v *ElfMachineTemplateValidator) ValidateUpdate(ctx goctx.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldElfMachineTemplate, ok := oldObj.(*infrav1.ElfMachineTemplate) //nolint:forcetypeassert
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected an ElfMachineTemplate but got a %T", oldObj))
	}
	elfMachineTemplate, ok := newObj.(*infrav1.ElfMachineTemplate) //nolint:forcetypeassert
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected an ElfMachineTemplate but got a %T", newObj))
	}

	var allErrs field.ErrorList
	if elfMachineTemplate.Spec.Template.Spec.DiskGiB < oldElfMachineTemplate.Spec.Template.Spec.DiskGiB {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec", "diskGiB"), elfMachineTemplate.Spec.Template.Spec.DiskGiB, diskCapacityCanOnlyBeExpandedMsg))
	}

	if elfMachineTemplate.Spec.Template.Spec.MemoryMiB <= 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec", "memoryMiB"), elfMachineTemplate.Spec.Template.Spec.MemoryMiB, memoryCannotLessThanZeroMsg))
	}

	if elfMachineTemplate.Spec.Template.Spec.NumCPUs <= 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec", "numCPUs"), elfMachineTemplate.Spec.Template.Spec.NumCPUs, numCPUsCannotLessThanZeroMsg))
	}

	if elfMachineTemplate.Spec.Template.Spec.NumCoresPerSocket <= 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec", "numCoresPerSocket"), elfMachineTemplate.Spec.Template.Spec.NumCoresPerSocket, numCoresPerSocketCannotLessThanZeroMsg))
	}

	return nil, aggregateObjErrors(elfMachineTemplate.GroupVersionKind().GroupKind(), elfMachineTemplate.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (v *ElfMachineTemplateValidator) ValidateDelete(ctx goctx.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
