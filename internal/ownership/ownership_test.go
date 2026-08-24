package ownership

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/config"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

func TestEnrichUsesDeterministicOwnershipPrecedence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, ".github", "CODEOWNERS"), `
* @global-owner
/dashboards/ @dashboard-owner
/dashboards/checkout.json @checkout-codeowner @observability
`)
	writeFixture(t, filepath.Join(root, ".tmr", "ownership.yaml"), `
apiVersion: tmr.ownership/v1alpha1
kind: Ownership
spec:
  rules:
    - id: checkout-default
      match:
        path: /dashboards/checkout.json
      owner:
        name: Superseded Owner
    - id: checkout-explicit
      match:
        path: /dashboards/checkout.json
        consumerKind: dashboard_panel
      owner:
        name: Checkout Platform
        email: checkout@example.com
`)

	discovery := domain.Discovery{Consumers: []domain.Consumer{
		{
			ID:     "checkout",
			Kind:   domain.ConsumerKindDashboardPanel,
			Source: domain.SourceLocation{File: filepath.Join(root, "dashboards", "checkout.json")},
			Metadata: map[string]string{
				DashboardTagsKey: `["owner:Tag Owner","team:Tag Team"]`,
			},
		},
		{
			ID:     "codeowned",
			Kind:   domain.ConsumerKindDashboardPanel,
			Source: domain.SourceLocation{File: filepath.Join(root, "dashboards", "other.json")},
			Metadata: map[string]string{
				DashboardTagsKey: `["team:Tag Team"]`,
			},
		},
		{
			ID:     "tagged",
			Kind:   domain.ConsumerKindDashboard,
			Source: domain.SourceLocation{URL: "https://grafana.example.test/d/tagged"},
			Metadata: map[string]string{
				DashboardTagsKey: `["owner:Runtime"]`,
			},
		},
		{
			ID:     "ambiguous",
			Kind:   domain.ConsumerKindDashboard,
			Source: domain.SourceLocation{URL: "https://grafana.example.test/d/ambiguous"},
			Metadata: map[string]string{
				DashboardTagsKey: `["team:Payments","owner:Checkout","team:Payments"]`,
			},
		},
	}}
	configuration := config.OwnershipConfig{
		Enabled:        true,
		RepositoryRoot: root,
		Metadata: []config.OwnershipMetadataSource{{
			Pattern: ".tmr/ownership.yaml",
		}},
		Codeowners:    config.CodeownersConfig{Enabled: true},
		DashboardTags: true,
	}
	if err := Enrich(context.Background(), configuration, &discovery); err != nil {
		t.Fatal(err)
	}
	if len(discovery.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", discovery.Diagnostics)
	}

	checkout := discovery.Consumers[0]
	if checkout.Owner == nil || checkout.Owner.Name != "Checkout Platform" || checkout.Owner.Email != "checkout@example.com" {
		t.Fatalf("explicit owner = %#v", checkout.Owner)
	}
	if got := checkout.Metadata[MetadataSourceKey]; got != sourceExplicitMetadata {
		t.Fatalf("explicit source = %q", got)
	}
	if got := checkout.Metadata[MetadataConfidenceKey]; got != string(domain.ConfidenceConfirmed) {
		t.Fatalf("explicit confidence = %q", got)
	}

	codeowned := discovery.Consumers[1]
	if codeowned.Owner == nil || codeowned.Owner.Name != "@dashboard-owner" {
		t.Fatalf("CODEOWNERS owner = %#v", codeowned.Owner)
	}
	if got := codeowned.Metadata[MetadataSourceKey]; got != sourceCodeowners {
		t.Fatalf("CODEOWNERS source = %q", got)
	}

	tagged := discovery.Consumers[2]
	if tagged.Owner == nil || tagged.Owner.Name != "Runtime" {
		t.Fatalf("tag owner = %#v", tagged.Owner)
	}
	if got := tagged.Metadata[MetadataConfidenceKey]; got != string(domain.ConfidenceMedium) {
		t.Fatalf("tag confidence = %q", got)
	}

	ambiguous := discovery.Consumers[3]
	if ambiguous.Owner != nil || !Ambiguous(ambiguous) {
		t.Fatalf("ambiguous owner = %#v metadata = %#v", ambiguous.Owner, ambiguous.Metadata)
	}
	if got, want := Candidates(ambiguous), []string{"Checkout", "Payments"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
}

func TestCodeownersSearchOrderAndLastMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "CODEOWNERS"), `* @root-owner`)
	writeFixture(t, filepath.Join(root, "docs", "CODEOWNERS"), `* @docs-owner`)
	writeFixture(t, filepath.Join(root, ".github", "CODEOWNERS"), `
* @global-owner
*.go @go-owner
/internal/ownership/ @ownership-owner
/internal/ownership/file.go @last-owner
`)
	discovery := domain.Discovery{Consumers: []domain.Consumer{{
		ID:     "source",
		Kind:   domain.ConsumerKindSourceCode,
		Source: domain.SourceLocation{File: filepath.Join(root, "internal", "ownership", "file.go")},
	}}}
	if err := Enrich(context.Background(), config.OwnershipConfig{
		Enabled:        true,
		RepositoryRoot: root,
		Codeowners:     config.CodeownersConfig{Enabled: true},
	}, &discovery); err != nil {
		t.Fatal(err)
	}
	owner := discovery.Consumers[0].Owner
	if owner == nil || owner.Name != "@last-owner" {
		t.Fatalf("owner = %#v", owner)
	}
	if rule := discovery.Consumers[0].Metadata[MetadataRuleKey]; !strings.Contains(rule, ".github/CODEOWNERS:4") {
		t.Fatalf("rule = %q", rule)
	}
}

func TestInvalidOwnershipEvidenceProducesAdvisoryDiagnostics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "CODEOWNERS"), `
!secret/** @invalid-negation
[ab].go @invalid-range
*.go not-an-owner
*.yaml @valid-owner
`)
	writeFixture(t, filepath.Join(root, ".tmr", "ownership.yaml"), `apiVersion: wrong
kind: Ownership
spec: {rules: []}
`)
	discovery := domain.Discovery{}
	if err := Enrich(context.Background(), config.OwnershipConfig{
		Enabled:        true,
		RepositoryRoot: root,
		Metadata: []config.OwnershipMetadataSource{{
			Pattern: ".tmr/ownership.yaml",
		}},
		Codeowners: config.CodeownersConfig{Enabled: true},
	}, &discovery); err != nil {
		t.Fatal(err)
	}
	if got, want := len(discovery.Diagnostics), 4; got != want {
		t.Fatalf("diagnostics = %d, want %d: %#v", got, want, discovery.Diagnostics)
	}
	for _, diagnostic := range discovery.Diagnostics {
		if diagnostic.Required {
			t.Fatalf("blocking diagnostic = %#v", diagnostic)
		}
	}
}

func TestCodeownersMultipleOwnersAreJointNotAmbiguous(t *testing.T) {
	t.Parallel()

	rules, issues, err := parseCodeowners(strings.NewReader(`*.go @platform ops@example.com @platform # owners`))
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 || len(rules) != 1 {
		t.Fatalf("rules = %#v issues = %#v", rules, issues)
	}
	evidence := evidenceFromCodeowners("CODEOWNERS", rules[0])
	if evidence.owner == nil || evidence.owner.Name != "@platform, ops@example.com" || evidence.ambiguous {
		t.Fatalf("evidence = %#v", evidence)
	}
}

func TestEmptyCodeownersRuleClearsTagFallback(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "CODEOWNERS"), `
* @global
/apps/ @apps
/apps/github
`)
	consumer := domain.Consumer{
		ID:     "github-app",
		Source: domain.SourceLocation{File: filepath.Join(root, "apps", "github", "main.go")},
		Metadata: map[string]string{
			DashboardTagsKey: `["team:Tag Fallback"]`,
		},
	}
	discovery := domain.Discovery{Consumers: []domain.Consumer{consumer}}
	if err := Enrich(context.Background(), config.OwnershipConfig{
		Enabled:        true,
		RepositoryRoot: root,
		Codeowners:     config.CodeownersConfig{Enabled: true},
		DashboardTags:  true,
	}, &discovery); err != nil {
		t.Fatal(err)
	}
	got := discovery.Consumers[0]
	if got.Owner != nil || !Unassigned(got) || got.Metadata[MetadataSourceKey] != sourceCodeowners {
		t.Fatalf("consumer = %#v", got)
	}
}

func TestEnrichIsDeterministic(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "CODEOWNERS"), `* @owner`)
	configuration := config.OwnershipConfig{
		Enabled:        true,
		RepositoryRoot: root,
		Codeowners:     config.CodeownersConfig{Enabled: true},
	}
	base := domain.Discovery{Consumers: []domain.Consumer{{
		ID:       "one",
		Source:   domain.SourceLocation{File: filepath.Join(root, "one.yaml")},
		Metadata: map[string]string{},
	}}}
	first := cloneDiscovery(base)
	second := cloneDiscovery(base)
	if err := Enrich(context.Background(), configuration, &first); err != nil {
		t.Fatal(err)
	}
	if err := Enrich(context.Background(), configuration, &second); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("enrichment is not deterministic:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func TestDisabledOwnershipDoesNotExposeAdapterTagState(t *testing.T) {
	t.Parallel()

	discovery := domain.Discovery{Consumers: []domain.Consumer{{
		ID:       "dashboard",
		Metadata: map[string]string{DashboardTagsKey: `["team:Checkout"]`},
	}}}
	if err := Enrich(context.Background(), config.OwnershipConfig{}, &discovery); err != nil {
		t.Fatal(err)
	}
	if _, exists := discovery.Consumers[0].Metadata[DashboardTagsKey]; exists {
		t.Fatalf("disabled ownership leaked dashboard tags: %#v", discovery.Consumers[0].Metadata)
	}
}

func TestOwnershipFileSymlinkCannotEscapeRepository(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "CODEOWNERS")
	writeFixture(t, outside, `* @outside-owner`)
	link := filepath.Join(root, "CODEOWNERS")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	discovery := domain.Discovery{Consumers: []domain.Consumer{{
		ID:     "file",
		Source: domain.SourceLocation{File: filepath.Join(root, "file.go")},
	}}}
	if err := Enrich(context.Background(), config.OwnershipConfig{
		Enabled:        true,
		RepositoryRoot: root,
		Codeowners:     config.CodeownersConfig{Enabled: true},
	}, &discovery); err != nil {
		t.Fatal(err)
	}
	if discovery.Consumers[0].Owner != nil {
		t.Fatalf("escaped CODEOWNERS assigned owner %#v", discovery.Consumers[0].Owner)
	}
	if len(discovery.Diagnostics) != 1 || !strings.Contains(discovery.Diagnostics[0].Message, "outside repository root") {
		t.Fatalf("diagnostics = %#v", discovery.Diagnostics)
	}
}

func TestParseMetadataRejectsUnknownFieldsAndSelectors(t *testing.T) {
	t.Parallel()

	for _, document := range []string{
		`apiVersion: tmr.ownership/v1alpha1
kind: Ownership
surprise: true
spec: {rules: []}
`,
		`apiVersion: tmr.ownership/v1alpha1
kind: Ownership
spec:
  rules:
    - id: no-selector
      match: {}
      owner: {name: Nobody}
`,
	} {
		if _, err := parseMetadata("ownership.yaml", strings.NewReader(document)); err == nil {
			t.Fatalf("parseMetadata() accepted:\n%s", document)
		}
	}
}

func TestPathMatcherDocumentedSubset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		path    string
		match   bool
	}{
		{pattern: "*", path: "nested/file.go", match: true},
		{pattern: "*.go", path: "nested/file.go", match: true},
		{pattern: "/docs/", path: "docs/nested/file.md", match: true},
		{pattern: "/docs/", path: "nested/docs/file.md", match: false},
		{pattern: "docs/*", path: "docs/file.md", match: true},
		{pattern: "docs/*", path: "docs/nested/file.md", match: false},
		{pattern: "/apps/github", path: "apps/github/workflow.yaml", match: true},
		{pattern: "**/logs", path: "deeply/nested/logs/output.txt", match: true},
		{pattern: "internal/**/file.go", path: "internal/file.go", match: true},
		{pattern: "internal/**/file.go", path: "internal/a/b/file.go", match: true},
		{pattern: "*.go", path: "file.yaml", match: false},
	}
	for _, test := range tests {
		matcher, err := compilePathMatcher(test.pattern)
		if err != nil {
			t.Fatalf("compilePathMatcher(%q): %v", test.pattern, err)
		}
		if got := matcher.Match(test.path); got != test.match {
			t.Errorf("%q matches %q = %t, want %t", test.pattern, test.path, got, test.match)
		}
	}
	for _, invalid := range []string{"", "!secret", "[ab].go", `\\#literal`, "a/***/b"} {
		if _, err := compilePathMatcher(invalid); err == nil {
			t.Errorf("compilePathMatcher(%q) error = nil", invalid)
		}
	}
}

func FuzzParseCodeowners(f *testing.F) {
	f.Add("*.go @platform\n/docs/ docs@example.com")
	f.Add("!secret/** @owner")
	f.Fuzz(func(t *testing.T, input string) {
		_, _, _ = parseCodeowners(strings.NewReader(input))
	})
}

func FuzzParseMetadata(f *testing.F) {
	f.Add(`apiVersion: tmr.ownership/v1alpha1
kind: Ownership
spec:
  rules:
    - id: all
      match: {path: "**/*.yaml"}
      owner: {name: Platform}
`)
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = parseMetadata("fuzz.yaml", strings.NewReader(input))
	})
}

func writeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cloneDiscovery(discovery domain.Discovery) domain.Discovery {
	clone := discovery
	clone.Consumers = append([]domain.Consumer(nil), discovery.Consumers...)
	for index := range clone.Consumers {
		metadata := make(map[string]string, len(clone.Consumers[index].Metadata))
		for key, value := range clone.Consumers[index].Metadata {
			metadata[key] = value
		}
		clone.Consumers[index].Metadata = metadata
	}
	return clone
}
