package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/readiness"
)

func TestRunValidateSuccess(t *testing.T) {
	t.Parallel()

	manifest := filepath.Join("..", "..", "internal", "config", "testdata", "valid", "all-change-kinds.yaml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run(context.Background(), []string{"validate", "--migration", manifest}, &stdout, &stderr)
	if got, want := exitCode, 0; got != want {
		t.Fatalf("exit code = %d, want %d; stderr = %q", got, want, stderr.String())
	}
	if got, want := stdout.String(), "Migration manifest is valid.\nChanges: 4\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Errorf("stderr = %q, want empty", got)
	}
}

func TestRunValidateFailure(t *testing.T) {
	t.Parallel()

	manifest := filepath.Join("..", "..", "internal", "config", "testdata", "invalid", "validation-errors.yaml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run(context.Background(), []string{"validate", "--migration", manifest}, &stdout, &stderr)
	if got, want := exitCode, 1; got != want {
		t.Fatalf("exit code = %d, want %d", got, want)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}
	if got := stderr.String(); !strings.Contains(got, "metadata.name: is required") {
		t.Errorf("stderr = %q, want validation error", got)
	}
}

func TestRunValidateRequiresMigrationFlag(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"validate"}, &stdout, &stderr)

	if got, want := exitCode, 1; got != want {
		t.Fatalf("exit code = %d, want %d", got, want)
	}
	if got, want := stderr.String(), "--migration, --weaver-diff with --weaver-mapping, or --config is required\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestRunValidateWeaverSuccess(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), []string{
		"validate",
		"--weaver-diff", filepath.Join("..", "..", "adapters", "weaver", "testdata", "diff-v2.json"),
		"--weaver-mapping", filepath.Join("..", "..", "adapters", "weaver", "testdata", "mapping.yaml"),
	}, &stdout, &stderr)
	if got, want := exitCode, 0; got != want {
		t.Fatalf("exit code = %d, want %d; stderr = %q", got, want, stderr.String())
	}
	if got, want := stdout.String(), "Weaver diff and mapping are valid.\nChanges: 2\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunValidateWeaverMissingMappingIsIncomplete(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), []string{
		"validate",
		"--weaver-diff", filepath.Join("..", "..", "adapters", "weaver", "testdata", "diff-v2.json"),
		"--weaver-mapping", filepath.Join("..", "..", "adapters", "weaver", "testdata", "mapping-incomplete.yaml"),
	}, &stdout, &stderr)
	if got, want := exitCode, 3; got != want {
		t.Fatalf("exit code = %d, want %d; stderr = %q", got, want, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "requiresMapping=true") {
		t.Fatalf("stderr = %q, want mapping requirement", got)
	}
}

func TestRunValidateWeaverUnsupportedChangeIsIncomplete(t *testing.T) {
	t.Parallel()

	diffPath := filepath.Join(t.TempDir(), "updated.json")
	diff := `{
  "head":{"semconv_version":"v2"},
  "baseline":{"semconv_version":"v1"},
  "changes":{"registry_attributes":[],"metrics":[{"type":"updated"}],"events":[],"spans":[],"entities":[]}
}`
	if err := os.WriteFile(diffPath, []byte(diff), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), []string{
		"validate",
		"--weaver-diff", diffPath,
		"--weaver-mapping", filepath.Join("..", "..", "adapters", "weaver", "testdata", "mapping.yaml"),
	}, &stdout, &stderr)
	if got, want := exitCode, 3; got != want {
		t.Fatalf("exit code = %d, want %d; stderr = %q", got, want, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "field-level mapping information is unavailable") {
		t.Fatalf("stderr = %q, want unsupported-change evidence", got)
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"bogus"}, &stdout, &stderr)

	if got, want := exitCode, 1; got != want {
		t.Fatalf("exit code = %d, want %d", got, want)
	}
	if got := stderr.String(); !strings.Contains(got, `unknown command "bogus"`) {
		t.Errorf("stderr = %q, want unknown-command error", got)
	}
}

func TestAnalyzeCLIContract(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	command := exec.Command(os.Args[0], "-test.run=TestCLIHelperProcess", "--",
		"analyze",
		"--config", "examples/checkout-migration/tmr.yaml",
		"--migration", "examples/checkout-migration/migration.yaml",
		"--format", "json",
	)
	command.Dir = root
	command.Env = append(os.Environ(), "TMR_CLI_HELPER=1")
	output, err := command.Output()
	if err == nil {
		t.Fatal("analyze succeeded, want blocked exit code 2")
	}
	var exitError *exec.ExitError
	if !errorsAs(err, &exitError) || exitError.ExitCode() != 2 {
		t.Fatalf("analyze error = %v, want exit code 2", err)
	}
	var result readiness.Result
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode report: %v\n%s", err, output)
	}
	if result.SchemaVersion != readiness.ResultSchemaVersion || result.Summary.Status != readiness.StatusBlocked {
		t.Fatalf("result schema/status = %q/%q", result.SchemaVersion, result.Summary.Status)
	}
}

func TestAnalyzeWeaverCLIContract(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	command := exec.Command(os.Args[0], "-test.run=TestCLIHelperProcess", "--",
		"analyze",
		"--config", "examples/checkout-migration/tmr.yaml",
		"--weaver-diff", "adapters/weaver/testdata/diff-v2.json",
		"--weaver-mapping", "adapters/weaver/testdata/mapping-checkout.yaml",
		"--format", "json",
	)
	command.Dir = root
	command.Env = append(os.Environ(), "TMR_CLI_HELPER=1")
	output, err := command.Output()
	if err == nil {
		t.Fatal("analyze succeeded, want blocked exit code 2")
	}
	var exitError *exec.ExitError
	if !errorsAs(err, &exitError) || exitError.ExitCode() != 2 {
		t.Fatalf("analyze error = %v, want exit code 2", err)
	}
	var result readiness.Result
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode report: %v\n%s", err, output)
	}
	if result.Summary.Status != readiness.StatusBlocked {
		t.Fatalf("result status = %q, want BLOCKED", result.Summary.Status)
	}
	if got := result.Migration.Changes[0].Metadata["source.adapter"]; got != "weaver" {
		t.Fatalf("source adapter metadata = %q, want weaver", got)
	}
}

func TestCheckoutReportGolden(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	command := exec.Command(os.Args[0], "-test.run=TestCLIHelperProcess", "--",
		"analyze",
		"--config", "examples/checkout-migration/tmr.yaml",
		"--migration", "examples/checkout-migration/migration.yaml",
		"--format", "json",
	)
	command.Dir = root
	command.Env = append(os.Environ(), "TMR_CLI_HELPER=1")
	actual, err := command.Output()
	if exitError, ok := err.(*exec.ExitError); !ok || exitError.ExitCode() != 2 {
		t.Fatalf("analyze error = %v, want blocked exit code 2", err)
	}
	expected, err := os.ReadFile(filepath.Join(root, "examples", "checkout-migration", "expected", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("report differs from examples/checkout-migration/expected/report.json")
	}
}

func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("TMR_CLI_HELPER") != "1" {
		return
	}
	separator := 0
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index + 1
			break
		}
	}
	os.Exit(run(context.Background(), os.Args[separator:], os.Stdout, os.Stderr))
}

func errorsAs(err error, target **exec.ExitError) bool {
	exitError, ok := err.(*exec.ExitError)
	if ok {
		*target = exitError
	}
	return ok
}

func TestReadinessExitCodesArePermanentContract(t *testing.T) {
	t.Parallel()
	for status, expected := range map[readiness.Status]int{
		readiness.StatusReady:      0,
		readiness.StatusBlocked:    2,
		readiness.StatusIncomplete: 3,
		readiness.StatusError:      1,
	} {
		t.Run(string(status), func(t *testing.T) {
			if actual := readinessExitCode(status); actual != expected {
				t.Fatalf("readinessExitCode(%s) = %s, want %d", status, strconv.Itoa(actual), expected)
			}
		})
	}
}
