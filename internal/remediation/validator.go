package remediation

import (
	"context"
	"fmt"
	"sort"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/explanation"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/impact"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/readiness"
	tmrpromql "github.com/tadurisaikiran/telemetry-migration-readiness/pkg/promql"
)

type Validation struct {
	PromQLParsed       bool                     `json:"promqlParsed"`
	ArtifactParsed     bool                     `json:"artifactParsed"`
	LegacyRemoved      bool                     `json:"legacyReferenceRemoved"`
	DestinationPresent bool                     `json:"destinationReferencePresent"`
	GraphReanalyzed    bool                     `json:"graphReanalyzed"`
	CurrentStatus      readiness.Status         `json:"currentStatus"`
	SimulatedStatus    readiness.Status         `json:"simulatedStatus"`
	SimulatedClass     readiness.Classification `json:"simulatedClassification"`
}

type ValidatedCandidate struct {
	ID               string                `json:"id"`
	TargetID         string                `json:"targetId"`
	ChangeID         string                `json:"changeId"`
	ConsumerID       string                `json:"consumerId"`
	ConsumerName     string                `json:"consumerName"`
	ArtifactKind     ArtifactKind          `json:"artifactKind"`
	Source           domain.SourceLocation `json:"source"`
	Locator          Locator               `json:"locator"`
	BeforeExpression string                `json:"beforeExpression"`
	AfterExpression  string                `json:"afterExpression"`
	Rationale        string                `json:"rationale"`
	Validation       Validation            `json:"validation"`
}

func Validate(
	ctx context.Context,
	request Request,
	response Response,
	migration domain.Migration,
	original domain.Discovery,
	policy readiness.Policy,
) ([]ValidatedCandidate, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	if err := validateResponse(response, request); err != nil {
		return nil, err
	}
	targets := make(map[string]Target, len(request.Targets))
	for _, target := range request.Targets {
		targets[target.ID] = target
	}
	validated := make([]ValidatedCandidate, 0, len(response.Candidates))
	for _, candidate := range response.Candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		target := targets[candidate.TargetID]
		result, err := validateCandidate(ctx, request, candidate, target, migration, original, policy)
		if err != nil {
			return nil, fmt.Errorf("candidate %q: %w", candidate.ID, err)
		}
		validated = append(validated, result)
	}
	sort.Slice(validated, func(i, j int) bool {
		left, right := targets[validated[i].TargetID], targets[validated[j].TargetID]
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return validated[i].ID < validated[j].ID
	})
	return validated, nil
}

func validateCandidate(
	ctx context.Context,
	request Request,
	candidate Candidate,
	target Target,
	migration domain.Migration,
	original domain.Discovery,
	policy readiness.Policy,
) (ValidatedCandidate, error) {
	if err := validateTargetProvenance(target, migration, original); err != nil {
		return ValidatedCandidate{}, err
	}
	if explanation.Redact(candidate.AfterExpression) != candidate.AfterExpression {
		return ValidatedCandidate{}, fmt.Errorf("replacement expression contains secret-like text")
	}
	analysis, err := tmrpromql.Analyze(candidate.AfterExpression)
	if err != nil {
		return ValidatedCandidate{}, fmt.Errorf("replacement PromQL is invalid: %w", err)
	}
	if len(analysis.Unresolved) != 0 {
		return ValidatedCandidate{}, fmt.Errorf("replacement PromQL remains unresolved: %s", analysis.Unresolved[0].Reason)
	}
	if hasDirectReference(analysis.References, target.From) {
		return ValidatedCandidate{}, fmt.Errorf("replacement still references legacy symbol %q", target.From.Name)
	}
	if !hasDirectReference(analysis.References, target.To) {
		return ValidatedCandidate{}, fmt.Errorf("replacement does not reference destination symbol %q", target.To.Name)
	}

	contents, locator, err := loadAndPatchArtifact(target, candidate.AfterExpression)
	if err != nil {
		return ValidatedCandidate{}, err
	}
	candidateDiscovery, err := parseCandidateArtifact(ctx, target, contents)
	if err != nil {
		return ValidatedCandidate{}, err
	}
	candidateConsumer, exists := consumerByID(candidateDiscovery, target.ConsumerID)
	if !exists {
		return ValidatedCandidate{}, fmt.Errorf("candidate artifact no longer contains consumer %q", target.ConsumerID)
	}
	if candidateConsumer.Expression != candidate.AfterExpression {
		return ValidatedCandidate{}, fmt.Errorf("candidate artifact expression = %q, want exact replacement", candidateConsumer.Expression)
	}
	candidateReferences := referencesByConsumer(candidateDiscovery, target.ConsumerID)
	if hasDirectReference(candidateReferences, target.From) {
		return ValidatedCandidate{}, fmt.Errorf("candidate artifact still contains the legacy reference")
	}
	if !hasDirectReference(candidateReferences, target.To) {
		return ValidatedCandidate{}, fmt.Errorf("candidate artifact does not contain the destination reference")
	}

	simulatedDiscovery := replaceSourceDiscovery(original, candidateDiscovery, target.Source.File)
	simulatedGraph, err := impact.BuildGraph(simulatedDiscovery)
	if err != nil {
		return ValidatedCandidate{}, fmt.Errorf("rebuild candidate dependency graph: %w", err)
	}
	simulated, err := readiness.Evaluate(migration, simulatedDiscovery, simulatedGraph, policy)
	if err != nil {
		return ValidatedCandidate{}, fmt.Errorf("reanalyze candidate readiness: %w", err)
	}
	classification, exists := classificationFor(simulated, target.ChangeID, target.ConsumerID)
	if !exists {
		return ValidatedCandidate{}, fmt.Errorf("candidate reanalysis lost consumer %q", target.ConsumerID)
	}
	if classification == readiness.ClassificationLegacyOnly || classification == readiness.ClassificationUncertain {
		return ValidatedCandidate{}, fmt.Errorf("candidate reanalysis classification remains %s", classification)
	}

	return ValidatedCandidate{
		ID:               explanation.Redact(candidate.ID),
		TargetID:         target.ID,
		ChangeID:         target.ChangeID,
		ConsumerID:       target.ConsumerID,
		ConsumerName:     target.ConsumerName,
		ArtifactKind:     target.ArtifactKind,
		Source:           target.Source,
		Locator:          locator,
		BeforeExpression: target.BeforeExpression,
		AfterExpression:  candidate.AfterExpression,
		Rationale:        explanation.Redact(candidate.Rationale),
		Validation: Validation{
			PromQLParsed:       true,
			ArtifactParsed:     true,
			LegacyRemoved:      true,
			DestinationPresent: true,
			GraphReanalyzed:    true,
			CurrentStatus:      request.Authoritative.Status,
			SimulatedStatus:    simulated.Summary.Status,
			SimulatedClass:     classification,
		},
	}, nil
}

func validateTargetProvenance(target Target, migration domain.Migration, original domain.Discovery) error {
	var matchedChange *domain.Change
	for index := range migration.Changes {
		if migration.Changes[index].ID == target.ChangeID {
			matchedChange = &migration.Changes[index]
			break
		}
	}
	if matchedChange == nil || matchedChange.To == nil || matchedChange.From != target.From || *matchedChange.To != target.To {
		return fmt.Errorf("target does not match the canonical migration change")
	}
	consumer, exists := consumerByID(original, target.ConsumerID)
	if !exists {
		return fmt.Errorf("target consumer %q is absent from deterministic discovery", target.ConsumerID)
	}
	artifactKind, supported := supportedArtifact(consumer)
	if !supported || artifactKind != target.ArtifactKind ||
		consumer.Source.File != target.Source.File || consumer.Expression != target.BeforeExpression {
		return fmt.Errorf("target does not match deterministic consumer source evidence")
	}
	if !hasDirectReference(referencesByConsumer(original, target.ConsumerID), target.From) {
		return fmt.Errorf("target consumer has no confirmed direct legacy reference")
	}
	return nil
}

func consumerByID(discovery domain.Discovery, consumerID string) (domain.Consumer, bool) {
	for _, consumer := range discovery.Consumers {
		if consumer.ID == consumerID {
			return consumer, true
		}
	}
	return domain.Consumer{}, false
}

func referencesByConsumer(discovery domain.Discovery, consumerID string) []domain.Reference {
	var references []domain.Reference
	for _, reference := range discovery.References {
		if reference.ConsumerID == consumerID {
			references = append(references, reference)
		}
	}
	return references
}

func replaceSourceDiscovery(original, candidate domain.Discovery, source string) domain.Discovery {
	replacedIDs := make(map[string]struct{})
	var result domain.Discovery
	for _, consumer := range original.Consumers {
		if consumer.Source.File == source {
			replacedIDs[consumer.ID] = struct{}{}
			continue
		}
		result.Consumers = append(result.Consumers, consumer)
	}
	for _, reference := range original.References {
		if _, replaced := replacedIDs[reference.ConsumerID]; !replaced {
			result.References = append(result.References, reference)
		}
	}
	for _, production := range original.Productions {
		if _, replaced := replacedIDs[production.ConsumerID]; !replaced {
			result.Productions = append(result.Productions, production)
		}
	}
	for _, diagnostic := range original.Diagnostics {
		if diagnostic.Source.File != source {
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}
	}
	result.Append(candidate)
	return result
}

func classificationFor(result readiness.Result, changeID, consumerID string) (readiness.Classification, bool) {
	for _, change := range result.Changes {
		if change.Change.ID != changeID {
			continue
		}
		for _, consumer := range change.Consumers {
			if consumer.Consumer.ID == consumerID {
				return consumer.Classification, true
			}
		}
	}
	return "", false
}
