/*
Copyright 2018 The Kubernetes Authors.

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

package capi

import (
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	g := NewWithT(t)
	g.Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
	return scheme
}

func TestGetOwnerClusterSuccessByName(t *testing.T) {
	g := NewWithT(t)

	myCluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: metav1.NamespaceDefault,
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(newScheme(t)).
		WithObjects(myCluster).
		Build()

	objm := metav1.ObjectMeta{
		OwnerReferences: []metav1.OwnerReference{
			{
				Kind:       "Cluster",
				APIVersion: clusterv1.GroupVersion.String(),
				Name:       "my-cluster",
			},
		},
		Namespace: metav1.NamespaceDefault,
		Name:      "my-resource-owned-by-cluster",
	}
	cluster, err := GetOwnerCluster(t.Context(), c, objm)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cluster).NotTo(BeNil())

	// Make sure API version does not matter
	objm.OwnerReferences[0].APIVersion = "cluster.x-k8s.io/v1alpha1234"
	cluster, err = GetOwnerCluster(t.Context(), c, objm)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cluster).NotTo(BeNil())
}
