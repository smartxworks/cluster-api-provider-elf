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

import "time"

// TTLMap is Go built-in map with TTL.
type TTLMap struct {
	Values     map[string]*ttlMapValue
	GCInterval time.Duration // interval time clearing expired values
	LastGCTime time.Time     // timestamp of the last cleanup of expired values
}

func NewTTLMap(gcInterval time.Duration) *TTLMap {
	return &TTLMap{
		Values:     make(map[string]*ttlMapValue),
		GCInterval: gcInterval,
		LastGCTime: time.Now(),
	}
}

type ttlMapValue struct {
	Val        interface{}
	Expiration time.Time // expiration time
}

func (t *TTLMap) Set(key string, val interface{}, duration time.Duration) {
	t.Values[key] = &ttlMapValue{Val: val, Expiration: time.Now().Add(duration)}
}

func (t *TTLMap) Get(key string) (interface{}, bool) {
	if t.Has(key) {
		return t.Values[key].Val, true
	}

	return nil, false
}

// Has returns whether the key exists and has not expired.
func (t *TTLMap) Has(key string) bool {
	// Delete expired values lazily.
	if time.Now().After(t.LastGCTime.Add(t.GCInterval)) {
		t.CleanExpiredData()
	}

	if value, ok := t.Values[key]; ok {
		if time.Now().After(value.Expiration) {
			t.Del(key)
		} else {
			return true
		}
	}

	return false
}

func (t *TTLMap) Del(key string) {
	delete(t.Values, key)
}

// CleanExpiredData removes all of the expired elements from this map.
func (t *TTLMap) CleanExpiredData() {
	for key, value := range t.Values {
		if time.Now().After(value.Expiration) {
			t.Del(key)
		}
	}

	t.LastGCTime = time.Now()
}

// Len returns a count of all values including expired values.
func (t *TTLMap) Len() int {
	return len(t.Values)
}
