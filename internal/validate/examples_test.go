package validate

import (
	"os"
	"testing"

	"github.com/really-knows-ai/ey-present/internal/template"
)

// fixtureLibraryDir is the phase-3 fixture library's project root, relative
// to this package, holding templates/slides/hello, templates/sections/item,
// templates/themes/plain and templates/media/logo.svg.
const fixtureLibraryDir = "../template/testdata/library"

// TestValidateBuiltinExamples covers ValidateBuiltinExamples, the build-time
// proof that every compiled-in template's example slide survives the full
// validation pipeline (whole-deck-validation, template-build-checks):
//
//   - every built-in template's example validates end to end with no error;
//   - a fixture registry whose example is deliberately broken — a bad Markdown
//     construct, a bad section fence, a field rule violation — reports exactly
//     that template's name and the first error, rendered by Format;
//   - failures are returned in template-name order and the result is
//     deterministic;
//   - a nil registry is a reported failure, not a panic.
//
// Every fixture registry is built in code with template.NewRegistry(nil), so an
// example's Markdown is carried on the Template itself and the suite touches
// neither the disk nor the network; the built-in check reads the compiled-in
// assets through template.Builtins.
func TestValidateBuiltinExamples(t *testing.T) {
	t.Run("built-in examples are valid", testBuiltinExamplesValid)
	for _, tc := range brokenExampleCases() {
		tc := tc
		t.Run("broken example: "+tc.tmpl.Name, func(t *testing.T) {
			wantExampleFailure(t, tc.tmpl, tc.tmpl.Name, tc.want)
		})
	}
	t.Run("failures are in template-name order", testExampleFailuresOrdered)
	t.Run("nil registry", testExampleNilRegistry)
}

// testBuiltinExamplesValid asserts every compiled-in template's example passes
// the whole pipeline: ValidateBuiltinExamples returns no failures.
func testBuiltinExamplesValid(t *testing.T) {
	reg := mustBuiltins(t)
	errs := ValidateBuiltinExamples(reg)
	if len(errs) == 0 {
		return
	}
	for _, e := range errs {
		t.Errorf("template %q example invalid: %s", e.Template, Format(e.Err))
	}
	t.Fatalf("ValidateBuiltinExamples(builtins) reported %d failing template(s), want 0", len(errs))
}

// exampleCase is one deliberately broken example fixture and the exact
// formatted first error its template must produce.
type exampleCase struct {
	tmpl *template.Template
	want string
}

// brokenExampleCases returns the three fixture templates, each carrying one
// distinct class of first failure, together with the string Format must render
// from the returned ExampleError.Err. The synthetic deck's single slide is
// always slides/1-example.md.
func brokenExampleCases() []exampleCase {
	return []exampleCase{
		// A bad Markdown construct in the body: mdcheck rejects extension
		// syntax the CommonMark core leaves as literal text. The body starts at
		// line 4, so the strikethrough is reported at line 5.
		{
			tmpl: &template.Template{
				Name:  "md",
				Usage: template.UsageSlide,
				Body:  template.BodyRule{Mode: template.BodyOptional},
				Example: template.Example{
					Markdown: "---\ntemplate: md\n---\n\nThis is ~~struck~~ text.\n",
				},
			},
			want: `slides/1-example.md:5: strikethrough (~~text~~) is not supported — remove the ~~ markers or use plain text`,
		},

		// A bad section fence: a language tag on a section's frontmatter fence.
		// The parser reports it at the fence line, 5.
		{
			tmpl: &template.Template{
				Name:  "fence",
				Usage: template.UsageSlide,
				Sections: []template.SectionDecl{{
					Name:     "block",
					Accepted: []string{"block"},
				}},
				Body: template.BodyRule{Mode: template.BodyOptional},
				Example: template.Example{
					Markdown: "---\ntemplate: fence\n---\n# block\n```yaml\ntitle: x\n```\n",
				},
			},
			want: `slides/1-example.md:5: section frontmatter fence must not have a language tag ("yaml")`,
		},

		// A field rule violation: the required `heading` field is absent. Field
		// errors carry no retained source line, so the formatted error has no
		// :line suffix and reports the field path instead.
		{
			tmpl: &template.Template{
				Name:  "fields",
				Usage: template.UsageSlide,
				Fields: []template.Field{{
					Name:     "heading",
					Type:     template.FieldText,
					Required: true,
				}},
				Body: template.BodyRule{Mode: template.BodyOptional},
				Example: template.Example{
					Markdown: "---\ntemplate: fields\n---\n",
				},
			},
			want: `slides/1-example.md › heading: required: field "heading" is required but missing — add a heading: value`,
		},
	}
}

// wantExampleFailure registers tmpl alone and asserts ValidateBuiltinExamples
// reports exactly one failure — for wantTemplate — whose Err formats to want.
func wantExampleFailure(t *testing.T, tmpl *template.Template, wantTemplate, want string) {
	t.Helper()
	errs := ValidateBuiltinExamples(mustRegistry(t, tmpl))
	if len(errs) != 1 {
		t.Fatalf("ValidateBuiltinExamples(%q) = %d failures, want exactly 1", tmpl.Name, len(errs))
	}
	if errs[0].Template != wantTemplate {
		t.Errorf("ExampleError.Template = %q, want %q", errs[0].Template, wantTemplate)
	}
	if got := Format(errs[0].Err); got != want {
		t.Errorf("Format(ExampleError.Err) =\n  %q\nwant\n  %q", got, want)
	}
}

// testExampleFailuresOrdered registers all three broken templates at once and
// asserts every one is reported, each with its expected first error, in
// template-name order: fence, fields, md.
func testExampleFailuresOrdered(t *testing.T) {
	cases := brokenExampleCases()
	tmpls := make([]*template.Template, len(cases))
	want := make(map[string]string, len(cases))
	for i, tc := range cases {
		tmpls[i] = tc.tmpl
		want[tc.tmpl.Name] = tc.want
	}

	errs := ValidateBuiltinExamples(mustRegistry(t, tmpls...))
	if len(errs) != len(cases) {
		t.Fatalf("ValidateBuiltinExamples = %d failures, want %d", len(errs), len(cases))
	}
	for i, e := range errs {
		if i > 0 && errs[i-1].Template > e.Template {
			t.Errorf("failures not in template-name order: %q reported before %q", errs[i-1].Template, e.Template)
		}
		if got := Format(e.Err); got != want[e.Template] {
			t.Errorf("template %q: Format(ExampleError.Err) =\n  %q\nwant\n  %q", e.Template, got, want[e.Template])
		}
	}

	// The same registry must yield the same failures on every run.
	again := ValidateBuiltinExamples(mustRegistry(t, tmpls...))
	if len(again) != len(errs) {
		t.Fatalf("second run = %d failures, first run = %d", len(again), len(errs))
	}
	for i := range errs {
		if again[i].Template != errs[i].Template || Format(again[i].Err) != Format(errs[i].Err) {
			t.Fatalf("run mismatch at %d: got (%q, %q), first (%q, %q)",
				i, again[i].Template, Format(again[i].Err), errs[i].Template, Format(errs[i].Err))
		}
	}
}

// testExampleNilRegistry asserts a nil registry is a reported failure carrying
// the documented guidance, never a panic.
func testExampleNilRegistry(t *testing.T) {
	errs := ValidateBuiltinExamples(nil)
	if len(errs) != 1 {
		t.Fatalf("ValidateBuiltinExamples(nil) = %d failures, want exactly 1", len(errs))
	}
	if errs[0].Template != "" {
		t.Errorf("ExampleError.Template = %q, want the empty string", errs[0].Template)
	}
	const want = ": no template registry given — pass template.Builtins()' registry"
	if got := Format(errs[0].Err); got != want {
		t.Errorf("Format(ExampleError.Err) = %q, want %q", got, want)
	}
}

// TestValidateFixtureLibraryExamples covers ValidateBuiltinExamples against
// the phase-3 fixture library's own example.md files
// (internal/template/testdata/library/templates), loaded through
// template.LoadLibrary + template.NewRegistryFromLibrary: the hello slide's
// example, once given the `template:` selector a deck author's slide file
// would carry (library example.md files omit it, since checkLibraryExamples
// validates them without slide.Parse), must survive the full pipeline with
// no error; a deliberately broken example (a missing required field) reports
// the expected first error.
func TestValidateFixtureLibraryExamples(t *testing.T) {
	reg := mustFixtureRegistry(t)
	hello, ok := reg.Lookup("hello")
	if !ok {
		t.Fatal(`registry has no "hello" template`)
	}

	t.Run("fixture library example is valid", func(t *testing.T) {
		withHeader := *hello
		withHeader.Example = template.Example{
			Markdown: "---\ntemplate: hello\ntitle: Hello, world\n---\nA short greeting shown on the hello slide.\n",
		}
		errs := ValidateBuiltinExamples(mustRegistry(t, &withHeader))
		for _, e := range errs {
			t.Errorf("template %q example invalid: %s", e.Template, Format(e.Err))
		}
	})

	t.Run("broken example reports the missing required field", func(t *testing.T) {
		broken := *hello
		broken.Example = template.Example{Markdown: "---\ntemplate: hello\n---\n"}
		errs := ValidateBuiltinExamples(mustRegistry(t, &broken))
		if len(errs) != 1 {
			t.Fatalf("ValidateBuiltinExamples = %d failures, want exactly 1", len(errs))
		}
		want := `slides/1-example.md › title: required: field "title" is required but missing — add a title: value`
		if got := Format(errs[0].Err); got != want {
			t.Errorf("Format(ExampleError.Err) =\n  %q\nwant\n  %q", got, want)
		}
	})
}

// mustFixtureRegistry loads the phase-3 fixture library and returns the
// registry it builds.
func mustFixtureRegistry(t *testing.T) *template.Registry {
	t.Helper()
	fsys := os.DirFS(fixtureLibraryDir)
	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	return reg
}
