package tempo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxValidationResponseBytes = 1 << 20

// Validator proves that a TraceQL expression is accepted by Tempo's official
// parser. Implementations must be read-only.
type Validator interface {
	Validate(context.Context, string) error
}

// Client validates TraceQL through Tempo's documented Search API.
type Client struct {
	BaseURL     string
	Timeout     time.Duration
	BearerToken string
	HTTPClient  *http.Client
}

// Validate submits a bounded search against an empty historical interval. TMR
// ignores query results; only Tempo's parser acceptance is evidence.
func (client Client) Validate(ctx context.Context, expression string) error {
	baseURL, err := parseTempoURL(client.BaseURL)
	if err != nil {
		return err
	}
	timeout := client.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := *baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/search"
	query := endpoint.Query()
	query.Set("q", expression)
	query.Set("limit", "1")
	query.Set("start", "1")
	query.Set("end", "2")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create Tempo validation request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "tmr/tempo-traceql-validation")
	if client.BearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+client.BearerToken)
	}

	httpClient := client.httpClient(baseURL, timeout)
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("validate TraceQL through Tempo: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Tempo rejected TraceQL with HTTP status %s", response.Status)
	}
	written, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxValidationResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read Tempo validation response: %w", err)
	}
	if written > maxValidationResponseBytes {
		return fmt.Errorf("Tempo validation response exceeds the %d-byte size limit", maxValidationResponseBytes)
	}
	return nil
}

func parseTempoURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" ||
		(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return nil, fmt.Errorf("Tempo URL must be an absolute http or https URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("Tempo URL must not contain user information, a query, or a fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func (client Client) httpClient(baseURL *url.URL, timeout time.Duration) *http.Client {
	result := http.Client{Timeout: timeout}
	if client.HTTPClient != nil {
		result = *client.HTTPClient
		result.Timeout = timeout
	}
	result.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stop after 10 Tempo redirects")
		}
		if !strings.EqualFold(request.URL.Scheme, baseURL.Scheme) ||
			!strings.EqualFold(request.URL.Host, baseURL.Host) {
			return fmt.Errorf("refuse redirect outside configured Tempo origin")
		}
		return nil
	}
	return &result
}
