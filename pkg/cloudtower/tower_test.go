package cloudtower

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega"
	towerclient "github.com/smartxworks/cloudtower-go-sdk/v2/client"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func resetTowerCache(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		ClearClientCache()
	})
}

func TestClearClientCache(t *testing.T) {
	t.Run("should clear client cache", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		clientKey := getTowerClientCacheKey(&infrav1.TowerClientConfig{})
		cachedClient := &towerclient.Cloudtower{}
		cacheMap.Store(clientKey, &cacheItem{TowerClient: cachedClient})

		ClearClientCache()

		isEmpty := true
		cacheMap.Range(func(key, value any) bool {
			isEmpty = true
			return false
		})
		g.Expect(isEmpty).To(gomega.BeTrue())
	})
}

func TestNewTowerClient(t *testing.T) {
	t.Run("returns error when secret does not exist", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		resetTowerCache(t)

		k8sClient := fake.NewClientBuilder().Build()
		secretKey := apitypes.NamespacedName{Namespace: "sks-system", Name: "missing-secret"}

		client, err := NewTowerClient(t.Context(), k8sClient, secretKey)
		g.Expect(client).To(gomega.BeNil())
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("failed to get tower secret sks-system/missing-secret"))
	})

	t.Run("should cache a secret client by secret key and reuse it while the secret is unchanged", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		resetTowerCache(t)

		secretKey := apitypes.NamespacedName{Namespace: "sks-system", Name: "cloudtower-server"}
		secret := newTowerSecret(secretKey.Name, secretKey.Namespace, "127.0.0.1", "tower", "tower")
		k8sClient := fake.NewClientBuilder().WithObjects(secret).Build()
		cacheKey := getTowerSecretCacheKey(secretKey)
		cachedClient := &towerclient.Cloudtower{}
		cacheMap.Store(cacheKey, &cacheItem{
			LastUsedTime: time.Now(),
			TowerClient:  cachedClient,
			TowerConfig:  &infrav1.TowerClientConfig{Server: "127.0.0.1", Username: "tower", Password: "tower", AuthMode: "LOCAL", SkipTLSVerify: true},
		})

		client, err := NewTowerClient(t.Context(), k8sClient, secretKey)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(client).To(gomega.Equal(cachedClient))

		item, ok := loadCacheItem(cacheKey)
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(item.TowerClient).To(gomega.Equal(cachedClient))
	})

	t.Run("should drop the cached client when the secret config changes", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		resetTowerCache(t)

		secretKey := apitypes.NamespacedName{Namespace: "sks-system", Name: "cloudtower-server"}
		secret := newTowerSecret(secretKey.Name, secretKey.Namespace, "127.0.0.1", "tower", "new-password")
		k8sClient := fake.NewClientBuilder().WithObjects(secret).Build()
		cacheKey := getTowerSecretCacheKey(secretKey)
		cacheMap.Store(cacheKey, &cacheItem{
			LastUsedTime: time.Now(),
			TowerClient:  &towerclient.Cloudtower{},
			TowerConfig:  &infrav1.TowerClientConfig{Server: "127.0.0.1", Username: "tower", Password: "old-password", AuthMode: "LOCAL", SkipTLSVerify: true},
		})

		_, err := NewTowerClient(t.Context(), k8sClient, secretKey)
		g.Expect(err).To(gomega.HaveOccurred())

		_, ok := cacheMap.Load(cacheKey)
		g.Expect(ok).To(gomega.BeFalse())
	})
}

func TestNewTowerClientWithConfig(t *testing.T) {
	t.Run("should get cached client and clear inactive sessions", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		resetTowerCache(t)

		lastGCTime = time.Now().Add(-gcMinInterval - time.Second)
		config := infrav1.TowerClientConfig{Server: "127.0.0.1", Username: "tower", Password: "tower"}
		inactiveConfig := config
		inactiveConfig.Username = "inactive"
		invalidConfig := config
		invalidConfig.Username = "invalid"

		clientKey := getTowerClientCacheKey(&config)
		cachedClient := &towerclient.Cloudtower{}
		cacheMap.Store(clientKey, &cacheItem{TowerClient: cachedClient})
		inactiveClientKey := getTowerClientCacheKey(&inactiveConfig)
		cacheMap.Store(inactiveClientKey, &cacheItem{TowerClient: &towerclient.Cloudtower{}, LastUsedTime: time.Now().Add(-cacheIdleTime - time.Second)})
		inactiveSecretKey := getTowerSecretCacheKey(apitypes.NamespacedName{Namespace: "default", Name: "inactive-secret"})
		cacheMap.Store(inactiveSecretKey, &cacheItem{TowerConfig: &infrav1.TowerClientConfig{}, LastUsedTime: time.Now().Add(-cacheIdleTime - time.Second)})

		client, err := NewTowerClientWithConfig(t.Context(), config)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(client).To(gomega.Equal(cachedClient))

		_, ok := cacheMap.Load(clientKey)
		g.Expect(ok).To(gomega.BeTrue())
		_, ok = cacheMap.Load(inactiveClientKey)
		g.Expect(ok).To(gomega.BeFalse())
		_, ok = cacheMap.Load(inactiveSecretKey)
		g.Expect(ok).To(gomega.BeFalse())

		client, err = NewTowerClientWithConfig(t.Context(), invalidConfig)
		g.Expect(client).To(gomega.BeNil())
		g.Expect(err).To(gomega.HaveOccurred())
	})
}

func TestGetTowerClientConfig(t *testing.T) {
	t.Run("reads secret config without caching it", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		resetTowerCache(t)
		ctx := t.Context()
		secretKey := apitypes.NamespacedName{Namespace: "sks-system", Name: "cloudtower-server"}
		secret := newTowerSecret(secretKey.Name, secretKey.Namespace, "10.255.0.4", "system-service", "K5yt3hcjtUE4Teqe")
		k8sClient := fake.NewClientBuilder().WithObjects(secret).Build()

		config, err := GetTowerClientConfig(ctx, k8sClient, secretKey)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(config).To(gomega.Equal(infrav1.TowerClientConfig{
			Server:        "10.255.0.4",
			Username:      "system-service",
			Password:      "K5yt3hcjtUE4Teqe",
			AuthMode:      "LOCAL",
			SkipTLSVerify: true,
		}))

		cacheKey := getTowerSecretCacheKey(secretKey)
		_, ok := cacheMap.Load(cacheKey)
		g.Expect(ok).To(gomega.BeFalse())

		_, err = GetTowerClientConfig(ctx, fake.NewClientBuilder().Build(), secretKey)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("failed to get tower secret sks-system/cloudtower-server"))
	})

	t.Run("uses namespace and name to read secret configs", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		resetTowerCache(t)
		ctx := t.Context()
		secretA := newTowerSecret("cloudtower-server", "namespace-a", "10.255.0.4", "user-a", "password-a")
		secretB := newTowerSecret("cloudtower-server", "namespace-b", "10.255.0.5", "user-b", "password-b")
		k8sClient := fake.NewClientBuilder().WithObjects(secretA, secretB).Build()

		configA, err := GetTowerClientConfig(ctx, k8sClient, apitypes.NamespacedName{Namespace: "namespace-a", Name: "cloudtower-server"})
		g.Expect(err).ToNot(gomega.HaveOccurred())
		configB, err := GetTowerClientConfig(ctx, k8sClient, apitypes.NamespacedName{Namespace: "namespace-b", Name: "cloudtower-server"})
		g.Expect(err).ToNot(gomega.HaveOccurred())

		g.Expect(configA.Server).To(gomega.Equal("10.255.0.4"))
		g.Expect(configA.Username).To(gomega.Equal("user-a"))
		g.Expect(configB.Server).To(gomega.Equal("10.255.0.5"))
		g.Expect(configB.Username).To(gomega.Equal("user-b"))
	})

	t.Run("returns contextual errors for missing secret", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		resetTowerCache(t)

		_, err := GetTowerClientConfig(t.Context(), fake.NewClientBuilder().Build(), apitypes.NamespacedName{Namespace: "sks-system", Name: "missing"})
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("failed to get tower secret sks-system/missing"))
	})
}

func TestParseTowerClientConfigFromSecret(t *testing.T) {
	t.Run("parses yaml and json cloudtower config", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		yamlConfig, err := ParseTowerClientConfigFromSecret(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "yaml-secret", Namespace: "sks-system"},
			Data: map[string][]byte{
				"cloudtower.yaml": []byte("authMode: LOCAL\npassword: yaml-password\nserver: 10.255.0.4\nskipTLSVerify: true\nusername: yaml-user\n"),
			},
		})
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(yamlConfig).To(gomega.Equal(infrav1.TowerClientConfig{Server: "10.255.0.4", Username: "yaml-user", Password: "yaml-password", AuthMode: "LOCAL", SkipTLSVerify: true}))

		jsonConfig, err := ParseTowerClientConfigFromSecret(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "json-secret", Namespace: "sks-system"},
			Data: map[string][]byte{
				"cloudtower.yaml": []byte(`{"authMode":"LDAP","password":"json-password","server":"10.255.0.5","skipTLSVerify":false,"username":"json-user"}`),
			},
		})
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(jsonConfig).To(gomega.Equal(infrav1.TowerClientConfig{Server: "10.255.0.5", Username: "json-user", Password: "json-password", AuthMode: "LDAP"}))
	})

	t.Run("rejects missing cloudtower yaml key", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		_, err := ParseTowerClientConfigFromSecret(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-secret", Namespace: "sks-system"},
			Data:       map[string][]byte{"other.yaml": []byte("server: 10.255.0.4\n")},
		})
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("tower secret sks-system/invalid-secret missing cloudtower.yaml"))
	})

	t.Run("rejects malformed cloudtower yaml", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		_, err := ParseTowerClientConfigFromSecret(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "malformed-secret", Namespace: "sks-system"},
			Data:       map[string][]byte{"cloudtower.yaml": []byte("server: [unterminated\n")},
		})
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("failed to decode cloudtower.yaml in tower secret sks-system/malformed-secret"))
	})
}

func TestTowerCacheKeys(t *testing.T) {
	t.Run("client cache key hashes password without exposing plaintext", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		config := &infrav1.TowerClientConfig{Server: "10.255.0.4", Username: "system-service", Password: "super-secret", AuthMode: "LOCAL", SkipTLSVerify: true}
		key := getTowerClientCacheKey(config)

		g.Expect(key).To(gomega.HavePrefix("tower-client:"))
		g.Expect(key).ToNot(gomega.ContainSubstring("super-secret"))
		g.Expect(key).To(gomega.ContainSubstring("system-service"))
		g.Expect(config.Password).To(gomega.Equal("super-secret"))
	})

	t.Run("client cache key changes when password changes", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		base := &infrav1.TowerClientConfig{Server: "10.255.0.4", Username: "system-service", Password: "password-a", AuthMode: "LOCAL"}
		changedPassword := *base
		changedPassword.Password = "password-b"

		g.Expect(getTowerClientCacheKey(base)).ToNot(gomega.Equal(getTowerClientCacheKey(&changedPassword)))
	})

	t.Run("secret cache key includes namespace and name", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		g.Expect(getTowerSecretCacheKey(apitypes.NamespacedName{Namespace: "sks-system", Name: "cloudtower-server"})).To(gomega.Equal("tower-secret:sks-system/cloudtower-server"))
	})
}

func TestWithHeaderRoundTripper(t *testing.T) {
	t.Run("uses default transport when nil delegate is provided", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		rt := NewWithHeaderRoundTripper(nil)

		g.Expect(rt.rt).ToNot(gomega.BeNil())
	})

	t.Run("does not clone or mutate headers when no configured headers exist", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		called := false
		req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		req.Header.Set("Existing", "value")

		rt := NewWithHeaderRoundTripper(roundTripFunc(func(got *http.Request) (*http.Response, error) {
			called = true
			g.Expect(got).To(gomega.BeIdenticalTo(req))
			g.Expect(got.Header.Get("Existing")).To(gomega.Equal("value"))
			return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		}))

		resp, err := rt.RoundTrip(req)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()
		g.Expect(resp.StatusCode).To(gomega.Equal(http.StatusNoContent))
		g.Expect(called).To(gomega.BeTrue())
	})

	t.Run("adds configured headers to a cloned request", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		req.Header.Set("Existing", "value")

		rt := NewWithHeaderRoundTripper(roundTripFunc(func(got *http.Request) (*http.Response, error) {
			g.Expect(got).ToNot(gomega.BeIdenticalTo(req))
			g.Expect(got.Header.Get("Existing")).To(gomega.Equal("value"))
			g.Expect(got.Header.Values("X-Test")).To(gomega.Equal([]string{"a", "b"}))
			return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		}))
		rt.Set("X-Test", "a")
		rt.Add("X-Test", "b")

		resp, err := rt.RoundTrip(req)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()
		g.Expect(resp.StatusCode).To(gomega.Equal(http.StatusAccepted))
		g.Expect(req.Header.Values("X-Test")).To(gomega.BeEmpty())
	})
}

func newTowerSecret(name, namespace, server, username, password string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data: map[string][]byte{
			"cloudtower.yaml": []byte("authMode: LOCAL\npassword: " + password + "\nserver: " + server + "\nskipTLSVerify: true\nusername: " + username + "\n"),
		},
	}
}
