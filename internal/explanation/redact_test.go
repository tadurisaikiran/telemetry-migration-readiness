package explanation

import (
	"strings"
	"testing"
)

func TestRedactRemovesCommonCredentialForms(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"password=hunter2",
		"Authorization: Bearer eyJhbGciOi.test.signature",
		"api_key:abcd1234",
		"https://user:pass@example.test/path",
		"AKIAABCDEFGHIJKLMNOP",
		"-----BEGIN PRIVATE KEY-----\nprivate-material\n-----END PRIVATE KEY-----",
	}, "\n")
	redacted := Redact(input)
	for _, secret := range []string{"hunter2", "eyJhbGciOi", "abcd1234", "user:pass", "AKIAABCDEFGHIJKLMNOP", "private-material"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted text contains %q: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "[REDACTED") {
		t.Fatalf("redacted text = %q", redacted)
	}
}

func TestRedactPreservesOrdinaryPromQL(t *testing.T) {
	t.Parallel()

	input := `sum by (service) (rate(http_requests_total{method="GET"}[5m]))`
	if got := Redact(input); got != input {
		t.Fatalf("Redact() = %q, want %q", got, input)
	}
}
