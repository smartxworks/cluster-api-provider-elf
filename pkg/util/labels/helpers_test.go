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
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			labelValue := ConvertToLabelValue(tc.str)
			g.Expect(labelValue).To(gomega.Equal(tc.labelValue))
		})
	}
}
