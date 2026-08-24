package explanation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCommandClientAcceptsGroundedReadOnlyResponse(t *testing.T) {
	t.Setenv("TMR_EXPLANATION_HELPER", "1")

	response, err := helperClient("success", 10*time.Second).Explain(context.Background(), protocolFixtureRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.Answer != "The critical alert still follows the legacy metric path." {
		t.Fatalf("answer = %q", response.Answer)
	}
	if len(response.Priorities) != 1 || response.Priorities[0].ConsumerID != "alert" {
		t.Fatalf("priorities = %#v", response.Priorities)
	}
}

func TestCommandClientRejectsStatusOverrideAndUnknownConsumer(t *testing.T) {
	t.Setenv("TMR_EXPLANATION_HELPER", "1")

	tests := []struct {
		mode string
		want string
	}{
		{mode: "status", want: "unknown field"},
		{mode: "unknown-consumer", want: "unknown consumer"},
		{mode: "malformed", want: "decode AI provider response"},
	}
	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			_, err := helperClient(test.mode, 10*time.Second).Explain(context.Background(), protocolFixtureRequest())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCommandClientEnforcesTimeoutAndOutputBound(t *testing.T) {
	t.Setenv("TMR_EXPLANATION_HELPER", "1")

	_, err := helperClient("slow", 10*time.Millisecond).Explain(context.Background(), protocolFixtureRequest())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	_, err = helperClient("oversize", 10*time.Second).Explain(context.Background(), protocolFixtureRequest())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize error = %v", err)
	}
}

func TestCommandClientRedactsProviderErrors(t *testing.T) {
	t.Setenv("TMR_EXPLANATION_HELPER", "1")

	_, err := helperClient("failure", 10*time.Second).Explain(context.Background(), protocolFixtureRequest())
	if err == nil || !strings.Contains(err.Error(), "[REDACTED]") || strings.Contains(err.Error(), "provider-secret") {
		t.Fatalf("error = %v", err)
	}
}

func TestRenderRepeatsAuthoritativeStatusAndSanitizesProviderText(t *testing.T) {
	t.Parallel()

	request := protocolFixtureRequest()
	response := Response{
		SchemaVersion: ResponseSchemaVersion,
		Answer:        "Review the alert.\x1b[31m token=render-secret",
		Priorities: []Priority{
			{Order: 2, ConsumerID: "dashboard", Action: "Resolve template", Rationale: "The reference is unknown."},
			{Order: 1, ConsumerID: "alert", Action: "Migrate alert", Rationale: "It is a critical blocker."},
		},
		Limitations: []string{"No runtime evidence was provided."},
	}
	var output bytes.Buffer
	if err := Render(&output, request, response); err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	if strings.Count(rendered, "BLOCKED") != 2 {
		t.Fatalf("rendered status count = %d\n%s", strings.Count(rendered, "BLOCKED"), rendered)
	}
	if strings.Index(rendered, "1. Alert") > strings.Index(rendered, "2. Dashboard") {
		t.Fatalf("priorities not sorted:\n%s", rendered)
	}
	if strings.Contains(rendered, "\x1b") || strings.Contains(rendered, "render-secret") {
		t.Fatalf("unsafe provider text rendered:\n%s", rendered)
	}
}

func helperClient(mode string, timeout time.Duration) CommandClient {
	return CommandClient{
		Path:    os.Args[0],
		Args:    []string{"-test.run=TestExplanationProviderHelper", "--", mode},
		Timeout: timeout,
	}
}

func protocolFixtureRequest() Request {
	return Request{
		SchemaVersion: RequestSchemaVersion,
		Task:          TaskReadOnlyExplain,
		Question:      "Why is this blocked?",
		Guardrails:    append([]string(nil), guardrails...),
		Authoritative: AuthoritativeContext{
			Status:           "BLOCKED",
			DecisionMaker:    "tmr_deterministic_readiness_engine",
			AIMayAlterStatus: false,
		},
		Findings: []Finding{
			{Consumer: ConsumerContext{ID: "alert", Name: "Alert"}},
			{Consumer: ConsumerContext{ID: "dashboard", Name: "Dashboard"}},
		},
	}
}

func TestExplanationProviderHelper(t *testing.T) {
	if os.Getenv("TMR_EXPLANATION_HELPER") != "1" {
		return
	}
	contents, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var request Request
	if err := json.Unmarshal(contents, &request); err != nil || request.Authoritative.Status != "BLOCKED" {
		fmt.Fprintln(os.Stderr, "invalid request")
		os.Exit(2)
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "success":
		fmt.Fprint(os.Stdout, `{"schemaVersion":"tmr-ai-explanation-response/v1alpha1","answer":"The critical alert still follows the legacy metric path.","priorities":[{"order":1,"consumerId":"alert","action":"Migrate the alert query","rationale":"It is a confirmed critical blocker."}]}`)
	case "status":
		fmt.Fprint(os.Stdout, `{"schemaVersion":"tmr-ai-explanation-response/v1alpha1","answer":"Everything is safe.","status":"READY"}`)
	case "unknown-consumer":
		fmt.Fprint(os.Stdout, `{"schemaVersion":"tmr-ai-explanation-response/v1alpha1","answer":"Review it.","priorities":[{"order":1,"consumerId":"invented","action":"Change it","rationale":"Because."}]}`)
	case "malformed":
		fmt.Fprint(os.Stdout, `{not-json`)
	case "slow":
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(os.Stdout, `{"schemaVersion":"tmr-ai-explanation-response/v1alpha1","answer":"late"}`)
	case "oversize":
		fmt.Fprint(os.Stdout, strings.Repeat("x", maxResponseBytes+1))
	case "failure":
		fmt.Fprint(os.Stderr, "token=provider-secret")
		os.Exit(9)
	default:
		fmt.Fprintln(os.Stderr, "unknown mode")
		os.Exit(2)
	}
	os.Exit(0)
}

func FuzzDecodeResponseDoesNotPanic(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":"tmr-ai-explanation-response/v1alpha1","answer":"ok"}`))
	f.Add([]byte(`{"status":"READY"}`))
	f.Fuzz(func(t *testing.T, contents []byte) {
		_, _ = decodeResponse(bytes.NewReader(contents))
	})
}
