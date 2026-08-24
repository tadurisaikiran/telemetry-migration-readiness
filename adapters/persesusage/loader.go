package persesusage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

const defaultTimeout = 10 * time.Second

// Loader imports consumer evidence from a Perses metrics-usage service.
// The service is optional architecture: only its documented HTTP API crosses
// the adapter boundary.
type Loader struct {
	BaseURL     string
	Required    bool
	Timeout     time.Duration
	BearerToken string
	Client      *http.Client
}

// Discover fetches all supported metrics-usage endpoints. A metrics endpoint
// failure invalidates the source. Supplemental partial and pending endpoint
// failures are retained as diagnostics so exact evidence is still useful while
// readiness remains fail-closed for required sources.
func (loader Loader) Discover(ctx context.Context) (domain.Discovery, error) {
	baseURL, err := parseBaseURL(loader.BaseURL)
	if err != nil {
		return domain.Discovery{}, err
	}
	timeout := loader.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := loader.httpClient(baseURL, timeout)
	builder := newDiscoveryBuilder(strings.TrimRight(baseURL.String(), "/"), loader.Required)

	metricsResponse, err := loader.get(requestContext, client, baseURL, originMetrics, true)
	if err != nil {
		return domain.Discovery{}, err
	}
	metrics, decodeErr := decodeMetrics(metricsResponse)
	metricsResponse.Close()
	if decodeErr != nil {
		return domain.Discovery{}, fmt.Errorf("decode %s: %w", endpointURL(loader.BaseURL, originMetrics), decodeErr)
	}
	builder.addMetrics(metrics)

	partialResponse, err := loader.get(requestContext, client, baseURL, originPartial, false)
	if err != nil {
		builder.addEndpointDiagnostic(originPartial, err)
	} else {
		partial, decodeErr := decodePartialMetrics(partialResponse)
		partialResponse.Close()
		if decodeErr != nil {
			builder.addEndpointDiagnostic(originPartial, fmt.Errorf("decode response: %w", decodeErr))
		} else {
			builder.addPartialMetrics(partial)
		}
	}

	pendingResponse, err := loader.get(requestContext, client, baseURL, originPending, false)
	if err != nil {
		builder.addEndpointDiagnostic(originPending, err)
	} else {
		pending, decodeErr := decodePendingUsage(pendingResponse)
		pendingResponse.Close()
		if decodeErr != nil {
			builder.addEndpointDiagnostic(originPending, fmt.Errorf("decode response: %w", decodeErr))
		} else {
			builder.addPendingUsage(pending)
		}
	}

	return builder.build(), nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" ||
		(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return nil, fmt.Errorf("Perses metrics-usage URL must be an absolute http or https URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("Perses metrics-usage URL must not contain user information, a query, or a fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func (loader Loader) httpClient(baseURL *url.URL, timeout time.Duration) *http.Client {
	client := http.Client{Timeout: timeout}
	if loader.Client != nil {
		client = *loader.Client
		client.Timeout = timeout
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stop after 10 Perses metrics-usage redirects")
		}
		if !strings.EqualFold(request.URL.Scheme, baseURL.Scheme) ||
			!strings.EqualFold(request.URL.Host, baseURL.Host) {
			return fmt.Errorf("refuse redirect outside configured Perses metrics-usage origin")
		}
		return nil
	}
	return &client
}

func (loader Loader) get(
	ctx context.Context,
	client *http.Client,
	baseURL *url.URL,
	origin evidenceOrigin,
	usedOnly bool,
) (io.ReadCloser, error) {
	endpoint := *baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/v1/" + string(origin)
	if usedOnly {
		query := endpoint.Query()
		query.Set("used", "true")
		endpoint.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", endpoint.Redacted(), err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "tmr/perses-metrics-usage")
	if loader.BearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+loader.BearerToken)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", endpoint.Redacted(), err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("GET %s: unexpected HTTP status %s", endpoint.Redacted(), response.Status)
	}
	return response.Body, nil
}
