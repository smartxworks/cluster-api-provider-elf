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
	goctx "context"
	"encoding/json"
	"net/http"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/version"
)

func (m *ElfMachineMutation) SetupWebhookWithManager(mgr ctrl.Manager) error {
	if m.decoder == nil {
		m.decoder = admission.NewDecoder(mgr.GetScheme())
	}

	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/mutate-infrastructure-cluster-x-k8s-io-v1beta1-elfmachine", &webhook.Admission{Handler: m})
	return ctrl.NewWebhookManagedBy(mgr).
		For(&infrav1.ElfMachine{}).
		Complete()
}

//+kubebuilder:object:generate=false
//+kubebuilder:webhook:verbs=create,path=/mutate-infrastructure-cluster-x-k8s-io-v1beta1-elfmachine,mutating=true,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=elfmachines,versions=v1beta1,name=mutation.elfmachine.infrastructure.x-k8s.io,admissionReviewVersions=v1

type ElfMachineMutation struct {
	client.Client
	decoder *admission.Decoder
	logr.Logger
}

func (m *ElfMachineMutation) Handle(ctx goctx.Context, request admission.Request) admission.Response {
	var elfMachine infrav1.ElfMachine
	if err := m.decoder.Decode(request, &elfMachine); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if request.Operation == admissionv1.Create {
		version.SetCurrentCAPEVersion(&elfMachine)
	}

	if elfMachine.Spec.NumCoresPerSocket <= 0 {
		elfMachine.Spec.NumCoresPerSocket = elfMachine.Spec.NumCPUs
	}

	if marshaledElfMachine, err := json.Marshal(elfMachine); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	} else {
		return admission.PatchResponseFromRaw(request.Object.Raw, marshaledElfMachine)
	}
}

// InjectDecoder injects the decoder.
func (m *ElfMachineMutation) InjectDecoder(d *admission.Decoder) error {
	m.decoder = d
	return nil
}
