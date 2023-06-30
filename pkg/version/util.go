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

package version

import (
	"github.com/Masterminds/semver/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	annotationsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/annotations"
)

// GetCAPEVersion returns the CAPE version of the object.
func GetCAPEVersion(o metav1.Object) string {
	annotations := o.GetAnnotations()
	if annotations == nil {
		return ""
	}

	return annotations[infrav1.CAPEVersionAnnotation]
}

// SetCAPEVersion sets the CAPE version for the object.
func SetCAPEVersion(o metav1.Object, version string) {
	if !annotationsutil.HasAnnotation(o, infrav1.CAPEVersionAnnotation) {
		annotationsutil.AddAnnotations(o, map[string]string{infrav1.CAPEVersionAnnotation: version})
	}
}

// SetCurrentCAPEVersion sets the latest CAPE version for the object.
func SetCurrentCAPEVersion(o metav1.Object) {
	SetCAPEVersion(o, CAPEVersion())
}

// IsCompatiblePlacementGroup returns whether the current object can use a placement group.
func IsCompatiblePlacementGroup(o metav1.Object) bool {
	capeVersion := GetCAPEVersion(o)
	if (capeVersion == CAPEVersionLatest) ||
		(IsSemverVersion(capeVersion) && capeVersion >= CAPEVersion1_2_0) {
		return true
	}

	return false
}

// IsSemverVersion returns whether the version is an valid Semantic Version.
func IsSemverVersion(version string) bool {
	if _, err := semver.NewVersion(version); err != nil {
		return false
	}

	return true
}
