package cli

// This file is the CLI half of plan.phase-05.task-8's integration test: the
// schema-documentation vocabulary internal/template.FieldLimits/FieldFormats/
// SectionRepeats/BodyLimits is shared by `kalide templates <name>`
// (internal/cli, indented text) and the /templates gallery (internal/server,
// HTML tables). The gallery half lives in
// internal/server/gallery_doc_int_test.go.
//
// Both halves load the SAME on-disk fixture library
// (internal/template/testdata/schemadoc) — one slide template carrying a
// bounded-repeats section, non-enum fields with limits and formats, and a
// limited body — and assert their surface prints the strings the shared
// vocabulary renders for it. Deriving the expected strings by calling those
// same helpers is what makes a single edit to the vocabulary show up in both
// renders: each helper's new output becomes the expected string in the CLI
// text test and the gallery HTML test alike, and a surface that stopped routing
// through the shared helper (an inlined duplicate renderer) would print the old
// string and fail.
//
// The fixture deliberately carries no enum field: an enum's variants are
// presented differently per surface by design (the CLI's separate
// printTemplateEnumVariants line versus the gallery's "one of …" limits cell),
// so only the shared non-enum vocabulary is compared.

import (
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
)

// schemadocFixtureDir is the shared on-disk library both documentation tests
// load; schemadocTemplateName is the one probe template inside it.
const (
	schemadocFixtureDir   = "../template/testdata/schemadoc"
	schemadocTemplateName = "schemadoc"
)

// TestTemplatesShowSharesSchemaDocVocabularyWithGallery proves `kalide
// templates <name>` prints exactly the schema-documentation strings the shared
// internal/template vocabulary renders for the one fixture template, the same
// strings the gallery half asserts on the served HTML. It runs the real
// command over a real on-disk library, so it is skipped under -short.
func TestTemplatesShowSharesSchemaDocVocabularyWithGallery(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test runs `kalide templates` over a real on-disk library")
	}

	t.Chdir(schemadocFixtureDir)

	code, stdout, stderr := runCLI("templates", schemadocTemplateName)
	if code != 0 {
		t.Fatalf("Run(templates %s) exit = %d, want 0 (stderr = %q)", schemadocTemplateName, code, stderr)
	}
	if stderr != "" {
		t.Fatalf("Run(templates %s) stderr = %q, want empty", schemadocTemplateName, stderr)
	}

	reg := mustFixtureRegistry(t)
	tmpl, ok := reg.Lookup(schemadocTemplateName)
	if !ok || tmpl == nil {
		t.Fatalf("registry.Lookup(%q) = _, false, want the shared fixture template", schemadocTemplateName)
	}

	for _, want := range schemaDocStrings(t, tmpl) {
		if !strings.Contains(stdout, want) {
			t.Errorf("`kalide templates %s` does not print the shared schema-doc string %q:\n%s",
				schemadocTemplateName, want, stdout)
		}
	}
}

// schemaDocStrings returns the schema-documentation strings the shared
// vocabulary (internal/template) renders for tmpl, derived by calling the very
// helpers both surfaces use. It fails when the fixture no longer carries one of
// the rules, so a removed rule cannot make an assertion vacuously pass on the
// empty string. It is duplicated in internal/server so each package's test can
// derive the same expected strings from the loaded schema.
func schemaDocStrings(t *testing.T, tmpl *template.Template) []string {
	t.Helper()

	field := func(name string) *template.Field {
		for i := range tmpl.Fields {
			if tmpl.Fields[i].Name == name {
				return &tmpl.Fields[i]
			}
		}
		t.Fatalf("template %q declares no field %q", tmpl.Name, name)
		return nil
	}
	section := func(name string) *template.SectionDecl {
		for i := range tmpl.Sections {
			if tmpl.Sections[i].Name == name {
				return &tmpl.Sections[i]
			}
		}
		t.Fatalf("template %q declares no section %q", tmpl.Name, name)
		return nil
	}

	want := []struct {
		what  string
		value string
	}{
		{"heading FieldLimits", template.FieldLimits(field("heading"))},
		{"ratio FieldLimits", template.FieldLimits(field("ratio"))},
		{"ratio FieldFormats", template.FieldFormats(field("ratio"))},
		{"items SectionRepeats", template.SectionRepeats(section("items"))},
		{"body BodyLimits", template.BodyLimits(tmpl.Body)},
	}
	out := make([]string, 0, len(want))
	for _, w := range want {
		if w.value == "" {
			t.Fatalf("shared schema-doc %s is empty; the fixture must carry that rule", w.what)
		}
		out = append(out, w.value)
	}
	return out
}
