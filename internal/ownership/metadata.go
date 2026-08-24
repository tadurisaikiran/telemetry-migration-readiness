package ownership

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"gopkg.in/yaml.v3"
)

const (
	MetadataAPIVersion = "tmr.ownership/v1alpha1"
	MetadataKind       = "Ownership"
	maxMetadataBytes   = 1 << 20
)

type metadataDocument struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Spec       struct {
		Rules []metadataRuleDocument `yaml:"rules"`
	} `yaml:"spec"`
}

type metadataRuleDocument struct {
	ID    string                `yaml:"id"`
	Match metadataMatchDocument `yaml:"match"`
	Owner struct {
		Name  string `yaml:"name"`
		Email string `yaml:"email"`
	} `yaml:"owner"`
}

type metadataMatchDocument struct {
	Path         string `yaml:"path"`
	ConsumerID   string `yaml:"consumerId"`
	ConsumerKind string `yaml:"consumerKind"`
	DashboardTag string `yaml:"dashboardTag"`
}

type metadataRule struct {
	id           string
	path         *pathMatcher
	consumerID   string
	consumerKind domain.ConsumerKind
	dashboardTag string
	owner        domain.Owner
	source       string
}

func parseMetadata(source string, reader io.Reader) ([]metadataRule, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxMetadataBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read ownership metadata: %w", err)
	}
	if len(contents) > maxMetadataBytes {
		return nil, fmt.Errorf("ownership metadata exceeds the %d-byte size limit", maxMetadataBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	var document metadataDocument
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("ownership metadata is empty")
		}
		return nil, fmt.Errorf("decode ownership metadata: %w", err)
	}
	var additional any
	if err := decoder.Decode(&additional); err == nil {
		return nil, fmt.Errorf("ownership metadata must contain exactly one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode trailing ownership metadata: %w", err)
	}
	if document.APIVersion != MetadataAPIVersion {
		return nil, fmt.Errorf("apiVersion must be %q", MetadataAPIVersion)
	}
	if document.Kind != MetadataKind {
		return nil, fmt.Errorf("kind must be %q", MetadataKind)
	}
	if len(document.Spec.Rules) == 0 {
		return nil, fmt.Errorf("spec.rules must contain at least one rule")
	}

	ids := make(map[string]struct{}, len(document.Spec.Rules))
	rules := make([]metadataRule, 0, len(document.Spec.Rules))
	for index, candidate := range document.Spec.Rules {
		prefix := fmt.Sprintf("spec.rules[%d]", index)
		candidate.ID = strings.TrimSpace(candidate.ID)
		if candidate.ID == "" {
			return nil, fmt.Errorf("%s.id is required", prefix)
		}
		if _, exists := ids[candidate.ID]; exists {
			return nil, fmt.Errorf("%s.id %q is duplicated", prefix, candidate.ID)
		}
		ids[candidate.ID] = struct{}{}
		match := candidate.Match
		match.Path = strings.TrimSpace(match.Path)
		match.ConsumerID = strings.TrimSpace(match.ConsumerID)
		match.ConsumerKind = strings.TrimSpace(match.ConsumerKind)
		match.DashboardTag = strings.TrimSpace(match.DashboardTag)
		if match.Path == "" && match.ConsumerID == "" && match.ConsumerKind == "" && match.DashboardTag == "" {
			return nil, fmt.Errorf("%s.match must configure at least one selector", prefix)
		}
		var matcher *pathMatcher
		if match.Path != "" {
			compiled, err := compilePathMatcher(match.Path)
			if err != nil {
				return nil, fmt.Errorf("%s.match.path: %w", prefix, err)
			}
			matcher = &compiled
		}
		kind := domain.ConsumerKind(match.ConsumerKind)
		if kind != "" && !validConsumerKind(kind) {
			return nil, fmt.Errorf("%s.match.consumerKind %q is not supported", prefix, match.ConsumerKind)
		}
		owner := domain.Owner{
			Name:  strings.TrimSpace(candidate.Owner.Name),
			Email: strings.TrimSpace(candidate.Owner.Email),
		}
		if owner.Name == "" {
			return nil, fmt.Errorf("%s.owner.name is required", prefix)
		}
		rules = append(rules, metadataRule{
			id:           candidate.ID,
			path:         matcher,
			consumerID:   match.ConsumerID,
			consumerKind: kind,
			dashboardTag: match.DashboardTag,
			owner:        owner,
			source:       source,
		})
	}
	return rules, nil
}

func validConsumerKind(kind domain.ConsumerKind) bool {
	switch kind {
	case domain.ConsumerKindDashboard,
		domain.ConsumerKindDashboardPanel,
		domain.ConsumerKindAlertRule,
		domain.ConsumerKindRecordingRule,
		domain.ConsumerKindSLO,
		domain.ConsumerKindCollector,
		domain.ConsumerKindQuery,
		domain.ConsumerKindSourceCode,
		domain.ConsumerKindRunbook:
		return true
	default:
		return false
	}
}

func (rule metadataRule) matches(consumer domain.Consumer, repositoryPath string, dashboardTags []string) bool {
	if rule.path != nil && !rule.path.Match(repositoryPath) {
		return false
	}
	if rule.consumerID != "" && rule.consumerID != consumer.ID {
		return false
	}
	if rule.consumerKind != "" && rule.consumerKind != consumer.Kind {
		return false
	}
	if rule.dashboardTag != "" && !containsExact(dashboardTags, rule.dashboardTag) {
		return false
	}
	return true
}

func containsExact(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
