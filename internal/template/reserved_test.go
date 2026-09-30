package template

import (
	"strings"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-01.task-7: the reserved
// template vocabulary.
//
// It proves the three ends of that vocabulary:
//
//   - ReservedNames() reports the reserved declared-name tokens, including the
//     new `raw` and `data` namespaces (requirements.constraint.reserved-template-names).
//   - The registry's reserved-name check (checkDeclaredName, reached through
//     Register -> checkDefinition) rejects a template that declares a field or a
//     section named `raw` or `data`, just as it always rejected body/notes/deck/
//     slide/item and the `_format` suffix.
//   - ContextKeys() enumerates the reserved render-context vocabulary and
//     includes the dotted `.raw` and `.data` namespaces (raw-source-context,
//     section-data-context); they are read as `.raw`/`.data` by design, and the
//     dynamic source/authoring views are not enumerable as leaf keys. `.item`
//     is intentionally NOT listed — it is an instance-local key, not a
//     top-level context namespace.
//
// Everything is in-process: no library, filesystem or watcher is touched, so
// the tests run under -short. It reuses the registry fixtures and helpers
// declared in registry_test.go (regSlide, regSection, buildAndValidate,
// assertStringSlice).

// TestReservedNames pins ReservedNames() to the exact reserved-name vocabulary.
// It is the single source of truth for the tokens checkDeclaredName enforces, so
// the exact, sorted set is asserted — not just membership of raw/data — and the
// result is checked to be a fresh allocation.
func TestReservedNames(t *testing.T) {
	got := ReservedNames()

	want := []string{"_format", "body", "data", "deck", "item", "notes", "raw", "slide"}
	assertStringSlice(t, "ReservedNames()", got, want)

	// The two source-context namespaces this change adds must be present under
	// their exact declared-name spellings.
	for _, name := range []string{"raw", "data"} {
		if !containsString(got, name) {
			t.Errorf("ReservedNames() = %v, want it to contain %q", got, name)
		}
	}

	// A fresh allocation: mutating a prior result does not change a later one.
	got[0] = "mutated"
	if again := ReservedNames(); again[0] != "_format" {
		t.Errorf("ReservedNames() after mutating a prior result = %v, want the vocabulary unchanged", again)
	}
}

// TestReservedDeclaredNamesRejected proves the registry rejects a field or a
// section named `raw` or `data`, the names this change adds to the reserved
// vocabulary, with the same "reserved and cannot be declared" error it reports
// for body/notes/deck/slide/item. Registration (Register -> checkDefinition ->
// checkDeclaredName) is the failing step.
func TestReservedDeclaredNamesRejected(t *testing.T) {
	tests := []struct {
		name string
		tmpl *Template
		want string
	}{
		{
			name: "reserved field name raw",
			tmpl: regSlide("resvraw", Field{Name: "raw", Type: FieldText}),
			want: `template "resvraw": field name "raw" is reserved and cannot be declared`,
		},
		{
			name: "reserved field name data",
			tmpl: regSlide("resvdata", Field{Name: "data", Type: FieldText}),
			want: `template "resvdata": field name "data" is reserved and cannot be declared`,
		},
		{
			name: "reserved section name raw",
			tmpl: func() *Template {
				tmpl := regSlide("resvrawsec")
				tmpl.Sections = []SectionDecl{{Name: "raw", Accepted: []string{"col"}}}
				return tmpl
			}(),
			want: `template "resvrawsec": section name "raw" is reserved and cannot be declared`,
		},
		{
			name: "reserved section name data",
			tmpl: func() *Template {
				tmpl := regSlide("resvdatasec")
				tmpl.Sections = []SectionDecl{{Name: "data", Accepted: []string{"col"}}}
				return tmpl
			}(),
			want: `template "resvdatasec": section name "data" is reserved and cannot be declared`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := buildAndValidate(tt.tmpl)
			if err == nil {
				t.Fatalf("buildAndValidate() = nil, want error %q", tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("buildAndValidate() error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

// TestReservedNamesAllRejected ties ReservedNames() to the check that enforces
// it: every token it reports is rejected when declared as a field name or as a
// section name. For an exact reserved name the error says the name is reserved;
// for the `_format` suffix token the trailing-suffix rule fires instead. This
// guards against the vocabulary list and checkDeclaredName drifting apart.
func TestReservedNamesAllRejected(t *testing.T) {
	for _, name := range ReservedNames() {
		t.Run("field "+name, func(t *testing.T) {
			err := buildAndValidate(regSlide("resv", Field{Name: name, Type: FieldText}))
			if err == nil || !strings.Contains(err.Error(), "reserved") {
				t.Errorf("buildAndValidate(field %q) error = %v, want a reserved-name rejection", name, err)
			}
		})
		t.Run("section "+name, func(t *testing.T) {
			tmpl := regSlide("resv")
			tmpl.Sections = []SectionDecl{{Name: name, Accepted: []string{"col"}}}
			err := buildAndValidate(tmpl)
			if err == nil || !strings.Contains(err.Error(), "reserved") {
				t.Errorf("buildAndValidate(section %q) error = %v, want a reserved-name rejection", name, err)
			}
		})
	}
}

// TestContextKeys pins ContextKeys() to the reserved render-context vocabulary:
// the fixed `.deck`/`.slide` leaf keys plus the dotted `.raw` and `.data`
// namespaces. It proves raw/data are reported in their dotted form (the form a
// layout reads), that no bare `raw`/`data` token leaks in, and that `.item` is
// deliberately absent. The result is checked to be a fresh allocation.
func TestContextKeys(t *testing.T) {
	got := ContextKeys()

	want := []string{
		".data",
		".raw",
		"deck.author",
		"deck.date",
		"deck.properties",
		"deck.title",
		"slide.number",
		"slide.total",
	}
	assertStringSlice(t, "ContextKeys()", got, want)

	// raw/data are context namespaces, read as `.raw`/`.data`.
	for _, key := range []string{".raw", ".data"} {
		if !containsString(got, key) {
			t.Errorf("ContextKeys() = %v, want it to contain %q", got, key)
		}
	}
	// The bare spellings are declared-name tokens (ReservedNames), not context
	// keys, and must not appear.
	for _, key := range []string{"raw", "data"} {
		if containsString(got, key) {
			t.Errorf("ContextKeys() = %v, want no bare %q key (the dotted form is the context key)", got, key)
		}
	}
	// `.item` is instance-local, not a top-level context namespace.
	for _, key := range []string{".item", "item"} {
		if containsString(got, key) {
			t.Errorf("ContextKeys() = %v, want no %q (`.item` is not a context namespace)", got, key)
		}
	}

	// A fresh allocation: mutating a prior result does not change a later one.
	got[0] = "mutated"
	if again := ContextKeys(); again[0] != ".data" {
		t.Errorf("ContextKeys() after mutating a prior result = %v, want the vocabulary unchanged", again)
	}
}
