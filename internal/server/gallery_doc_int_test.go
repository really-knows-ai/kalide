package server

// This file is the gallery half of plan.phase-05.task-8's integration test: the
// schema-documentation vocabulary internal/template.FieldLimits/FieldFormats/
// SectionRepeats/BodyLimits is shared by `kalide templates <name>`
// (internal/cli, indented text) and the /templates gallery (internal/server,
// HTML tables). The CLI half lives in
// internal/cli/templates_doc_int_test.go.
//
// Both halves load the SAME on-disk fixture library
// (internal/template/testdata/schemadoc) — one slide template carrying a
// bounded-repeats section, non-enum fields with limits and formats, and a
// limited body — and assert their surface prints the strings the shared
// vocabulary renders for it. Deriving the expected strings by calling those
// same helpers is what makes a single edit to the vocabulary show up in both
// renders: each helper's new output becomes the expected string in the gallery
// HTML test and the CLI text test alike, and a surface that stopped routing
// through the shared helper (an inlined duplicate renderer) would print the old
// string and fail.
//
// The fixture deliberately carries no enum field: an enum's variants are
// presented differently per surface by design (the CLI's separate
// printTemplateEnumVariants line versus the gallery's "one of …" limits cell),
// so only the shared non-enum vocabulary is compared.

import (
	"net/http"
	"net/http/httptest"
	"os"
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

// TestGallerySharesSchemaDocVocabularyWithCLI proves the served /templates
// gallery HTML prints exactly the schema-documentation strings the shared
// internal/template vocabulary renders for the one fixture template, the same
// strings the CLI half asserts on `kalide templates schemadoc`. It loads a real
// on-disk library and serves galleryHandler, so it is skipped under -short.
func TestGallerySharesSchemaDocVocabularyWithCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test serves the gallery over a real on-disk library")
	}

	lib, err := template.LoadLibrary(os.DirFS(schemadocFixtureDir), template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	tmpl, ok := reg.Lookup(schemadocTemplateName)
	if !ok || tmpl == nil {
		t.Fatalf("registry.Lookup(%q) = _, false, want the shared fixture template", schemadocTemplateName)
	}

	funcMap := template.LayoutFuncMap(lib.Media, MediaPath)
	rec := httptest.NewRecorder()
	galleryHandler(reg, funcMap).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, galleryPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status %d, want 200 (body %q)", galleryPath, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "Example unavailable") {
		t.Fatalf("gallery reports an unavailable example for the shared fixture template:\n%s", body)
	}

	for _, want := range schemaDocStrings(t, tmpl) {
		if !strings.Contains(body, want) {
			t.Errorf("gallery HTML does not print the shared schema-doc string %q:\n%s", want, body)
		}
	}
}

// schemaDocStrings returns the schema-documentation strings the shared
// vocabulary (internal/template) renders for tmpl, derived by calling the very
// helpers both surfaces use. It fails when the fixture no longer carries one of
// the rules, so a removed rule cannot make an assertion vacuously pass on the
// empty string. It is duplicated in internal/cli so each package's test can
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
