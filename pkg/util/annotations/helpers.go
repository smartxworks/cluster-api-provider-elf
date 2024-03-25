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

package annotations

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/annotations"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

// HasAnnotation returns true if the object has the specified annotation.
func HasAnnotation(o metav1.Object, annotationKey string) bool {
	annotations := o.GetAnnotations()
	if annotations == nil {
		return false
	}

	_, ok := annotations[annotationKey]
	return ok
}

func GetPlacementGroupName(o metav1.Object) string {
	annotations := o.GetAnnotations()
	if annotations == nil {
		return ""
	}

	return annotations[infrav1.PlacementGroupNameAnnotation]
}

func GetCreatedBy(o metav1.Object) string {
	annotations := o.GetAnnotations()
	if annotations == nil {
		return ""
	}

	return annotations[infrav1.CreatedByAnnotation]
}

func GetTemplateClonedFromName(o metav1.Object) string {
	annotations := o.GetAnnotations()
	if annotations == nil {
		return ""
	}

	return annotations[clusterv1.TemplateClonedFromNameAnnotation]
}

// AddAnnotations sets the desired annotations on the object and returns true if the annotations have changed.
func AddAnnotations(o metav1.Object, desired map[string]string) bool {
	return annotations.AddAnnotations(o, desired)
}

// RemoveAnnotation deletes the desired annotation on the object.
func RemoveAnnotation(o metav1.Object, annotation string) {
	annotations := o.GetAnnotations()
	if annotations == nil {
		return
	}
	delete(annotations, annotation)
	o.SetAnnotations(annotations)
}
