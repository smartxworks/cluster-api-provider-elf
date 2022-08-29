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
	"fmt"
	"sync"

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"github.com/pkg/errors"
	towerclient "github.com/smartxworks/cloudtower-go-sdk/v2/client"
	"github.com/smartxworks/cloudtower-go-sdk/v2/client/user"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	ctrl "sigs.k8s.io/controller-runtime"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

// global Session map against sessionKeys
// in map[sessionKey]Session.
var sessionCache sync.Map

type TowerSession struct {
	*towerclient.Cloudtower
}

// GetOrCreate gets a cached session or creates a new one if one does not
// already exist.
func GetOrCreate(ctx goctx.Context, tower infrav1.Tower) (*TowerSession, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("session").WithValues("server", tower.Server, "username", tower.Username, "source", tower.AuthMode)

	sessionKey := getSessionKey(tower)
	if cachedSession, ok := sessionCache.Load(sessionKey); ok {
		session := cachedSession.(*TowerSession)
		logger.V(3).Info("found active cached tower client session")

		return session, nil
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
	sessionCache.Store(sessionKey, session)
	logger.V(3).Info("cached tower client session")

	return session, nil
}

func getSessionKey(tower infrav1.Tower) string {
	return fmt.Sprintf("%s-%s-%s", tower.Server, tower.Username, tower.AuthMode)
}

func createTowerClient(tlsOpts httptransport.TLSClientOptions, clientConfig towerclient.ClientConfig, userConfig towerclient.UserConfig) (*towerclient.Cloudtower, error) {
	rt := httptransport.New(clientConfig.Host, clientConfig.BasePath, clientConfig.Schemes)
	var err error
	rt.Transport, err = httptransport.TLSTransport(tlsOpts)
	if err != nil {
		return nil, err
	}

	client := towerclient.New(rt, strfmt.Default)
	params := user.NewLoginParams()
	params.RequestBody = &models.LoginInput{
		Username: &userConfig.Name,
		Password: &userConfig.Password,
		Source:   userConfig.Source.Pointer(),
	}
	resp, err := client.User.Login(params)
	if err != nil {
		return nil, err
	}
	rt.DefaultAuthentication = httptransport.APIKeyAuth("Authorization", "header", *resp.Payload.Data.Token)
	return client, nil
}
