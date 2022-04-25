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
	"github.com/go-openapi/runtime"
	openapiclient "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	towerclient "github.com/smartxworks/cloudtower-go-sdk/client"
	clientuser "github.com/smartxworks/cloudtower-go-sdk/client/user"
	"github.com/smartxworks/cloudtower-go-sdk/models"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

type TowerSession struct {
	*towerclient.Cloudtower
}

func NewTowerSession(tower infrav1.Tower) (*TowerSession, error) {
	transport := openapiclient.New(tower.Server, "/v2/api", []string{"http"})
	client := towerclient.New(transport, strfmt.Default)

	loginParams := clientuser.NewLoginParams()
	loginParams.RequestBody = &models.LoginInput{
		Username: &tower.Username,
		Password: &tower.Password,
		Source:   models.NewUserSource(models.UserSource(tower.AuthMode)),
	}

	loginResp, err := client.User.Login(loginParams, func(*runtime.ClientOperation) {})
	if err != nil {
		return nil, err
	}

	token := openapiclient.BearerToken(*loginResp.Payload.Data.Token)
	transport.DefaultAuthentication = token
	client = towerclient.New(transport, strfmt.Default)

	return &TowerSession{client}, nil
}
