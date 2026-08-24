package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ConfigAPIVersion = "tmr/v1alpha1"
	maxConfigBytes   = 1 << 20
)

// SourcePattern configures one local filesystem source.
type SourcePattern struct {
	Pattern  string `json:"pattern"`
	Required bool   `json:"required"`
}

// PersesUsageSource configures one optional Perses metrics-usage API.
// Secrets are referenced by environment-variable name and never stored here.
type PersesUsageSource struct {
	URL            string `json:"url"`
	Required       bool   `json:"required"`
	Timeout        string `json:"timeout"`
	BearerTokenEnv string `json:"bearerTokenEnv,omitempty"`
}

// Sources configures implemented local and optional remote consumer adapters.
type Sources struct {
	PrometheusRules []SourcePattern     `json:"prometheusRules,omitempty"`
	Grafana         []SourcePattern     `json:"grafana,omitempty"`
	Sloth           []SourcePattern     `json:"sloth,omitempty"`
	Pyrra           []SourcePattern     `json:"pyrra,omitempty"`
	PersesUsage     []PersesUsageSource `json:"persesUsage,omitempty"`
}

// AnalysisConfig controls graph behavior.
type AnalysisConfig struct {
	IncludeTransitiveDependencies bool   `json:"includeTransitiveDependencies"`
	UnresolvedReferencePolicy     string `json:"unresolvedReferencePolicy"`
}

// PolicyConfig controls deterministic readiness gates.
type PolicyConfig struct {
	FailOnCriticalLegacyConsumer bool   `json:"failOnCriticalLegacyConsumer"`
	FailOnCriticalUnknown        bool   `json:"failOnCriticalUnknown"`
	MinimumBlockingCriticality   string `json:"minimumBlockingCriticality"`
}

// OutputConfig selects report formats.
type OutputConfig struct {
	Formats []string `json:"formats"`
}

// CodeownersConfig controls repository-local GitHub CODEOWNERS discovery.
// Path is repository-relative; an empty path uses GitHub's documented search
// order: .github/CODEOWNERS, CODEOWNERS, then docs/CODEOWNERS.
type CodeownersConfig struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path,omitempty"`
}

// OwnershipMetadataSource identifies one advisory repository metadata file or
// glob. Ownership diagnostics are always non-blocking.
type OwnershipMetadataSource struct {
	Pattern string `json:"pattern"`
}

// OwnershipConfig controls advisory owner enrichment. Ownership evidence is
// deliberately separate from consumer sources and cannot affect readiness.
type OwnershipConfig struct {
	Enabled        bool                      `json:"enabled"`
	RepositoryRoot string                    `json:"repositoryRoot,omitempty"`
	Metadata       []OwnershipMetadataSource `json:"metadata,omitempty"`
	Codeowners     CodeownersConfig          `json:"codeowners"`
	DashboardTags  bool                      `json:"dashboardTags"`
}

// Config is the validated TMR analysis configuration.
type Config struct {
	APIVersion string          `json:"apiVersion"`
	Sources    Sources         `json:"sources"`
	Ownership  OwnershipConfig `json:"ownership,omitempty"`
	Analysis   AnalysisConfig  `json:"analysis"`
	Policy     PolicyConfig    `json:"policy"`
	Output     OutputConfig    `json:"output"`
}

type configDocument struct {
	APIVersion string             `yaml:"apiVersion"`
	Sources    sourcesDocument    `yaml:"sources"`
	Ownership  *ownershipDocument `yaml:"ownership"`
	Analysis   analysisDocument   `yaml:"analysis"`
	Policy     policyDocument     `yaml:"policy"`
	Output     outputDocument     `yaml:"output"`
}

type ownershipDocument struct {
	RepositoryRoot string                            `yaml:"repositoryRoot"`
	Metadata       []ownershipMetadataSourceDocument `yaml:"metadata"`
	Codeowners     *codeownersDocument               `yaml:"codeowners"`
	DashboardTags  *bool                             `yaml:"dashboardTags"`
}

type codeownersDocument struct {
	Enabled *bool  `yaml:"enabled"`
	Path    string `yaml:"path"`
}

type ownershipMetadataSourceDocument struct {
	Path string
}

func (source *ownershipMetadataSourceDocument) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		source.Path = node.Value
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("ownership metadata source must be a path string or mapping")
	}
	for index := 0; index < len(node.Content); index += 2 {
		key := node.Content[index].Value
		value := node.Content[index+1]
		switch key {
		case "path":
			if err := value.Decode(&source.Path); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown ownership metadata source field %q", key)
		}
	}
	return nil
}

type sourcesDocument struct {
	PrometheusRules []sourcePatternDocument     `yaml:"prometheusRules"`
	Grafana         []sourcePatternDocument     `yaml:"grafana"`
	Sloth           []sourcePatternDocument     `yaml:"sloth"`
	Pyrra           []sourcePatternDocument     `yaml:"pyrra"`
	PersesUsage     []persesUsageSourceDocument `yaml:"persesUsage"`
}

type persesUsageSourceDocument struct {
	URL            string `yaml:"url"`
	Required       *bool  `yaml:"required"`
	Timeout        string `yaml:"timeout"`
	BearerTokenEnv string `yaml:"bearerTokenEnv"`
}

type sourcePatternDocument struct {
	Path     string
	Required *bool
}

func (source *sourcePatternDocument) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		source.Path = node.Value
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("source must be a path string or mapping")
	}
	for index := 0; index < len(node.Content); index += 2 {
		key := node.Content[index].Value
		value := node.Content[index+1]
		switch key {
		case "path":
			if err := value.Decode(&source.Path); err != nil {
				return err
			}
		case "required":
			var required bool
			if err := value.Decode(&required); err != nil {
				return err
			}
			source.Required = &required
		default:
			return fmt.Errorf("unknown source field %q", key)
		}
	}
	return nil
}

type analysisDocument struct {
	IncludeTransitiveDependencies *bool  `yaml:"includeTransitiveDependencies"`
	UnresolvedReferencePolicy     string `yaml:"unresolvedReferencePolicy"`
}

type policyDocument struct {
	FailOnCriticalLegacyConsumer *bool  `yaml:"failOnCriticalLegacyConsumer"`
	FailOnCriticalUnknown        *bool  `yaml:"failOnCriticalUnknown"`
	MinimumBlockingCriticality   string `yaml:"minimumBlockingCriticality"`
}

type outputDocument struct {
	Formats []string `yaml:"formats"`
}

// LoadConfig reads one local TMR config file.
func LoadConfig(ctx context.Context, path string) (Config, error) {
	if err := ctx.Err(); err != nil {
		return Config{}, fmt.Errorf("load config %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer file.Close()

	result, err := ParseConfig(file)
	if err != nil {
		return Config{}, fmt.Errorf("load config %q: %w", path, err)
	}
	return result, nil
}

// ParseConfig strictly decodes and validates one TMR config document.
func ParseConfig(reader io.Reader) (Config, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxConfigBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if len(contents) > maxConfigBytes {
		return Config{}, fmt.Errorf("config exceeds the %d-byte size limit", maxConfigBytes)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	var document configDocument
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, fmt.Errorf("config is empty")
		}
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var additional any
	if err := decoder.Decode(&additional); err == nil {
		return Config{}, fmt.Errorf("config must contain exactly one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("decode trailing config document: %w", err)
	}

	result := normalizeConfig(document)
	if err := ValidateConfig(result); err != nil {
		return Config{}, err
	}
	return result, nil
}

// ValidateConfig validates a normalized TMR config.
func ValidateConfig(config Config) error {
	issues := &ValidationError{}
	if config.APIVersion != ConfigAPIVersion {
		issues.add("apiVersion", fmt.Sprintf("must be %q", ConfigAPIVersion))
	}

	totalSources := 0
	validateSourcePatterns := func(path string, patterns []SourcePattern) {
		totalSources += len(patterns)
		for index, pattern := range patterns {
			if isBlank(pattern.Pattern) {
				issues.add(fmt.Sprintf("%s[%d]", path, index), "path is required")
			}
		}
	}
	validateSourcePatterns("sources.prometheusRules", config.Sources.PrometheusRules)
	validateSourcePatterns("sources.grafana", config.Sources.Grafana)
	validateSourcePatterns("sources.sloth", config.Sources.Sloth)
	validateSourcePatterns("sources.pyrra", config.Sources.Pyrra)
	totalSources += len(config.Sources.PersesUsage)
	for index, source := range config.Sources.PersesUsage {
		path := fmt.Sprintf("sources.persesUsage[%d]", index)
		parsed, err := url.Parse(source.URL)
		if err != nil ||
			(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) ||
			parsed.Host == "" {
			issues.add(path+".url", "must be an absolute http or https URL")
		} else {
			if parsed.User != nil {
				issues.add(path+".url", "must not contain user information")
			}
			if parsed.RawQuery != "" || parsed.Fragment != "" {
				issues.add(path+".url", "must not contain a query or fragment")
			}
		}
		timeout, err := time.ParseDuration(source.Timeout)
		if err != nil || timeout <= 0 || timeout > 2*time.Minute {
			issues.add(path+".timeout", "must be a positive duration no greater than 2m")
		}
		if source.BearerTokenEnv != "" && !environmentNamePattern.MatchString(source.BearerTokenEnv) {
			issues.add(path+".bearerTokenEnv", "must be a valid environment variable name")
		}
	}
	if config.Ownership.Enabled {
		if isBlank(config.Ownership.RepositoryRoot) {
			issues.add("ownership.repositoryRoot", "is required when ownership discovery is enabled")
		}
		for index, source := range config.Ownership.Metadata {
			path := fmt.Sprintf("ownership.metadata[%d].path", index)
			if isBlank(source.Pattern) {
				issues.add(path, "is required")
			} else if !isRepositoryRelativePath(source.Pattern) {
				issues.add(path, "must be relative to ownership.repositoryRoot and must not escape it")
			}
		}
		if configuredPath := config.Ownership.Codeowners.Path; configuredPath != "" && !isRepositoryRelativePath(configuredPath) {
			issues.add("ownership.codeowners.path", "must be relative to ownership.repositoryRoot and must not escape it")
		}
	}
	if totalSources == 0 {
		issues.add("sources", "must configure at least one consumer source")
	}

	if config.Analysis.UnresolvedReferencePolicy != "warn" &&
		config.Analysis.UnresolvedReferencePolicy != "error" {
		issues.add("analysis.unresolvedReferencePolicy", `must be "warn" or "error"`)
	}
	switch config.Policy.MinimumBlockingCriticality {
	case "low", "medium", "high", "critical":
	default:
		issues.add("policy.minimumBlockingCriticality", "must be low, medium, high, or critical")
	}
	if len(config.Output.Formats) == 0 {
		issues.add("output.formats", "must contain at least one format")
	}
	for index, format := range config.Output.Formats {
		switch format {
		case "console", "json", "markdown":
		default:
			issues.add(fmt.Sprintf("output.formats[%d]", index), "must be console, json, or markdown")
		}
	}
	return issues.errOrNil()
}

func normalizeConfig(document configDocument) Config {
	transitive := true
	if document.Analysis.IncludeTransitiveDependencies != nil {
		transitive = *document.Analysis.IncludeTransitiveDependencies
	}
	unresolvedPolicy := document.Analysis.UnresolvedReferencePolicy
	if unresolvedPolicy == "" {
		unresolvedPolicy = "warn"
	}
	failLegacy := true
	if document.Policy.FailOnCriticalLegacyConsumer != nil {
		failLegacy = *document.Policy.FailOnCriticalLegacyConsumer
	}
	failUnknown := true
	if document.Policy.FailOnCriticalUnknown != nil {
		failUnknown = *document.Policy.FailOnCriticalUnknown
	}
	minimumCriticality := document.Policy.MinimumBlockingCriticality
	if minimumCriticality == "" {
		minimumCriticality = "high"
	}
	formats := document.Output.Formats
	if len(formats) == 0 {
		formats = []string{"console"}
	}

	return Config{
		APIVersion: document.APIVersion,
		Sources: Sources{
			PrometheusRules: normalizePatterns(document.Sources.PrometheusRules),
			Grafana:         normalizePatterns(document.Sources.Grafana),
			Sloth:           normalizePatterns(document.Sources.Sloth),
			Pyrra:           normalizePatterns(document.Sources.Pyrra),
			PersesUsage:     normalizePersesUsageSources(document.Sources.PersesUsage),
		},
		Ownership: normalizeOwnership(document.Ownership),
		Analysis: AnalysisConfig{
			IncludeTransitiveDependencies: transitive,
			UnresolvedReferencePolicy:     unresolvedPolicy,
		},
		Policy: PolicyConfig{
			FailOnCriticalLegacyConsumer: failLegacy,
			FailOnCriticalUnknown:        failUnknown,
			MinimumBlockingCriticality:   minimumCriticality,
		},
		Output: OutputConfig{Formats: formats},
	}
}

func normalizeOwnership(document *ownershipDocument) OwnershipConfig {
	if document == nil {
		return OwnershipConfig{}
	}
	codeownersEnabled := true
	codeownersPath := ""
	if document.Codeowners != nil {
		if document.Codeowners.Enabled != nil {
			codeownersEnabled = *document.Codeowners.Enabled
		}
		codeownersPath = filepath.ToSlash(strings.TrimSpace(document.Codeowners.Path))
	}
	dashboardTags := true
	if document.DashboardTags != nil {
		dashboardTags = *document.DashboardTags
	}
	repositoryRoot := strings.TrimSpace(document.RepositoryRoot)
	if repositoryRoot == "" {
		repositoryRoot = "."
	}
	return OwnershipConfig{
		Enabled:        true,
		RepositoryRoot: repositoryRoot,
		Metadata:       normalizeOwnershipMetadataSources(document.Metadata),
		Codeowners: CodeownersConfig{
			Enabled: codeownersEnabled,
			Path:    codeownersPath,
		},
		DashboardTags: dashboardTags,
	}
}

func normalizeOwnershipMetadataSources(documents []ownershipMetadataSourceDocument) []OwnershipMetadataSource {
	sources := make([]OwnershipMetadataSource, 0, len(documents))
	for _, document := range documents {
		sources = append(sources, OwnershipMetadataSource{Pattern: strings.TrimSpace(document.Path)})
	}
	return sources
}

func isRepositoryRelativePath(value string) bool {
	value = filepath.Clean(strings.TrimSpace(value))
	if value == "." || filepath.IsAbs(value) {
		return false
	}
	return value != ".." && !strings.HasPrefix(value, ".."+string(filepath.Separator))
}

var environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func normalizePersesUsageSources(documents []persesUsageSourceDocument) []PersesUsageSource {
	sources := make([]PersesUsageSource, 0, len(documents))
	for _, document := range documents {
		required := true
		if document.Required != nil {
			required = *document.Required
		}
		timeout := strings.TrimSpace(document.Timeout)
		if timeout == "" {
			timeout = "10s"
		}
		sources = append(sources, PersesUsageSource{
			URL:            strings.TrimRight(strings.TrimSpace(document.URL), "/"),
			Required:       required,
			Timeout:        timeout,
			BearerTokenEnv: strings.TrimSpace(document.BearerTokenEnv),
		})
	}
	return sources
}

func normalizePatterns(documents []sourcePatternDocument) []SourcePattern {
	patterns := make([]SourcePattern, 0, len(documents))
	for _, document := range documents {
		required := true
		if document.Required != nil {
			required = *document.Required
		}
		patterns = append(patterns, SourcePattern{Pattern: strings.TrimSpace(document.Path), Required: required})
	}
	return patterns
}
