// Package report renders deterministic readiness results for humans and
// machines.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/graph"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/ownership"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/readiness"
)

// JSON renders the versioned machine result.
func JSON(result readiness.Result) ([]byte, error) {
	contents, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode JSON report: %w", err)
	}
	return append(contents, '\n'), nil
}

// Console renders a compact terminal report.
func Console(writer io.Writer, result readiness.Result) error {
	var output bytes.Buffer
	fmt.Fprintln(&output, "Telemetry Migration Readiness")
	fmt.Fprintln(&output, strings.Repeat("=", 29))
	fmt.Fprintf(&output, "Migration: %s\n", result.Migration.Metadata.Name)
	for _, change := range result.Migration.Changes {
		if change.To == nil {
			fmt.Fprintf(&output, "  %s: remove %s\n", change.ID, change.From.Name)
		} else {
			fmt.Fprintf(&output, "  %s: %s -> %s\n", change.ID, change.From.Name, change.To.Name)
		}
	}
	fmt.Fprintln(&output)
	fmt.Fprintf(&output, "Consumers scanned: %d\n", result.Summary.TotalConsumers)
	fmt.Fprintf(&output, "Migrated:          %d\n", result.Summary.Migrated)
	fmt.Fprintf(&output, "Legacy only:       %d\n", result.Summary.LegacyOnly)
	fmt.Fprintf(&output, "Dual-compatible:   %d\n", result.Summary.Dual)
	fmt.Fprintf(&output, "Uncertain:         %d\n", result.Summary.Uncertain)
	fmt.Fprintf(&output, "Unaffected:        %d\n", result.Summary.Unaffected)
	fmt.Fprintf(&output, "Progress:          %d%% (informational only)\n", result.Summary.Progress)

	blockers := findings(result, readiness.ClassificationLegacyOnly)
	if len(blockers) != 0 {
		fmt.Fprintln(&output, "\nBLOCKERS")
		for _, finding := range blockers {
			fmt.Fprintf(&output, "  - %s [%s] (%s)\n", finding.name, finding.changeID, formatLocation(finding.file, finding.line))
			if finding.owner != "" {
				fmt.Fprintf(&output, "    Owner: %s\n", finding.owner)
			}
			if finding.path != "" {
				fmt.Fprintf(&output, "    Path: %s\n", finding.path)
			}
		}
	}

	uncertain := findings(result, readiness.ClassificationUncertain)
	if len(uncertain) != 0 {
		fmt.Fprintln(&output, "\nUNCERTAIN")
		for _, finding := range uncertain {
			fmt.Fprintf(&output, "  - %s [%s] (%s)\n", finding.name, finding.changeID, formatLocation(finding.file, finding.line))
			if finding.owner != "" {
				fmt.Fprintf(&output, "    Owner: %s\n", finding.owner)
			}
		}
	}

	if len(result.Diagnostics) != 0 {
		fmt.Fprintln(&output, "\nDIAGNOSTICS")
		for _, diagnostic := range result.Diagnostics {
			required := "optional"
			if diagnostic.Required {
				required = "required"
			}
			fmt.Fprintf(&output, "  - [%s/%s] %s: %s\n", diagnostic.Adapter, required, formatLocation(diagnostic.Source.File, diagnostic.Source.Line), diagnostic.Message)
		}
	}

	fmt.Fprintf(&output, "\nSTATUS: %s\n", result.Summary.Status)
	_, err := writer.Write(output.Bytes())
	return err
}

// Markdown renders a report suitable for pull-request comments and artifacts.
func Markdown(writer io.Writer, result readiness.Result) error {
	var output bytes.Buffer
	fmt.Fprintln(&output, "# Telemetry Migration Readiness")
	fmt.Fprintf(&output, "\n**Migration:** `%s`  \n", result.Migration.Metadata.Name)
	fmt.Fprintf(&output, "**Status:** **%s**  \n", result.Summary.Status)
	fmt.Fprintf(&output, "**Progress:** %d%% _(informational only)_\n", result.Summary.Progress)
	fmt.Fprintln(&output, "\n| Classification | Consumers |")
	fmt.Fprintln(&output, "| --- | ---: |")
	fmt.Fprintf(&output, "| Migrated | %d |\n", result.Summary.Migrated)
	fmt.Fprintf(&output, "| Legacy only | %d |\n", result.Summary.LegacyOnly)
	fmt.Fprintf(&output, "| Dual-compatible | %d |\n", result.Summary.Dual)
	fmt.Fprintf(&output, "| Uncertain | %d |\n", result.Summary.Uncertain)
	fmt.Fprintf(&output, "| Unaffected | %d |\n", result.Summary.Unaffected)

	for _, section := range []struct {
		title          string
		classification readiness.Classification
	}{
		{title: "Blockers", classification: readiness.ClassificationLegacyOnly},
		{title: "Uncertainties", classification: readiness.ClassificationUncertain},
	} {
		items := findings(result, section.classification)
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&output, "\n## %s\n", section.title)
		for _, item := range items {
			fmt.Fprintf(&output, "\n- **%s** (`%s`) — %s\n", item.name, item.changeID, formatLocation(item.file, item.line))
			if item.owner != "" {
				fmt.Fprintf(&output, "  - Owner: %s\n", item.owner)
			}
			if item.path != "" {
				fmt.Fprintf(&output, "  - Dependency path: `%s`\n", item.path)
			}
		}
	}

	if len(result.Diagnostics) != 0 {
		fmt.Fprintln(&output, "\n## Diagnostics")
		for _, diagnostic := range result.Diagnostics {
			fmt.Fprintf(&output, "\n- `%s`: %s\n", diagnostic.Adapter, diagnostic.Message)
		}
	}

	_, err := writer.Write(output.Bytes())
	return err
}

// GraphJSON renders the dependency graph as a stable JSON document.
func GraphJSON(target *graph.Graph) ([]byte, error) {
	document := struct {
		Nodes []graph.Node `json:"nodes"`
		Edges []graph.Edge `json:"edges"`
	}{
		Nodes: target.Nodes(),
		Edges: target.Edges(),
	}
	contents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode graph JSON: %w", err)
	}
	return append(contents, '\n'), nil
}

type finding struct {
	changeID string
	name     string
	file     string
	line     int
	path     string
	owner    string
}

func findings(result readiness.Result, classification readiness.Classification) []finding {
	var resultFindings []finding
	for _, change := range result.Changes {
		for _, consumer := range change.Consumers {
			if consumer.Classification != classification {
				continue
			}
			path := ""
			if len(consumer.Paths) != 0 {
				path = strings.Join(consumer.Paths[0].Nodes, " -> ")
			}
			resultFindings = append(resultFindings, finding{
				changeID: change.Change.ID,
				name:     consumer.Consumer.Name,
				file:     consumer.Consumer.Source.File,
				line:     consumer.Consumer.Source.Line,
				path:     path,
				owner:    ownerLabel(consumer.Consumer),
			})
		}
	}
	sort.Slice(resultFindings, func(i, j int) bool {
		if resultFindings[i].changeID != resultFindings[j].changeID {
			return resultFindings[i].changeID < resultFindings[j].changeID
		}
		return resultFindings[i].name < resultFindings[j].name
	})
	return resultFindings
}

func ownerLabel(consumer domain.Consumer) string {
	if consumer.Owner != nil {
		return consumer.Owner.Name
	}
	candidates := ownership.Candidates(consumer)
	if len(candidates) != 0 {
		return "ambiguous: " + strings.Join(candidates, ", ")
	}
	if ownership.Unassigned(consumer) {
		return "unassigned by CODEOWNERS"
	}
	return ""
}

func formatLocation(file string, line int) string {
	if file == "" {
		return "unknown source"
	}
	if line == 0 {
		return file
	}
	return fmt.Sprintf("%s:%d", file, line)
}
