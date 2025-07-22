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

package session

import (
	goctx "context"
	"crypto/sha256"
	"encoding/hex"
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
	ctrl "sigs.k8s.io/controller-runtime"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

var lastGCTime = time.Now()
var gcMinInterval = 10 * time.Minute
var sessionIdleTime = 10 * time.Minute

// global Session map against sessionKeys
// in map[sessionKey]cacheItem.
var sessionCache sync.Map

type cacheItem struct {
	LastUsedTime time.Time
	Session      *TowerSession
}

type TowerSession struct {
	*towerclient.Cloudtower
}

// GetOrCreate gets a cached session or creates a new one if one does not
// already exist.
func GetOrCreate(ctx goctx.Context, tower infrav1.Tower) (*TowerSession, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("session").WithValues("server", tower.Server, "username", tower.Username, "source", tower.AuthMode)

	defer func() {
		if lastGCTime.Add(gcMinInterval).Before(time.Now()) {
			cleanupSessionCache(logger)
			lastGCTime = time.Now()
		}
	}()

	sessionKey := getSessionKey(&tower)
	if value, ok := sessionCache.Load(sessionKey); ok {
		item := value.(*cacheItem)
		item.LastUsedTime = time.Now()
		logger.V(3).Info("found active cached tower client session")

		return item.Session, nil
	}

	client, err := createTowerClient(httptransport.TLSClientOptions{
		InsecureSkipVerify: tower.SkipTLSVerify,
	}, towerclient.ClientConfig{
		Host:     tower.Server,
		BasePath: "/v2/api",
		Schemes:  []string{"https"},
	}, towerclient.UserConfig{
		Name:     tower.Username,
		Password: tower.Password,
		Source:   models.UserSource(tower.AuthMode),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create tower client")
	}

	session := &TowerSession{client}

	// Cache the session.
	sessionCache.Store(sessionKey, &cacheItem{LastUsedTime: time.Now(), Session: session})
	logger.V(3).Info("cached tower client session")

	return session, nil
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

func cleanupSessionCache(logger logr.Logger) {
	sessionCache.Range(func(key interface{}, value interface{}) bool {
		item := value.(*cacheItem)
		if item.LastUsedTime.Add(sessionIdleTime).Before(time.Now()) {
			sessionCache.Delete(key)
			logger.V(3).Info(fmt.Sprintf("delete inactive tower client session %s from sessionCache", key))
		}

		return true
	})
}

func getSessionKey(tower *infrav1.Tower) string {
	encryptedTower := tower.DeepCopy()
	sum256 := sha256.Sum256([]byte(tower.Password))
	encryptedTower.Password = hex.EncodeToString(sum256[:])

	return fmt.Sprintf("%v", encryptedTower)
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
