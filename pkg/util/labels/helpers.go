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

package labels

import (
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

const (
	// ClusterAutoscalerCAPIGPULabel is the label added to nodes with GPU resource, which will be used by clusterAutoscaler-CAPI.
	// ref: https://github.com/kubernetes/autoscaler/blob/master/cluster-autoscaler/cloudprovider/clusterapi/README.md#special-note-on-gpu-instances
	ClusterAutoscalerCAPIGPULabel = "cluster-api/accelerator"
)

var (
	noLabelCharReg = regexp.MustCompile(`[^-a-zA-Z0-9-.]`)
)

func HasLabel(o metav1.Object, label string) bool {
	labels := o.GetLabels()
	if labels == nil {
		return false
	}

	_, ok := labels[label]

	return ok
}

func GetHostServerIDLabel(o metav1.Object) string {
	return GetLabelValue(o, infrav1.HostServerIDLabel)
}

func GetHostServerNameLabel(o metav1.Object) string {
	return GetLabelValue(o, infrav1.HostServerNameLabel)
}

func GetZoneIDLabel(o metav1.Object) string {
	return GetLabelValue(o, infrav1.ZoneIDLabel)
}

func GetZoneTypeLabel(o metav1.Object) string {
	return GetLabelValue(o, infrav1.ZoneTypeLabel)
}

func GetTowerVMIDLabel(o metav1.Object) string {
	return GetLabelValue(o, infrav1.TowerVMIDLabel)
}

func GetClusterAutoscalerCAPIGPULabel(o metav1.Object) string {
	return GetLabelValue(o, ClusterAutoscalerCAPIGPULabel)
}

func GetNodeGroupLabel(o metav1.Object) string {
	return GetLabelValue(o, infrav1.NodeGroupLabel)
}

func GetClusterNameLabelLabel(o metav1.Object) string {
	return GetLabelValue(o, clusterv1.ClusterNameLabel)
}

func GetDeploymentNameLabel(o metav1.Object) string {
	return GetLabelValue(o, clusterv1.MachineDeploymentNameLabel)
}

func GetClusterNameLabel(o metav1.Object) string {
	return GetLabelValue(o, clusterv1.ClusterNameLabel)
}

func GetLabelValue(o metav1.Object, label string) string {
	labels := o.GetLabels()
	if labels == nil {
		return ""
	}

	val, ok := labels[label]
	if !ok {
		return ""
	}

	return val
}

func SetLabelValue(o metav1.Object, label string, value string) {
	labels := o.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[label] = value
	o.SetLabels(labels)
}

// ConvertToLabelValue converts a string to a valid label value.
// A valid label value must be an empty string or consist of alphanumeric characters, '-', '_' or '.'.
func ConvertToLabelValue(str string) string {
	result := noLabelCharReg.ReplaceAll([]byte(str), []byte("-"))

	if len(result) > validation.LabelValueMaxLength {
		return string(result[:validation.LabelValueMaxLength])
	}

	for len(result) > 0 && (result[0] == '-' || result[0] == '_' || result[0] == '.') {
		result = result[1:]
	}
	for len(result) > 0 && (result[len(result)-1] == '-' || result[len(result)-1] == '_' || result[len(result)-1] == '.') {
		result = result[:len(result)-1]
	}

	return string(result)
}
