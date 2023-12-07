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

package types

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestIsUUID(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	testCases := []struct {
		name   string
		uuid   string
		isUUID bool
	}{
		{
			name:   "empty uuid",
			uuid:   "",
			isUUID: false,
		},
		{
			name:   "invalid uuid",
			uuid:   "1234",
			isUUID: false,
		},
		{
			name:   "valid uuid",
			uuid:   "12345678-1234-1234-1234-123456789abc",
			isUUID: true,
		},
		{
			name:   "mixed case",
			uuid:   "12345678-1234-1234-1234-123456789AbC",
			isUUID: true,
		},
		{
			name:   "invalid hex chars",
			uuid:   "12345678-1234-1234-1234-123456789abg",
			isUUID: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isUUID := IsUUID(tc.uuid)
			g.Expect(isUUID).To(gomega.Equal(tc.isUUID))
		})
	}
}
