/*
Copyright 2022.

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

package cloudtower

import (
	"bytes"
	goctx "context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/go-logr/logr"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"github.com/pkg/errors"
	towerclient "github.com/smartxworks/cloudtower-go-sdk/v2/client"
	"github.com/smartxworks/cloudtower-go-sdk/v2/client/user"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	"golang.org/x/sync/singleflight"
	corev1 "k8s.io/api/core/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

var lastGCTime = time.Now()
var gcMinInterval = 10 * time.Minute
var cacheIdleTime = 15 * time.Minute

// global cache map against cache keys.
// It stores Tower clients and, for clients created from a Secret, the config
// they were created from so the cached client can be invalidated when the
// cloudtower.yaml in the Secret changes.
var cacheMap sync.Map

var towerSecretConfigGroup singleflight.Group

type cacheItem struct {
	LastUsedTime time.Time
	TowerClient  *towerclient.Cloudtower
	TowerConfig  *infrav1.TowerClientConfig
}

// NewTowerClient gets a cached client or creates a new one if one does not
// already exist. The client is created from the cloudtower.yaml in the Secret
// referenced by tower.SecretRef. Clients are cached by the Secret reference so
// a changed cloudtower.yaml invalidates them.
func NewTowerClient(ctx goctx.Context, k8sClient client.Client, secretKey apitypes.NamespacedName) (*towerclient.Cloudtower, error) {
	clientConfig, err := GetTowerClientConfig(ctx, k8sClient, secretKey)
	if err != nil {
		return nil, err
	}

	return getOrCreateTowerClient(ctx, getTowerSecretCacheKey(secretKey), clientConfig, true)
}

// NewTowerClientWithConfig gets a cached client or creates a new one if one
// does not already exist. The client is created from the given inline
// TowerClientConfig, and clients are cached by the config content.
func NewTowerClientWithConfig(ctx goctx.Context, clientConfig infrav1.TowerClientConfig) (*towerclient.Cloudtower, error) {
	return getOrCreateTowerClient(ctx, getTowerClientCacheKey(&clientConfig), clientConfig, false)
}

// getOrCreateTowerClient returns the cached client for clientKey if it is still
// valid, otherwise it creates, caches and returns a new one. Clients created
// from an inline TowerClientConfig are cached by the config content, so a cache
// hit is always valid. Clients created from a Secret are cached by the Secret
// reference and must be dropped when the config it was created from has
// changed, e.g. because the cloudtower.yaml in the Secret was updated.
func getOrCreateTowerClient(ctx goctx.Context, clientKey string, clientConfig infrav1.TowerClientConfig, fromSecret bool) (*towerclient.Cloudtower, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("client").WithValues("server", clientConfig.Server, "username", clientConfig.Username, "source", clientConfig.AuthMode)

	defer func() {
		if lastGCTime.Add(gcMinInterval).Before(time.Now()) {
			cleanupCache(logger)
		}
	}()

	if item, ok := loadCacheItem(clientKey); ok && item.TowerClient != nil {
		if !fromSecret || reflect.DeepEqual(item.TowerConfig, &clientConfig) {
			logger.V(3).Info("found active cached tower client")

			return item.TowerClient, nil
		}

		logger.V(1).Info("tower secret config changed, removing stale cached tower client")
		cacheMap.Delete(clientKey)
	}

	client, err := createTowerClient(httptransport.TLSClientOptions{
		InsecureSkipVerify: clientConfig.SkipTLSVerify,
	}, towerclient.ClientConfig{
		Host:     clientConfig.Server,
		BasePath: "/v2/api",
		Schemes:  []string{"https"},
	}, towerclient.UserConfig{
		Name:     clientConfig.Username,
		Password: clientConfig.Password,
		Source:   models.UserSource(clientConfig.AuthMode),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create tower client")
	}

	// Cache the client alongside the config it was created from so a later
	// change to the Secret can be detected.
	cacheMap.Store(clientKey, &cacheItem{LastUsedTime: time.Now(), TowerClient: client, TowerConfig: &clientConfig})
	logger.V(3).Info("cached tower client")

	return client, nil
}

func createTowerClient(tlsOpts httptransport.TLSClientOptions, clientConfig towerclient.ClientConfig, userConfig towerclient.UserConfig) (*towerclient.Cloudtower, error) {
	transport := httptransport.New(clientConfig.Host, clientConfig.BasePath, clientConfig.Schemes)
	roundTripper, err := httptransport.TLSTransport(tlsOpts)
	if err != nil {
		return nil, err
	}

	// For Arcfra vendor, we need to bypass the whitelist for AOC(CloudTower)
	rtWithHeader := NewWithHeaderRoundTripper(roundTripper)
	rtWithHeader.Set("x-bypass-whitelist", "true") //nolint:canonicalheader
	transport.Transport = rtWithHeader

	client := towerclient.New(transport, strfmt.Default)
	params := user.NewLoginParams()
	params.WithTimeout(10 * time.Second)
	params.RequestBody = &models.LoginInput{
		Username: &userConfig.Name,
		Password: &userConfig.Password,
		Source:   userConfig.Source.Pointer(),
	}
	resp, err := client.User.Login(params)
	if err != nil {
		return nil, err
	}
	transport.DefaultAuthentication = httptransport.APIKeyAuth("Authorization", "header", *resp.Payload.Data.Token)
	return client, nil
}

func cleanupCache(logger logr.Logger) {
	cacheMap.Range(func(key interface{}, value interface{}) bool {
		item := value.(*cacheItem)
		if item.LastUsedTime.Add(cacheIdleTime).Before(time.Now()) {
			cacheMap.Delete(key)
			logger.V(3).Info(fmt.Sprintf("delete inactive tower cache %s from cacheMap", key))
		}

		return true
	})

	lastGCTime = time.Now()
}

// ClearClientCache removes all cached Tower clients.
func ClearClientCache() {
	cacheMap = sync.Map{}
	lastGCTime = time.Now()
}

// GetTowerClientConfig returns the TowerClientConfig from either the inline
// tower config or the referenced Secret. Configs parsed from Secrets are
// deliberately not cached so changes to the cloudtower.yaml are always picked
// up on the next call.
func GetTowerClientConfig(ctx goctx.Context, k8sClient client.Client, secretKey apitypes.NamespacedName) (infrav1.TowerClientConfig, error) {
	value, err, _ := towerSecretConfigGroup.Do(secretKey.String(), func() (interface{}, error) {
		var secret corev1.Secret
		if err := k8sClient.Get(ctx, secretKey, &secret); err != nil {
			return nil, errors.Wrapf(err, "failed to get tower secret %s", secretKey.String())
		}

		return ParseTowerClientConfigFromSecret(&secret)
	})
	if err != nil {
		return infrav1.TowerClientConfig{}, err
	}

	return value.(infrav1.TowerClientConfig), nil
}

func loadCacheItem(cacheKey string) (*cacheItem, bool) {
	value, ok := cacheMap.Load(cacheKey)
	if !ok {
		return nil, false
	}

	item := value.(*cacheItem)
	item.LastUsedTime = time.Now()

	return item, true
}

func ParseTowerClientConfigFromSecret(secret *corev1.Secret) (infrav1.TowerClientConfig, error) {
	data, ok := secret.Data["cloudtower.yaml"]
	if !ok {
		return infrav1.TowerClientConfig{}, errors.Errorf("tower secret %s missing cloudtower.yaml", client.ObjectKeyFromObject(secret))
	}

	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 1024)
	var config infrav1.TowerClientConfig
	if err := decoder.Decode(&config); err != nil {
		return infrav1.TowerClientConfig{}, errors.Wrapf(err, "failed to decode cloudtower.yaml in tower secret %s", client.ObjectKeyFromObject(secret))
	}

	return config, nil
}

func getTowerClientCacheKey(tower *infrav1.TowerClientConfig) string {
	return "tower-client:" + getClientKey(tower)
}

func getTowerSecretCacheKey(secretKey apitypes.NamespacedName) string {
	return "tower-secret:" + secretKey.String()
}

func getClientKey(tower *infrav1.TowerClientConfig) string {
	encryptedTower := *tower
	sum256 := sha256.Sum256([]byte(tower.Password))
	encryptedTower.Password = hex.EncodeToString(sum256[:])
	key, err := json.Marshal(encryptedTower)
	if err != nil {
		return fmt.Sprintf("%v", encryptedTower)
	}

	return string(key)
}

type withHeaderRoundTripper struct {
	http.Header

	rt http.RoundTripper
}

func NewWithHeaderRoundTripper(rt http.RoundTripper) withHeaderRoundTripper {
	if rt == nil {
		rt = http.DefaultTransport
	}

	return withHeaderRoundTripper{Header: make(http.Header), rt: rt}
}

func (h withHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if len(h.Header) == 0 {
		return h.rt.RoundTrip(req)
	}

	req = req.Clone(req.Context())
	for k, v := range h.Header {
		req.Header[k] = v
	}

	return h.rt.RoundTrip(req)
}
