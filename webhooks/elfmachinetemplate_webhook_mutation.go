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
	"encoding/json"
	"net/http"

	"github.com/go-logr/logr"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

const (
	defaultIPPoolAPIGroup = "ipam.metal3.io"
	defaultIPPoolKind     = "IPPool"
)

func (m *ElfMachineTemplateMutation) SetupWebhookWithManager(mgr ctrl.Manager) error {
	if m.decoder == nil {
		m.decoder = admission.NewDecoder(mgr.GetScheme())
	}

	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/mutate-infrastructure-cluster-x-k8s-io-v1beta1-elfmachinetemplate", &webhook.Admission{Handler: m})
	return ctrl.NewWebhookManagedBy(mgr).
		For(&infrav1.ElfMachine{}).
		Complete()
}

//+kubebuilder:object:generate=false
//+kubebuilder:webhook:verbs=create;update,path=/mutate-infrastructure-cluster-x-k8s-io-v1beta1-elfmachinetemplate,mutating=true,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=elfmachinetemplates,versions=v1beta1,name=mutation.elfmachinetemplate.infrastructure.x-k8s.io,admissionReviewVersions=v1

type ElfMachineTemplateMutation struct {
	client.Client
	decoder *admission.Decoder
	logr.Logger
}

func (m *ElfMachineTemplateMutation) Handle(ctx goctx.Context, request admission.Request) admission.Response {
	var elfMachineTemplate infrav1.ElfMachineTemplate
	if err := m.decoder.Decode(request, &elfMachineTemplate); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	devices := elfMachineTemplate.Spec.Template.Spec.Network.Devices
	for i := 0; i < len(devices); i++ {
		for j := 0; j < len(devices[i].AddressesFromPools); j++ {
			if devices[i].AddressesFromPools[j].APIGroup == nil || *devices[i].AddressesFromPools[j].APIGroup == "" {
				devices[i].AddressesFromPools[j].APIGroup = pointer.String(defaultIPPoolAPIGroup)
			}
			if devices[i].AddressesFromPools[j].Kind == "" {
				devices[i].AddressesFromPools[j].Kind = defaultIPPoolKind
			}
		}
	}

	if marshaledElfMachineTemplate, err := json.Marshal(elfMachineTemplate); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	} else {
		return admission.PatchResponseFromRaw(request.Object.Raw, marshaledElfMachineTemplate)
	}
}

// InjectDecoder injects the decoder.
func (m *ElfMachineTemplateMutation) InjectDecoder(d *admission.Decoder) error {
	m.decoder = d
	return nil
}
