package explanation

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
)

// Render writes a clearly non-authoritative explanation while repeating the
// deterministic status before and after provider-authored text.
func Render(writer io.Writer, request Request, response Response) error {
	if err := validateRequest(request); err != nil {
		return err
	}
	if err := validateResponse(response, request); err != nil {
		return err
	}
	consumerNames := make(map[string]string, len(request.Findings))
	for _, finding := range request.Findings {
		consumerNames[finding.Consumer.ID] = finding.Consumer.Name
	}

	var output strings.Builder
	fmt.Fprintln(&output, "TMR AI Explanation (non-authoritative)")
	fmt.Fprintf(&output, "Authoritative status: %s\n", request.Authoritative.Status)
	fmt.Fprintln(&output, "Only the deterministic readiness engine can change this status.")
	fmt.Fprintf(&output, "\n%s\n", safeText(response.Answer))

	priorities := append([]Priority(nil), response.Priorities...)
	sort.Slice(priorities, func(i, j int) bool { return priorities[i].Order < priorities[j].Order })
	if len(priorities) != 0 {
		fmt.Fprintln(&output, "\nSuggested migration order:")
		for _, priority := range priorities {
			fmt.Fprintf(
				&output,
				"%d. %s (%s): %s\n   %s\n",
				priority.Order,
				safeText(consumerNames[priority.ConsumerID]),
				safeText(priority.ConsumerID),
				safeText(priority.Action),
				safeText(priority.Rationale),
			)
		}
	}
	if len(response.Limitations) != 0 {
		fmt.Fprintln(&output, "\nLimitations:")
		for _, limitation := range response.Limitations {
			fmt.Fprintf(&output, "- %s\n", safeText(limitation))
		}
	}
	fmt.Fprintf(&output, "\nAuthoritative status remains: %s\n", request.Authoritative.Status)
	_, err := io.WriteString(writer, output.String())
	return err
}

func safeText(value string) string {
	value = Redact(value)
	return strings.Map(func(character rune) rune {
		if character == '\n' || character == '\t' || !unicode.IsControl(character) {
			return character
		}
		return -1
	}, value)
}
