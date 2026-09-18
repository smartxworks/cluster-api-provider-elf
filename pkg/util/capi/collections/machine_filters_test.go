/*
Copyright 2020 The Kubernetes Authors.

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
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
)

func falseFilter(_ *clusterv1.Machine) bool {
	return false
}

func trueFilter(_ *clusterv1.Machine) bool {
	return true
}

func TestNot(t *testing.T) {
	t.Run("returns false given a machine filter that returns true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		m := &clusterv1.Machine{}
		g.Expect(Not(trueFilter)(m)).To(gomega.BeFalse())
	})
	t.Run("returns true given a machine filter that returns false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		m := &clusterv1.Machine{}
		g.Expect(Not(falseFilter)(m)).To(gomega.BeTrue())
	})
}

func TestAnd(t *testing.T) {
	t.Run("returns true if both given machine filters return true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		m := &clusterv1.Machine{}
		g.Expect(And(trueFilter, trueFilter)(m)).To(gomega.BeTrue())
	})
	t.Run("returns false if either given machine filter returns false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		m := &clusterv1.Machine{}
		g.Expect(And(trueFilter, falseFilter)(m)).To(gomega.BeFalse())
	})
}

func TestOr(t *testing.T) {
	t.Run("returns true if either given machine filters return true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		m := &clusterv1.Machine{}
		g.Expect(Or(trueFilter, falseFilter)(m)).To(gomega.BeTrue())
	})
	t.Run("returns false if both given machine filter returns false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		m := &clusterv1.Machine{}
		g.Expect(Or(falseFilter, falseFilter)(m)).To(gomega.BeFalse())
	})
}

func TestWithVersion(t *testing.T) {
	t.Run("nil machine returns false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(WithVersion()(nil)).To(gomega.BeFalse())
	})

	t.Run("nil machine.Spec.Version returns false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		machine := &clusterv1.Machine{
			Spec: clusterv1.MachineSpec{
				Version: nil,
			},
		}
		g.Expect(WithVersion()(machine)).To(gomega.BeFalse())
	})

	t.Run("empty machine.Spec.Version returns false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		machine := &clusterv1.Machine{
			Spec: clusterv1.MachineSpec{
				Version: ptr.To(""),
			},
		}
		g.Expect(WithVersion()(machine)).To(gomega.BeFalse())
	})

	t.Run("invalid machine.Spec.Version returns false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		machine := &clusterv1.Machine{
			Spec: clusterv1.MachineSpec{
				Version: ptr.To("1..20"),
			},
		}
		g.Expect(WithVersion()(machine)).To(gomega.BeFalse())
	})

	t.Run("valid machine.Spec.Version returns true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		machine := &clusterv1.Machine{
			Spec: clusterv1.MachineSpec{
				Version: ptr.To("1.20"),
			},
		}
		g.Expect(WithVersion()(machine)).To(gomega.BeTrue())
	})
}
