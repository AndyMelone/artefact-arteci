package observability

import "testing"

func TestIsTLSEndpoint(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"explicit https", "https://ingest.us2.signoz.cloud:443", true},
		{"explicit http", "http://signoz-otel-collector.monitoring.svc.cluster.local:4317", false},
		{"no scheme, port 443", "ingest.us2.signoz.cloud:443", true},
		{"no scheme, port 4317", "signoz-ingester:4317", false},
		{"no scheme, in-cluster service", "signoz-otel-collector.monitoring.svc.cluster.local:4317", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTLSEndpoint(tc.raw); got != tc.want {
				t.Errorf("isTLSEndpoint(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
