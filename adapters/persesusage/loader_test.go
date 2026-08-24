package persesusage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoaderFetchesAllEndpointsWithAuthentication(t *testing.T) {
	t.Parallel()

	responses := map[string][]byte{
		"/base/api/v1/metrics":         mustReadFixture(t, "testdata/metrics.json"),
		"/base/api/v1/partial_metrics": mustReadFixture(t, "testdata/partial_metrics.json"),
		"/base/api/v1/pending_usages":  mustReadFixture(t, "testdata/pending_usages.json"),
	}
	var mutex sync.Mutex
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		if request.Header.Get("Accept") != "application/json" {
			http.Error(writer, "missing accept", http.StatusBadRequest)
			return
		}
		if request.URL.Path == "/base/api/v1/metrics" && request.URL.Query().Get("used") != "true" {
			http.Error(writer, "missing used filter", http.StatusBadRequest)
			return
		}
		response, exists := responses[request.URL.Path]
		if !exists {
			http.NotFound(writer, request)
			return
		}
		mutex.Lock()
		seen[request.URL.Path] = true
		mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		writer.Write(response)
	}))
	defer server.Close()

	discovery, err := (Loader{
		BaseURL:     server.URL + "/base",
		Required:    true,
		Timeout:     time.Second,
		BearerToken: "test-token",
	}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", discovery.Diagnostics)
	}
	mutex.Lock()
	seenCount := len(seen)
	mutex.Unlock()
	if got, want := seenCount, 3; got != want {
		t.Fatalf("endpoints fetched = %d, want %d", got, want)
	}
}

func TestLoaderRetainsExactEvidenceWhenSupplementalEndpointFails(t *testing.T) {
	t.Parallel()

	metrics := mustReadFixture(t, "testdata/metrics.json")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/metrics") {
			writer.Write(metrics)
			return
		}
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	discovery, err := (Loader{BaseURL: server.URL, Required: true, Timeout: time.Second}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) == 0 || len(discovery.Diagnostics) != 2 {
		t.Fatalf("discovery = %#v", discovery)
	}
	for _, diagnostic := range discovery.Diagnostics {
		if !diagnostic.Required {
			t.Fatalf("diagnostic = %#v, want required", diagnostic)
		}
	}
}

func TestLoaderRejectsExactEndpointFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := (Loader{BaseURL: server.URL, Timeout: time.Second}).Discover(context.Background())
	if err == nil || !strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoaderEnforcesTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		writer.Write([]byte(`{}`))
	}))
	defer server.Close()

	_, err := (Loader{BaseURL: server.URL, Timeout: 10 * time.Millisecond}).Discover(context.Background())
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("error = %v, want deadline", err)
	}
}

func TestLoaderRejectsCrossOriginRedirectWithoutLeakingToken(t *testing.T) {
	t.Parallel()

	var targetReached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		targetReached.Store(true)
		if request.Header.Get("Authorization") != "" {
			t.Error("authorization header reached redirected origin")
		}
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL+request.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	_, err := (Loader{
		BaseURL:     redirect.URL,
		Timeout:     time.Second,
		BearerToken: "must-not-leak",
	}).Discover(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refuse redirect") {
		t.Fatalf("error = %v, want redirect refusal", err)
	}
	if targetReached.Load() {
		t.Fatal("redirect target was reached")
	}
	if strings.Contains(err.Error(), "must-not-leak") {
		t.Fatal("error contains bearer token")
	}
}

func mustReadFixture(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}
