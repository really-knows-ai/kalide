package template

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-02.task-16: the
// load-time section helper tested directly — ResolveSectionCall's
// resolution/defaults/validation and exampleSectionHelper's example execution
// (requirements.requirement.section-helper, requirement item-context).
//
// The other sectionhelper_*_test.go files drive the same behavior through
// Registry.Validate or a real on-disk library; this file exercises the two
// functions the phase-03 render-time helper will reuse, with in-process
// fixtures and a *Library built by hand, so it runs under -short and pins the
// contract exactly:
//
//   - ResolveSectionCall resolves only section-usage targets — an unknown name
//     or a slide-usage template is rejected, and a nil resolver is an error,
//     not a panic;
//   - it APPLIES THE TARGET'S DEFAULTS FIRST and then validates the effective
//     map with CheckValues, so an omitted required field carrying a default is
//     satisfied while an undefaulted required field is still rejected, a
//     caller-supplied value wins over the default, and the caller's map is
//     never mutated (the note-15 mismatch, made consistent with the step-4
//     load check);
//   - CheckValues rejects a key the target does not declare;
//   - CheckBody enforces the target's body rule, and a body heading naming a
//     declared child section is rejected while a fenced heading is ignored;
//   - the one-item `.item` descriptor is index 0 / number+count 1 / first+last
//     true / section+template the target name / parent the caller's fields
//     (nil from a slide); and
//   - exampleSectionHelper executes a target whose own layout calls
//     {{ section … }}, rendering the nested helper output, with the nested
//     call's `.item.parent` the outer target's effective fields.
//
// It reuses the registry fixtures declared in registry_test.go (regSlide,
// regSection).

// resolveSectionCallResolver returns a name → *Template resolver over the given
// templates, the shape ResolveSectionCall and exampleSectionHelper take, so a
// direct call resolves exactly like the registry's Lookup does.
func resolveSectionCallResolver(templates ...*Template) func(string) (*Template, bool) {
	byName := make(map[string]*Template, len(templates))
	for _, tmpl := range templates {
		byName[tmpl.Name] = tmpl
	}
	return func(name string) (*Template, bool) {
		tmpl, ok := byName[name]
		return tmpl, ok
	}
}

// sectionHelperTestLibrary returns a *Library whose sections/ map holds the
// given section-usage templates, each wrapped in the *LibraryTemplate
// exampleSectionHelper resolves through (Library.TemplateByName). Only
// Name/Kind/Definition are read, so no manifest or layout files are needed.
func sectionHelperTestLibrary(templates ...*Template) *Library {
	lib := &Library{Sections: make(map[string]*LibraryTemplate, len(templates))}
	for _, tmpl := range templates {
		lib.Sections[tmpl.Name] = &LibraryTemplate{
			Name:       tmpl.Name,
			Kind:       KindSection,
			Definition: tmpl,
		}
	}
	return lib
}

// TestResolveSectionCallDefaultsFirst proves the defaults-first half of
// ResolveSectionCall: the target's defaults are applied BEFORE CheckValues runs,
// so an omitted required field carrying a default is satisfied, an undefaulted
// required field is still rejected, a caller-supplied value wins over the
// default, and the caller's own map is never mutated.
func TestResolveSectionCallDefaultsFirst(t *testing.T) {
	t.Run("a required field carrying a default is satisfied and the default is applied", func(t *testing.T) {
		resolve := resolveSectionCallResolver(&Template{
			Name:    "footer",
			Usage:   UsageSection,
			Fields:  []Field{{Name: "title", Type: FieldText, Required: true, Default: "Untitled"}},
			Example: Example{Frontmatter: map[string]any{"title": "Example"}},
		})

		call, err := ResolveSectionCall(resolve, "footer", nil, "", nil)
		if err != nil {
			t.Fatalf("ResolveSectionCall() error = %v, want nil: the required field carries a default", err)
		}
		if got := call.Values["title"]; got != "Untitled" {
			t.Errorf("call.Values[title] = %v, want the applied default %q", got, "Untitled")
		}
	})

	t.Run("an undefaulted required field is rejected", func(t *testing.T) {
		resolve := resolveSectionCallResolver(&Template{
			Name:   "footer",
			Usage:  UsageSection,
			Fields: []Field{{Name: "title", Type: FieldText, Required: true}},
		})

		_, err := ResolveSectionCall(resolve, "footer", nil, "", nil)
		if err == nil {
			t.Fatal("ResolveSectionCall() error = nil, want the undefaulted required field rejected")
		}
		var ve ValueError
		if !errors.As(err, &ve) {
			t.Fatalf("ResolveSectionCall() error = %v, want a ValueError", err)
		}
		if ve.Rule != "required" || ve.PathString() != "title" {
			t.Errorf("error = rule %q path %q, want rule \"required\" path \"title\"", ve.Rule, ve.PathString())
		}
	})

	t.Run("a caller-supplied value wins over the default", func(t *testing.T) {
		resolve := resolveSectionCallResolver(&Template{
			Name:   "footer",
			Usage:  UsageSection,
			Fields: []Field{{Name: "title", Type: FieldText, Required: true, Default: "Untitled"}},
		})

		call, err := ResolveSectionCall(resolve, "footer", map[string]any{"title": "Given"}, "", nil)
		if err != nil {
			t.Fatalf("ResolveSectionCall() error = %v, want nil", err)
		}
		if got := call.Values["title"]; got != "Given" {
			t.Errorf("call.Values[title] = %v, want the caller-supplied %q", got, "Given")
		}
	})

	t.Run("the caller's map is not mutated", func(t *testing.T) {
		resolve := resolveSectionCallResolver(&Template{
			Name:  "footer",
			Usage: UsageSection,
			Fields: []Field{
				{Name: "subtitle", Type: FieldText},
				{Name: "title", Type: FieldText, Required: true, Default: "Untitled"},
			},
		})

		fields := map[string]any{"subtitle": "S"}
		call, err := ResolveSectionCall(resolve, "footer", fields, "", nil)
		if err != nil {
			t.Fatalf("ResolveSectionCall() error = %v, want nil", err)
		}
		if _, added := fields["title"]; added {
			t.Errorf("caller's fields map was mutated with the applied default: %v", fields)
		}
		if len(fields) != 1 || fields["subtitle"] != "S" {
			t.Errorf("caller's fields map = %v, want it unchanged (%d entries, subtitle=S)", fields, len(fields))
		}
		if got := call.Values["title"]; got != "Untitled" {
			t.Errorf("call.Values[title] = %v, want the applied default %q", got, "Untitled")
		}
		if len(call.Values) != 2 {
			t.Errorf("call.Values = %v, want the caller value plus the applied default", call.Values)
		}
	})
}

// TestResolveSectionCallTargets proves ResolveSectionCall resolves only
// section-usage targets: a nil resolver, an unknown name and a slide-usage
// template are each rejected with the specific error, and the exact text is
// pinned.
func TestResolveSectionCallTargets(t *testing.T) {
	tests := []struct {
		name    string
		resolve func(string) (*Template, bool)
		target  string
		want    string
	}{
		{
			name:    "a nil resolver is an error, not a panic",
			resolve: nil,
			target:  "footer",
			want:    `section helper: no template resolver for "footer"`,
		},
		{
			name:    "an unknown target is rejected",
			resolve: resolveSectionCallResolver(regSection("other")),
			target:  "missing",
			want:    `section helper: section template "missing" is not defined`,
		},
		{
			name:    "a slide-usage target is rejected",
			resolve: resolveSectionCallResolver(regSlide("hero")),
			target:  "hero",
			want:    `section helper: template "hero" is not a section-usage template`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveSectionCall(tt.resolve, tt.target, nil, "", nil)
			if err == nil {
				t.Fatalf("ResolveSectionCall() error = nil, want %q", tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("ResolveSectionCall() error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

// TestResolveSectionCallUndeclaredField proves CheckValues is applied to the
// effective map: a key the target does not declare is rejected as an
// unknown-field ValueError naming the key.
func TestResolveSectionCallUndeclaredField(t *testing.T) {
	resolve := resolveSectionCallResolver(&Template{
		Name:   "footer",
		Usage:  UsageSection,
		Fields: []Field{{Name: "title", Type: FieldText}},
	})

	_, err := ResolveSectionCall(resolve, "footer", map[string]any{"nope": "v"}, "", nil)
	if err == nil {
		t.Fatal("ResolveSectionCall() error = nil, want the undeclared key rejected")
	}
	var ve ValueError
	if !errors.As(err, &ve) {
		t.Fatalf("ResolveSectionCall() error = %v, want a ValueError", err)
	}
	if ve.Rule != "unknown-field" || ve.PathString() != "nope" {
		t.Errorf("error = rule %q path %q, want rule \"unknown-field\" path \"nope\"", ve.Rule, ve.PathString())
	}
}

// TestResolveSectionCallBodyRules proves the body half: the target's body rule
// is enforced by CheckBody (a missing required body and a present disallowed
// body are rejected), and a body heading naming a declared child section is
// rejected because the helper passes no child sections — while a heading inside
// a fenced code block is not a heading and is ignored, and an undeclared or
// absent heading loads.
func TestResolveSectionCallBodyRules(t *testing.T) {
	// childTarget declares a child section "widgets" and an optional body, so
	// the child-heading check is the only rejection possible for its bodies.
	childTarget := &Template{
		Name:     "footer",
		Usage:    UsageSection,
		Body:     BodyRule{Mode: BodyOptional},
		Sections: []SectionDecl{{Name: "widgets", Accepted: []string{"widget"}}},
	}

	tests := []struct {
		name   string
		target *Template
		body   string
		rule   string // "" means the call is expected to succeed
	}{
		{
			name:   "a missing required body is rejected",
			target: &Template{Name: "footer", Usage: UsageSection, Body: BodyRule{Mode: BodyRequired}},
			rule:   "required",
		},
		{
			name:   "a present disallowed body is rejected",
			target: &Template{Name: "footer", Usage: UsageSection, Body: BodyRule{Mode: BodyDisallowed}},
			body:   "hello",
			rule:   "disallowed",
		},
		{
			name:   "a body heading naming a declared child section is rejected",
			target: childTarget,
			body:   "# widgets",
			rule:   "subheadings",
		},
		{
			name:   "a heading naming a declared child section inside a code fence is ignored",
			target: childTarget,
			body:   "```\n# widgets\n```",
		},
		{
			name:   "a heading naming an undeclared section loads",
			target: childTarget,
			body:   "# other",
		},
		{
			name:   "a body with no heading loads",
			target: childTarget,
			body:   "just prose",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolve := resolveSectionCallResolver(tt.target)
			call, err := ResolveSectionCall(resolve, tt.target.Name, nil, tt.body, nil)

			if tt.rule == "" {
				if err != nil {
					t.Fatalf("ResolveSectionCall() error = %v, want nil", err)
				}
				if call.Body != tt.body {
					t.Errorf("call.Body = %q, want the validated %q", call.Body, tt.body)
				}
				return
			}

			if err == nil {
				t.Fatalf("ResolveSectionCall() error = nil, want rule %q", tt.rule)
			}
			var ve ValueError
			if !errors.As(err, &ve) {
				t.Fatalf("ResolveSectionCall() error = %v, want a ValueError", err)
			}
			if ve.Rule != tt.rule || ve.PathString() != "body" {
				t.Errorf("error = rule %q path %q, want rule %q path \"body\"", ve.Rule, ve.PathString(), tt.rule)
			}
		})
	}
}

// TestResolveSectionCallItemDescriptor proves the one-item `.item` descriptor a
// helper-rendered target executes under, both with a nil parent (a call made
// from a slide layout) and with the caller's fields (a call made from a section
// layout). The resolved template is the target pointer the resolver returned,
// and the effective values carry the call's supplied fields.
func TestResolveSectionCallItemDescriptor(t *testing.T) {
	target := &Template{
		Name:   "footer",
		Usage:  UsageSection,
		Fields: []Field{{Name: "title", Type: FieldText}},
	}
	resolve := resolveSectionCallResolver(target)

	t.Run("parent nil from a slide layout", func(t *testing.T) {
		call, err := ResolveSectionCall(resolve, "footer", map[string]any{"title": "T"}, "", nil)
		if err != nil {
			t.Fatalf("ResolveSectionCall() error = %v, want nil", err)
		}
		// A nil caller-fields argument is stored as a nil map, so the
		// descriptor's parent is nil in the map sense: a template's
		// truthiness test on .item.parent reads false exactly as it would
		// for a section layout's absent parent.
		want := map[string]any{
			"index":    0,
			"number":   1,
			"count":    1,
			"first":    true,
			"last":     true,
			"section":  "footer",
			"template": "footer",
			"parent":   map[string]any(nil),
		}
		if !reflect.DeepEqual(call.Item, want) {
			t.Errorf("call.Item = %v, want %v", call.Item, want)
		}
		if got, ok := call.Item["parent"].(map[string]any); !ok || got != nil {
			t.Errorf("call.Item[parent] = %v, want a nil parent from a slide layout", call.Item["parent"])
		}
		if call.Template != target {
			t.Errorf("call.Template = %p, want the resolved target %p", call.Template, target)
		}
		if got := call.Values["title"]; got != "T" {
			t.Errorf("call.Values[title] = %v, want the supplied %q", got, "T")
		}
	})

	t.Run("parent is the caller's fields from a section layout", func(t *testing.T) {
		parent := map[string]any{"title": "Outer"}
		call, err := ResolveSectionCall(resolve, "footer", nil, "", parent)
		if err != nil {
			t.Fatalf("ResolveSectionCall() error = %v, want nil", err)
		}
		want := map[string]any{
			"index":    0,
			"number":   1,
			"count":    1,
			"first":    true,
			"last":     true,
			"section":  "footer",
			"template": "footer",
			"parent":   parent,
		}
		if !reflect.DeepEqual(call.Item, want) {
			t.Errorf("call.Item = %v, want %v", call.Item, want)
		}
		if got, ok := call.Item["parent"].(map[string]any); !ok || !reflect.DeepEqual(got, parent) {
			t.Errorf("call.Item[parent] = %v, want the caller's fields %v", call.Item["parent"], parent)
		}
	})
}

// TestExampleSectionHelperRendersTargetHTML proves exampleSectionHelper returns
// the target's trusted HTML: the target layout executes with the call's
// effective field values (the target's default applied when the call omits the
// field), and a resolution failure is propagated as an error.
func TestExampleSectionHelperRendersTargetHTML(t *testing.T) {
	footer := &Template{
		Name:   "footer",
		Usage:  UsageSection,
		Fields: []Field{{Name: "title", Type: FieldText, Required: true, Default: "Untitled"}},
		Layout: Layout{Name: "footer", Text: "<footer>{{ .title }}</footer>"},
	}
	lib := sectionHelperTestLibrary(footer)
	helper := exampleSectionHelper(lib, nil)

	t.Run("renders the target's HTML with the supplied fields", func(t *testing.T) {
		got, err := helper("footer", map[string]any{"title": "Hi"})
		if err != nil {
			t.Fatalf("helper(footer) error = %v, want nil", err)
		}
		if want := "<footer>Hi</footer>"; string(got) != want {
			t.Errorf("helper(footer) = %q, want %q", string(got), want)
		}
	})

	t.Run("applies the target's default when the field is omitted", func(t *testing.T) {
		got, err := helper("footer")
		if err != nil {
			t.Fatalf("helper(footer) error = %v, want nil", err)
		}
		if want := "<footer>Untitled</footer>"; string(got) != want {
			t.Errorf("helper(footer) = %q, want %q", string(got), want)
		}
	})

	t.Run("propagates a resolution failure", func(t *testing.T) {
		_, err := helper("missing")
		if err == nil {
			t.Fatal("helper(missing) error = nil, want the unknown target rejected")
		}
		if !strings.Contains(err.Error(), "not defined") {
			t.Errorf("helper(missing) error = %v, want it to name the undefined target", err)
		}
	})
}

// TestExampleSectionHelperNestedCallParent proves the nested case: a target
// layout that itself calls {{ section … }} both parses and executes, and the
// nested call's `.item.parent` is the outer target's effective fields — the
// outer call's supplied values, or the default applied for an omitted field.
// The inner layout reads `.item.parent.title`, so the rendered HTML is only
// correct if the factory was re-bound to the outer resolution's values.
func TestExampleSectionHelperNestedCallParent(t *testing.T) {
	inner := &Template{
		Name:   "inner",
		Usage:  UsageSection,
		Layout: Layout{Name: "inner", Text: "<span>{{ .item.parent.title }}</span>"},
	}
	outer := &Template{
		Name:   "outer",
		Usage:  UsageSection,
		Fields: []Field{{Name: "title", Type: FieldText, Required: true, Default: "Defaulted"}},
		Layout: Layout{Name: "outer", Text: "<div>{{ .title }}{{ section \"inner\" (dict) }}</div>"},
	}
	lib := sectionHelperTestLibrary(inner, outer)
	helper := exampleSectionHelper(lib, nil)

	t.Run("the nested call's parent is the outer target's supplied fields", func(t *testing.T) {
		got, err := helper("outer", map[string]any{"title": "Outer"})
		if err != nil {
			t.Fatalf("helper(outer) error = %v, want nil", err)
		}
		if want := "<div>Outer<span>Outer</span></div>"; string(got) != want {
			t.Errorf("helper(outer) = %q, want %q", string(got), want)
		}
	})

	t.Run("the nested call's parent carries the outer target's applied default", func(t *testing.T) {
		got, err := helper("outer")
		if err != nil {
			t.Fatalf("helper(outer) error = %v, want nil", err)
		}
		if want := "<div>Defaulted<span>Defaulted</span></div>"; string(got) != want {
			t.Errorf("helper(outer) = %q, want %q", string(got), want)
		}
	})
}
