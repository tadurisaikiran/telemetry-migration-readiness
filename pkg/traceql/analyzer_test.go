package traceql

import (
	"reflect"
	"testing"
)

func TestAnalyzeExtractsScopedAttributesAndIgnoresValuesAndIntrinsics(t *testing.T) {
	t.Parallel()

	analysis := Analyze(`{ span.http.request.method = "resource.fake" && resource.service.name = "checkout" && span:duration = 1s } | by(resource."deployment environment")`)
	want := []Reference{
		{Scope: ScopeResource, Name: "deployment environment"},
		{Scope: ScopeResource, Name: "service.name"},
		{Scope: ScopeSpan, Name: "http.request.method"},
	}
	if !reflect.DeepEqual(analysis.References, want) {
		t.Fatalf("references = %#v, want %#v", analysis.References, want)
	}
	if len(analysis.Unresolved) != 0 {
		t.Fatalf("unresolved = %#v", analysis.Unresolved)
	}
}

func TestAnalyzeMarksDynamicAndUnsupportedScopesUnresolved(t *testing.T) {
	t.Parallel()

	analysis := Analyze(`{ .http.method = "GET" && parent.span.http.route = "/" && event.exception.type = "x" && span.${attribute} = "y" }`)
	if len(analysis.References) != 0 {
		t.Fatalf("references = %#v", analysis.References)
	}
	if got, want := len(analysis.Unresolved), 4; got != want {
		t.Fatalf("unresolved = %#v, want %d reasons", analysis.Unresolved, want)
	}
}

func TestAnalyzeDeduplicatesAndSortsReferences(t *testing.T) {
	t.Parallel()

	analysis := Analyze(`{ span.z = 1 || resource.a = 2 || span.z = 3 }`)
	want := []Reference{{Scope: ScopeResource, Name: "a"}, {Scope: ScopeSpan, Name: "z"}}
	if !reflect.DeepEqual(analysis.References, want) {
		t.Fatalf("references = %#v, want %#v", analysis.References, want)
	}
}

func FuzzAnalyzeDoesNotPanic(f *testing.F) {
	f.Add(`{ span.http.request.method = "GET" && resource.service.name = "checkout" }`)
	f.Add(`{ span."quoted attribute" = ` + "`raw`" + ` }`)
	f.Fuzz(func(t *testing.T, expression string) {
		_ = Analyze(expression)
	})
}
