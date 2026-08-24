// Package persesusage imports optional consumer evidence from the Perses
// metrics-usage HTTP API without making Perses a core dependency.
package persesusage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const maxResponseBytes = 32 << 20

type metricDocument struct {
	Labels []string       `json:"labels"`
	Usage  *usageDocument `json:"usage"`
}

type partialMetricDocument struct {
	Usage           *usageDocument  `json:"usage"`
	MatchingMetrics []string        `json:"matchingMetrics"`
	MatchingRegexp  json.RawMessage `json:"matchingRegexp"`
}

type usageDocument struct {
	Dashboards     []dashboardUsage `json:"dashboards"`
	RecordingRules []ruleUsage      `json:"recordingRules"`
	AlertRules     []ruleUsage      `json:"alertRules"`
}

// dashboardUsage accepts both the current uid/title fields and the id/name
// fields used by earlier metrics-usage response examples.
type dashboardUsage struct {
	UID        string `json:"uid"`
	LegacyID   string `json:"id"`
	Title      string `json:"title"`
	LegacyName string `json:"name"`
	URL        string `json:"url"`
}

type ruleUsage struct {
	PromLink   string `json:"prom_link"`
	GroupName  string `json:"group_name"`
	Name       string `json:"name"`
	Expression string `json:"expression"`
}

func decodeMetrics(reader io.Reader) (map[string]*metricDocument, error) {
	result := map[string]*metricDocument{}
	if err := decodeResponse(reader, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("metrics response must be a JSON object")
	}
	return result, nil
}

func decodePartialMetrics(reader io.Reader) (map[string]*partialMetricDocument, error) {
	result := map[string]*partialMetricDocument{}
	if err := decodeResponse(reader, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("partial metrics response must be a JSON object")
	}
	return result, nil
}

func decodePendingUsage(reader io.Reader) (map[string]*usageDocument, error) {
	result := map[string]*usageDocument{}
	if err := decodeResponse(reader, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("pending usage response must be a JSON object")
	}
	return result, nil
}

func decodeResponse(reader io.Reader, target any) error {
	contents, err := io.ReadAll(io.LimitReader(reader, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(contents) > maxResponseBytes {
		return fmt.Errorf("response exceeds the %d-byte size limit", maxResponseBytes)
	}
	if len(bytes.TrimSpace(contents)) == 0 {
		return errors.New("response is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode response JSON: %w", err)
	}
	var additional any
	if err := decoder.Decode(&additional); err == nil {
		return errors.New("response must contain exactly one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode trailing response JSON: %w", err)
	}
	return nil
}

func (dashboard dashboardUsage) id() string {
	if dashboard.UID != "" {
		return dashboard.UID
	}
	return dashboard.LegacyID
}

func (dashboard dashboardUsage) name() string {
	if dashboard.Title != "" {
		return dashboard.Title
	}
	return dashboard.LegacyName
}
