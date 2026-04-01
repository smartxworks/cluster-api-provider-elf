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

package labels

import (
	"strconv"
	"testing"

	"github.com/onsi/gomega"
)

func TestConvertToLabelValue(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	testCases := []struct {
		str        string
		labelValue string
	}{
		{
			str:        "test",
			labelValue: "test",
		}, {
			str:        "Test",
			labelValue: "Test",
		}, {
			str:        "TEST",
			labelValue: "TEST",
		}, {
			str:        "Tesla V100-PCIE-16GB",
			labelValue: "Tesla-V100-PCIE-16GB",
		}, {
			str:        "Tesla T4",
			labelValue: "Tesla-T4",
		}, {
			str:        ".test.",
			labelValue: "test",
		}, {
			str:        "-test-",
			labelValue: "test",
		}, {
			str:        "_test_",
			labelValue: "test",
		}, {
			str:        "_test_",
			labelValue: "test",
		}, {
			str:        "!!! te st !!!",
			labelValue: "te-st",
		}, {
			str:        "toolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolong",
			labelValue: "toolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolong",
		}, {
			str:        "Cluster!@#$%^&*()-_=+[]{}|:',.<>/?`~01",
			labelValue: "Cluster----------------------.------01",
		}, {
			str:        "Host-Name-01",
			labelValue: "Host-Name-01",
		}, {
			str:        "host-name-with-only-lowercase-letters-and-digits-123",
			labelValue: "host-name-with-only-lowercase-letters-and-digits-123",
		}, {
			str:        "",
			labelValue: "",
		}, {
			str:        "!!!",
			labelValue: "",
		}, {
			str:        "...---___...",
			labelValue: "",
		}, {
			str:        ".-_-edge-_-.",
			labelValue: "edge",
		}, {
			str:        "cluster;name\\with\"forbidden",
			labelValue: "cluster-name-with-forbidden",
		}, {
			str:        "cluster_name.v1",
			labelValue: "cluster-name.v1",
		}, {
			str:        "node---name",
			labelValue: "node---name",
		}, {
			str:        "___leading.and.trailing---",
			labelValue: "leading.and.trailing",
		}, {
			str:        "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-",
			labelValue: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		}, {
			str:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			labelValue: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			labelValue := ConvertToLabelValue(tc.str)
			g.Expect(labelValue).To(gomega.Equal(tc.labelValue))
		})
	}
}
