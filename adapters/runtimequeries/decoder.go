package runtimequeries

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	FormatPrometheusQueryLog = "prometheus_query_log"
	FormatTMRQueryHistory    = "tmr_query_history"
	HistorySchemaVersion     = "tmr-runtime-query/v1alpha1"

	maxQueryBytes        = 128 << 10
	maxOriginBytes       = 128
	maxOriginDetailBytes = 4096
)

var originPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

type observedEvent struct {
	query         string
	timestamp     time.Time
	origins       []string
	originDetails []string
	line          int
}

func decodeObservedEvent(format string, line []byte, lineNumber int) (observedEvent, error) {
	var event observedEvent
	var err error
	switch format {
	case FormatPrometheusQueryLog:
		event, err = decodePrometheusQueryLog(line)
	case FormatTMRQueryHistory:
		event, err = decodeTMRQueryHistory(line)
	default:
		return observedEvent{}, fmt.Errorf("unsupported runtime query format %q", format)
	}
	if err != nil {
		return observedEvent{}, err
	}
	event.line = lineNumber
	event.query = strings.TrimSpace(event.query)
	if event.query == "" {
		return observedEvent{}, fmt.Errorf("query is required")
	}
	if len(event.query) > maxQueryBytes {
		return observedEvent{}, fmt.Errorf("query exceeds the %d-byte size limit", maxQueryBytes)
	}
	if event.timestamp.IsZero() {
		return observedEvent{}, fmt.Errorf("timestamp is required")
	}
	return event, nil
}

func decodePrometheusQueryLog(line []byte) (observedEvent, error) {
	var document struct {
		Params struct {
			Query string `json:"query"`
		} `json:"params"`
		Timestamp   string `json:"ts"`
		HTTPRequest *struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"httpRequest"`
		RuleGroup *struct {
			File string `json:"file"`
			Name string `json:"name"`
		} `json:"ruleGroup"`
	}
	if err := decodeJSONLine(line, &document, false); err != nil {
		return observedEvent{}, fmt.Errorf("decode Prometheus query log: %w", err)
	}
	timestamp, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(document.Timestamp))
	if err != nil {
		return observedEvent{}, fmt.Errorf("parse Prometheus query timestamp: %w", err)
	}
	event := observedEvent{query: document.Params.Query, timestamp: timestamp.UTC()}
	if document.HTTPRequest != nil {
		event.origins = append(event.origins, "prometheus_api")
		detail := strings.TrimSpace(strings.ToUpper(document.HTTPRequest.Method) + " " + document.HTTPRequest.Path)
		if detail != "" {
			event.originDetails = append(event.originDetails, detail)
		}
	}
	if document.RuleGroup != nil {
		event.origins = append(event.origins, "prometheus_rule_group")
		detail := strings.Trim(strings.TrimSpace(document.RuleGroup.File)+"#"+strings.TrimSpace(document.RuleGroup.Name), "#")
		if detail != "" {
			event.originDetails = append(event.originDetails, detail)
		}
	}
	if len(event.origins) == 0 {
		event.origins = []string{"prometheus_engine"}
	}
	if err := validateOriginDetails(event.originDetails); err != nil {
		return observedEvent{}, err
	}
	return event, nil
}

func decodeTMRQueryHistory(line []byte) (observedEvent, error) {
	var document struct {
		SchemaVersion string `json:"schemaVersion"`
		Timestamp     string `json:"timestamp"`
		Query         string `json:"query"`
		Origin        string `json:"origin"`
		Source        string `json:"source,omitempty"`
	}
	if err := decodeJSONLine(line, &document, true); err != nil {
		return observedEvent{}, fmt.Errorf("decode TMR query history: %w", err)
	}
	if document.SchemaVersion != HistorySchemaVersion {
		return observedEvent{}, fmt.Errorf("schemaVersion must be %q", HistorySchemaVersion)
	}
	timestamp, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(document.Timestamp))
	if err != nil {
		return observedEvent{}, fmt.Errorf("parse query-history timestamp: %w", err)
	}
	origin := strings.TrimSpace(document.Origin)
	if len(origin) > maxOriginBytes || !originPattern.MatchString(origin) {
		return observedEvent{}, fmt.Errorf("origin must be a bounded identifier containing letters, numbers, dot, dash, or underscore")
	}
	detail := strings.TrimSpace(document.Source)
	if len(detail) > maxOriginDetailBytes {
		return observedEvent{}, fmt.Errorf("source exceeds the %d-byte size limit", maxOriginDetailBytes)
	}
	event := observedEvent{
		query:     document.Query,
		timestamp: timestamp.UTC(),
		origins:   []string{origin},
	}
	if detail != "" {
		event.originDetails = []string{detail}
	}
	return event, nil
}

func decodeJSONLine(line []byte, target any, strict bool) error {
	decoder := json.NewDecoder(bytes.NewReader(line))
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var additional any
	if err := decoder.Decode(&additional); err == nil {
		return fmt.Errorf("line must contain exactly one JSON object")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func validateOriginDetails(details []string) error {
	for _, detail := range details {
		if len(detail) > maxOriginDetailBytes {
			return fmt.Errorf("origin detail exceeds the %d-byte size limit", maxOriginDetailBytes)
		}
	}
	return nil
}
