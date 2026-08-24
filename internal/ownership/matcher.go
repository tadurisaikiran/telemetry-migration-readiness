package ownership

import (
	"fmt"
	"regexp"
	"strings"
)

// pathMatcher implements the documented CODEOWNERS wildcard subset used by
// TMR. Unsupported gitignore constructs are rejected instead of being treated
// as evidence for an owner.
type pathMatcher struct {
	pattern string
	regexp  *regexp.Regexp
}

func compilePathMatcher(pattern string) (pathMatcher, error) {
	original := pattern
	if pattern == "" {
		return pathMatcher{}, fmt.Errorf("pattern is empty")
	}
	if strings.HasPrefix(pattern, "!") {
		return pathMatcher{}, fmt.Errorf("negated patterns are not supported by CODEOWNERS")
	}
	if strings.ContainsAny(pattern, "[]\\\x00") {
		return pathMatcher{}, fmt.Errorf("pattern uses an unsupported CODEOWNERS construct")
	}
	if strings.Contains(pattern, "//") {
		return pathMatcher{}, fmt.Errorf("pattern contains an empty path segment")
	}
	if strings.Contains(pattern, "***") {
		return pathMatcher{}, fmt.Errorf("pattern contains more than two consecutive stars")
	}

	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	directoryOnly := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "" {
		return pathMatcher{}, fmt.Errorf("pattern does not identify a repository path")
	}
	hasSlash := strings.Contains(pattern, "/")

	var expression strings.Builder
	expression.WriteString("^")
	if !hasSlash && !anchored {
		expression.WriteString("(.*/)?")
	}
	for index := 0; index < len(pattern); {
		switch pattern[index] {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				index += 2
				if index < len(pattern) && pattern[index] == '/' {
					expression.WriteString("(.*/)?")
					index++
				} else {
					expression.WriteString(".*")
				}
				continue
			}
			expression.WriteString("[^/]*")
		case '?':
			expression.WriteString("[^/]")
		default:
			expression.WriteString(regexp.QuoteMeta(string(pattern[index])))
		}
		index++
	}
	lastSegment := pattern
	if separator := strings.LastIndex(pattern, "/"); separator >= 0 {
		lastSegment = pattern[separator+1:]
	}
	if directoryOnly || !hasSlash || !strings.ContainsAny(lastSegment, "*?") {
		expression.WriteString("(/.*)?")
	}
	expression.WriteString("$")
	compiled, err := regexp.Compile(expression.String())
	if err != nil {
		return pathMatcher{}, fmt.Errorf("compile pattern %q: %w", original, err)
	}
	return pathMatcher{pattern: original, regexp: compiled}, nil
}

func (matcher pathMatcher) Match(repositoryPath string) bool {
	repositoryPath = strings.TrimPrefix(strings.ReplaceAll(repositoryPath, "\\", "/"), "./")
	return repositoryPath != "" && matcher.regexp.MatchString(repositoryPath)
}
