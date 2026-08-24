// Package traceql conservatively extracts scoped trace-attribute references
// after an expression has been accepted by Tempo's official parser.
package traceql

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Scope identifies the two OpenTelemetry attribute scopes supported by the
// trace milestone.
type Scope string

const (
	ScopeSpan     Scope = "span"
	ScopeResource Scope = "resource"
)

// Reference is one exact scoped attribute found in a validated expression.
type Reference struct {
	Scope Scope
	Name  string
}

// Analysis contains exact references and conservative unresolved reasons.
type Analysis struct {
	References []Reference
	Unresolved []string
}

// Analyze extracts span and resource attributes. It deliberately does not
// claim to validate TraceQL grammar: callers must first submit the expression
// to a configured Tempo Search API, which owns the official parser.
func Analyze(expression string) Analysis {
	references := make(map[Reference]struct{})
	unresolved := make(map[string]struct{})
	for index := 0; index < len(expression); {
		character := expression[index]
		if character == '"' || character == '`' || character == '\'' {
			next, valid := skipQuoted(expression, index, character)
			if !valid {
				unresolved["unterminated quoted value"] = struct{}{}
				break
			}
			index = next
			continue
		}

		matched := false
		for _, candidate := range []struct {
			prefix string
			scope  Scope
		}{
			{prefix: "span.", scope: ScopeSpan},
			{prefix: "resource.", scope: ScopeResource},
		} {
			if !hasTokenPrefix(expression, index, candidate.prefix) {
				continue
			}
			name, next, valid := readAttribute(expression, index+len(candidate.prefix))
			if !valid || name == "" {
				unresolved["invalid "+string(candidate.scope)+" attribute"] = struct{}{}
			} else if containsTemplate(name) {
				unresolved["templated "+string(candidate.scope)+" attribute "+name] = struct{}{}
			} else {
				references[Reference{Scope: candidate.scope, Name: name}] = struct{}{}
			}
			index = next
			matched = true
			break
		}
		if matched {
			continue
		}

		for _, prefix := range []string{"parent.", "event.", "link.", "instrumentation."} {
			if !hasTokenPrefix(expression, index, prefix) {
				continue
			}
			name, next, _ := readAttribute(expression, index+len(prefix))
			unresolved["unsupported TraceQL attribute scope "+strings.TrimSuffix(prefix, ".")+"."+name] = struct{}{}
			index = next
			matched = true
			break
		}
		if matched {
			continue
		}

		if character == '.' && tokenBoundaryBefore(expression, index) {
			name, next, _ := readAttribute(expression, index+1)
			unresolved["unscoped TraceQL attribute ."+name] = struct{}{}
			index = next
			continue
		}
		if character == '$' {
			unresolved["templated TraceQL expression"] = struct{}{}
		}
		_, size := utf8.DecodeRuneInString(expression[index:])
		if size == 0 {
			size = 1
		}
		index += size
	}

	result := Analysis{
		References: make([]Reference, 0, len(references)),
		Unresolved: make([]string, 0, len(unresolved)),
	}
	for reference := range references {
		result.References = append(result.References, reference)
	}
	for reason := range unresolved {
		result.Unresolved = append(result.Unresolved, reason)
	}
	sort.Slice(result.References, func(i, j int) bool {
		if result.References[i].Scope != result.References[j].Scope {
			return result.References[i].Scope < result.References[j].Scope
		}
		return result.References[i].Name < result.References[j].Name
	})
	sort.Strings(result.Unresolved)
	return result
}

func hasTokenPrefix(expression string, index int, prefix string) bool {
	return tokenBoundaryBefore(expression, index) && strings.HasPrefix(expression[index:], prefix)
}

func tokenBoundaryBefore(expression string, index int) bool {
	if index == 0 {
		return true
	}
	previous, _ := utf8.DecodeLastRuneInString(expression[:index])
	return !isIdentifierRune(previous)
}

func isIdentifierRune(character rune) bool {
	return character == '_' || character == '.' || character == '$' ||
		character >= '0' && character <= '9' ||
		character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' || character >= utf8.RuneSelf
}

func readAttribute(expression string, index int) (string, int, bool) {
	var result strings.Builder
	for index < len(expression) {
		character, size := utf8.DecodeRuneInString(expression[index:])
		if character == '"' {
			value, next, valid := readQuotedAttribute(expression, index)
			if !valid {
				return result.String(), len(expression), false
			}
			result.WriteString(value)
			index = next
			continue
		}
		if isAttributeTerminator(character) {
			break
		}
		result.WriteRune(character)
		index += size
	}
	return result.String(), index, true
}

func readQuotedAttribute(expression string, index int) (string, int, bool) {
	var result strings.Builder
	index++
	for index < len(expression) {
		character, size := utf8.DecodeRuneInString(expression[index:])
		if character == '"' {
			return result.String(), index + size, true
		}
		if character == '\\' {
			if index+size >= len(expression) {
				return result.String(), len(expression), false
			}
			next, nextSize := utf8.DecodeRuneInString(expression[index+size:])
			if next != '\\' && next != '"' {
				return result.String(), len(expression), false
			}
			result.WriteRune(next)
			index += size + nextSize
			continue
		}
		result.WriteRune(character)
		index += size
	}
	return result.String(), len(expression), false
}

func skipQuoted(expression string, index int, quote byte) (int, bool) {
	index++
	for index < len(expression) {
		if expression[index] == quote {
			return index + 1, true
		}
		if expression[index] == '\\' && quote != '`' {
			index += 2
			continue
		}
		_, size := utf8.DecodeRuneInString(expression[index:])
		index += size
	}
	return len(expression), false
}

func isAttributeTerminator(character rune) bool {
	if character == ' ' || character == '\t' || character == '\r' || character == '\n' {
		return true
	}
	return strings.ContainsRune("{}()=~!<>&|^,", character)
}

func containsTemplate(value string) bool {
	return strings.Contains(value, "$")
}
