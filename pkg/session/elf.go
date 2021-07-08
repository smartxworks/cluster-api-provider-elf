package session

import (
	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha4"
	"github.smartx.com/smartx/elf-sdk-go/client"
)

type Session struct {
	*client.Client
	Auth infrav1.ElfAuth `json:"auth"`
}

func NewElfSession(auth infrav1.ElfAuth) (*Session, error) {
	c := client.NewClient(auth.Host, nil, auth.Username, auth.Password)
	authSessin := &Session{c, auth}

	return authSessin, nil
}
