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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

func GetControlPlaneLabel(o metav1.Object) string {
	return GetLabelValue(o, clusterv1.MachineControlPlaneNameLabel)
}

func GetDeploymentNameLabel(o metav1.Object) string {
	return GetLabelValue(o, clusterv1.MachineDeploymentLabelName)
}

func GetClusterNameLabel(o metav1.Object) string {
	return GetLabelValue(o, clusterv1.ClusterLabelName)
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
