/*
Copyright 2026.

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

package manager

import (
	"reflect"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestConfigureSecretCaching(t *testing.T) {
	t.Run("When a Secret is in the default namespace should cache it regardless of labels", func(t *testing.T) {
		g := gomega.NewWithT(t)
		opts := Options{}

		g.Expect(ConfigureSecretCaching(&opts)).To(gomega.Succeed())

		secretCache, ok := cacheOptionsFor(&opts, &corev1.Secret{})
		g.Expect(ok).To(gomega.BeTrue())
		defaultNamespaceCache, ok := secretCache.Namespaces[metav1.NamespaceDefault]
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(defaultNamespaceCache.LabelSelector.Matches(labels.Set{"app": "unrelated"})).To(gomega.BeTrue())
		g.Expect(opts.Client.Cache).NotTo(gomega.BeNil())
		g.Expect(containsObjectType(opts.Client.Cache.DisableFor, &corev1.Secret{})).To(gomega.BeFalse())
	})

	t.Run("When a Secret is outside the default namespace should cache it only with the cluster-name label", func(t *testing.T) {
		g := gomega.NewWithT(t)
		opts := Options{}

		g.Expect(ConfigureSecretCaching(&opts)).To(gomega.Succeed())

		secretCache, ok := cacheOptionsFor(&opts, &corev1.Secret{})
		g.Expect(ok).To(gomega.BeTrue())
		otherNamespaceCache, ok := secretCache.Namespaces[cache.AllNamespaces]
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(otherNamespaceCache.LabelSelector.Matches(labels.Set{clusterv1.ClusterNameLabel: "cluster-1"})).To(gomega.BeTrue())
		g.Expect(otherNamespaceCache.LabelSelector.Matches(labels.Set{"app": "unrelated"})).To(gomega.BeFalse())
	})

	t.Run("When other cache options exist should preserve them", func(t *testing.T) {
		g := gomega.NewWithT(t)
		configMapCache := cache.ByObject{Label: labels.Everything()}
		opts := Options{}
		opts.Cache.ByObject = map[client.Object]cache.ByObject{
			&corev1.ConfigMap{}: configMapCache,
		}
		opts.Client.Cache = &client.CacheOptions{
			DisableFor:   []client.Object{&corev1.ConfigMap{}},
			Unstructured: true,
		}

		g.Expect(ConfigureSecretCaching(&opts)).To(gomega.Succeed())

		gotConfigMapCache, ok := cacheOptionsFor(&opts, &corev1.ConfigMap{})
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(gotConfigMapCache.Label.String()).To(gomega.Equal(configMapCache.Label.String()))
		g.Expect(containsObjectType(opts.Client.Cache.DisableFor, &corev1.ConfigMap{})).To(gomega.BeTrue())
		g.Expect(opts.Client.Cache.Unstructured).To(gomega.BeTrue())
	})

	t.Run("When getting a Secret in the default namespace should read it from the cache", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cachedSecret := newTestSecret(metav1.NamespaceDefault, "credentials", "cache", nil)
		apiSecret := newTestSecret(metav1.NamespaceDefault, "credentials", "api-server", nil)
		k8sClient := newTestManagerClient(t, []*corev1.Secret{cachedSecret}, []*corev1.Secret{apiSecret})

		got := &corev1.Secret{}
		g.Expect(k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cachedSecret), got)).To(gomega.Succeed())
		g.Expect(got.Data).To(gomega.HaveKeyWithValue("source", []byte("cache")))
	})

	t.Run("When getting a CAPI Secret outside the default namespace should read it from the cache", func(t *testing.T) {
		g := gomega.NewWithT(t)
		secretLabels := map[string]string{clusterv1.ClusterNameLabel: "cluster-1"}
		cachedSecret := newTestSecret("tenant-1", "kubeconfig", "cache", secretLabels)
		apiSecret := newTestSecret("tenant-1", "kubeconfig", "api-server", secretLabels)
		k8sClient := newTestManagerClient(t, []*corev1.Secret{cachedSecret}, []*corev1.Secret{apiSecret})

		got := &corev1.Secret{}
		g.Expect(k8sClient.Get(t.Context(), client.ObjectKeyFromObject(cachedSecret), got)).To(gomega.Succeed())
		g.Expect(got.Data).To(gomega.HaveKeyWithValue("source", []byte("cache")))
	})

	t.Run("When getting a non-CAPI Secret outside the default namespace should read it from the API server", func(t *testing.T) {
		g := gomega.NewWithT(t)
		apiSecret := newTestSecret("tenant-1", "bootstrap-data", "api-server", nil)
		k8sClient := newTestManagerClient(t, nil, []*corev1.Secret{apiSecret})

		got := &corev1.Secret{}
		g.Expect(k8sClient.Get(t.Context(), client.ObjectKeyFromObject(apiSecret), got)).To(gomega.Succeed())
		g.Expect(got.Data).To(gomega.HaveKeyWithValue("source", []byte("api-server")))
	})

	t.Run("When listing Secrets in the default namespace should read them from the cache", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cachedSecret := newTestSecret(metav1.NamespaceDefault, "cached", "cache", nil)
		apiSecret := newTestSecret(metav1.NamespaceDefault, "live", "api-server", nil)
		k8sClient := newTestManagerClient(t, []*corev1.Secret{cachedSecret}, []*corev1.Secret{apiSecret})

		got := &corev1.SecretList{}
		g.Expect(k8sClient.List(t.Context(), got, client.InNamespace(metav1.NamespaceDefault))).To(gomega.Succeed())
		g.Expect(got.Items).To(gomega.HaveLen(1))
		g.Expect(got.Items[0].Name).To(gomega.Equal("cached"))
	})

	t.Run("When listing CAPI Secrets outside the default namespace should read them from the cache", func(t *testing.T) {
		g := gomega.NewWithT(t)
		secretLabels := map[string]string{clusterv1.ClusterNameLabel: "cluster-1"}
		cachedSecret := newTestSecret("tenant-1", "cached", "cache", secretLabels)
		apiSecret := newTestSecret("tenant-1", "live", "api-server", secretLabels)
		k8sClient := newTestManagerClient(t, []*corev1.Secret{cachedSecret}, []*corev1.Secret{apiSecret})

		got := &corev1.SecretList{}
		g.Expect(k8sClient.List(t.Context(), got,
			client.InNamespace("tenant-1"),
			client.HasLabels{clusterv1.ClusterNameLabel},
		)).To(gomega.Succeed())
		g.Expect(got.Items).To(gomega.HaveLen(1))
		g.Expect(got.Items[0].Name).To(gomega.Equal("cached"))
	})

	t.Run("When listing non-CAPI Secrets outside the default namespace should read them from the API server", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cachedSecret := newTestSecret("tenant-1", "cached", "cache", map[string]string{clusterv1.ClusterNameLabel: "cluster-1"})
		apiSecret := newTestSecret("tenant-1", "live", "api-server", nil)
		k8sClient := newTestManagerClient(t, []*corev1.Secret{cachedSecret}, []*corev1.Secret{apiSecret})

		got := &corev1.SecretList{}
		g.Expect(k8sClient.List(t.Context(), got, client.InNamespace("tenant-1"))).To(gomega.Succeed())
		g.Expect(got.Items).To(gomega.HaveLen(1))
		g.Expect(got.Items[0].Name).To(gomega.Equal("live"))
	})
}

func newTestManagerClient(t *testing.T, cachedSecrets, apiSecrets []*corev1.Secret) client.Client {
	t.Helper()
	g := gomega.NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(gomega.Succeed())

	cacheObjects := make([]client.Object, 0, len(cachedSecrets))
	for _, secret := range cachedSecrets {
		cacheObjects = append(cacheObjects, secret)
	}
	cachedClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cacheObjects...).Build()
	apiObjects := make([]client.Object, 0, len(apiSecrets))
	for _, secret := range apiSecrets {
		apiObjects = append(apiObjects, secret)
	}
	apiClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiObjects...).Build()

	opts := Options{}
	opts.Client.Cache = &client.CacheOptions{Reader: cachedClient}
	opts.NewClient = func(_ *rest.Config, clientOptions client.Options) (client.Client, error) {
		if clientOptions.Cache == nil || containsObjectType(clientOptions.Cache.DisableFor, &corev1.Secret{}) {
			return apiClient, nil
		}
		return cachedClient, nil
	}
	g.Expect(ConfigureSecretCaching(&opts)).To(gomega.Succeed())

	k8sClient, err := opts.NewClient(nil, opts.Client)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	return k8sClient
}

func newTestSecret(namespace, name, source string, objectLabels map[string]string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    objectLabels,
		},
		Data: map[string][]byte{"source": []byte(source)},
	}
}

func cacheOptionsFor(opts *Options, target client.Object) (cache.ByObject, bool) {
	for obj, options := range opts.Cache.ByObject {
		if reflect.TypeOf(obj) == reflect.TypeOf(target) {
			return options, true
		}
	}

	return cache.ByObject{}, false
}

func containsObjectType(objects []client.Object, target client.Object) bool {
	for _, obj := range objects {
		if reflect.TypeOf(obj) == reflect.TypeOf(target) {
			return true
		}
	}

	return false
}
