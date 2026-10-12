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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSecretCachingWithRealCache(t *testing.T) {
	t.Run("When getting an uncached Secret outside the default namespace should fall back to the API server", func(t *testing.T) {
		g := gomega.NewWithT(t)
		secret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "outside-default",
				Name:      "bootstrap-data",
			},
			Data: map[string][]byte{"value": []byte("from-api-server")},
		}
		apiServer := newSecretTestAPIServer(t, secret)

		scheme := runtime.NewScheme()
		g.Expect(corev1.AddToScheme(scheme)).To(gomega.Succeed())
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
		mapper.Add(corev1.SchemeGroupVersion.WithKind("Secret"), meta.RESTScopeNamespace)
		mapper.Add(corev1.SchemeGroupVersion.WithKind("SecretList"), meta.RESTScopeNamespace)
		config := &rest.Config{Host: apiServer.URL}

		opts := Options{}
		g.Expect(ConfigureSecretCaching(&opts)).To(gomega.Succeed())
		realCache, err := cache.New(config, cache.Options{
			Scheme:   scheme,
			Mapper:   mapper,
			ByObject: opts.Cache.ByObject,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		ctx, cancel := context.WithCancel(t.Context())
		_, err = realCache.GetInformer(ctx, &corev1.Secret{}, cache.BlockUntilSynced(false))
		g.Expect(err).NotTo(gomega.HaveOccurred())

		cacheStopped := make(chan error, 1)
		go func() {
			cacheStopped <- realCache.Start(ctx)
		}()
		t.Cleanup(func() {
			cancel()
			g.Expect(<-cacheStopped).To(gomega.Succeed())
		})
		g.Expect(realCache.WaitForCacheSync(ctx)).To(gomega.BeTrue())

		cachedSecret := &corev1.Secret{}
		err = realCache.Get(ctx, client.ObjectKeyFromObject(secret), cachedSecret)
		g.Expect(apierrors.IsNotFound(err)).To(gomega.BeTrue(), "filtered cache should return NotFound, got %v", err)

		k8sClient, err := opts.NewClient(config, client.Options{
			Scheme: scheme,
			Mapper: mapper,
			Cache:  &client.CacheOptions{Reader: realCache},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		got := &corev1.Secret{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), got)).To(gomega.Succeed())
		g.Expect(got.Data).To(gomega.HaveKeyWithValue("value", []byte("from-api-server")))
	})
}

func newSecretTestAPIServer(t *testing.T, secret *corev1.Secret) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		isSecretCollection := r.URL.Path == "/api/v1/secrets" || r.URL.Path == "/api/v1/namespaces/default/secrets"
		switch {
		case r.Method == http.MethodGet && isSecretCollection && r.URL.Query().Get("watch") != "true":
			list := &corev1.SecretList{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "SecretList"},
				ListMeta: metav1.ListMeta{ResourceVersion: "1"},
			}
			if err := json.NewEncoder(w).Encode(list); err != nil {
				t.Errorf("failed to encode SecretList: %v", err)
			}
		case r.Method == http.MethodGet && isSecretCollection && r.URL.Query().Get("watch") == "true":
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/namespaces/"+secret.Namespace+"/secrets/"+secret.Name:
			if err := json.NewEncoder(w).Encode(secret); err != nil {
				t.Errorf("failed to encode Secret: %v", err)
			}
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)

	return server
}
