package remediation

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/explanation"
)

// Render prints validated candidate operations. It never writes the candidate
// artifact and repeats that the current source-tree status is unchanged.
func Render(writer io.Writer, request Request, candidates []ValidatedCandidate) error {
	if err := validateRequest(request); err != nil {
		return err
	}
	var output strings.Builder
	fmt.Fprintln(&output, "TMR Validated Candidate Remediation")
	fmt.Fprintln(&output, "CANDIDATES ONLY — NO FILES WERE MODIFIED")
	fmt.Fprintf(&output, "Current authoritative status: %s\n", request.Authoritative.Status)

	for index, candidate := range candidates {
		fmt.Fprintf(&output, "\nVALIDATED CANDIDATE %d: %s\n", index+1, safeText(candidate.ConsumerName))
		fmt.Fprintf(&output, "Source: %s", safeText(candidate.Source.File))
		if candidate.Locator.JSONPointer != "" {
			fmt.Fprintf(&output, " (%s)", safeText(candidate.Locator.JSONPointer))
		} else if candidate.Locator.Line != 0 {
			fmt.Fprintf(&output, ":%d:%d", candidate.Locator.Line, candidate.Locator.Column)
		}
		fmt.Fprintln(&output)
		fmt.Fprintf(&output, "Artifact: %s\n", candidate.ArtifactKind)
		fmt.Fprintf(&output, "Change: %s\n", safeText(candidate.ChangeID))
		fmt.Fprintln(&output, "Expression replacement:")
		fmt.Fprintf(&output, "- %s\n", quoteExpression(candidate.BeforeExpression))
		fmt.Fprintf(&output, "+ %s\n", quoteExpression(candidate.AfterExpression))
		fmt.Fprintf(&output, "Rationale: %s\n", safeText(candidate.Rationale))
		fmt.Fprintf(
			&output,
			"Validation: PromQL parsed; artifact parsed; legacy removed; destination present; graph reanalyzed; consumer => %s\n",
			candidate.Validation.SimulatedClass,
		)
		fmt.Fprintf(&output, "Simulated status if this candidate alone were applied: %s\n", candidate.Validation.SimulatedStatus)
	}

	fmt.Fprintln(&output, "\nHuman review and an explicit separate edit are required.")
	fmt.Fprintf(&output, "Current authoritative status remains: %s\n", request.Authoritative.Status)
	_, err := io.WriteString(writer, output.String())
	return err
}

func quoteExpression(expression string) string {
	return fmt.Sprintf("%q", safeText(expression))
}

func safeText(value string) string {
	value = explanation.Redact(value)
	return strings.Map(func(character rune) rune {
		if character == '\n' || character == '\t' || !unicode.IsControl(character) {
			return character
		}
		return -1
	}, value)
}
