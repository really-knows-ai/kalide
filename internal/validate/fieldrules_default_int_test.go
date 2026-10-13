package validate

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/really-knows-ai/kalide/internal/template"
)

// This file is the internal/validate.checkFields half of the integration-test
// deliverable for section-helpers/plan.phase-07.task-2 (field-rules), extended
// by section-helpers/plan.phase-02.task-10 (field-types/field-rules): the single
// function every slide/section field check flows through treats an omitted
// required field that declares a Default as satisfied (it delegates to the
// schema-only template.CheckValues and materialises nothing), while an omitted
// required field with NO Default is still reported. The apply-at-render half and
// the example-context half live in
// internal/render/fieldrules_default_integration_test.go and
// internal/template/fieldrules_default_int_test.go respectively.
//
// checkFields is the SLIDE-FRONTMATTER validation site (validateSlide calls it
// with a nil containment prefix) and the SECTION-INSTANCE site (checkSections
// calls it with the instance's containment chain). Task 10 below pins the
// slide-frontmatter site: through the shared CheckValues engine it reports every
// field type and rule with a positioned, path-qualified error. Task 14 pins the
// section-instance site: the same engine reaches the same verdict and the same
// first error at every instance nesting depth, only with the instance's
// containment chain prepended to the value path (nested-section-validation).
//
// checkFields has no filesystem dependency of its own (fsys is used only for
// image existence), but it is the field-check step of the real int pipeline, so
// this test is guarded with testing.Short alongside its siblings.

// slideFrontmatterSite is the deck-relative slide path every error in this test
// is positioned in: the exact file validateSlide passes to checkFields for a
// slide's frontmatter. The frontmatter call attaches no line source (only the
// engine's image-existence callback is passed), so the rendered location is the
// file alone — validate.wantError pins the same `slides/… › field: …` shape for
// a built-in template.
const slideFrontmatterSite = "slides/1-a.md"

// TestFieldRulesDefaultCheckFieldsInt pins checkFields — the slide-frontmatter
// validation site, the call validateSlide makes with a nil containment prefix —
// as the internal/validate half of the field-types/field-rules deliverable.
//
// The default rule is pinned first, directly: the defaulted omission passes, the
// undefaulted omission fails with the field named.
//
// Then, because checkFields is a thin adapter over the shared
// template.CheckValues engine (plan.phase-02.task-10), the test drives that
// engine through the site for every field type — text, number, date, boolean,
// enum, image, link, list, record and section-template — and every rule:
// required (with default), max_length, number and date bounds, date format, enum
// membership, image prefix/existence, link scheme and label, min_items/
// max_items, unknown-field suggestion and nullable. Each case asserts the
// positioned (`slides/…` file), path-qualified error that the engine's own unit
// table (internal/template/checkvalues_test.go) fixes, including the
// `items › [i] › field` chain that record/list nesting produces.
//
// The section-instance site is pinned in this same test function
// (plan.phase-02.task-14): a dedicated containment-path case and, for every
// table entry above, a second run through checkFields with the instance's
// containment chain as prefix, asserting the same shared-engine verdict and
// error with the chain prepended — at a top-level instance and at deeper,
// nested instances.
func TestFieldRulesDefaultCheckFieldsInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: the field-check step of the real validate pipeline")
	}

	emptyReg := template.NewRegistry(nil)
	emptyFS := fstest.MapFS{}

	// sectionInstancePrefixes are containment chains checkSections hands to
	// checkFields: the current instance's own `name[index]` segment extends the
	// enclosing chain, so one segment is a top-level section instance and two or
	// more are instances nested that deep. The section-instance site must reach
	// the same verdict and the same shared-engine first error at each depth,
	// only with the chain prepended to the value path
	// (nested-section-validation).
	sectionInstancePrefixes := [][]string{
		{"columns[2]"},
		{"columns[2]", "blocks[1]"},
		{"columns[0]", "blocks[1]", "rows[2]"},
	}

	t.Run("an omitted required field with a default passes", func(t *testing.T) {
		tmpl := &template.Template{
			Name:   "footer",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "title", Type: template.FieldText, Required: true, Default: "Untitled"}},
		}
		verr, invalid := checkFields(emptyFS, slideFrontmatterSite, map[string]any{}, tmpl, nil, emptyReg)
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
		verr, invalid := checkFields(emptyFS, slideFrontmatterSite, map[string]any{}, tmpl, nil, emptyReg)
		if !invalid {
			t.Fatal("checkFields() invalid = false, want the required error")
		}
		got := Format(verr)
		for _, want := range []string{slideFrontmatterSite, "title", "required"} {
			if !strings.Contains(got, want) {
				t.Errorf("checkFields() error = %q, want it to contain %q", got, want)
			}
		}
	})

	// The site is a thin adapter over the shared engine: for the same data and
	// template, the error checkFields returns is exactly the engine's FIRST
	// error, adapted to the slide file position with its path carried across.
	t.Run("the site surfaces the shared engine's first error", func(t *testing.T) {
		tmpl := slideSiteTmpl(
			template.Field{Name: "bad_text", Type: template.FieldText},
			template.Field{Name: "bad_num", Type: template.FieldNumber},
		)
		data := map[string]any{"bad_text": 42, "bad_num": true}

		res := template.CheckValues(data, tmpl, emptyReg.Lookup)
		if len(res.Errors) == 0 {
			t.Fatal("CheckValues() reported no error for the offending data")
		}

		verr, invalid := checkFields(emptyFS, slideFrontmatterSite, data, tmpl, nil, emptyReg)
		if !invalid {
			t.Fatal("checkFields() invalid = false, want the engine's first error")
		}
		want := New(slideFrontmatterSite, res.Errors[0].Line, valuePath(res.Errors[0].Path), res.Errors[0].What, res.Errors[0].Fix)
		if !reflect.DeepEqual(verr, want) {
			t.Errorf("checkFields() error = %#v, want the adapted shared-engine error %#v", verr, want)
		}
	})

	// The section-instance site is the same checkFields, only called with the
	// instance's containment chain instead of a nil prefix. Its error must carry
	// that full chain — the nested-section-validation AC example, a `label`
	// typo `lable` inside blocks[1] under columns[2] — with the shared engine's
	// exact What and Fix.
	t.Run("the section-instance site carries the instance's containment path", func(t *testing.T) {
		tmpl := slideSiteTmpl(template.Field{Name: "label", Type: template.FieldText})
		prefix := []string{"columns[2]", "blocks[1]"}

		verr, invalid := checkFields(emptyFS, slideFrontmatterSite, map[string]any{"lable": "x"}, tmpl, prefix, emptyReg)
		if !invalid {
			t.Fatal("checkFields() invalid = false, want the nested unknown-field error")
		}
		want := displayFile(slideFrontmatterSite) +
			` › columns[2] › blocks[1] › lable: unknown field "lable" — did you mean "label"?`
		if got := Format(verr); got != want {
			t.Errorf("checkFields() error =\n  %q\nwant\n  %q", got, want)
		}
		if verr.File != slideFrontmatterSite {
			t.Errorf("error File = %q, want %q", verr.File, slideFrontmatterSite)
		}
		wantPath := []string{"columns[2]", "blocks[1]", "lable"}
		if !reflect.DeepEqual(verr.Path, wantPath) {
			t.Errorf("error Path = %v, want %v", verr.Path, wantPath)
		}
	})

	// --- every field type and rule, positioned and path-qualified ---------

	zero := 0.0

	textTmpl := slideSiteTmpl(template.Field{Name: "text", Type: template.FieldText})
	numTmpl := slideSiteTmpl(template.Field{Name: "num", Type: template.FieldNumber})
	numMinTmpl := slideSiteTmpl(template.Field{Name: "num", Type: template.FieldNumber, Min: siteFloatPtr(0)})
	numMaxTmpl := slideSiteTmpl(template.Field{Name: "num", Type: template.FieldNumber, Max: siteFloatPtr(10)})
	dateTmpl := slideSiteTmpl(template.Field{Name: "date", Type: template.FieldDate})
	dateMinTmpl := slideSiteTmpl(template.Field{Name: "date", Type: template.FieldDate, MinDate: "2020-01-01"})
	dateMaxTmpl := slideSiteTmpl(template.Field{Name: "date", Type: template.FieldDate, MaxDate: "2025-12-31"})
	boolTmpl := slideSiteTmpl(template.Field{Name: "flag", Type: template.FieldBoolean})
	enumTmpl := slideSiteTmpl(template.Field{Name: "mode", Type: template.FieldEnum, Variants: []string{"a", "b"}})
	imageTmpl := slideSiteTmpl(template.Field{Name: "img", Type: template.FieldImage})
	linkTmpl := slideSiteTmpl(template.Field{Name: "url", Type: template.FieldLink})
	listTmpl := slideSiteTmpl(template.Field{
		Name: "items", Type: template.FieldList, Item: &template.Field{Type: template.FieldText},
		MinItems: 1, MaxItems: 2,
	})
	reqTmpl := slideSiteTmpl(template.Field{Name: "req", Type: template.FieldText, Required: true})
	longTmpl := slideSiteTmpl(template.Field{Name: "long", Type: template.FieldText, MaxLength: 2})
	suggestTmpl := slideSiteTmpl(template.Field{Name: "title", Type: template.FieldText})
	nullableTmpl := slideSiteTmpl(template.Field{Name: "title", Type: template.FieldText, Nullable: true})
	coerceTmpl := slideSiteTmpl(template.Field{Name: "coerced", Type: template.FieldText, Coerce: true})

	// A section template the section-template field resolves against; its
	// required `title` exercises the engine's recursion into a nested schema.
	col := &template.Template{
		Name:   "col",
		Usage:  template.UsageSection,
		Fields: []template.Field{{Name: "title", Type: template.FieldText, Required: true}},
	}
	colReg := mustSlideSiteRegistry(t, col)
	sectionTmpl := slideSiteTmpl(template.Field{
		Name: "block", Type: template.FieldSectionTemplate, SectionTemplate: "col",
	})

	recordTmpl := slideSiteTmpl(template.Field{
		Name: "items", Type: template.FieldList,
		Item: &template.Field{Type: template.FieldRecord, Fields: []template.Field{
			{Name: "title", Type: template.FieldText, Required: true},
		}},
	})
	nestedRecordTmpl := slideSiteTmpl(template.Field{
		Name: "items", Type: template.FieldList,
		Item: &template.Field{Type: template.FieldRecord, Fields: []template.Field{
			{Name: "values", Type: template.FieldList, Item: &template.Field{
				Type: template.FieldRecord, Fields: []template.Field{
					{Name: "n", Type: template.FieldNumber, Min: &zero},
				},
			}},
		}},
	})

	// imageFS holds the one image the accept case references, so the site's
	// real fs.Stat existence check (validateSlide's fsys) is exercised.
	imageFS := fstest.MapFS{"assets/pic.png": &fstest.MapFile{}}

	// frontmatterWant renders the exact string the slide-frontmatter site
	// produces for one violation: the slide file, the path-qualified field
	// chain, then what — fix. It mirrors Format for a line-less error.
	frontmatterWant := func(path, what, fix string) string {
		s := displayFile(slideFrontmatterSite) + segmentSeparator + path + ": " + what
		if fix != "" {
			s += fixSeparator + fix
		}
		return s
	}

	cases := []struct {
		name  string
		tmpl  *template.Template
		reg   *template.Registry // nil -> emptyReg
		fsys  fstest.MapFS       // nil -> emptyFS
		data  map[string]any
		valid bool
		want  string
	}{
		// --- every field type: accept then reject -------------------------
		{
			name: "text rejects a number", tmpl: textTmpl, data: map[string]any{"text": 42},
			want: frontmatterWant("text", "type: expected a text field, got a number", "quote the value as a string"),
		},
		{
			name: "number rejects a boolean", tmpl: numTmpl, data: map[string]any{"num": true},
			want: frontmatterWant("num", "type: expected a number, got a boolean", "write the number without quotes"),
		},
		{
			name: "number rejects a quoted value", tmpl: numTmpl, data: map[string]any{"num": "42"},
			want: frontmatterWant("num", `type: expected a number, got the quoted value "42"`, "write the number without quotes"),
		},
		{
			name: "date rejects a number", tmpl: dateTmpl, data: map[string]any{"date": 1},
			want: frontmatterWant("date", "type: expected a YYYY-MM-DD date, got a number", "write the date as YYYY-MM-DD"),
		},
		{
			name: "boolean rejects a quoted string", tmpl: boolTmpl, data: map[string]any{"flag": "yes"},
			want: frontmatterWant("flag", "type: expected a boolean, got a quoted string", "write true or false without quotes"),
		},
		{
			name: "enum rejects a number", tmpl: enumTmpl, data: map[string]any{"mode": 3},
			want: frontmatterWant("mode", "type: expected one of the enum variants, got a number", "use one of: a, b"),
		},
		{
			name: "image rejects a number", tmpl: imageTmpl, data: map[string]any{"img": 5},
			want: frontmatterWant("img", "type: expected an image path, got a number", "write the path as a string"),
		},
		{
			name: "link rejects a number", tmpl: linkTmpl, data: map[string]any{"url": 5},
			want: frontmatterWant("url", "type: expected a link, got a number", "write the link as a string"),
		},
		{
			name: "list rejects a quoted string", tmpl: listTmpl, data: map[string]any{"items": "notalist"},
			want: frontmatterWant("items", "type: expected a list, got a quoted string", "write the value as a YAML sequence"),
		},
		{
			name: "section-template rejects a quoted string", tmpl: sectionTmpl, reg: colReg, data: map[string]any{"block": "notmap"},
			want: frontmatterWant("block", `type: expected a mapping for section template "col", got a quoted string`, "write the section data as YAML key: value pairs"),
		},
		{
			name: "coerce text still rejects a mapping", tmpl: coerceTmpl, data: map[string]any{"coerced": map[string]any{"a": 1}},
			want: frontmatterWant("coerced", "type: expected a text field, got a mapping", "quote the value as a string"),
		},

		// --- every field rule --------------------------------------------
		{
			name: "required absent is rejected", tmpl: reqTmpl, data: map[string]any{},
			want: frontmatterWant("req", `required: field "req" is required but missing`, "add a req: value"),
		},
		{
			name: "max_length is enforced", tmpl: longTmpl, data: map[string]any{"long": "abc"},
			want: frontmatterWant("long", "max_length: text is 3 characters, maximum is 2", "shorten the value"),
		},
		{
			name: "number min is enforced", tmpl: numMinTmpl, data: map[string]any{"num": -1},
			want: frontmatterWant("num", "min: -1 is below the minimum 0", "use a value of at least 0"),
		},
		{
			name: "number max is enforced", tmpl: numMaxTmpl, data: map[string]any{"num": 11},
			want: frontmatterWant("num", "max: 11 exceeds the maximum 10", "use a value of at most 10"),
		},
		{
			name: "date min is enforced", tmpl: dateMinTmpl, data: map[string]any{"date": "2019-01-01"},
			want: frontmatterWant("date", "min: 2019-01-01 is below the minimum 2020-01-01", "use a date on or after 2020-01-01"),
		},
		{
			name: "date max is enforced", tmpl: dateMaxTmpl, data: map[string]any{"date": "2026-01-01"},
			want: frontmatterWant("date", "max: 2026-01-01 exceeds the maximum 2025-12-31", "use a date on or before 2025-12-31"),
		},
		{
			name: "malformed date is rejected", tmpl: dateTmpl, data: map[string]any{"date": "25/09/2026"},
			want: frontmatterWant("date", `date: "25/09/2026" is not a YYYY-MM-DD date`, "write the date as YYYY-MM-DD, for example 2024-01-02"),
		},
		{
			name: "enum membership is enforced", tmpl: enumTmpl, data: map[string]any{"mode": "c"},
			want: frontmatterWant("mode", `enum: "c" is not one of the allowed variants (a, b)`, `did you mean "a"? use one of: a, b`),
		},
		{
			name: "image must be under assets", tmpl: imageTmpl, data: map[string]any{"img": "pic.png"},
			want: frontmatterWant("img", `image: "pic.png" is not under assets/`, "move the image into the deck's assets/ directory and reference it as assets/…"),
		},
		{
			name: "image must exist in the deck", tmpl: imageTmpl, fsys: fstest.MapFS{}, data: map[string]any{"img": "assets/missing.png"},
			want: frontmatterWant("img", `image: "assets/missing.png" does not exist`, "add the file under assets/ or fix the path"),
		},
		{
			name: "link must be a label or an http URL", tmpl: linkTmpl, data: map[string]any{"url": "mailto:x@example.com"},
			want: frontmatterWant("url", `link: "mailto:x@example.com" is neither a #label anchor nor an http(s) URL`, "use a #label anchor or an http(s) URL"),
		},
		{
			name: "link label must be non-empty", tmpl: linkTmpl, data: map[string]any{"url": "#"},
			want: frontmatterWant("url", `link: "#" has an empty label`, "use a non-empty #label, for example #summary"),
		},
		{
			name: "list min_items is enforced", tmpl: listTmpl, data: map[string]any{"items": []any{}},
			want: frontmatterWant("items", "min_items: 0 item(s) is below the minimum 1", "add at least 1 item(s)"),
		},
		{
			name: "list max_items is enforced", tmpl: listTmpl, data: map[string]any{"items": []any{"a", "b", "c"}},
			want: frontmatterWant("items", "max_items: 3 item(s) exceeds the maximum 2", "remove items until at most 2 remain"),
		},
		{
			name: "unknown field carries a suggestion", tmpl: suggestTmpl, data: map[string]any{"titel": "x"},
			want: frontmatterWant("titel", `unknown field "titel"`, `did you mean "title"?`),
		},
		{
			name: "non-nullable null is rejected", tmpl: suggestTmpl, data: map[string]any{"title": nil},
			want: frontmatterWant("title", "got null", "this field isn't nullable"),
		},

		// --- path qualification through nesting ---------------------------
		{
			name: "record item required field carries the full path", tmpl: recordTmpl,
			data: map[string]any{"items": []any{map[string]any{}}},
			want: frontmatterWant("items › [0] › title", `required: field "title" is required but missing`, "add a title: value"),
		},
		{
			name: "record-list-record violation carries the full path", tmpl: nestedRecordTmpl,
			data: map[string]any{"items": []any{map[string]any{"values": []any{map[string]any{"n": -1}}}}},
			want: frontmatterWant("items › [0] › values › [0] › n", "min: -1 is below the minimum 0", "use a value of at least 0"),
		},
		{
			name: "section-template field violation carries the nested path", tmpl: sectionTmpl, reg: colReg,
			data: map[string]any{"block": map[string]any{}},
			want: frontmatterWant("block › title", `required: field "title" is required but missing`, "add a title: value"),
		},

		// --- every field type accepts a valid value -----------------------
		{name: "text accepts a string", tmpl: textTmpl, data: map[string]any{"text": "hello"}, valid: true},
		{name: "number accepts a number", tmpl: numMinTmpl, data: map[string]any{"num": 3}, valid: true},
		{name: "date accepts a date", tmpl: dateTmpl, data: map[string]any{"date": "2026-09-25"}, valid: true},
		{name: "boolean accepts a boolean", tmpl: boolTmpl, data: map[string]any{"flag": true}, valid: true},
		{name: "enum accepts a variant", tmpl: enumTmpl, data: map[string]any{"mode": "a"}, valid: true},
		{name: "image accepts an existing assets path", tmpl: imageTmpl, fsys: imageFS, data: map[string]any{"img": "assets/pic.png"}, valid: true},
		{name: "link accepts a #label", tmpl: linkTmpl, data: map[string]any{"url": "#summary"}, valid: true},
		{name: "list accepts its items", tmpl: listTmpl, data: map[string]any{"items": []any{"a"}}, valid: true},
		{
			name: "section-template accepts a mapping", tmpl: sectionTmpl, reg: colReg,
			data: map[string]any{"block": map[string]any{"title": "x"}}, valid: true,
		},
		{
			name: "required with a default accepts omission",
			tmpl: slideSiteTmpl(template.Field{Name: "title", Type: template.FieldText, Required: true, Default: "Untitled"}),
			data: map[string]any{}, valid: true,
		},
		{name: "nullable accepts null", tmpl: nullableTmpl, data: map[string]any{"title": nil}, valid: true},
		{name: "coerce text accepts a number", tmpl: coerceTmpl, data: map[string]any{"coerced": 42}, valid: true},
		{
			name: "the reserved template selector is stripped",
			tmpl: suggestTmpl, data: map[string]any{"template": "slide", "title": "x"}, valid: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			caseReg := tt.reg
			if caseReg == nil {
				caseReg = emptyReg
			}
			caseFS := tt.fsys
			if caseFS == nil {
				caseFS = emptyFS
			}

			verr, invalid := checkFields(caseFS, slideFrontmatterSite, tt.data, tt.tmpl, nil, caseReg)

			if tt.valid {
				if invalid {
					t.Fatalf("checkFields() invalid = true (%s), want false: the value is valid for its type and rules", Format(verr))
				}
				if !isZeroValidationError(verr) {
					t.Errorf("checkFields() error = %#v, want the zero ValidationError", verr)
				}
			} else {
				if !invalid {
					t.Fatalf("checkFields() invalid = false, want the error %q", tt.want)
				}
				if got := Format(verr); got != tt.want {
					t.Errorf("checkFields() error =\n  %q\nwant\n  %q", got, tt.want)
				}
				// Positioned in the slide file: the slide-frontmatter call
				// attaches no line source, so the location is the file alone and
				// the path carries the field chain.
				if verr.File != slideFrontmatterSite {
					t.Errorf("error File = %q, want %q", verr.File, slideFrontmatterSite)
				}
			}

			// The section-instance site: checkSections calls the same checkFields
			// with the instance's containment chain as prefix. Through the shared
			// CheckValues engine every field type and rule must reach the same
			// verdict and the same first error at every nesting depth, only with
			// the chain prepended to the value path (nested-section-validation).
			for _, prefix := range sectionInstancePrefixes {
				instVerr, instInvalid := checkFields(caseFS, slideFrontmatterSite, tt.data, tt.tmpl, prefix, caseReg)
				if instInvalid != invalid {
					t.Fatalf("checkFields(prefix %v) invalid = %v, want %v: the shared engine decides both sites", prefix, instInvalid, invalid)
				}
				if !invalid {
					continue
				}
				if instVerr.File != slideFrontmatterSite {
					t.Errorf("checkFields(prefix %v) File = %q, want %q", prefix, instVerr.File, slideFrontmatterSite)
				}
				if instVerr.What != verr.What || instVerr.Fix != verr.Fix {
					t.Errorf("checkFields(prefix %v) = (%q, %q), want the slide site's (%q, %q)",
						prefix, instVerr.What, instVerr.Fix, verr.What, verr.Fix)
				}
				wantPath := append(append([]string(nil), prefix...), verr.Path...)
				if !reflect.DeepEqual(instVerr.Path, wantPath) {
					t.Errorf("checkFields(prefix %v) Path = %v, want %v", prefix, instVerr.Path, wantPath)
				}
			}
		})
	}
}

// slideSiteTmpl builds a slide-usage template holding the given fields; the
// slide-frontmatter site only inspects Usage and Fields, so no layout, example
// or body is needed.
func slideSiteTmpl(fields ...template.Field) *template.Template {
	return &template.Template{Name: "slide", Usage: template.UsageSlide, Fields: fields}
}

// mustSlideSiteRegistry registers the given templates into a fresh registry so a
// section-template field can resolve them, mirroring how validateSlide's reg is
// built.
func mustSlideSiteRegistry(t *testing.T, tmpls ...*template.Template) *template.Registry {
	t.Helper()
	reg := template.NewRegistry(nil)
	for _, tmpl := range tmpls {
		if err := reg.Register(tmpl); err != nil {
			t.Fatalf("register %q: %v", tmpl.Name, err)
		}
	}
	return reg
}

// siteFloatPtr returns a pointer to v, for a Field.Min/Max inclusive bound.
func siteFloatPtr(v float64) *float64 { return &v }

// isZeroValidationError reports whether e is the zero ValidationError.
func isZeroValidationError(e ValidationError) bool {
	return e.File == "" && e.Line == 0 && len(e.Path) == 0 && e.What == "" && e.Fix == ""
}
