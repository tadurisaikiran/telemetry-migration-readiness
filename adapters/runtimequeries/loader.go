// Package runtimequeries imports bounded runtime PromQL observations without
// treating absence of an observation as evidence of safety.
package runtimequeries

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	tmrpromql "github.com/tadurisaikiran/telemetry-migration-readiness/pkg/promql"
)

const (
	maxRuntimeQueryFileBytes = 64 << 20
	maxRuntimeQueryLineBytes = 1 << 20
	maxRuntimeQueryRecords   = 500_000
)

// Loader configures one runtime-query JSONL source.
type Loader struct {
	Required    bool
	Format      string
	Window      time.Duration
	Criticality domain.Criticality
}

// LoadFile imports one local runtime-query export.
func (loader Loader) LoadFile(ctx context.Context, path string) (domain.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, fmt.Errorf("load runtime queries %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("open runtime queries %q: %w", path, err)
	}
	defer file.Close()
	discovery, err := loader.Parse(ctx, filepath.Clean(path), file)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("load runtime queries %q: %w", path, err)
	}
	return discovery, nil
}

// Parse decodes, time-filters, aggregates, and formally analyzes JSONL query
// observations. Window zero means all valid records in the file.
func (loader Loader) Parse(ctx context.Context, source string, reader io.Reader) (domain.Discovery, error) {
	if loader.Window < 0 {
		return domain.Discovery{}, fmt.Errorf("runtime query window must not be negative")
	}
	switch loader.Format {
	case FormatPrometheusQueryLog, FormatTMRQueryHistory:
	default:
		return domain.Discovery{}, fmt.Errorf("unsupported runtime query format %q", loader.Format)
	}
	if loader.Criticality == "" {
		loader.Criticality = domain.CriticalityHigh
	}
	contents, err := io.ReadAll(io.LimitReader(reader, maxRuntimeQueryFileBytes+1))
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("read runtime queries: %w", err)
	}
	if len(contents) > maxRuntimeQueryFileBytes {
		return domain.Discovery{}, fmt.Errorf("runtime query file exceeds the %d-byte size limit", maxRuntimeQueryFileBytes)
	}

	var discovery domain.Discovery
	var events []observedEvent
	recordCount := 0
	for index, line := range bytes.Split(contents, []byte{'\n'}) {
		if err := ctx.Err(); err != nil {
			return domain.Discovery{}, err
		}
		lineNumber := index + 1
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		recordCount++
		if recordCount > maxRuntimeQueryRecords {
			discovery.Diagnostics = append(discovery.Diagnostics, loader.diagnostic(
				source, lineNumber, fmt.Sprintf("runtime query file exceeds the %d-record limit", maxRuntimeQueryRecords),
			))
			break
		}
		if len(line) > maxRuntimeQueryLineBytes {
			discovery.Diagnostics = append(discovery.Diagnostics, loader.diagnostic(
				source, lineNumber, fmt.Sprintf("runtime query record exceeds the %d-byte line limit", maxRuntimeQueryLineBytes),
			))
			continue
		}
		event, err := decodeObservedEvent(loader.Format, line, lineNumber)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, loader.diagnostic(source, lineNumber, err.Error()))
			continue
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return discovery, nil
	}

	anchor := events[0].timestamp
	for _, event := range events[1:] {
		if event.timestamp.After(anchor) {
			anchor = event.timestamp
		}
	}
	cutoff := time.Time{}
	if loader.Window > 0 {
		cutoff = anchor.Add(-loader.Window)
	}
	aggregates := make(map[string]*queryAggregate)
	for _, event := range events {
		if !cutoff.IsZero() && event.timestamp.Before(cutoff) {
			continue
		}
		aggregate := aggregates[event.query]
		if aggregate == nil {
			aggregate = &queryAggregate{
				query:         event.query,
				firstSeen:     event.timestamp,
				lastSeen:      event.timestamp,
				latestLine:    event.line,
				origins:       make(map[string]struct{}),
				originDetails: make(map[string]struct{}),
			}
			aggregates[event.query] = aggregate
		}
		aggregate.executionCount++
		if event.timestamp.Before(aggregate.firstSeen) {
			aggregate.firstSeen = event.timestamp
		}
		if event.timestamp.After(aggregate.lastSeen) ||
			(event.timestamp.Equal(aggregate.lastSeen) && event.line < aggregate.latestLine) {
			aggregate.lastSeen = event.timestamp
			aggregate.latestLine = event.line
		}
		for _, origin := range event.origins {
			aggregate.origins[origin] = struct{}{}
		}
		for _, detail := range event.originDetails {
			aggregate.originDetails[detail] = struct{}{}
		}
	}

	queries := make([]string, 0, len(aggregates))
	for query := range aggregates {
		queries = append(queries, query)
	}
	sort.Strings(queries)
	for _, query := range queries {
		loader.appendAggregate(source, anchor, cutoff, aggregates[query], &discovery)
	}
	return discovery, nil
}

type queryAggregate struct {
	query          string
	executionCount int
	firstSeen      time.Time
	lastSeen       time.Time
	latestLine     int
	origins        map[string]struct{}
	originDetails  map[string]struct{}
}

func (loader Loader) appendAggregate(
	source string,
	anchor time.Time,
	cutoff time.Time,
	aggregate *queryAggregate,
	discovery *domain.Discovery,
) {
	hash := queryID(loader.Format, source, aggregate.query)
	origins := sortedKeys(aggregate.origins)
	details := sortedKeys(aggregate.originDetails)
	runtimeEvidence := &domain.RuntimeEvidence{
		Format:         loader.Format,
		ExecutionCount: aggregate.executionCount,
		FirstSeen:      aggregate.firstSeen.UTC().Format(time.RFC3339Nano),
		LastSeen:       aggregate.lastSeen.UTC().Format(time.RFC3339Nano),
		Window:         "all",
		WindowAnchor:   anchor.UTC().Format(time.RFC3339Nano),
		Origins:        origins,
		OriginDetails:  details,
	}
	if loader.Window > 0 {
		runtimeEvidence.Window = loader.Window.String()
		runtimeEvidence.WindowStart = cutoff.UTC().Format(time.RFC3339Nano)
		perDay := float64(aggregate.executionCount) * 24 / loader.Window.Hours()
		runtimeEvidence.ExecutionsPerDay = strconv.FormatFloat(perDay, 'f', 6, 64)
	}
	consumer := domain.Consumer{
		ID:          "runtime_query:" + hash,
		Kind:        domain.ConsumerKindQuery,
		Name:        "Observed PromQL " + hash[:12],
		Source:      domain.SourceLocation{File: source, Line: aggregate.latestLine},
		Criticality: loader.Criticality,
		Runtime:     runtimeEvidence,
		Expression:  aggregate.query,
	}
	analysis, err := tmrpromql.Analyze(aggregate.query)
	if err != nil || len(analysis.Unresolved) != 0 {
		consumer.Unresolved = true
		message := "runtime PromQL expression is unresolved"
		if err != nil {
			message = err.Error()
		} else {
			message = analysis.Unresolved[0].Reason
		}
		discovery.Diagnostics = append(discovery.Diagnostics, loader.diagnostic(source, aggregate.latestLine, message))
		discovery.Consumers = append(discovery.Consumers, consumer)
		return
	}
	for _, reference := range analysis.References {
		reference.ConsumerID = consumer.ID
		reference.Evidence.Method = domain.EvidenceMethodRuntimeQuery
		reference.Evidence.Source = consumer.Source
		reference.Evidence.Expression = aggregate.query
		reference.Evidence.Explanation = fmt.Sprintf(
			"observed %d execution(s); last seen %s",
			aggregate.executionCount,
			runtimeEvidence.LastSeen,
		)
		discovery.References = append(discovery.References, reference)
	}
	discovery.Consumers = append(discovery.Consumers, consumer)
}

func queryID(format, source, query string) string {
	digest := sha256.Sum256([]byte(format + "\x00" + source + "\x00" + query))
	return hex.EncodeToString(digest[:16])
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (loader Loader) diagnostic(source string, line int, message string) domain.Diagnostic {
	return domain.Diagnostic{
		Adapter:  "runtime_queries",
		Source:   domain.SourceLocation{File: source, Line: line},
		Message:  message,
		Required: loader.Required,
	}
}
