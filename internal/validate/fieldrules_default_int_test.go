package validate

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/really-knows-ai/kalide/internal/template"
)

// This file is the internal/validate.checkFields half of the integration-test
// deliverable for section-helpers/plan.phase-07.task-2 (field-rules): the single
// function every slide/section field check flows through treats an omitted
// required field that declares a Default as satisfied (it delegates to the
// schema-only template.CheckValues and materialises nothing), while an omitted
// required field with NO Default is still reported. The apply-at-render half and
// the example-context half live in
// internal/render/fieldrules_default_integration_test.go and
// internal/template/fieldrules_default_int_test.go respectively.
//
// checkFields has no filesystem dependency of its own (fsys is used only for
// image existence), but it is the field-check step of the real int pipeline, so
// this test is guarded with testing.Short alongside its siblings.

// TestFieldRulesDefaultCheckFieldsInt pins checkFields' required+default rule
// directly: the defaulted omission passes, the undefaulted omission fails with
// the field named.
func TestFieldRulesDefaultCheckFieldsInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: the field-check step of the real validate pipeline")
	}

	reg := template.NewRegistry(nil)
	fsys := fstest.MapFS{}

	t.Run("an omitted required field with a default passes", func(t *testing.T) {
		tmpl := &template.Template{
			Name:   "footer",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "title", Type: template.FieldText, Required: true, Default: "Untitled"}},
		}
		verr, invalid := checkFields(fsys, "slides/1-a.md", map[string]any{}, tmpl, nil, reg)
		if invalid {
			t.Fatalf("checkFields() invalid = true (%s), want false: the required field carries a default", Format(verr))
		}
		if !isZeroValidationError(verr) {
			t.Errorf("checkFields() error = %#v, want the zero ValidationError", verr)
		}
	})

	t.Run("an omitted required field with no default fails", func(t *testing.T) {
		tmpl := &template.Template{
			Name:   "footer",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "title", Type: template.FieldText, Required: true}},
		}
		verr, invalid := checkFields(fsys, "slides/1-a.md", map[string]any{}, tmpl, nil, reg)
		if !invalid {
			t.Fatal("checkFields() invalid = false, want the required error")
		}
		got := Format(verr)
		for _, want := range []string{"slides/1-a.md", "title", "required"} {
			if !strings.Contains(got, want) {
				t.Errorf("checkFields() error = %q, want it to contain %q", got, want)
			}
		}
	})
}

// isZeroValidationError reports whether e is the zero ValidationError.
func isZeroValidationError(e ValidationError) bool {
	return e.File == "" && e.Line == 0 && len(e.Path) == 0 && e.What == "" && e.Fix == ""
}
