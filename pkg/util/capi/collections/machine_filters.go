/*
Copyright 2019 The Kubernetes Authors.

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

package collections

import (
	"github.com/blang/semver/v4"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
)

// Copied from https://github.com/kubernetes-sigs/cluster-api/blob/release-1.10/util/collections/machine_filters.go.

// Func is the functon definition for a filter.
type Func func(machine *clusterv1.Machine) bool

// And returns a filter that returns true if all of the given filters returns true.
func And(filters ...Func) Func {
	return func(machine *clusterv1.Machine) bool {
		for _, f := range filters {
			if !f(machine) {
				return false
			}
		}
		return true
	}
}

// Or returns a filter that returns true if any of the given filters returns true.
func Or(filters ...Func) Func {
	return func(machine *clusterv1.Machine) bool {
		for _, f := range filters {
			if f(machine) {
				return true
			}
		}
		return false
	}
}

// Not returns a filter that returns the opposite of the given filter.
func Not(mf Func) Func {
	return func(machine *clusterv1.Machine) bool {
		return !mf(machine)
	}
}

// WithVersion returns a filter to find machine that have a non empty and valid version.
func WithVersion() Func {
	return func(machine *clusterv1.Machine) bool {
		if machine == nil {
			return false
		}
		if machine.Spec.Version == nil {
			return false
		}
		if _, err := semver.ParseTolerant(*machine.Spec.Version); err != nil {
			return false
		}
		return true
	}
}
