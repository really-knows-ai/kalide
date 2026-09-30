package template

import (
	htmltemplate "html/template"
	"os"
	"strings"
	"testing"
)

// This file is the integration-test deliverable for
// section-helpers/plan.phase-07.task-2, the example-context half of the
// required+default rule (field-rules): an omitted required field that declares
// a Default is APPLIED at every composition depth, and the application is
// OBSERVABLE.
//
// LoadLibrary returns no output — it only reports success — and executing a
// missing map key is not itself an error, so a load-only assertion would pass
// even if validateExampleBlockAt (the load-time materialisation site,
// task-13) were never implemented. This test therefore repeats step 7's exact
// binding in-package, as sectionhelper_int_test.go does: it re-parses the
// host layout under LayoutFuncMap with its `section` entry overridden by the
// load-time exampleSectionHelper, and executes it against the SAME example
// context validateExampleBlock builds (the default-materialising step-7 path),
// so the rendered bytes are captured and asserted.
//
// The observable dependency is made explicit rather than implied by a value:
//
//   - TOP LEVEL: the host's omitted `heading` default is passed as the
//     required, NON-defaulted `target` field of a `{{ section "probe" … }}`
//     call. Without the default the argument is nil, so ResolveSectionCall's
//     CheckValues rejects a nil text field and the step-7 execution ERRORS. The
//     control subtest deletes the materialised default and asserts that error.
//   - NESTED: an authored `probe` instance omits its own required, defaulted
//     `label`; the host layout reads the nested instance's materialised value
//     from the example context. The control deletes the nested default and
//     asserts the rendered value changes.
//
// It also pins the source-view rule in the example context: `.raw` EXCLUDES
// the default (a declared default is not an authored source value), and that an
// undefaulted omitted required field still fails LoadLibrary's example.md check.
//
// The unit tier (fieldrules_required_default_test.go) pins CheckValues'
// schema-only half and ResolveSectionCall's materialisation; the render pipeline
// half lives in internal/render/fieldrules_default_integration_test.go. These
// tests touch the real filesystem, so they are skipped under `go test -short`.

const (
	// defaultProbeHostManifest is a slide template whose required `heading`
	// carries a Default and whose one child section accepts `probe`.
	defaultProbeHostManifest = `name: host
description: default-probe host with an omitted defaulted required field
fields:
  - name: heading
    type: text
    required: true
    default: HEAD-DEF
sections:
  - name: items
    accepted:
      - probe
    max: 4
body:
  mode: optional
`

	// defaultProbeHostLayout exercises the defaulted `heading` three ways: a
	// direct read, a `.raw` source-view probe (which must stay `absent`), and as
	// the required non-defaulted `target` of a `{{ section }}` call — the
	// error-on-nil dependency. It then reads each nested instance's materialised
	// `label` default.
	defaultProbeHostLayout = `<section class="host">` +
		`<h1 class="host-heading">{{ .heading }}</h1>` +
		`<span class="host-raw-heading">{{ if index .raw "heading" }}present{{ else }}absent{{ end }}</span>` +
		`<span class="host-helper">{{ section "probe" (dict "target" .heading) }}</span>` +
		`{{ range .items }}<span class="host-nested">{{ .label }}</span>{{ end }}` +
		`</section>`

	// defaultProbeHostExample omits the defaulted `heading` entirely and omits
	// the nested `probe` instance's defaulted `label`, while supplying probe's
	// required non-defaulted `target`.
	defaultProbeHostExample = "---\ntemplate: host\n---\n" +
		"Host body source.\n" +
		"# items\n" +
		"```\ntemplate: probe\ntarget: T1\n```\n"

	// defaultProbeSectionManifest declares a required `label` with a Default and
	// a required `target` with none.
	defaultProbeSectionManifest = `name: probe
description: default-probe section
fields:
  - name: label
    type: text
    required: true
    default: PROBE-DEF
  - name: target
    type: text
    required: true
body:
  mode: disallowed
`

	defaultProbeSectionLayout = `<probe data-label="{{ .label }}" data-target="{{ .target }}"></probe>`

	// defaultProbeSectionExample omits the defaulted `label`, so the section
	// template's own example also exercises the load-time materialisation.
	defaultProbeSectionExample = "```\ntarget: T1\n```\n"
)

// executeStep7ExampleInt repeats checkLibraryExample's (step 7) binding for one
// loaded template and returns the bytes it executes, which LoadLibrary itself
// discards. It builds the example context through the SAME production path
// validateExampleBlock (and so validateExampleBlockAt, the default-
// materialising site) uses, publishes the reserved `.raw`/`.data` context with
// the shared RawContext/DataContext builders exactly as checkLibraryExample
// does, then parses the layout under LayoutFuncMap with its `section` entry
// overridden by the load-time exampleSectionHelper — the same override step 7
// installs. mutate, when non-nil, runs after the context is built and before
// execution, so a caller can remove a materialised default to prove the
// execution depends on it.
func executeStep7ExampleInt(t *testing.T, lib *Library, name string, mutate func(ctx map[string]any)) (string, error) {
	t.Helper()
	lt, _, ok := lib.TemplateByName(name)
	if !ok || lt == nil || lt.Definition == nil {
		t.Fatalf("TemplateByName(%q) not loaded with a Definition", name)
	}
	def := lt.Definition
	resolve := func(n string) (*Template, bool) {
		other, _, ok := lib.TemplateByName(n)
		if !ok || other == nil || other.Definition == nil {
			return nil, false
		}
		return other.Definition, true
	}

	ctx := map[string]any{
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
	}
	block, err := parseExampleBlock(string(lt.ExampleBytes), lt.Kind, 1)
	if err != nil {
		t.Fatalf("parseExampleBlock(%s): %v", name, err)
	}
	if err := validateExampleBlock(lt.ExamplePath, def, block, resolve, ctx); err != nil {
		t.Fatalf("validateExampleBlock(%s): %v", name, err)
	}

	// The reserved source contexts, exactly as checkLibraryExample publishes
	// them: `.raw` from the authored frontmatter minus the reserved template
	// selector, `.data` from the authored section tree.
	source := make(map[string]any, len(block.Frontmatter))
	for k, v := range block.Frontmatter {
		if k == "template" {
			continue
		}
		source[k] = v
	}
	ctx["raw"] = RawContext(source, def, block.Body)
	ctx["data"] = DataContext(exampleContextNodes(block.Sections, def, resolve), nil)

	if mutate != nil {
		mutate(ctx)
	}

	funcMap := LayoutFuncMap(lib.Media, "/"+MediaDir)
	funcMap["section"] = exampleSectionHelper(lib, nil) // a slide layout's parent is the slide
	layout, err := htmltemplate.New(name).Funcs(funcMap).Parse(lt.LayoutText)
	if err != nil {
		t.Fatalf("parse %s layout under the step-7 section binding: %v", name, err)
	}
	var buf strings.Builder
	execErr := layout.Execute(&buf, ctx)
	return buf.String(), execErr
}

// TestFieldRulesDefaultAppliedExampleInt proves that an omitted required field
// declaring a Default is materialised into the step-7 example context at every
// composition depth, with an observable dependency, and that the example
// context's `.raw` view excludes the default (field-rules,
// raw-source-context).
func TestFieldRulesDefaultAppliedExampleInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	files := mergeIntFiles(
		templateIntFiles(SlidesDir, "host", defaultProbeHostManifest, defaultProbeHostLayout, defaultProbeHostExample),
		templateIntFiles(SectionsDir, "probe", defaultProbeSectionManifest, defaultProbeSectionLayout, defaultProbeSectionExample),
	)
	projectDir := writeSectionHelperIntLibrary(t, files)
	lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil: an omitted required field with a default must load", err)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}

	got, execErr := executeStep7ExampleInt(t, lib, "host", nil)
	if execErr != nil {
		t.Fatalf("step-7 example execution error = %v, want nil: the default must be materialised before execution", execErr)
	}

	// The direct read and the required-target field both show the materialised
	// top-level default; the nested instance shows its own materialised default;
	// and `.raw` does NOT carry the top-level default.
	for _, want := range []string{
		`<h1 class="host-heading">HEAD-DEF</h1>`,
		`<probe data-label="PROBE-DEF" data-target="HEAD-DEF"></probe>`,
		`<span class="host-raw-heading">absent</span>`,
		`<span class="host-nested">PROBE-DEF</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("step-7 rendered host layout does not contain %q:\n%s", want, got)
		}
	}

	// The top-level dependency is real: removing the materialised `heading`
	// passes nil to the helper target's required, non-defaulted `target` field,
	// which CheckValues rejects, so the step-7 execution errors.
	t.Run("top-level execution depends on the materialised default", func(t *testing.T) {
		_, err := executeStep7ExampleInt(t, lib, "host", func(ctx map[string]any) {
			delete(ctx, "heading")
		})
		if err == nil {
			t.Fatal("step-7 execution without the materialised top-level default succeeded; want the required-target type error")
		}
		if !strings.Contains(err.Error(), "expected a text field") {
			t.Errorf("error = %v, want the nil-argument text-field type error", err)
		}
	})

	// The nested dependency is real: removing the nested instance's materialised
	// `label` changes what the host layout renders for it.
	t.Run("nested execution depends on the materialised default", func(t *testing.T) {
		var nested int
		got, err := executeStep7ExampleInt(t, lib, "host", func(ctx map[string]any) {
			items, ok := ctx["items"].([]map[string]any)
			if !ok || len(items) == 0 {
				t.Fatalf("example context items = %#v, want the materialised nested instance", ctx["items"])
			}
			nested = len(items)
			delete(items[0], "label")
		})
		if err != nil {
			t.Fatalf("step-7 execution error = %v, want nil", err)
		}
		if nested == 0 {
			t.Fatal("no nested instance was materialised into the example context")
		}
		if strings.Contains(got, `<span class="host-nested">PROBE-DEF</span>`) {
			t.Errorf("nested instance rendered its default label after that default was removed:\n%s", got)
		}
	})
}

// TestFieldRulesUndefaultedExampleInt proves the other half of the rule at the
// same example.md site: an omitted required field with NO Default still fails
// LoadLibrary's example check, positioned at the template's example.md
// (field-rules). It complements the defaulted case above: the default is what
// makes the omission legal.
func TestFieldRulesUndefaultedExampleInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	files := mergeIntFiles(
		templateIntFiles(SlidesDir, "strict",
			"name: strict\nfields:\n  - name: must\n    type: text\n    required: true\n",
			"<section>{{ .must }}</section>",
			"---\ntemplate: strict\n---\n"),
	)
	projectDir := writeSectionHelperIntLibrary(t, files)
	lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
	if err == nil {
		t.Fatalf("LoadLibrary() = %+v, nil; want the undefaulted omitted required field to fail the example check", lib)
	}
	if lib != nil {
		t.Fatalf("LoadLibrary() Library = %+v, want nil with the error", lib)
	}
	libErr := asLibraryError(t, err)
	if want := TemplatesDir + "/" + SlidesDir + "/strict/" + ExampleFile; libErr.Path != want {
		t.Errorf("Path = %q, want the example.md site %q", libErr.Path, want)
	}
	if !strings.Contains(libErr.Message, "required") || !strings.Contains(libErr.Message, "must") {
		t.Errorf("Message = %q, want a required error naming the field", libErr.Message)
	}
}
