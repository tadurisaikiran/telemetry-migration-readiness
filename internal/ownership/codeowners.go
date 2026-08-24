package ownership

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

const maxCodeownersBytes = 3 << 20

var (
	githubOwnerPattern = regexp.MustCompile(`^@[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})(?:/[A-Za-z0-9_.-]+)?$`)
	emailOwnerPattern  = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

type codeownersRule struct {
	pattern pathMatcher
	owners  []domain.Owner
	line    int
}

type parseIssue struct {
	line    int
	message string
}

func parseCodeowners(reader io.Reader) ([]codeownersRule, []parseIssue, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxCodeownersBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read CODEOWNERS: %w", err)
	}
	if len(contents) >= maxCodeownersBytes {
		return nil, nil, fmt.Errorf("CODEOWNERS must be smaller than %d bytes", maxCodeownersBytes)
	}

	var rules []codeownersRule
	var issues []parseIssue
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 64*1024), maxCodeownersBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		matcher, err := compilePathMatcher(fields[0])
		if err != nil {
			issues = append(issues, parseIssue{line: lineNumber, message: err.Error()})
			continue
		}
		var owners []domain.Owner
		valid := true
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "#") {
				break
			}
			owner, ok := parseCodeowner(field)
			if !ok {
				issues = append(issues, parseIssue{
					line:    lineNumber,
					message: fmt.Sprintf("invalid CODEOWNERS owner %q", field),
				})
				valid = false
				break
			}
			owners = appendUniqueOwner(owners, owner)
		}
		if valid {
			rules = append(rules, codeownersRule{pattern: matcher, owners: owners, line: lineNumber})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, issues, fmt.Errorf("scan CODEOWNERS: %w", err)
	}
	return rules, issues, nil
}

func parseCodeowner(value string) (domain.Owner, bool) {
	switch {
	case githubOwnerPattern.MatchString(value):
		return domain.Owner{Name: value}, true
	case emailOwnerPattern.MatchString(value):
		return domain.Owner{Name: value, Email: value}, true
	default:
		return domain.Owner{}, false
	}
}

func appendUniqueOwner(owners []domain.Owner, candidate domain.Owner) []domain.Owner {
	for _, owner := range owners {
		if owner.Name == candidate.Name && owner.Email == candidate.Email {
			return owners
		}
	}
	return append(owners, candidate)
}

func matchingCodeownersRule(rules []codeownersRule, repositoryPath string) (codeownersRule, bool) {
	var result codeownersRule
	matched := false
	for _, rule := range rules {
		if rule.pattern.Match(repositoryPath) {
			result = rule
			matched = true
		}
	}
	return result, matched
}
