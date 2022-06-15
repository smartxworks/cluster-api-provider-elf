package session

import (
	goctx "context"
	"testing"

	"github.com/onsi/gomega"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

func TestGetOrCreate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("should get cached session", func(t *testing.T) {
		tower := infrav1.Tower{Server: "127.0.0.1", Username: "tower", Password: "tower"}

		sessionKey := getSessionKey(tower)
		cachedSession := &TowerSession{}
		sessionCache.Store(sessionKey, cachedSession)

		session, err := GetOrCreate(goctx.Background(), tower)
		g.Expect(err).To(gomega.BeNil())
		g.Expect(session).To(gomega.Equal(cachedSession))
	})
}
