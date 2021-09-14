package util

import (
	"testing"

	"github.com/onsi/gomega"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha4"
)

func TestConvertProviderIDToUUID(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	testCases := []struct {
		name         string
		providerID   *string
		expectedUUID string
	}{
		{
			name:         "nil providerID",
			providerID:   nil,
			expectedUUID: "",
		},
		{
			name:         "empty providerID",
			providerID:   toStringPtr(""),
			expectedUUID: "",
		},
		{
			name:         "invalid providerID",
			providerID:   toStringPtr("1234"),
			expectedUUID: "",
		},
		{
			name:         "missing prefix",
			providerID:   toStringPtr("12345678-1234-1234-1234-123456789abc"),
			expectedUUID: "",
		},
		{
			name:         "valid providerID",
			providerID:   toStringPtr("elf://12345678-1234-1234-1234-123456789abc"),
			expectedUUID: "12345678-1234-1234-1234-123456789abc",
		},
		{
			name:         "mixed case",
			providerID:   toStringPtr("elf://12345678-1234-1234-1234-123456789AbC"),
			expectedUUID: "12345678-1234-1234-1234-123456789AbC",
		},
		{
			name:         "invalid hex chars",
			providerID:   toStringPtr("elf://12345678-1234-1234-1234-123456789abg"),
			expectedUUID: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualUUID := ConvertProviderIDToUUID(tc.providerID)
			g.Expect(actualUUID).To(gomega.Equal(tc.expectedUUID))
		})
	}
}

func TestConvertUUIDtoProviderID(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	testCases := []struct {
		name               string
		uuid               string
		expectedProviderID string
	}{
		{
			name:               "empty uuid",
			uuid:               "",
			expectedProviderID: "",
		},
		{
			name:               "invalid uuid",
			uuid:               "1234",
			expectedProviderID: "",
		},
		{
			name:               "valid uuid",
			uuid:               "12345678-1234-1234-1234-123456789abc",
			expectedProviderID: "elf://12345678-1234-1234-1234-123456789abc",
		},
		{
			name:               "mixed case",
			uuid:               "12345678-1234-1234-1234-123456789AbC",
			expectedProviderID: "elf://12345678-1234-1234-1234-123456789AbC",
		},
		{
			name:               "invalid hex chars",
			uuid:               "12345678-1234-1234-1234-123456789abg",
			expectedProviderID: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualProviderID := ConvertUUIDToProviderID(tc.uuid)
			g.Expect(actualProviderID).To(gomega.Equal(tc.expectedProviderID))
		})
	}
}

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

func TestGetNetworkStatus(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	testCases := []struct {
		name          string
		ips           string
		networkStatus []infrav1.NetworkStatus
	}{
		{
			name:          "empty",
			ips:           "",
			networkStatus: nil,
		},
		{
			name:          "local ip",
			ips:           "127.0.0.1",
			networkStatus: nil,
		},
		{
			name:          "169.254 prefix",
			ips:           "169.254.0.1",
			networkStatus: nil,
		},
		{
			name:          "172.17.0 prefix",
			ips:           "172.17.0.1",
			networkStatus: nil,
		},
		{
			name: "valid IP",
			ips:  "116.116.116.116",
			networkStatus: []infrav1.NetworkStatus{{
				NetworkIndex: 0,
				IPAddrs:      []string{"116.116.116.116"},
			}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			networkStatus := GetNetworkStatus(tc.ips)
			g.Expect(networkStatus).To(gomega.Equal(tc.networkStatus))
		})
	}
}

func toStringPtr(s string) *string {
	return &s
}
