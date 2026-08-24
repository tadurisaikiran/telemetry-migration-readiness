package remediation

import (
	"bytes"
	"strings"
	"testing"
)

func TestDecodeResponseRejectsStatusAndPatchClaims(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		`{"schemaVersion":"tmr-ai-remediation-response/v1alpha1","candidates":[],"status":"READY"}`,
		`{"schemaVersion":"tmr-ai-remediation-response/v1alpha1","candidates":[],"patch":"applied"}`,
	} {
		_, err := decodeResponse(strings.NewReader(input))
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestValidateResponseGroundsExactTargetAndBeforeExpression(t *testing.T) {
	t.Parallel()

	request := Request{
		SchemaVersion: RequestSchemaVersion,
		Task:          TaskCandidatePatch,
		Targets: []Target{{
			ID:               "known",
			BeforeExpression: "old_metric",
		}},
	}
	for _, response := range []Response{
		{SchemaVersion: ResponseSchemaVersion, Candidates: []Candidate{{ID: "one", TargetID: "unknown", BeforeExpression: "old_metric", AfterExpression: "new_metric", Rationale: "why"}}},
		{SchemaVersion: ResponseSchemaVersion, Candidates: []Candidate{{ID: "one", TargetID: "known", BeforeExpression: "invented", AfterExpression: "new_metric", Rationale: "why"}}},
	} {
		if err := validateResponse(response, request); err == nil {
			t.Fatalf("validateResponse(%#v) error = nil", response)
		}
	}
}

func FuzzDecodeResponseDoesNotPanic(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":"tmr-ai-remediation-response/v1alpha1","candidates":[]}`))
	f.Add([]byte(`{"status":"READY"}`))
	f.Fuzz(func(t *testing.T, contents []byte) {
		_, _ = decodeResponse(bytes.NewReader(contents))
	})
}
