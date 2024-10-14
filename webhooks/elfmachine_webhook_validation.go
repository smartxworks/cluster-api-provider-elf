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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

// Error messages.
const (
	canOnlyModifiedThroughElfMachineTemplate = "virtual machine resources can only be modified through ElfMachineTemplate %s"
)

func (v *ElfMachineValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&infrav1.ElfMachine{}).
		WithValidator(v).
		Complete()
}

//+kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-elfmachine,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=elfmachines,verbs=create;update,versions=v1beta1,name=validation.elfmachine.infrastructure.x-k8s.io,admissionReviewVersions=v1

// ElfMachineValidator implements a validation webhook for ElfMachine.
type ElfMachineValidator struct {
	client.Client
}

var _ webhook.CustomValidator = &ElfMachineTemplateValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (v *ElfMachineValidator) ValidateCreate(ctx goctx.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (v *ElfMachineValidator) ValidateUpdate(ctx goctx.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldElfMachine, ok := oldObj.(*infrav1.ElfMachine) //nolint:forcetypeassert
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected an ElfMachine but got a %T", oldObj))
	}
	elfMachine, ok := newObj.(*infrav1.ElfMachine) //nolint:forcetypeassert
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected an ElfMachine but got a %T", newObj))
	}

	var allErrs field.ErrorList

	if elfMachine.Spec.DiskGiB < oldElfMachine.Spec.DiskGiB {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "diskGiB"), elfMachine.Spec.DiskGiB, diskCapacityCanOnlyBeExpanded))
	}

	return nil, aggregateObjErrors(elfMachine.GroupVersionKind().GroupKind(), elfMachine.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (v *ElfMachineValidator) ValidateDelete(ctx goctx.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
