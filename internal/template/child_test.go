package template

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-01.task-14: the
// `child`/`children` layout constructors and their resolution, tested directly
// with in-process fixtures so they run under -short
// (requirements.requirement.child-constructor,
// requirements.requirement.children-forwarding,
// requirements.requirement.section-helper-load-checks).
//
// It pins the contract of the three units child.go adds:
//
//   - ChildFunc builds a ChildRequest from
//     `child "<section>" [fields] [body] [child…]` — the named section, the
//     fields map, the body and whether one was supplied, and the recursively
//     nested child arguments — and rejects a malformed argument type;
//   - ChildrenFunc builds the forwarding ChildRequest from
//     `children "<section>" <list>` — Forward set and Entries carrying the
//     []map[string]any a DataContext produces, a []any a layout built, or nil
//     for an authored-absent section — and rejects a missing/extra argument or
//     a non-list;
//   - ResolveChildCall resolves one literal child exactly like an authored
//     instance: the accepted-template check, the `template` selector stripped
//     from a copy of the fields (required iff the declaration accepts more than
//     one template), defaults applied first, the .item descriptor, and the
//     child-body-heading rejection; and ResolveChildren expands a forwarded
//     `.data` list into literal-equivalent children keeping each entry's own
//     template/raw fields/body/nested data, counts sibling index/count over the
//     full passed set, enforces each declaration's min/max when checkCounts is
//     true, and leaves Source as the authored fields the selector and defaults
//     never touch.
//
// It reuses the registry/section-helper fixtures declared in registry_test.go
// and sectionhelper_resolve_test.go (regSlide, resolveSectionCallResolver,
// sectionHelperTestLibrary) so nothing is duplicated.

// childTestResolver returns a name → *Template resolver over the templates
// ResolveChildCall and ResolveChildren take: a "column" target with a required
// field carrying a default, "person"/"company" the two accepted templates of a
// multi-template declaration, a "widgetHost" declaring a child section, and a
// slide-usage "hero" for the non-section refusal.
func childTestResolver() func(string) (*Template, bool) {
	return resolveSectionCallResolver(
		&Template{
			Name:   "column",
			Usage:  UsageSection,
			Fields: []Field{{Name: "title", Type: FieldText, Required: true, Default: "Untitled"}},
		},
		&Template{
			Name:   "person",
			Usage:  UsageSection,
			Fields: []Field{{Name: "name", Type: FieldText, Required: true}},
		},
		&Template{
			Name:   "company",
			Usage:  UsageSection,
			Fields: []Field{{Name: "name", Type: FieldText, Required: true}},
		},
		&Template{
			Name:     "widgetHost",
			Usage:    UsageSection,
			Body:     BodyRule{Mode: BodyOptional},
			Sections: []SectionDecl{{Name: "widgets", Accepted: []string{"widget"}}},
		},
		&Template{Name: "widget", Usage: UsageSection},
		regSlide("hero"),
	)
}

// childTestParent returns the section-usage parent whose declared sections the
// child requests name. Its declarations cover the accepted-template cases:
// exactly one accepted ("column"), none ("empty"), a name the resolver does not
// define ("ghostHost"), a slide-usage name ("heroHost"), a single accepted
// child-declaring template ("host") and two accepted templates ("card").
func childTestParent() *Template {
	return &Template{
		Name:  "parent",
		Usage: UsageSection,
		Sections: []SectionDecl{
			{Name: "column", Accepted: []string{"column"}},
			{Name: "empty"},
			{Name: "ghostHost", Accepted: []string{"ghost"}},
			{Name: "heroHost", Accepted: []string{"hero"}},
			{Name: "host", Accepted: []string{"widgetHost"}},
			{Name: "card", Accepted: []string{"person", "company"}},
		},
	}
}

// TestChildFunc proves the `child` constructor builds the request the same way
// an authored instance carries its data: its named section, fields map, body
// and HasBody, and its nested `child` arguments in order — and that every
// malformed argument type is rejected rather than building a partial request.
func TestChildFunc(t *testing.T) {
	t.Run("builds a request from section, fields, body and nested children", func(t *testing.T) {
		leaf, err := ChildFunc("leaf", map[string]any{"text": "L"})
		if err != nil {
			t.Fatalf("ChildFunc(leaf) error = %v, want nil", err)
		}

		got, err := ChildFunc("column", map[string]any{"title": "T"}, "body markdown", leaf)
		if err != nil {
			t.Fatalf("ChildFunc(column) error = %v, want nil", err)
		}
		if got.Section != "column" {
			t.Errorf("Section = %q, want %q", got.Section, "column")
		}
		if want := map[string]any{"title": "T"}; !reflect.DeepEqual(got.Fields, want) {
			t.Errorf("Fields = %v, want %v", got.Fields, want)
		}
		if got.Body != "body markdown" || !got.HasBody {
			t.Errorf("Body/HasBody = %q/%v, want %q/true", got.Body, got.HasBody, "body markdown")
		}
		if len(got.Children) != 1 || got.Children[0] != leaf {
			t.Errorf("Children = %v, want the one nested child %p", got.Children, leaf)
		}
		if got.Forward || got.Entries != nil {
			t.Errorf("Forward/Entries = %v/%v, want false/nil for a literal child", got.Forward, got.Entries)
		}
	})

	t.Run("distinguishes an omitted body from a provided empty one", func(t *testing.T) {
		omitted, err := ChildFunc("column")
		if err != nil {
			t.Fatalf("ChildFunc(column) error = %v, want nil", err)
		}
		if omitted.HasBody {
			t.Errorf("HasBody = true for an omitted body, want false")
		}
		if omitted.Body != "" {
			t.Errorf("Body = %q for an omitted body, want empty", omitted.Body)
		}

		empty, err := ChildFunc("column", nil, "")
		if err != nil {
			t.Fatalf("ChildFunc(column, nil, \"\") error = %v, want nil", err)
		}
		if !empty.HasBody {
			t.Errorf("HasBody = false for a provided empty body, want true")
		}
		if empty.Body != "" {
			t.Errorf("Body = %q for a provided empty body, want empty", empty.Body)
		}
	})

	t.Run("nil placeholders leave fields and body unset", func(t *testing.T) {
		got, err := ChildFunc("column", nil, nil)
		if err != nil {
			t.Fatalf("ChildFunc(column, nil, nil) error = %v, want nil", err)
		}
		if got.Fields != nil {
			t.Errorf("Fields = %v, want nil for a nil fields placeholder", got.Fields)
		}
		if got.HasBody || got.Body != "" {
			t.Errorf("Body/HasBody = %q/%v, want empty/false for a nil body placeholder", got.Body, got.HasBody)
		}
		if got.Children != nil {
			t.Errorf("Children = %v, want nil when no child arguments are passed", got.Children)
		}
	})

	t.Run("rejects malformed arguments", func(t *testing.T) {
		tests := []struct {
			name string
			args []any
			want string
		}{
			{
				name: "no arguments",
				want: "child: section must be a string, got no arguments",
			},
			{
				name: "a non-string section",
				args: []any{42},
				want: "child: section must be a string, got a number",
			},
			{
				name: "non-map fields",
				args: []any{"column", []string{"title"}},
				want: `child "column": fields must be a map, got []string`,
			},
			{
				name: "non-string body",
				args: []any{"column", nil, 7},
				want: `child "column": body must be a string, got int`,
			},
			{
				name: "a non-child argument",
				args: []any{"column", nil, nil, "leaf"},
				want: `child "column": child argument 1 must be a child value, got string`,
			},
			{
				name: "a nil child argument",
				args: []any{"column", nil, nil, (*ChildRequest)(nil)},
				want: `child "column": child argument 1 must not be nil`,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := ChildFunc(tt.args...)
				if err == nil {
					t.Fatalf("ChildFunc(%v) error = nil, want %q", tt.args, tt.want)
				}
				if err.Error() != tt.want {
					t.Errorf("ChildFunc(%v) error = %q, want %q", tt.args, err.Error(), tt.want)
				}
			})
		}
	})
}

// TestChildrenFunc proves the `children` constructor builds the forwarding
// request for a `<list>` argument: a []map[string]any (the shape DataContext
// produces) is normalised to []any, a []any is kept as-is, and nil is an empty
// forwarded set rather than a malformed call. A missing/extra argument or a
// non-list value is rejected, and Fields/Body/Children stay zero.
func TestChildrenFunc(t *testing.T) {
	t.Run("forwards a []map[string]any as entries", func(t *testing.T) {
		list := []map[string]any{{"title": "A"}, {"title": "B"}}
		got, err := ChildrenFunc("column", list)
		if err != nil {
			t.Fatalf("ChildrenFunc(column) error = %v, want nil", err)
		}
		if got.Section != "column" {
			t.Errorf("Section = %q, want %q", got.Section, "column")
		}
		if !got.Forward {
			t.Errorf("Forward = false, want true for a children forwarding")
		}
		if len(got.Entries) != 2 {
			t.Fatalf("len(Entries) = %d, want 2", len(got.Entries))
		}
		for i := range list {
			if !reflect.DeepEqual(got.Entries[i], list[i]) {
				t.Errorf("Entries[%d] = %v, want %v", i, got.Entries[i], list[i])
			}
		}
		if got.Fields != nil || got.Body != "" || got.HasBody || got.Children != nil {
			t.Errorf("Fields/Body/HasBody/Children = %v/%q/%v/%v, want all zero for a forwarding",
				got.Fields, got.Body, got.HasBody, got.Children)
		}
	})

	t.Run("accepts and keeps a []any entries list", func(t *testing.T) {
		list := []any{map[string]any{"title": "A"}}
		got, err := ChildrenFunc("column", list)
		if err != nil {
			t.Fatalf("ChildrenFunc(column) error = %v, want nil", err)
		}
		if !reflect.DeepEqual(got.Entries, list) {
			t.Errorf("Entries = %v, want the passed []any %v", got.Entries, list)
		}
	})

	t.Run("nil entries is an empty forwarded set, not malformed", func(t *testing.T) {
		got, err := ChildrenFunc("column", nil)
		if err != nil {
			t.Fatalf("ChildrenFunc(column, nil) error = %v, want nil", err)
		}
		if !got.Forward {
			t.Errorf("Forward = false, want true")
		}
		if got.Entries != nil {
			t.Errorf("Entries = %v, want nil for a missing .data section", got.Entries)
		}
	})

	t.Run("rejects malformed arguments", func(t *testing.T) {
		tests := []struct {
			name string
			args []any
			want string
		}{
			{
				name: "no arguments",
				want: "children: section must be a string, got no arguments",
			},
			{
				name: "a non-string section",
				args: []any{true},
				want: "children: section must be a string, got a boolean",
			},
			{
				name: "a missing entries argument",
				args: []any{"column"},
				want: `children "column": entries must be a list, got no argument`,
			},
			{
				name: "an extra argument",
				args: []any{"column", nil, nil},
				want: `children "column": expected a section and entries, got 3 arguments`,
			},
			{
				name: "a non-list entries argument",
				args: []any{"column", "not-a-list"},
				want: `children "column": entries must be a list, got string`,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := ChildrenFunc(tt.args...)
				if err == nil {
					t.Fatalf("ChildrenFunc(%v) error = nil, want %q", tt.args, tt.want)
				}
				if err.Error() != tt.want {
					t.Errorf("ChildrenFunc(%v) error = %q, want %q", tt.args, err.Error(), tt.want)
				}
			})
		}
	})
}

// TestResolveChildCallSectionAndTemplateChecks proves the accepted-template
// half of ResolveChildCall: the named section must be declared by the parent,
// its declaration must accept at least one template, a supplied selector must
// be an accepted name, the selected template must be defined and section-usage,
// and a forwarding request is refused (ResolveChildren resolves it). Each
// message is pinned, and a nil request/parent/lookup is an error, not a panic.
func TestResolveChildCallSectionAndTemplateChecks(t *testing.T) {
	resolve := childTestResolver()
	parent := childTestParent()

	tests := []struct {
		name string
		req  *ChildRequest
		want string
	}{
		{
			name: "a nil request is an error",
			req:  nil,
			want: "child: request must not be nil",
		},
		{
			name: "an undeclared section is rejected",
			req:  &ChildRequest{Section: "missing"},
			want: `child: section "missing" is not declared by parent template "parent"`,
		},
		{
			name: "a section accepting no template is rejected",
			req:  &ChildRequest{Section: "empty"},
			want: `child: section "empty" on parent template "parent" accepts no section template`,
		},
		{
			name: "a selector the declaration does not accept is rejected",
			req:  &ChildRequest{Section: "card", Fields: map[string]any{"template": "ghost"}},
			want: `child: section "card" on parent template "parent" does not accept template "ghost" (accepts person, company)`,
		},
		{
			name: "a non-string selector is rejected",
			req:  &ChildRequest{Section: "card", Fields: map[string]any{"template": 7}},
			want: `child "card": key "template" must name a section template`,
		},
		{
			name: "a missing selector on a multi-template section is rejected",
			req:  &ChildRequest{Section: "card"},
			want: `child: section "card" on parent template "parent" accepts more than one template; a "template" selector is required`,
		},
		{
			name: "an accepted but undefined template is rejected",
			req:  &ChildRequest{Section: "ghostHost", Fields: map[string]any{"template": "ghost"}},
			want: `child "ghostHost": section template "ghost" is not defined`,
		},
		{
			name: "a slide-usage selected template is rejected",
			req:  &ChildRequest{Section: "heroHost"},
			want: `child "heroHost": template "hero" is not a section-usage template`,
		},
		{
			name: "a forwarding request is refused",
			req:  &ChildRequest{Section: "column", Forward: true},
			want: `child "column": a children forwarding is resolved by ResolveChildren, not ResolveChildCall`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveChildCall(resolve, nil, parent, tt.req, 0, 1, nil, nil)
			if err == nil {
				t.Fatalf("ResolveChildCall() error = nil, want %q", tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("ResolveChildCall() error = %q, want %q", err.Error(), tt.want)
			}
		})
	}

	t.Run("a nil parent is an error", func(t *testing.T) {
		_, err := ResolveChildCall(resolve, nil, nil, &ChildRequest{Section: "column"}, 0, 1, nil, nil)
		want := `child "column": no parent template to resolve the declared section against`
		if err == nil || err.Error() != want {
			t.Errorf("ResolveChildCall(nil parent) error = %v, want %q", err, want)
		}
	})

	t.Run("a nil resolver is an error, not a panic", func(t *testing.T) {
		_, err := ResolveChildCall(nil, nil, parent, &ChildRequest{Section: "column"}, 0, 1, nil, nil)
		want := `child "column": no template resolver`
		if err == nil || err.Error() != want {
			t.Errorf("ResolveChildCall(nil resolver) error = %v, want %q", err, want)
		}
	})
}

// TestResolveChildCallDefaultsAndSource proves the defaults-first half of
// ResolveChildCall: the instantiated template's declared defaults are
// materialised into Values, a supplied value wins, and Source is the AUTHORED
// fields before defaults — so a materialised default never leaks into the
// source view a consumer builds `.raw` from. The caller's own map is never
// mutated.
func TestResolveChildCallDefaultsAndSource(t *testing.T) {
	target := &Template{
		Name:   "column",
		Usage:  UsageSection,
		Fields: []Field{{Name: "title", Type: FieldText, Required: true, Default: "Untitled"}},
	}
	parent := &Template{
		Name:     "parent",
		Usage:    UsageSection,
		Sections: []SectionDecl{{Name: "column", Accepted: []string{"column"}}},
	}
	resolve := resolveSectionCallResolver(target)

	t.Run("the default is applied for an omitted required field", func(t *testing.T) {
		req := &ChildRequest{Section: "column"}
		call, err := ResolveChildCall(resolve, nil, parent, req, 0, 1, nil, nil)
		if err != nil {
			t.Fatalf("ResolveChildCall() error = %v, want nil", err)
		}
		if got := call.Values["title"]; got != "Untitled" {
			t.Errorf("Values[title] = %v, want the applied default %q", got, "Untitled")
		}
		if len(call.Source) != 0 {
			t.Errorf("Source = %v, want empty: the default is not an authored source value", call.Source)
		}
		if _, has := call.Source["title"]; has {
			t.Errorf("Source = %v, want the defaulted title excluded", call.Source)
		}
	})

	t.Run("a supplied value wins and is carried on Source", func(t *testing.T) {
		fields := map[string]any{"title": "Given"}
		call, err := ResolveChildCall(resolve, nil, parent, &ChildRequest{Section: "column", Fields: fields}, 0, 1, nil, nil)
		if err != nil {
			t.Fatalf("ResolveChildCall() error = %v, want nil", err)
		}
		if got := call.Values["title"]; got != "Given" {
			t.Errorf("Values[title] = %v, want the supplied %q", got, "Given")
		}
		if got := call.Source["title"]; got != "Given" {
			t.Errorf("Source[title] = %v, want the authored %q", got, "Given")
		}
		if len(fields) != 1 || fields["title"] != "Given" {
			t.Errorf("caller's fields map = %v, want it unchanged", fields)
		}
	})
}

// TestResolveChildCallItemDescriptor proves the child's own `.item` descriptor:
// the sibling position the caller passes (index/number/count/first/last), the
// named declared section and the instantiated template, `parent` the parent
// instance's fields, and `depth` the shared InstanceDepth of the enclosing
// ancestor chain against the child's resolved template — 0 for an ordinary
// child and 1 for a declared self-nesting child whose resolved template is the
// parent declaration's own, resolved inside a parent instance of the same
// template.
func TestResolveChildCallItemDescriptor(t *testing.T) {
	target := &Template{Name: "column", Usage: UsageSection}
	parent := &Template{
		Name:     "parent",
		Usage:    UsageSection,
		Sections: []SectionDecl{{Name: "column", Accepted: []string{"column"}}},
	}
	resolve := resolveSectionCallResolver(target)
	parentFields := map[string]any{"heading": "H"}

	call, err := ResolveChildCall(resolve, nil, parent, &ChildRequest{Section: "column"}, 1, 3, nil, parentFields)
	if err != nil {
		t.Fatalf("ResolveChildCall() error = %v, want nil", err)
	}
	want := map[string]any{
		"index":    1,
		"number":   2,
		"count":    3,
		"first":    false,
		"last":     false,
		"section":  "column",
		"template": "column",
		"depth":    0, // an ordinary child: no enclosing same-template ancestor
		"parent":   parentFields,
	}
	if !reflect.DeepEqual(call.Item, want) {
		t.Errorf("call.Item = %v, want %v", call.Item, want)
	}

	// The first of a three-child set is first and not last.
	first, err := ResolveChildCall(resolve, nil, parent, &ChildRequest{Section: "column"}, 0, 3, nil, nil)
	if err != nil {
		t.Fatalf("ResolveChildCall() error = %v, want nil", err)
	}
	if first.Item["first"] != true || first.Item["last"] != false {
		t.Errorf("first.Item first/last = %v/%v, want true/false", first.Item["first"], first.Item["last"])
	}
	last, err := ResolveChildCall(resolve, nil, parent, &ChildRequest{Section: "column"}, 2, 3, nil, nil)
	if err != nil {
		t.Fatalf("ResolveChildCall() error = %v, want nil", err)
	}
	if last.Item["first"] != false || last.Item["last"] != true {
		t.Errorf("last.Item first/last = %v/%v, want false/true", last.Item["first"], last.Item["last"])
	}

	// A declared self-nesting child: the parent template's own section accepts
	// the parent's own template, so a child resolving to that template, inside a
	// parent instance of the same template, counts the one enclosing
	// same-template ancestor the caller threads in — depth 1, beside its sibling
	// position, section and parent.
	t.Run("a self-nesting child counts its enclosing same-template ancestor", func(t *testing.T) {
		self := &Template{
			Name:     "column",
			Usage:    UsageSection,
			Sections: []SectionDecl{{Name: "column", Accepted: []string{"column"}}},
		}
		selfResolve := resolveSectionCallResolver(self)
		nested, err := ResolveChildCall(selfResolve, nil, self, &ChildRequest{Section: "column"}, 0, 1, []string{"column"}, parentFields)
		if err != nil {
			t.Fatalf("ResolveChildCall() error = %v, want nil", err)
		}
		wantNested := map[string]any{
			"index":    0,
			"number":   1,
			"count":    1,
			"first":    true,
			"last":     true,
			"section":  "column",
			"template": "column",
			"depth":    1, // one enclosing same-template ancestor: the parent instance
			"parent":   parentFields,
		}
		if !reflect.DeepEqual(nested.Item, wantNested) {
			t.Errorf("nested.Item = %v, want %v", nested.Item, wantNested)
		}
	})
}

// TestResolveChildCallTemplateSelector proves the reserved `template` selector:
// a single-template declaration needs none and instantiates that one; a
// multi-template declaration requires it; it is stripped from a COPY of the
// fields before validation, so it is never counted as an undeclared field and
// the caller's map is untouched; and it takes precedence over a
// template-declared field of the same name, so that field cannot be set
// through a child.
func TestResolveChildCallTemplateSelector(t *testing.T) {
	person := &Template{Name: "person", Usage: UsageSection, Fields: []Field{{Name: "name", Type: FieldText, Required: true}}}
	company := &Template{Name: "company", Usage: UsageSection, Fields: []Field{{Name: "name", Type: FieldText, Required: true}}}
	column := &Template{Name: "column", Usage: UsageSection}
	resolve := resolveSectionCallResolver(person, company, column)
	parent := &Template{
		Name:  "parent",
		Usage: UsageSection,
		Sections: []SectionDecl{
			{Name: "card", Accepted: []string{"person", "company"}},
			{Name: "column", Accepted: []string{"column"}},
		},
	}

	t.Run("the selector picks the template and is consumed, not counted", func(t *testing.T) {
		req := &ChildRequest{Section: "card", Fields: map[string]any{"template": "person", "name": "Ada"}}
		call, err := ResolveChildCall(resolve, nil, parent, req, 0, 1, nil, nil)
		if err != nil {
			t.Fatalf("ResolveChildCall() error = %v, want nil", err)
		}
		if call.Template.Name != "person" {
			t.Errorf("Template.Name = %q, want the selected %q", call.Template.Name, "person")
		}
		if got := call.Values["name"]; got != "Ada" {
			t.Errorf("Values[name] = %v, want %q", got, "Ada")
		}
		if _, has := call.Source["template"]; has {
			t.Errorf("Source = %v, want the template selector excluded", call.Source)
		}
		if _, has := call.Values["template"]; has {
			t.Errorf("Values = %v, want the template selector never counted as a field", call.Values)
		}
		if req.Fields["template"] != "person" {
			t.Errorf("caller's fields map = %v, want the selector left in place", req.Fields)
		}
		if got := call.Item["template"]; got != "person" {
			t.Errorf("Item[template] = %v, want the instantiated %q", got, "person")
		}
	})

	t.Run("an omitted selector on a single-template section instantiates that one", func(t *testing.T) {
		call, err := ResolveChildCall(resolve, nil, parent, &ChildRequest{Section: "column"}, 0, 1, nil, nil)
		if err != nil {
			t.Fatalf("ResolveChildCall() error = %v, want nil", err)
		}
		if call.Template.Name != "column" {
			t.Errorf("Template.Name = %q, want the single accepted %q", call.Template.Name, "column")
		}
	})

	t.Run("the selector takes precedence over a template-declared field of the same name", func(t *testing.T) {
		special := &Template{
			Name:   "special",
			Usage:  UsageSection,
			Fields: []Field{{Name: "template", Type: FieldText, Default: "declared"}},
		}
		host := &Template{
			Name:     "host",
			Usage:    UsageSection,
			Sections: []SectionDecl{{Name: "slot", Accepted: []string{"special"}}},
		}
		resolveSpecial := resolveSectionCallResolver(special)

		call, err := ResolveChildCall(resolveSpecial, nil, host, &ChildRequest{Section: "slot", Fields: map[string]any{"template": "special"}}, 0, 1, nil, nil)
		if err != nil {
			t.Fatalf("ResolveChildCall() error = %v, want nil", err)
		}
		if call.Template.Name != "special" {
			t.Errorf("Template.Name = %q, want the selected %q", call.Template.Name, "special")
		}
		if got := call.Values["template"]; got != "declared" {
			t.Errorf("Values[template] = %v, want the template's own default %q (the selector must not set it)", got, "declared")
		}
		if _, has := call.Source["template"]; has {
			t.Errorf("Source = %v, want the selector excluded so the field cannot be set through a child", call.Source)
		}
	})
}

// TestResolveChildCallBodyHeadingRejected proves a child body carrying a
// heading that names one of the instantiated template's declared child sections
// is rejected as a subheadings ValueError — a child body carries no child
// sections — while a fenced heading is ignored and a heading naming an
// undeclared section loads.
func TestResolveChildCallBodyHeadingRejected(t *testing.T) {
	target := &Template{
		Name:     "widgetHost",
		Usage:    UsageSection,
		Body:     BodyRule{Mode: BodyOptional},
		Sections: []SectionDecl{{Name: "widgets", Accepted: []string{"widget"}}},
	}
	parent := &Template{
		Name:     "parent",
		Usage:    UsageSection,
		Sections: []SectionDecl{{Name: "host", Accepted: []string{"widgetHost"}}},
	}
	resolve := resolveSectionCallResolver(target)

	t.Run("a heading naming a declared child section is rejected", func(t *testing.T) {
		_, err := ResolveChildCall(resolve, nil, parent, &ChildRequest{Section: "host", Body: "# widgets"}, 0, 1, nil, nil)
		if err == nil {
			t.Fatal("ResolveChildCall() error = nil, want the child-section heading rejected")
		}
		var ve ValueError
		if !errors.As(err, &ve) {
			t.Fatalf("ResolveChildCall() error = %v, want a ValueError", err)
		}
		if ve.Rule != "subheadings" || ve.PathString() != "body" {
			t.Errorf("error = rule %q path %q, want rule \"subheadings\" path \"body\"", ve.Rule, ve.PathString())
		}
	})

	t.Run("a heading inside a code fence is ignored", func(t *testing.T) {
		call, err := ResolveChildCall(resolve, nil, parent, &ChildRequest{Section: "host", Body: "```\n# widgets\n```"}, 0, 1, nil, nil)
		if err != nil {
			t.Fatalf("ResolveChildCall() error = %v, want nil for a fenced heading", err)
		}
		if call.Body != "```\n# widgets\n```" {
			t.Errorf("Body = %q, want the validated body carried through", call.Body)
		}
	})

	t.Run("a heading naming an undeclared section loads", func(t *testing.T) {
		if _, err := ResolveChildCall(resolve, nil, parent, &ChildRequest{Section: "host", Body: "# other"}, 0, 1, nil, nil); err != nil {
			t.Errorf("ResolveChildCall() error = %v, want nil for an undeclared heading", err)
		}
	})
}

// childTestForwardingResolver returns a resolver for the forwarding tests: a
// "person" template with a required field, a defaulted field, an optional body
// and its own child section "tag"; a "company" with a required field and an
// optional body; and the "tag" target. The two are the accepted templates of a
// multi-template `card` declaration.
func childTestForwardingResolver() func(string) (*Template, bool) {
	return resolveSectionCallResolver(
		&Template{
			Name:  "person",
			Usage: UsageSection,
			Fields: []Field{
				{Name: "name", Type: FieldText, Required: true},
				{Name: "subtitle", Type: FieldText, Default: "Untitled"},
			},
			Body:     BodyRule{Mode: BodyOptional},
			Sections: []SectionDecl{{Name: "tag", Accepted: []string{"tag"}}},
		},
		&Template{
			Name:   "company",
			Usage:  UsageSection,
			Fields: []Field{{Name: "name", Type: FieldText, Required: true}},
			Body:   BodyRule{Mode: BodyOptional},
		},
		&Template{
			Name:   "tag",
			Usage:  UsageSection,
			Fields: []Field{{Name: "label", Type: FieldText, Required: true}},
			Body:   BodyRule{Mode: BodyDisallowed},
		},
	)
}

// childTestEntry builds one forwarded `.data` entry in the shape DataContext
// produces: an `item` map whose `template` selects the instantiated template,
// an optional `raw` map of source fields/body, and an optional `data` map of
// the entry's own children.
func childTestEntry(templateName string, raw, data map[string]any) map[string]any {
	entry := map[string]any{"item": map[string]any{"template": templateName}}
	if raw != nil {
		entry["raw"] = raw
	}
	if data != nil {
		entry["data"] = data
	}
	return entry
}

// TestResolveChildrenForwarding proves a `children` forwarding expands each
// authored `.data` entry into a literal-equivalent child that keeps its OWN
// template (from item.template), its raw fields (raw without body) and body
// (raw.body), and its nested children (data), recursing one level down. It also
// proves ResolvedChild.Source is the authored fields with the selector and any
// materialised default excluded.
func TestResolveChildrenForwarding(t *testing.T) {
	resolve := childTestForwardingResolver()
	parent := &Template{
		Name:     "cards",
		Usage:    UsageSection,
		Sections: []SectionDecl{{Name: "card", Accepted: []string{"person", "company"}}},
	}

	personEntries := []any{
		childTestEntry("person", map[string]any{"name": "Ada", "body": "An Ada bio."}, map[string]any{
			"tag": []any{childTestEntry("tag", map[string]any{"label": "friend"}, nil)},
		}),
	}
	companyEntries := []any{
		childTestEntry("company", map[string]any{"name": "Acme"}, nil),
	}
	reqs := []*ChildRequest{
		{Section: "card", Forward: true, Entries: personEntries},
		{Section: "card", Forward: true, Entries: companyEntries},
	}

	got, err := ResolveChildren(resolve, nil, parent, reqs, nil, nil, true)
	if err != nil {
		t.Fatalf("ResolveChildren() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(ResolveChildren()) = %d, want 2 forwarded children", len(got))
	}

	ada := got[0]
	if ada.Call.Template.Name != "person" {
		t.Errorf("child[0].Template.Name = %q, want the entry's own template %q", ada.Call.Template.Name, "person")
	}
	if got := ada.Call.Values["name"]; got != "Ada" {
		t.Errorf("child[0].Values[name] = %v, want the raw field %q", got, "Ada")
	}
	if got := ada.Call.Values["subtitle"]; got != "Untitled" {
		t.Errorf("child[0].Values[subtitle] = %v, want the applied default %q", got, "Untitled")
	}
	if ada.Call.Body != "An Ada bio." {
		t.Errorf("child[0].Body = %q, want the entry's raw.body", ada.Call.Body)
	}
	if got := ada.Source["name"]; got != "Ada" {
		t.Errorf("child[0].Source[name] = %v, want the authored %q", got, "Ada")
	}
	for _, excluded := range []string{"subtitle", "body", "template"} {
		if _, has := ada.Source[excluded]; has {
			t.Errorf("child[0].Source = %v, want %q excluded (default/body/selector is not an authored field)", ada.Source, excluded)
		}
	}
	if ada.Call.Item["section"] != "card" {
		t.Errorf("child[0].Item[section] = %v, want the named declared section %q", ada.Call.Item["section"], "card")
	}
	if ada.Call.Item["template"] != "person" {
		t.Errorf("child[0].Item[template] = %v, want the entry's own template %q", ada.Call.Item["template"], "person")
	}
	if ada.Call.Item["index"] != 0 || ada.Call.Item["count"] != 2 {
		t.Errorf("child[0].Item index/count = %v/%v, want 0/2 over the forwarded set", ada.Call.Item["index"], ada.Call.Item["count"])
	}
	if len(ada.Children) != 1 {
		t.Fatalf("len(child[0].Children) = %d, want the entry's own forwarded child", len(ada.Children))
	}
	tagChild := ada.Children[0]
	if tagChild.Call.Template.Name != "tag" {
		t.Errorf("grandchild.Template.Name = %q, want %q", tagChild.Call.Template.Name, "tag")
	}
	if got := tagChild.Call.Values["label"]; got != "friend" {
		t.Errorf("grandchild.Values[label] = %v, want the nested entry's raw field %q", got, "friend")
	}

	acme := got[1]
	if acme.Call.Template.Name != "company" {
		t.Errorf("child[1].Template.Name = %q, want the entry's own template %q", acme.Call.Template.Name, "company")
	}
	if got := acme.Call.Values["name"]; got != "Acme" {
		t.Errorf("child[1].Values[name] = %v, want %q", got, "Acme")
	}
	if acme.Call.Item["index"] != 1 {
		t.Errorf("child[1].Item[index] = %v, want 1", acme.Call.Item["index"])
	}
}

// TestResolveChildrenCounts proves the min/max enforcement and the sibling
// counting over the passed set: with checkCounts true each declared section's
// min/max is applied to the count of literal children and expanded forwarded
// entries together, and with checkCounts false the counts are deferred. A
// literal child and a forwarded entry for the same section share one sibling
// group.
func TestResolveChildrenCounts(t *testing.T) {
	column := &Template{Name: "column", Usage: UsageSection}
	resolve := resolveSectionCallResolver(column)

	limited := &Template{
		Name:     "limited",
		Usage:    UsageSection,
		Sections: []SectionDecl{{Name: "column", Accepted: []string{"column"}, Min: 2, Max: 3}},
	}
	one := []*ChildRequest{{Section: "column"}}
	four := []*ChildRequest{
		{Section: "column"}, {Section: "column"}, {Section: "column"}, {Section: "column"},
	}

	t.Run("checkCounts true enforces the minimum", func(t *testing.T) {
		_, err := ResolveChildren(resolve, nil, limited, one, nil, nil, true)
		want := `children: section "column" on parent template "limited" has 1 child(ren), minimum is 2`
		if err == nil || err.Error() != want {
			t.Errorf("ResolveChildren() error = %v, want %q", err, want)
		}
	})

	t.Run("checkCounts true enforces the maximum", func(t *testing.T) {
		_, err := ResolveChildren(resolve, nil, limited, four, nil, nil, true)
		want := `children: section "column" on parent template "limited" has 4 child(ren), maximum is 3`
		if err == nil || err.Error() != want {
			t.Errorf("ResolveChildren() error = %v, want %q", err, want)
		}
	})

	t.Run("checkCounts false defers the counts", func(t *testing.T) {
		got, err := ResolveChildren(resolve, nil, limited, one, nil, nil, false)
		if err != nil {
			t.Fatalf("ResolveChildren(checkCounts=false) error = %v, want nil", err)
		}
		if len(got) != 1 {
			t.Errorf("len(ResolveChildren()) = %d, want the 1 resolved child", len(got))
		}
	})

	t.Run("the count is over the expanded forwarded set", func(t *testing.T) {
		reqs := []*ChildRequest{{
			Section: "column",
			Forward: true,
			Entries: []any{
				childTestEntry("column", nil, nil),
				childTestEntry("column", nil, nil),
				childTestEntry("column", nil, nil),
				childTestEntry("column", nil, nil),
			},
		}}
		_, err := ResolveChildren(resolve, nil, limited, reqs, nil, nil, true)
		want := `children: section "column" on parent template "limited" has 4 child(ren), maximum is 3`
		if err == nil || err.Error() != want {
			t.Errorf("ResolveChildren() error = %v, want %q", err, want)
		}
	})

	t.Run("a literal child and forwarded entries share one sibling group", func(t *testing.T) {
		parent := &Template{
			Name:     "columns",
			Usage:    UsageSection,
			Sections: []SectionDecl{{Name: "column", Accepted: []string{"column"}}},
		}
		reqs := []*ChildRequest{
			{Section: "column"},
			{Section: "column", Forward: true, Entries: []any{
				childTestEntry("column", nil, nil),
				childTestEntry("column", nil, nil),
			}},
		}
		got, err := ResolveChildren(resolve, nil, parent, reqs, nil, nil, true)
		if err != nil {
			t.Fatalf("ResolveChildren() error = %v, want nil", err)
		}
		if len(got) != 3 {
			t.Fatalf("len(ResolveChildren()) = %d, want 3 (1 literal + 2 forwarded)", len(got))
		}
		for i, child := range got {
			if got := child.Call.Item["index"]; got != i {
				t.Errorf("child[%d].Item[index] = %v, want %d", i, got, i)
			}
			if got := child.Call.Item["count"]; got != 3 {
				t.Errorf("child[%d].Item[count] = %v, want 3 over the full passed set", i, got)
			}
		}
	})
}

// TestResolveChildrenEmptyAndMalformed proves the empty-set and malformed-input
// contract: a nil parent with an empty set yields no children, a nil parent
// with requests is an error, a nil request element is an error, and a
// forwarded entry that is not a `.data` map is an error naming the section.
func TestResolveChildrenEmptyAndMalformed(t *testing.T) {
	column := &Template{Name: "column", Usage: UsageSection}
	resolve := resolveSectionCallResolver(column)
	parent := &Template{
		Name:     "parent",
		Usage:    UsageSection,
		Sections: []SectionDecl{{Name: "column", Accepted: []string{"column"}}},
	}

	t.Run("a nil parent with an empty set yields no children", func(t *testing.T) {
		got, err := ResolveChildren(resolve, nil, nil, nil, nil, nil, true)
		if err != nil {
			t.Fatalf("ResolveChildren(nil parent, empty set) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("ResolveChildren(nil parent, empty set) = %v, want nil", got)
		}
	})

	t.Run("a nil parent with requests is an error", func(t *testing.T) {
		_, err := ResolveChildren(resolve, nil, nil, []*ChildRequest{{Section: "column"}}, nil, nil, true)
		want := "children: no parent template to resolve declared sections against"
		if err == nil || err.Error() != want {
			t.Errorf("ResolveChildren(nil parent) error = %v, want %q", err, want)
		}
	})

	t.Run("a nil request element is an error", func(t *testing.T) {
		_, err := ResolveChildren(resolve, nil, parent, []*ChildRequest{nil}, nil, nil, true)
		want := "children: child request must not be nil"
		if err == nil || err.Error() != want {
			t.Errorf("ResolveChildren(nil request) error = %v, want %q", err, want)
		}
	})

	t.Run("a malformed forwarded entry is an error", func(t *testing.T) {
		reqs := []*ChildRequest{{Section: "column", Forward: true, Entries: []any{"nope"}}}
		_, err := ResolveChildren(resolve, nil, parent, reqs, nil, nil, true)
		want := `children "column": forwarded entry must be a .data entry map, got a quoted string`
		if err == nil || err.Error() != want {
			t.Errorf("ResolveChildren(bad entry) error = %v, want %q", err, want)
		}
	})
}

// TestExampleSectionHelperChildRecursion proves the load-time example helper
// resolves and renders helper children recursively: a section-usage container
// whose layout calls `{{ section "…" nil nil (child …) }}` with a child that
// itself passes nested `child` arguments renders each grandchild through its own
// template, spliced into the parent's declared child-section list bottom-up. It
// proves a nested helper child publishes its grandchildren to its OWN context,
// not only the parent's `.data` view: the card child reads its own rendered
// grandchild list `.labels` and its own `.data.labels` raw view, both carried on
// its own SectionCall.Children. It also proves a resolution violation in a
// helper child propagates as an error.
func TestExampleSectionHelperChildRecursion(t *testing.T) {
	label := &Template{
		Name:   "label",
		Usage:  UsageSection,
		Fields: []Field{{Name: "text", Type: FieldText, Required: true}},
		Body:   BodyRule{Mode: BodyDisallowed},
		Layout: Layout{Name: "label", Text: `<i class="label">{{ .text }}</i>`},
	}
	// card is the child the group helper call passes through `child`: it must be
	// able to read its OWN nested children, not only the group's `.data` view of
	// them. Its own rendered grandchild list `.labels` (the `<i class="label">`
	// markup the labels render) and its own `.data.labels` raw view (the labels'
	// authored `.raw.text` values) both live on the card child's own
	// SectionCall.Children, so this is the level that proves a nested helper
	// child publishes its grandchildren to its own `.data` and rendered lists.
	card := &Template{
		Name:     "card",
		Usage:    UsageSection,
		Fields:   []Field{{Name: "title", Type: FieldText, Required: true}},
		Body:     BodyRule{Mode: BodyOptional},
		Sections: []SectionDecl{{Name: "labels", Accepted: []string{"label"}, Min: 1}},
		Layout: Layout{Name: "card", Text: `<b class="card">{{ .title }}{{ range .labels }}{{ . }}{{ end }}</b>` +
			`<ul class="card-data">{{ range .data.labels }}<li>{{ .raw.text }}</li>{{ end }}</ul>`},
	}
	// group is the helper call's target: it ranges over its rendered child list
	// `.cards` and, in parallel, over the reserved `.data.cards` view, whose
	// entries recurse into the child's own `.data.labels`. That is the
	// parent-side view of the resolved child tree, so reading it proves the
	// nested grandchildren were resolved recursively and are visible from the
	// target's own `.data`; the card child above proves the same tree is
	// visible from the child's OWN context.
	group := &Template{
		Name:     "group",
		Usage:    UsageSection,
		Body:     BodyRule{Mode: BodyOptional},
		Sections: []SectionDecl{{Name: "cards", Accepted: []string{"card"}, Min: 1}},
		Layout: Layout{Name: "group", Text: `<div class="group">{{ range .cards }}{{ . }}{{ end }}` +
			`<ul>{{ range .data.cards }}<li>{{ .item.template }}{{ range .data.labels }}{{ .raw.text }}{{ end }}</li>{{ end }}</ul>` +
			`</div>`},
	}
	container := &Template{
		Name:  "container",
		Usage: UsageSection,
		Body:  BodyRule{Mode: BodyOptional},
		Layout: Layout{Name: "container", Text: `<div class="container">` +
			`{{ section "group" nil nil (child "cards" (dict "title" "T1") nil (child "labels" (dict "text" "L1")) (child "labels" (dict "text" "L2"))) }}` +
			`</div>`},
	}

	lib := sectionHelperTestLibrary(label, card, group, container)
	helper := exampleSectionHelper(lib, nil)

	t.Run("renders helper children recursively through their own templates", func(t *testing.T) {
		got, err := helper("container")
		if err != nil {
			t.Fatalf("helper(container) error = %v, want nil", err)
		}
		want := `<div class="container">` +
			`<div class="group">` +
			`<b class="card">T1<i class="label">L1</i><i class="label">L2</i></b>` +
			`<ul class="card-data"><li>L1</li><li>L2</li></ul>` +
			`<ul><li>cardL1L2</li></ul>` +
			`</div>` +
			`</div>`
		if string(got) != want {
			t.Errorf("helper(container) = %q, want %q", string(got), want)
		}

		// The card child reads its OWN context, not only the group's `.data.cards`
		// view: its own resolved nested children live on its own SectionCall, so
		// both its rendered grandchild list (`.labels`, the `<i class="label">`
		// markup) and its own `.data.labels` raw values must appear in the page.
		own := []struct {
			what string
			frag string
		}{
			{"rendered grandchild list", `<b class="card">T1<i class="label">L1</i><i class="label">L2</i></b>`},
			{"own .data.labels raw view", `<ul class="card-data"><li>L1</li><li>L2</li></ul>`},
		}
		for _, a := range own {
			if !strings.Contains(string(got), a.frag) {
				t.Errorf("helper(container) = %q, want it to contain the card child's %s %q", string(got), a.what, a.frag)
			}
		}
	})

	t.Run("propagates a nested helper child resolution failure", func(t *testing.T) {
		badContainer := &Template{
			Name:  "badcontainer",
			Usage: UsageSection,
			Body:  BodyRule{Mode: BodyOptional},
			Layout: Layout{Name: "badcontainer", Text: `<div>` +
				`{{ section "group" nil nil (child "cards" (dict "title" "T1") nil (child "bogus" nil)) }}` +
				`</div>`},
		}
		badLib := sectionHelperTestLibrary(label, card, group, badContainer)

		_, err := exampleSectionHelper(badLib, nil)("badcontainer")
		if err == nil {
			t.Fatal("helper(badcontainer) error = nil, want the undeclared nested child section rejected")
		}
		if want := `child: section "bogus" is not declared by parent template "card"`; !strings.Contains(err.Error(), want) {
			t.Errorf("helper(badcontainer) error = %v, want it to contain %q", err, want)
		}
	})
}
