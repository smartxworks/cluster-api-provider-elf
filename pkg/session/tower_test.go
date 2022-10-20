package session

import (
	goctx "context"
	"testing"
	"time"

	"github.com/onsi/gomega"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

func TestGetOrCreate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("should get cached session and clear inactive session", func(t *testing.T) {
		lastGCTime = lastGCTime.Add(-gcMinInterval)
		tower := infrav1.Tower{Server: "127.0.0.1", Username: "tower", Password: "tower"}
		inactiveTower := tower.DeepCopy()
		inactiveTower.Username = "inactive"
		invalidTower := tower.DeepCopy()
		invalidTower.Username = "invalid"

		sessionKey := getSessionKey(tower)
		cachedSession := &TowerSession{}
		sessionCache.Store(sessionKey, &cacheItem{Session: cachedSession})
		inactiveSessionKey := getSessionKey(*inactiveTower)
		sessionCache.Store(inactiveSessionKey, &cacheItem{Session: &TowerSession{}, LastUsedTime: time.Now().Add(-sessionIdleTime)})

		session, err := GetOrCreate(goctx.Background(), tower)
		g.Expect(err).To(gomega.BeNil())
		g.Expect(session).To(gomega.Equal(cachedSession))

		_, ok := sessionCache.Load(inactiveSessionKey)
		g.Expect(ok).To(gomega.BeFalse())

		session, err = GetOrCreate(goctx.Background(), *invalidTower)
		g.Expect(session).To(gomega.BeNil())
		g.Expect(err).To(gomega.HaveOccurred())
	})
}
