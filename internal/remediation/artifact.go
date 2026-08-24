package remediation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tadurisaikiran/telemetry-migration-readiness/adapters/grafana"
	"github.com/tadurisaikiran/telemetry-migration-readiness/adapters/prometheusrules"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

const maxArtifactBytes = 32 << 20

type Locator struct {
	Line        int    `json:"line,omitempty"`
	Column      int    `json:"column,omitempty"`
	JSONPointer string `json:"jsonPointer,omitempty"`
}

func loadAndPatchArtifact(target Target, after string) ([]byte, Locator, error) {
	file, err := os.Open(target.Source.File)
	if err != nil {
		return nil, Locator{}, fmt.Errorf("open candidate source %q: %w", target.Source.File, err)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxArtifactBytes+1))
	if err != nil {
		return nil, Locator{}, fmt.Errorf("read candidate source %q: %w", target.Source.File, err)
	}
	if len(contents) > maxArtifactBytes {
		return nil, Locator{}, fmt.Errorf("candidate source %q exceeds the %d-byte limit", target.Source.File, maxArtifactBytes)
	}
	switch target.ArtifactKind {
	case ArtifactKindPrometheusYAML:
		return patchYAML(contents, target.BeforeExpression, after)
	case ArtifactKindGrafanaJSON:
		return patchJSON(contents, target.BeforeExpression, after)
	default:
		return nil, Locator{}, fmt.Errorf("unsupported candidate artifact kind %q", target.ArtifactKind)
	}
}

func patchYAML(contents []byte, before, after string) ([]byte, Locator, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	var documents []*yaml.Node
	var matches []*yaml.Node
	for {
		var document yaml.Node
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, Locator{}, fmt.Errorf("decode candidate YAML: %w", err)
		}
		if len(document.Content) == 0 {
			continue
		}
		documents = append(documents, &document)
		collectYAMLScalarMatches(&document, before, &matches)
	}
	if len(matches) != 1 {
		return nil, Locator{}, fmt.Errorf("candidate YAML expression matched %d scalar values; exactly one is required", len(matches))
	}
	locator := Locator{Line: matches[0].Line, Column: matches[0].Column}
	matches[0].Value = after
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	for _, document := range documents {
		if err := encoder.Encode(document); err != nil {
			return nil, Locator{}, fmt.Errorf("encode candidate YAML: %w", err)
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, Locator{}, fmt.Errorf("close candidate YAML encoder: %w", err)
	}
	return output.Bytes(), locator, nil
}

func collectYAMLScalarMatches(node *yaml.Node, before string, matches *[]*yaml.Node) {
	if node.Kind == yaml.ScalarNode && node.Value == before {
		*matches = append(*matches, node)
	}
	for _, child := range node.Content {
		collectYAMLScalarMatches(child, before, matches)
	}
}

func patchJSON(contents []byte, before, after string) ([]byte, Locator, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, Locator{}, fmt.Errorf("decode candidate dashboard JSON: %w", err)
	}
	var additional any
	if err := decoder.Decode(&additional); err == nil {
		return nil, Locator{}, fmt.Errorf("candidate dashboard must contain exactly one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return nil, Locator{}, fmt.Errorf("decode trailing candidate dashboard JSON: %w", err)
	}
	var pointers []string
	replaceJSONScalar(&document, before, after, nil, &pointers)
	if len(pointers) != 1 {
		return nil, Locator{}, fmt.Errorf("candidate dashboard expression matched %d string values; exactly one is required", len(pointers))
	}
	output, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, Locator{}, fmt.Errorf("encode candidate dashboard JSON: %w", err)
	}
	return append(output, '\n'), Locator{JSONPointer: pointers[0]}, nil
}

func replaceJSONScalar(value *any, before, after string, path []string, pointers *[]string) {
	switch typed := (*value).(type) {
	case string:
		if typed == before {
			*value = after
			*pointers = append(*pointers, jsonPointer(path))
		}
	case []any:
		for index := range typed {
			child := typed[index]
			replaceJSONScalar(&child, before, after, appendPath(path, strconv.Itoa(index)), pointers)
			typed[index] = child
		}
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := typed[key]
			replaceJSONScalar(&child, before, after, appendPath(path, key), pointers)
			typed[key] = child
		}
	}
}

func appendPath(path []string, value string) []string {
	result := make([]string, len(path), len(path)+1)
	copy(result, path)
	return append(result, value)
}

func jsonPointer(path []string) string {
	if len(path) == 0 {
		return ""
	}
	escaped := make([]string, len(path))
	for index, element := range path {
		element = strings.ReplaceAll(element, "~", "~0")
		element = strings.ReplaceAll(element, "/", "~1")
		escaped[index] = element
	}
	return "/" + strings.Join(escaped, "/")
}

func parseCandidateArtifact(ctx context.Context, target Target, contents []byte) (domain.Discovery, error) {
	var (
		discovery domain.Discovery
		err       error
	)
	switch target.ArtifactKind {
	case ArtifactKindPrometheusYAML:
		discovery, err = (prometheusrules.Loader{Required: true}).Parse(ctx, target.Source.File, bytes.NewReader(contents))
	case ArtifactKindGrafanaJSON:
		discovery, err = (grafana.Loader{Required: true}).Parse(target.Source.File, bytes.NewReader(contents))
	default:
		return domain.Discovery{}, fmt.Errorf("unsupported candidate artifact kind %q", target.ArtifactKind)
	}
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("parse candidate artifact: %w", err)
	}
	for _, diagnostic := range discovery.Diagnostics {
		if diagnostic.Required {
			return domain.Discovery{}, fmt.Errorf("candidate artifact has unresolved required evidence: %s", diagnostic.Message)
		}
	}
	return discovery, nil
}
