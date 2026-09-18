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
	annotationsutil "github.com/smartxworks/cluster-api-provider-elf/pkg/util/annotations"
)

// constants.
const (
	// CloudTowerServerVersionAnnotation is the annotation identifying the version of cloud tower server configuration.
	CloudTowerServerVersionAnnotation = "cape.infrastructure.cluster.x-k8s.io/cloud-server-version"

	CloudTowerServerVersion1_0_0 = "v1.0.0"
)

var lastGCTime = time.Now()
var gcMinInterval = 10 * time.Minute
var cacheIdleTime = 15 * time.Minute

// global cache map against cache keys.
// It stores both Tower clients and parsed Tower client configs from immutable Secrets.
var cacheMap sync.Map

var towerSecretConfigGroup singleflight.Group

type cacheItem struct {
	LastUsedTime time.Time
	TowerClient  *towerclient.Cloudtower
	TowerConfig  *infrav1.TowerClientConfig
}

// NewTowerClient gets a cached client or creates a new one if one does not
// already exist.
func NewTowerClient(ctx goctx.Context, k8sClient client.Client, tower infrav1.Tower) (*towerclient.Cloudtower, error) {
	clientConfig, err := GetTowerClientConfig(ctx, k8sClient, tower)
	if err != nil {
		return nil, err
	}

	logger := ctrl.LoggerFrom(ctx).WithName("client").WithValues("server", clientConfig.Server, "username", clientConfig.Username, "source", clientConfig.AuthMode)

	defer func() {
		if lastGCTime.Add(gcMinInterval).Before(time.Now()) {
			cleanupCache(logger)
		}
	}()

	clientKey := getTowerClientCacheKey(clientConfig)
	if item, ok := loadCacheItem(clientKey); ok && item.TowerClient != nil {
		logger.V(3).Info("found active cached tower client")

		return item.TowerClient, nil
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

	// Cache the client.
	cacheMap.Store(clientKey, &cacheItem{LastUsedTime: time.Now(), TowerClient: client})
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

// ClearClientCache removes all cached Tower clients and client configs.
func ClearClientCache() {
	cacheMap = sync.Map{}
	lastGCTime = time.Now()
}

func GetTowerClientConfig(ctx goctx.Context, k8sClient client.Client, tower infrav1.Tower) (*infrav1.TowerClientConfig, error) {
	if tower.SecretRef == nil {
		return &tower.TowerClientConfig, nil
	}

	secretKey := apitypes.NamespacedName{Namespace: tower.SecretRef.Namespace, Name: tower.SecretRef.Name}
	cacheKey := getTowerSecretCacheKey(secretKey)
	if item, ok := loadCacheItem(cacheKey); ok && item.TowerConfig != nil {
		return item.TowerConfig, nil
	}

	value, err, _ := towerSecretConfigGroup.Do(cacheKey, func() (interface{}, error) {
		if item, ok := loadCacheItem(cacheKey); ok && item.TowerConfig != nil {
			return item.TowerConfig, nil
		}

		var secret corev1.Secret
		if err := k8sClient.Get(ctx, secretKey, &secret); err != nil {
			return nil, errors.Wrapf(err, "failed to get tower secret %s", secretKey.String())
		}

		config, err := ParseTowerClientConfigFromSecret(&secret)
		if err != nil {
			return nil, err
		}

		// Cache the config if the server version is annotated.
		if annotationsutil.HasAnnotation(&secret, CloudTowerServerVersionAnnotation) {
			cacheMap.Store(cacheKey, &cacheItem{LastUsedTime: time.Now(), TowerConfig: config})
		}

		return config, nil
	})
	if err != nil {
		return nil, err
	}

	return value.(*infrav1.TowerClientConfig), nil
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

func ParseTowerClientConfigFromSecret(secret *corev1.Secret) (*infrav1.TowerClientConfig, error) {
	data, ok := secret.Data["cloudtower.yaml"]
	if !ok {
		return nil, errors.Errorf("tower secret %s missing cloudtower.yaml", client.ObjectKeyFromObject(secret))
	}

	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 1024)
	var config infrav1.TowerClientConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, errors.Wrapf(err, "failed to decode cloudtower.yaml in tower secret %s", client.ObjectKeyFromObject(secret))
	}

	return &config, nil
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
