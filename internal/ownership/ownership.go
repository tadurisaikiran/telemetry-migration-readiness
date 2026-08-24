// Package ownership enriches normalized consumers with advisory ownership
// evidence. It cannot classify consumers or produce a readiness status.
package ownership

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/config"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	filesource "github.com/tadurisaikiran/telemetry-migration-readiness/internal/source"
)

const (
	MetadataSourceKey     = "ownership.source"
	MetadataConfidenceKey = "ownership.confidence"
	MetadataRuleKey       = "ownership.rule"
	MetadataCandidatesKey = "ownership.candidates"
	MetadataAmbiguousKey  = "ownership.ambiguous"
	MetadataUnassignedKey = "ownership.unassigned"
	DashboardTagsKey      = "dashboard_tags"
)

const (
	sourceExplicitMetadata = "tmr_metadata"
	sourceCodeowners       = "github_codeowners"
	sourceDashboardTags    = "grafana_tags"
	sourceAdapter          = "adapter"
)

var codeownersSearchOrder = []string{
	".github/CODEOWNERS",
	"CODEOWNERS",
	"docs/CODEOWNERS",
}

type evidence struct {
	owner      *domain.Owner
	candidates []string
	source     string
	confidence domain.Confidence
	rule       string
	ambiguous  bool
	unassigned bool
	rank       int
}

type codeownersEvidence struct {
	path  string
	rules []codeownersRule
}

// Enrich adds deterministic ownership evidence to every discovered consumer.
// Load and parse failures become advisory diagnostics. Ownership itself never
// changes unresolved state or readiness.
func Enrich(ctx context.Context, configuration config.OwnershipConfig, discovery *domain.Discovery) error {
	if discovery == nil {
		return nil
	}
	if !configuration.Enabled {
		for index := range discovery.Consumers {
			delete(discovery.Consumers[index].Metadata, DashboardTagsKey)
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	repositoryRoot, err := filepath.Abs(configuration.RepositoryRoot)
	if err != nil {
		discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
			"ownership", configuration.RepositoryRoot, 0, err.Error(),
		))
		return nil
	}
	repositoryRoot = filepath.Clean(repositoryRoot)

	metadataRules := loadMetadataRules(ctx, repositoryRoot, configuration.Metadata, discovery)
	codeowners := loadCodeowners(ctx, repositoryRoot, configuration.Codeowners, discovery)
	for index := range discovery.Consumers {
		if err := ctx.Err(); err != nil {
			return err
		}
		consumer := &discovery.Consumers[index]
		repositoryPath := repositoryRelativePath(repositoryRoot, consumer.Source.File)
		tags := dashboardTags(*consumer)

		selected := existingOwnerEvidence(*consumer)
		if configuration.DashboardTags {
			selected = chooseEvidence(selected, tagEvidence(tags))
		}
		if codeowners != nil && repositoryPath != "" {
			if rule, matched := matchingCodeownersRule(codeowners.rules, repositoryPath); matched {
				selected = chooseEvidence(selected, evidenceFromCodeowners(codeowners.path, rule))
			}
		}
		for _, rule := range metadataRules {
			if rule.matches(*consumer, repositoryPath, tags) {
				owner := rule.owner
				selected = evidence{
					owner:      &owner,
					source:     sourceExplicitMetadata,
					confidence: domain.ConfidenceConfirmed,
					rule:       rule.source + "#" + rule.id,
					rank:       4,
				}
			}
		}
		applyEvidence(consumer, selected)
	}
	return nil
}

func loadMetadataRules(
	ctx context.Context,
	repositoryRoot string,
	patterns []config.OwnershipMetadataSource,
	discovery *domain.Discovery,
) []metadataRule {
	var rules []metadataRule
	loaded := make(map[string]struct{})
	for _, pattern := range patterns {
		if ctx.Err() != nil {
			return rules
		}
		rootedPattern := filepath.Join(repositoryRoot, filepath.FromSlash(pattern.Pattern))
		files, err := filesource.Expand(rootedPattern)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
				"ownership_metadata", rootedPattern, 0, err.Error(),
			))
			continue
		}
		if len(files) == 0 {
			discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
				"ownership_metadata", rootedPattern, 0, "source pattern matched no files",
			))
			continue
		}
		for _, path := range files {
			if _, exists := loaded[path]; exists {
				continue
			}
			loaded[path] = struct{}{}
			if err := ensureRepositoryFile(repositoryRoot, path); err != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
					"ownership_metadata", path, 0, err.Error(),
				))
				continue
			}
			file, err := os.Open(path)
			if err != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
					"ownership_metadata", path, 0, err.Error(),
				))
				continue
			}
			repositoryPath := repositoryRelativePath(repositoryRoot, path)
			additional, parseErr := parseMetadata(repositoryPath, file)
			closeErr := file.Close()
			if parseErr != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
					"ownership_metadata", path, 0, parseErr.Error(),
				))
				continue
			}
			if closeErr != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
					"ownership_metadata", path, 0, closeErr.Error(),
				))
				continue
			}
			rules = append(rules, additional...)
		}
	}
	return rules
}

func loadCodeowners(
	ctx context.Context,
	repositoryRoot string,
	configuration config.CodeownersConfig,
	discovery *domain.Discovery,
) *codeownersEvidence {
	if !configuration.Enabled || ctx.Err() != nil {
		return nil
	}
	path, exists, err := findCodeowners(repositoryRoot, configuration.Path)
	if err != nil {
		discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
			"codeowners", path, 0, err.Error(),
		))
		return nil
	}
	if !exists {
		if configuration.Path != "" {
			discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
				"codeowners", path, 0, "CODEOWNERS file was not found",
			))
		}
		return nil
	}
	if err := ensureRepositoryFile(repositoryRoot, path); err != nil {
		discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
			"codeowners", path, 0, err.Error(),
		))
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
			"codeowners", path, 0, err.Error(),
		))
		return nil
	}
	rules, issues, parseErr := parseCodeowners(file)
	closeErr := file.Close()
	if parseErr != nil {
		discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
			"codeowners", path, 0, parseErr.Error(),
		))
		return nil
	}
	if closeErr != nil {
		discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
			"codeowners", path, 0, closeErr.Error(),
		))
		return nil
	}
	for _, issue := range issues {
		discovery.Diagnostics = append(discovery.Diagnostics, ownershipDiagnostic(
			"codeowners", path, issue.line, issue.message,
		))
	}
	return &codeownersEvidence{path: repositoryRelativePath(repositoryRoot, path), rules: rules}
}

func findCodeowners(repositoryRoot, configuredPath string) (string, bool, error) {
	if configuredPath != "" {
		path := filepath.Join(repositoryRoot, filepath.FromSlash(configuredPath))
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return path, false, nil
			}
			return path, false, err
		}
		if info.IsDir() {
			return path, false, fmt.Errorf("configured CODEOWNERS path is a directory")
		}
		return path, true, nil
	}
	for _, candidate := range codeownersSearchOrder {
		path := filepath.Join(repositoryRoot, filepath.FromSlash(candidate))
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return path, false, err
		}
		if info.IsDir() {
			return path, false, fmt.Errorf("CODEOWNERS candidate is a directory")
		}
		return path, true, nil
	}
	return filepath.Join(repositoryRoot, ".github", "CODEOWNERS"), false, nil
}

func evidenceFromCodeowners(path string, rule codeownersRule) evidence {
	names := make([]string, 0, len(rule.owners))
	for _, owner := range rule.owners {
		names = append(names, owner.Name)
	}
	result := evidence{
		candidates: names,
		source:     sourceCodeowners,
		confidence: domain.ConfidenceHigh,
		rule:       fmt.Sprintf("%s:%d %s", path, rule.line, rule.pattern.pattern),
		unassigned: len(rule.owners) == 0,
		rank:       2,
	}
	switch len(rule.owners) {
	case 0:
	case 1:
		owner := rule.owners[0]
		result.owner = &owner
	default:
		result.owner = &domain.Owner{Name: strings.Join(names, ", ")}
	}
	return result
}

func tagEvidence(tags []string) evidence {
	owners := make(map[string]struct{})
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		lower := strings.ToLower(trimmed)
		for _, prefix := range []string{"team:", "owner:", "owned-by:"} {
			if strings.HasPrefix(lower, prefix) {
				name := strings.TrimSpace(trimmed[len(prefix):])
				if name != "" {
					owners[name] = struct{}{}
				}
				break
			}
		}
	}
	if len(owners) == 0 {
		return evidence{}
	}
	candidates := make([]string, 0, len(owners))
	for owner := range owners {
		candidates = append(candidates, owner)
	}
	sort.Strings(candidates)
	result := evidence{
		candidates: candidates,
		source:     sourceDashboardTags,
		confidence: domain.ConfidenceMedium,
		rule:       strings.Join(candidates, ", "),
		ambiguous:  len(candidates) > 1,
		rank:       1,
	}
	if len(candidates) == 1 {
		result.owner = &domain.Owner{Name: candidates[0]}
	}
	return result
}

func existingOwnerEvidence(consumer domain.Consumer) evidence {
	if consumer.Owner == nil {
		return evidence{}
	}
	owner := *consumer.Owner
	return evidence{
		owner:      &owner,
		source:     sourceAdapter,
		confidence: domain.ConfidenceConfirmed,
		rank:       3,
	}
}

func chooseEvidence(current, candidate evidence) evidence {
	if candidate.rank > current.rank {
		return candidate
	}
	return current
}

func applyEvidence(consumer *domain.Consumer, selected evidence) {
	if consumer.Metadata == nil {
		consumer.Metadata = make(map[string]string)
	}
	for _, key := range []string{
		MetadataSourceKey,
		MetadataConfidenceKey,
		MetadataRuleKey,
		MetadataCandidatesKey,
		MetadataAmbiguousKey,
		MetadataUnassignedKey,
	} {
		delete(consumer.Metadata, key)
	}
	if selected.rank == 0 {
		return
	}
	consumer.Owner = selected.owner
	consumer.Metadata[MetadataSourceKey] = selected.source
	consumer.Metadata[MetadataConfidenceKey] = string(selected.confidence)
	if selected.rule != "" {
		consumer.Metadata[MetadataRuleKey] = selected.rule
	}
	if len(selected.candidates) > 1 || selected.ambiguous {
		encoded, _ := json.Marshal(selected.candidates)
		consumer.Metadata[MetadataCandidatesKey] = string(encoded)
	}
	if selected.ambiguous {
		consumer.Metadata[MetadataAmbiguousKey] = "true"
	}
	if selected.unassigned {
		consumer.Metadata[MetadataUnassignedKey] = "true"
	}
}

// Candidates returns stable, validated ownership candidates stored by Enrich.
func Candidates(consumer domain.Consumer) []string {
	if consumer.Metadata == nil {
		return nil
	}
	var candidates []string
	if err := json.Unmarshal([]byte(consumer.Metadata[MetadataCandidatesKey]), &candidates); err != nil {
		return nil
	}
	unique := make(map[string]struct{}, len(candidates))
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := unique[candidate]; exists {
			continue
		}
		unique[candidate] = struct{}{}
		result = append(result, candidate)
	}
	sort.Strings(result)
	return result
}

// Ambiguous reports whether deterministic evidence found competing owners.
func Ambiguous(consumer domain.Consumer) bool {
	return metadataBool(consumer, MetadataAmbiguousKey)
}

// Unassigned reports whether a matching CODEOWNERS rule intentionally clears
// ownership for the consumer path.
func Unassigned(consumer domain.Consumer) bool {
	return metadataBool(consumer, MetadataUnassignedKey)
}

func metadataBool(consumer domain.Consumer, key string) bool {
	value, err := strconv.ParseBool(consumer.Metadata[key])
	return err == nil && value
}

func dashboardTags(consumer domain.Consumer) []string {
	if consumer.Metadata == nil || consumer.Metadata[DashboardTagsKey] == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(consumer.Metadata[DashboardTagsKey]), &tags); err != nil {
		return nil
	}
	result := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}

func repositoryRelativePath(repositoryRoot, source string) string {
	if strings.TrimSpace(source) == "" {
		return ""
	}
	absolute := source
	if !filepath.IsAbs(absolute) {
		var err error
		absolute, err = filepath.Abs(source)
		if err != nil {
			return ""
		}
	}
	relative, err := filepath.Rel(repositoryRoot, filepath.Clean(absolute))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(relative)
}

func ensureRepositoryFile(repositoryRoot, path string) error {
	resolvedRoot, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve repository ownership path: %w", err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil {
		return fmt.Errorf("resolve repository-relative ownership path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("ownership path resolves outside repository root")
	}
	return nil
}

func ownershipDiagnostic(adapter, path string, line int, message string) domain.Diagnostic {
	return domain.Diagnostic{
		Adapter: adapter,
		Source: domain.SourceLocation{
			File: path,
			Line: line,
		},
		Message: message,
	}
}
