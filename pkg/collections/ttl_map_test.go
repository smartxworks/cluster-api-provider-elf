/*
Copyright 2023.

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

package collections

import (
	"testing"
	"time"

	"github.com/onsi/gomega"

	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

func TestTTLMap(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("should get cached session and clear inactive session", func(t *testing.T) {
		key := fake.UUID()
		testMap := NewTTLMap(1 * time.Minute)
		g.Expect(testMap.Len()).To(gomega.Equal(0))
		g.Expect(testMap.Has(key)).To(gomega.BeFalse())
		_, exists := testMap.Get(key)
		g.Expect(exists).To(gomega.BeFalse())

		testMap.Set(key, 1, 1*time.Minute)
		g.Expect(testMap.Has(key)).To(gomega.BeTrue())
		g.Expect(testMap.Len()).To(gomega.Equal(1))
		val, exists := testMap.Get(key)
		g.Expect(exists).To(gomega.BeTrue())
		ival, valid := val.(int)
		g.Expect(valid).To(gomega.BeTrue())
		g.Expect(ival).To(gomega.Equal(1))

		testMap.Del(key)
		g.Expect(testMap.Len()).To(gomega.Equal(0))
		g.Expect(testMap.Has(key)).To(gomega.BeFalse())

		testMap.Set(key, nil, 1*time.Minute)
		g.Expect(testMap.Has(key)).To(gomega.BeTrue())
		g.Expect(testMap.Len()).To(gomega.Equal(1))
		testMap.Values[key].Expiration = time.Now().Add(-1 * time.Minute)
		lastGCTime := testMap.LastGCTime
		testMap.CleanExpiredData()
		g.Expect(testMap.LastGCTime.Equal(lastGCTime.Add(testMap.GCInterval))).To(gomega.BeFalse())
		g.Expect(testMap.Has(key)).To(gomega.BeFalse())
		g.Expect(testMap.Len()).To(gomega.Equal(0))

		testMap.Set(key, nil, 1*time.Minute)
		g.Expect(testMap.Has(key)).To(gomega.BeTrue())
		testMap.Values[key].Expiration = time.Now().Add(-1 * time.Minute)
		g.Expect(testMap.Has(key)).To(gomega.BeFalse())
		g.Expect(testMap.Len()).To(gomega.Equal(0))

		testMap.Set(key, nil, 1*time.Minute)
		g.Expect(testMap.Has(key)).To(gomega.BeTrue())
		testMap.Values[key].Expiration = time.Now().Add(-1 * time.Minute)
		testMap.LastGCTime = time.Now().Add(-testMap.GCInterval)
		g.Expect(testMap.Has(key)).To(gomega.BeFalse())
		g.Expect(testMap.Len()).To(gomega.Equal(0))
	})
}
