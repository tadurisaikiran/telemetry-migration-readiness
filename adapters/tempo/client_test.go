package tempo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientValidatesThroughBoundedAuthenticatedTempoSearch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/base/api/search" || request.URL.Query().Get("q") != `{ span.http.method = "GET" }` ||
			request.URL.Query().Get("limit") != "1" || request.URL.Query().Get("start") != "1" || request.URL.Query().Get("end") != "2" {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if request.Header.Get("Authorization") != "Bearer secret" || request.Header.Get("Accept") != "application/json" {
			http.Error(writer, "bad headers", http.StatusUnauthorized)
			return
		}
		writer.Write([]byte(`{"traces":[]}`))
	}))
	defer server.Close()

	err := (Client{BaseURL: server.URL + "/base", Timeout: time.Second, BearerToken: "secret"}).Validate(
		context.Background(),
		`{ span.http.method = "GET" }`,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsStatusTimeoutOversizeAndCrossOriginRedirect(t *testing.T) {
	t.Parallel()

	t.Run("status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			http.Error(writer, "query echoed secret", http.StatusBadRequest)
		}))
		defer server.Close()
		err := (Client{BaseURL: server.URL, Timeout: time.Second}).Validate(context.Background(), "secret-query")
		if err == nil || !strings.Contains(err.Error(), "400 Bad Request") || strings.Contains(err.Error(), "secret-query") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			writer.Write([]byte(`{}`))
		}))
		defer server.Close()
		err := (Client{BaseURL: server.URL, Timeout: 10 * time.Millisecond}).Validate(context.Background(), "{}")
		if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("oversize", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Write([]byte(strings.Repeat("x", maxValidationResponseBytes+1)))
		}))
		defer server.Close()
		err := (Client{BaseURL: server.URL, Timeout: time.Second}).Validate(context.Background(), "{}")
		if err == nil || !strings.Contains(err.Error(), "size limit") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("redirect", func(t *testing.T) {
		var reached atomic.Bool
		target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			reached.Store(true)
		}))
		defer target.Close()
		redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, target.URL+request.URL.Path, http.StatusTemporaryRedirect)
		}))
		defer redirect.Close()
		err := (Client{BaseURL: redirect.URL, Timeout: time.Second, BearerToken: "must-not-leak"}).Validate(context.Background(), "{}")
		if err == nil || !strings.Contains(err.Error(), "refuse redirect") || reached.Load() || strings.Contains(err.Error(), "must-not-leak") {
			t.Fatalf("error = %v reached = %t", err, reached.Load())
		}
	})
}
