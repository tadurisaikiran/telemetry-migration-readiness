package persesusage

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestDecodeMetricsSupportsCurrentAndLegacyDashboardFields(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/metrics.json")
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := decodeMetrics(bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	current := metrics["checkout_request_duration_seconds_count"].Usage.Dashboards[0]
	if current.id() != "checkout-latency" || current.name() != "Checkout Latency" {
		t.Fatalf("current dashboard = %#v", current)
	}
	legacy := metrics["checkout_request_duration_seconds"].Usage.Dashboards[0]
	if legacy.id() != "legacy-checkout-overview" || legacy.name() != "Legacy Checkout Overview" {
		t.Fatalf("legacy dashboard = %#v", legacy)
	}
}

func TestDecodersRejectMalformedTopLevelDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		decode func() error
	}{
		{name: "metrics null", input: "null", decode: func() error {
			_, err := decodeMetrics(strings.NewReader("null"))
			return err
		}},
		{name: "partial array", input: "[]", decode: func() error {
			_, err := decodePartialMetrics(strings.NewReader("[]"))
			return err
		}},
		{name: "pending trailing", input: "{} {}", decode: func() error {
			_, err := decodePendingUsage(strings.NewReader("{} {}"))
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.decode(); err == nil {
				t.Fatalf("decode(%q) error = nil", test.input)
			}
		})
	}
}

func FuzzDecodeMetricsDoesNotPanic(f *testing.F) {
	contents, err := os.ReadFile("testdata/metrics.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(contents)
	f.Add([]byte(`{"metric":null}`))
	f.Fuzz(func(t *testing.T, contents []byte) {
		_, _ = decodeMetrics(bytes.NewReader(contents))
	})
}
