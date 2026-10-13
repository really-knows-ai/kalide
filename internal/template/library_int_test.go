package template

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/displaypath"
)

// This file is the integration-test deliverable for plan.phase-01.task-6:
// LoadLibrary against a real filesystem (os.DirFS over a temp dir), covering
// the no-built-in-fallback behavior when a project has no usable templates/
// directory, and a full on-disk library — slides, sections, a theme with an
// owned font file, and media including a video — loading cleanly.
//
// It also carries plan.phase-01.task-12's integration coverage of the layered
// load: LoadDeckLibrary over a real external + local two-layer deck, where a
// slide, a theme and a media file from each layer all resolve from the merged
// Library, each layer's own root and media stay addressable through Layers, and
// a theme name or a media path carried by both layers is rejected naming both
// paths with equal standing (layered-template-library, library-layer-uniqueness,
// theme-selection). The slides/sections merge, cross-kind uniqueness and dedupe
// of LoadDeckLibrary are covered by TestLoadDeckLibraryMergedLayers
// (resolve_test.go) and are not repeated here.
//
// These tests exercise real disk I/O and so are guarded with testing.Short();
// they are skipped under `go test -short`.

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// TestLoadLibraryIntMissingTemplatesDir proves LoadLibrary reports a
// *LibraryError naming the expected templates/ path with the `kalide init`
// hint when a project has no templates/ directory at all — no built-in
// fallback is substituted.
func TestLoadLibraryIntMissingTemplatesDir(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}
	projectDir := t.TempDir()
	// A project root with no templates/ entry whatsoever.
	writeFile(t, filepath.Join(projectDir, "kalide.yaml"), []byte("title: demo\n"))

	fsys := os.DirFS(projectDir)
	lib, err := LoadLibrary(fsys, TemplatesDir)
	if lib != nil {
		t.Fatalf("LoadLibrary() Library = %+v, want nil", lib)
	}
	libErr := asLibraryError(t, err)
	if libErr.Path != TemplatesDir {
		t.Errorf("Path = %q, want %q", libErr.Path, TemplatesDir)
	}
	if libErr.Hint == "" || !containsHint(libErr.Hint) {
		t.Errorf("Hint = %q, want it to mention `kalide init`", libErr.Hint)
	}
}

// TestLoadLibraryIntTemplatesIsRegularFile proves LoadLibrary reports a
// *LibraryError naming the expected templates/ path with the `kalide init`
// hint when templates/ exists but is a regular file, not a directory.
func TestLoadLibraryIntTemplatesIsRegularFile(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}
	projectDir := t.TempDir()
	writeFile(t, filepath.Join(projectDir, TemplatesDir), []byte("not a directory"))

	fsys := os.DirFS(projectDir)
	lib, err := LoadLibrary(fsys, TemplatesDir)
	if lib != nil {
		t.Fatalf("LoadLibrary() Library = %+v, want nil", lib)
	}
	libErr := asLibraryError(t, err)
	if libErr.Path != TemplatesDir {
		t.Errorf("Path = %q, want %q", libErr.Path, TemplatesDir)
	}
	if libErr.Hint == "" || !containsHint(libErr.Hint) {
		t.Errorf("Hint = %q, want it to mention `kalide init`", libErr.Hint)
	}
}

// containsHint reports whether hint mentions the `kalide init` remediation.
func containsHint(hint string) bool {
	return hint == "run `kalide init` to create one"
}

// TestLoadLibraryIntFullLibrary proves a complete on-disk library — slides,
// sections, a theme with an owned font file, and media including a video —
// loads cleanly via os.DirFS. Its record-field subtest adds a template whose
// list item is an inline record carrying a coerce text field, plus an example
// that exercises a nullable null, a coerce number and list-of-record values, so
// a full load both validates the record schema end to end and converts the
// example into the step-7 execution context (nullable-fields, record-item-type,
// text-coercion).
func TestLoadLibraryIntFullLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}
	projectDir := t.TempDir()
	root := filepath.Join(projectDir, TemplatesDir)

	writeFile(t, filepath.Join(root, LibraryFile), []byte("name: demo\nformat: 1\n"))

	// Slide template.
	writeFile(t, filepath.Join(root, "slides", "hello", "template.yaml"), []byte("name: hello\n"))
	writeFile(t, filepath.Join(root, "slides", "hello", "layout.html.tmpl"),
		[]byte(`<section><h1>{{.Title}}</h1><img src="{{media "logo.svg"}}"></section>`))
	writeFile(t, filepath.Join(root, "slides", "hello", "example.md"), []byte("# hello\n"))

	// Section template.
	writeFile(t, filepath.Join(root, "sections", "item", "template.yaml"), []byte("name: item\n"))
	writeFile(t, filepath.Join(root, "sections", "item", "layout.html.tmpl"), []byte("<li>{{.Title}}</li>"))
	writeFile(t, filepath.Join(root, "sections", "item", "example.md"), []byte("# item\n"))

	// Nested-composition section template: a section template may itself
	// declare `sections:` with the same schema as a slide's, so composition
	// nests (template-composition). group holds `items` children accepting
	// the item section, and its example nests a child instance under a
	// top-level `# items` heading.
	writeFile(t, filepath.Join(root, "sections", "group", "template.yaml"),
		[]byte("description: a group of item children\n"+
			"sections:\n  - name: items\n    accepted: [item]\n    min: 1\n    max: 4\n"))
	writeFile(t, filepath.Join(root, "sections", "group", "layout.html.tmpl"),
		[]byte("<ul>{{range .items}}<li>{{.}}</li>{{end}}</ul>"))
	writeFile(t, filepath.Join(root, "sections", "group", "example.md"), []byte("# items\n"))

	// Theme with an owned font file.
	writeFile(t, filepath.Join(root, "themes", "plain", "theme.css"), []byte("body { color: black; }"))
	writeFile(t, filepath.Join(root, "themes", "plain", "fonts", "body.woff2"), []byte("fontdata"))

	// Media including a video.
	writeFile(t, filepath.Join(root, "media", "logo.svg"), []byte("<svg/>"))
	writeFile(t, filepath.Join(root, "media", "video", "intro.mp4"), []byte("videodata"))

	fsys := os.DirFS(projectDir)
	lib, err := LoadLibrary(fsys, TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil", err)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}

	if _, _, ok := lib.TemplateByName("hello"); !ok {
		t.Error("slide template hello not loaded")
	}
	if _, _, ok := lib.TemplateByName("item"); !ok {
		t.Error("section template item not loaded")
	}
	// The nested container section loaded with its declared child section.
	group, kind, ok := lib.TemplateByName("group")
	if !ok || kind != KindSection {
		t.Fatalf("TemplateByName(group) = (%v, %q, %v), want (non-nil, %q, true)", group, kind, ok, KindSection)
	}
	if group.Definition == nil {
		t.Fatal("checkLibraryBuild left group.Definition nil")
	}
	if got := group.Definition.Sections; len(got) != 1 || got[0].Name != "items" ||
		len(got[0].Accepted) != 1 || got[0].Accepted[0] != "item" {
		t.Errorf("group.Sections = %+v, want one %q section accepting item", got, "items")
	}
	th, ok := lib.Themes["plain"]
	if !ok {
		t.Fatal("theme plain not loaded")
	}
	if _, ok := th.Files["fonts/body.woff2"]; !ok {
		t.Errorf("theme plain Files = %v, want fonts/body.woff2 present", th.Files)
	}
	if !lib.HasMedia {
		t.Fatal("HasMedia = false, want true")
	}
	if _, err := fs.Stat(lib.Media, "video/intro.mp4"); err != nil {
		t.Errorf("media video/intro.mp4 not present in Library.Media: %v", err)
	}

	// A library whose slide template declares a list field whose item is an
	// inline record, beside a nullable field and a coerce text field, loaded
	// through the real loader: step 7 must VALIDATE the example against the
	// record schema and CONVERT it into the execution context through the same
	// nullable/record/coerce conversion the renderer applies
	// (nullable-fields, record-item-type, text-coercion). LoadLibrary itself
	// discards the bytes it executes, so — exactly as
	// fieldrules_default_int_test.go does — the step-7 binding is repeated
	// in-package and the layout is executed against the context
	// validateExampleBlock builds, making the conversion observable.
	t.Run("record-field template and example validate and convert end to end", func(t *testing.T) {
		const catalogueManifest = `name: catalogue
fields:
  - name: heading
    type: text
    nullable: true
  - name: code
    type: text
    coerce: true
  - name: items
    type: list
    item:
      type: record
      fields:
        - name: name
          type: text
          required: true
        - name: id
          type: text
          coerce: true
        - name: note
          type: text
`
		// The `len` reads make the coercion observable: a coerce text field left
		// a YAML number would fail `len` at execution, and a record item left
		// unconverted would fail `len .id` likewise, so the conversion cannot be
		// silently skipped.
		const catalogueLayout = `<div class="catalogue">` +
			`<span class="catalogue-heading">{{ if .heading }}present{{ else }}nil{{ end }}</span>` +
			`<span class="catalogue-code">{{ .code }}</span>` +
			`<span class="catalogue-code-len">{{ len .code }}</span>` +
			`<span class="catalogue-count">{{ len .items }}</span>` +
			`{{ range .items }}` +
			`<span class="catalogue-item" data-name="{{ .name }}" data-id="{{ .id }}" data-id-len="{{ len .id }}" data-note="{{ if .note }}present{{ else }}nil{{ end }}"></span>` +
			`{{ end }}` +
			`</div>`
		const catalogueExample = "---\n" +
			"template: catalogue\n" +
			"heading: null\n" +
			"code: 7\n" +
			"items:\n" +
			"  - name: Ada\n" +
			"    id: 7\n" +
			"  - name: Grace\n" +
			"    id: 42\n" +
			"---\n" +
			"A catalogue of items.\n"

		// writeCatalogue writes the four required files of a full on-disk
		// library whose only template is the record-field slide above, with the
		// given example.md, and returns the project root for os.DirFS. The
		// manifest and layout are fixed; only the example varies, so the positive
		// and negative cases differ solely in the example's authored values.
		writeCatalogue := func(t *testing.T, example string) string {
			t.Helper()
			projectDir := t.TempDir()
			root := filepath.Join(projectDir, TemplatesDir)
			writeFile(t, filepath.Join(root, LibraryFile), []byte("name: demo\nformat: 1\n"))
			writeFile(t, filepath.Join(root, SlidesDir, "catalogue", ManifestFile), []byte(catalogueManifest))
			writeFile(t, filepath.Join(root, SlidesDir, "catalogue", LayoutFile), []byte(catalogueLayout))
			writeFile(t, filepath.Join(root, SlidesDir, "catalogue", ExampleFile), []byte(example))
			return projectDir
		}

		t.Run("the record example validates and converts", func(t *testing.T) {
			projectDir := writeCatalogue(t, catalogueExample)
			lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
			if err != nil {
				t.Fatalf("LoadLibrary() error = %v, want nil: a valid record-field example must load", err)
			}
			if lib == nil {
				t.Fatal("LoadLibrary() returned nil Library with nil error")
			}

			got, execErr := executeStep7ExampleInt(t, lib, "catalogue", nil)
			if execErr != nil {
				t.Fatalf("step-7 example execution error = %v, want nil: the converted record context must execute", execErr)
			}
			// The nullable null reads nil, the coerce number reads its string form
			// (whose `len` would fail if it were still a YAML number), and the
			// list-of-record reads as one per-item map with its coerce `id` a
			// string and its omitted optional `note` nil.
			for _, want := range []string{
				`<span class="catalogue-heading">nil</span>`,
				`<span class="catalogue-code">7</span>`,
				`<span class="catalogue-code-len">1</span>`,
				`<span class="catalogue-count">2</span>`,
				`<span class="catalogue-item" data-name="Ada" data-id="7" data-id-len="1" data-note="nil"></span>`,
				`<span class="catalogue-item" data-name="Grace" data-id="42" data-id-len="2" data-note="nil"></span>`,
			} {
				if !strings.Contains(got, want) {
					t.Errorf("step-7 rendered catalogue layout does not contain %q:\n%s", want, got)
				}
			}
		})

		// The validation half is real: a record item that omits its required
		// field is rejected by LoadLibrary, positioned at the template's
		// example.md and carrying the full `items › [0] › name` path.
		t.Run("a violating record item fails the load at the example.md site", func(t *testing.T) {
			projectDir := writeCatalogue(t,
				"---\n"+
					"template: catalogue\n"+
					"code: 7\n"+
					"items:\n"+
					"  - id: 7\n"+
					"---\n")
			lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
			if err == nil {
				t.Fatalf("LoadLibrary() = %+v, nil; want the record item's missing required field to fail the example check", lib)
			}
			if lib != nil {
				t.Fatalf("LoadLibrary() Library = %+v, want nil with the error", lib)
			}
			libErr := asLibraryError(t, err)
			if want := filepath.Join(TemplatesDir, SlidesDir, "catalogue", ExampleFile); libErr.Path != want {
				t.Errorf("Path = %q, want the example.md site %q", libErr.Path, want)
			}
			if !strings.Contains(libErr.Message, "items › [0] › name") || !strings.Contains(libErr.Message, "required") {
				t.Errorf("Message = %q, want a path-qualified required error naming the record field", libErr.Message)
			}
		})
	})

	// A library whose slide template declares list fields carrying a
	// cross-field bound (max_items_from/min_items_from) loads and validates end
	// to end: step 4 resolves each reference against a declared list field, and
	// step 7 enforces the bound against the example's own peer list, so an
	// example within both bounds loads cleanly while an out-of-bounds one fails
	// the load with the single two-field error naming both fields and both
	// lengths (cross-field-list-constraints).
	t.Run("cross-field list bounds validate end to end", func(t *testing.T) {
		// Two bound directions against one shared peer list: `capped` is bounded
		// above by `/peer` and `floored` below by `/peer`, so one example can
		// satisfy both and a single-authored violation exercises either.
		const crossFieldManifest = `name: crossfield
fields:
  - name: capped
    type: list
    max_items_from: /peer
    item:
      name: entry
      type: text
  - name: floored
    type: list
    min_items_from: /peer
    item:
      name: entry
      type: text
  - name: peer
    type: list
    item:
      name: entry
      type: text
`
		// The `len` reads make the converted lists observable: the very context
		// the bound was checked against is the one the layout executes.
		const crossFieldLayout = `<div class="crossfield">` +
			`<span class="capped-count">{{ len .capped }}</span>` +
			`<span class="floored-count">{{ len .floored }}</span>` +
			`<span class="peer-count">{{ len .peer }}</span>` +
			`</div>`

		// writeCrossField writes the four required files of a full on-disk
		// library whose only template is the bounded-list slide above, with the
		// given example.md, and returns the project root for os.DirFS. The
		// manifest and layout are fixed; only the example varies, so the positive
		// and negative cases differ solely in the authored list lengths.
		writeCrossField := func(t *testing.T, example string) string {
			t.Helper()
			projectDir := t.TempDir()
			root := filepath.Join(projectDir, TemplatesDir)
			writeFile(t, filepath.Join(root, LibraryFile), []byte("name: demo\nformat: 1\n"))
			writeFile(t, filepath.Join(root, SlidesDir, "crossfield", ManifestFile), []byte(crossFieldManifest))
			writeFile(t, filepath.Join(root, SlidesDir, "crossfield", LayoutFile), []byte(crossFieldLayout))
			writeFile(t, filepath.Join(root, SlidesDir, "crossfield", ExampleFile), []byte(example))
			return projectDir
		}

		t.Run("an example within both bounds loads", func(t *testing.T) {
			// peer has 3 items, so capped (2) satisfies max_items_from and
			// floored (4) satisfies min_items_from.
			projectDir := writeCrossField(t,
				"---\n"+
					"template: crossfield\n"+
					"peer:\n"+
					"  - p\n"+
					"  - q\n"+
					"  - r\n"+
					"capped:\n"+
					"  - a\n"+
					"  - b\n"+
					"floored:\n"+
					"  - a\n"+
					"  - b\n"+
					"  - c\n"+
					"  - d\n"+
					"---\n"+
					"A cross-field example.\n")
			lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
			if err != nil {
				t.Fatalf("LoadLibrary() error = %v, want nil: an example within both bounds must load", err)
			}
			if lib == nil {
				t.Fatal("LoadLibrary() returned nil Library with nil error")
			}
			if _, _, ok := lib.TemplateByName("crossfield"); !ok {
				t.Error("slide template crossfield not loaded")
			}
		})

		// The negative half: each bound direction is enforced at the example
		// site and reported as ONE error naming both fields and both lengths.
		cases := []struct {
			name      string
			example   string
			wantField string
			wantRule  string
			wantMsg   string
		}{
			{
				name: "a list over its max_items_from peer fails the load",
				example: "---\n" +
					"template: crossfield\n" +
					"peer:\n" +
					"  - p\n" +
					"  - q\n" +
					"  - r\n" +
					"capped:\n" +
					"  - a\n" +
					"  - b\n" +
					"  - c\n" +
					"  - d\n" +
					"floored:\n" +
					"  - a\n" +
					"  - b\n" +
					"  - c\n" +
					"---\n",
				wantField: "capped",
				wantRule:  "max_items_from",
				wantMsg:   `max_items_from: field "capped" has 4 item(s), more than field "peer" has 3`,
			},
			{
				name: "a list under its min_items_from peer fails the load",
				example: "---\n" +
					"template: crossfield\n" +
					"peer:\n" +
					"  - p\n" +
					"  - q\n" +
					"  - r\n" +
					"capped:\n" +
					"  - a\n" +
					"  - b\n" +
					"floored:\n" +
					"  - a\n" +
					"---\n",
				wantField: "floored",
				wantRule:  "min_items_from",
				wantMsg:   `min_items_from: field "floored" has 1 item(s), fewer than field "peer" has 3`,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				projectDir := writeCrossField(t, tc.example)
				lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
				if err == nil {
					t.Fatalf("LoadLibrary() = %+v, nil; want the out-of-bounds example to fail the load", lib)
				}
				if lib != nil {
					t.Fatalf("LoadLibrary() Library = %+v, want nil with the error", lib)
				}
				libErr := asLibraryError(t, err)
				if want := filepath.Join(TemplatesDir, SlidesDir, "crossfield", ExampleFile); libErr.Path != want {
					t.Errorf("Path = %q, want the example.md site %q", libErr.Path, want)
				}
				if !strings.Contains(libErr.Message, tc.wantField) || !strings.Contains(libErr.Message, tc.wantRule) {
					t.Errorf("Message = %q, want a path-qualified %s error naming the field %q", libErr.Message, tc.wantRule, tc.wantField)
				}
				if !strings.Contains(libErr.Message, tc.wantMsg) {
					t.Errorf("Message = %q, want the two-field error %q", libErr.Message, tc.wantMsg)
				}
			})
		}
	})

	// A full on-disk library whose library.yaml carries the three
	// named-definition sections — groups:, field_sets: and types: — loads
	// through os.DirFS, and a template's `type:` (a named type), `- include:`
	// (a named field set) and `@group` `accepted:` entry all resolve against
	// the library's OWN definitions; its example then validates end to end
	// against the expanded schema (named-library-definitions). This is the
	// single-layer LoadLibrary path: loadLibraryMeta parses the sections onto
	// the loader's unexported carrier, layerFromLibrary copies them onto the
	// loading layer's Definitions, and NewRegistryFromLibrary (step 4)
	// threads every template's owning-layer Definitions through
	// registerLibraryTemplate into parseManifest — the layerless step-4
	// *Library's fallback — so the references resolve while the example is
	// validated.
	t.Run("named definitions (groups, field_sets, types) resolve and validate end to end", func(t *testing.T) {
		// The three sections are additive to format 1: the library still
		// declares `format: 1`. callouts is the named accept-list (referenced
		// as `@callouts`), meta the named field set (`- include: meta`) and
		// colour the named field type (`type: colour`).
		const namedLibraryYAML = `name: demo
format: 1
groups:
  callouts:
    - note
field_sets:
  meta:
    - name: subtitle
      type: text
    - name: weight
      type: number
types:
  colour:
    type: enum
    variants:
      - red
      - green
      - blue
`
		// The report slide references each kind: `- include: meta` expands the
		// field set's subtitle/weight in place, `type: colour` expands the
		// named enum under the referencing name accent, and the asides section
		// accepts the named group `@callouts` combinable with the literal
		// template name tip.
		const reportManifest = `name: report
fields:
  - include: meta
  - name: accent
    type: colour
    required: true
sections:
  - name: asides
    accepted:
      - "@callouts"
      - tip
    max: 4
`
		const reportLayout = `<section class="report">` +
			`<span class="report-subtitle">{{ .subtitle }}</span>` +
			`<span class="report-weight">{{ .weight }}</span>` +
			`<span class="report-accent">{{ .accent }}</span>` +
			`{{ range .asides }}<span class="report-aside">{{ .text }}</span>{{ end }}` +
			`</section>`

		const noteManifest = "name: note\nfields:\n  - name: text\n    type: text\n"
		const noteLayout = `<aside class="note">{{ .text }}</aside>`
		const noteExample = "A note.\n"
		const tipManifest = "name: tip\nfields:\n  - name: text\n    type: text\n"
		const tipLayout = `<aside class="tip">{{ .text }}</aside>`
		const tipExample = "A tip.\n"

		// The report example supplies the included fields, a variant of the
		// named enum, and nests a note child (accepted only via `@callouts`)
		// and a tip child (accepted via the literal name), so BOTH the field
		// references and the group expansion reach the example-validation
		// site, not just the parsed declaration.
		const reportExample = "---\n" +
			"template: report\n" +
			"subtitle: Quarterly\n" +
			"weight: 3\n" +
			"accent: red\n" +
			"---\n" +
			"Report body.\n" +
			"# asides\n" +
			"```\n" +
			"template: note\n" +
			"text: hello\n" +
			"```\n" +
			"# asides\n" +
			"```\n" +
			"template: tip\n" +
			"text: bye\n" +
			"```\n"

		// writeNamedLibrary writes library.yaml plus the three templates to a
		// fresh temp dir, returning the project root for os.DirFS. Only the
		// report example varies, so the positive and negative cases differ
		// solely in the authored accent value.
		writeNamedLibrary := func(t *testing.T, reportEx string) string {
			t.Helper()
			projectDir := t.TempDir()
			root := filepath.Join(projectDir, TemplatesDir)
			writeFile(t, filepath.Join(root, LibraryFile), []byte(namedLibraryYAML))
			files := mergeIntFiles(
				templateIntFiles(SlidesDir, "report", reportManifest, reportLayout, reportEx),
				templateIntFiles(SectionsDir, "note", noteManifest, noteLayout, noteExample),
				templateIntFiles(SectionsDir, "tip", tipManifest, tipLayout, tipExample),
			)
			for rel, data := range files {
				writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), []byte(data))
			}
			return projectDir
		}

		t.Run("the template resolves type:/include:/@group and its example executes", func(t *testing.T) {
			projectDir := writeNamedLibrary(t, reportExample)
			lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
			if err != nil {
				t.Fatalf("LoadLibrary() error = %v, want nil: named references must resolve at load", err)
			}
			if lib == nil {
				t.Fatal("LoadLibrary() returned nil Library with nil error")
			}

			report, kind, ok := lib.TemplateByName("report")
			if !ok || kind != KindSlide {
				t.Fatalf("TemplateByName(report) = (%v, %q, %v), want (non-nil, %q, true)", report, kind, ok, KindSlide)
			}
			if report.Definition == nil {
				t.Fatal("checkLibraryBuild left report.Definition nil")
			}
			def := report.Definition

			// `- include: meta` expanded the named field set in place, ahead of
			// the literal accent field.
			if len(def.Fields) != 3 {
				t.Fatalf("report.Fields = %+v, want subtitle, weight (from include: meta) then accent", def.Fields)
			}
			if def.Fields[0].Name != "subtitle" || def.Fields[0].Type != FieldText {
				t.Errorf("Fields[0] = %+v, want the field set's subtitle:text", def.Fields[0])
			}
			if def.Fields[1].Name != "weight" || def.Fields[1].Type != FieldNumber {
				t.Errorf("Fields[1] = %+v, want the field set's weight:number", def.Fields[1])
			}
			// `type: colour` expanded the named enum under the referencing name
			// accent, keeping the named variants and the referencing field's own
			// required key.
			if def.Fields[2].Name != "accent" || def.Fields[2].Type != FieldEnum || !def.Fields[2].Required {
				t.Errorf("Fields[2] = %+v, want the named type colour expanded as accent:enum required", def.Fields[2])
			}
			assertStringSlice(t, "accent.Variants", def.Fields[2].Variants, []string{"red", "green", "blue"})

			// `@callouts` expanded the named group in place, then the literal tip
			// name, so the section's accepted list is the group's members plus the
			// literal.
			if len(def.Sections) != 1 {
				t.Fatalf("report.Sections = %+v, want one asides section", def.Sections)
			}
			assertStringSlice(t, "asides.Accepted", def.Sections[0].Accepted, []string{"note", "tip"})

			// The resolved schema is the one the example was validated against
			// and the one the layout executes: subtitle/weight from the field set
			// and accent from the named enum read their authored values, and both
			// the `@callouts` note child and the literal tip child materialise.
			got, execErr := executeStep7ExampleInt(t, lib, "report", nil)
			if execErr != nil {
				t.Fatalf("step-7 example execution error = %v, want nil", execErr)
			}
			for _, want := range []string{
				`<span class="report-subtitle">Quarterly</span>`,
				`<span class="report-weight">3</span>`,
				`<span class="report-accent">red</span>`,
				`<span class="report-aside">hello</span>`,
				`<span class="report-aside">bye</span>`,
			} {
				if !strings.Contains(got, want) {
					t.Errorf("step-7 rendered report layout does not contain %q:\n%s", want, got)
				}
			}
		})

		// The resolved named enum is enforced end to end: an example accent
		// outside the named type's variants fails the load at report's
		// example.md, so the loader really validated the example against the
		// EXPANDED schema, not just recorded it.
		t.Run("the resolved named enum is enforced at the example site", func(t *testing.T) {
			projectDir := writeNamedLibrary(t,
				"---\n"+
					"template: report\n"+
					"subtitle: Quarterly\n"+
					"weight: 3\n"+
					"accent: purple\n"+
					"---\n")
			lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
			if err == nil {
				t.Fatalf("LoadLibrary() = %+v, nil; want the named enum's out-of-variants value to fail the load", lib)
			}
			if lib != nil {
				t.Fatalf("LoadLibrary() Library = %+v, want nil with the error", lib)
			}
			libErr := asLibraryError(t, err)
			if want := filepath.Join(TemplatesDir, SlidesDir, "report", ExampleFile); libErr.Path != want {
				t.Errorf("Path = %q, want the example.md site %q", libErr.Path, want)
			}
			if !strings.Contains(libErr.Message, `accent: enum: "purple" is not one of the allowed variants (red, green, blue)`) {
				t.Errorf("Message = %q, want the named enum's variant error", libErr.Message)
			}
		})
	})
}

// TestLoadDeckLibraryMergedThemeAndMediaLayers covers LoadDeckLibrary's merged
// load over a real external + local two-layer deck with themes and media
// (layered-template-library, library-layer-uniqueness, theme-selection). It
// complements TestLoadDeckLibraryMergedLayers (resolve_test.go), which covers
// the slides/sections merge, cross-kind uniqueness and dedupe; here a slide, a
// theme and a media file defined in each layer all resolve from the merged
// Library, each contributing layer's own root is retained in Layers[i].RootPath
// and its media stays addressable through Layers[i].Media, and a theme name or a
// media path carried by both layers is rejected naming both paths. The layers
// merge with equal standing: a duplicate is an error whoever carries it, never a
// silent override, so both carrying paths are always named.
func TestLoadDeckLibraryMergedThemeAndMediaLayers(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	// writeLayerWithThemeAndMedia writes a minimal valid library at dir with
	// the given metadata name, one slide, one theme (theme.css) and one media
	// file, so a two-layer deck can show every kind resolving from each layer.
	writeLayerWithThemeAndMedia := func(dir, name, slide, theme, media string) {
		t.Helper()
		writeLibraryWithTemplates(t, dir, name, []string{slide}, nil)
		writeFile(t, filepath.Join(dir, ThemesDir, theme, ThemeStylesheet),
			[]byte("body { color: black; }"))
		writeFile(t, filepath.Join(dir, MediaDir, media), []byte(media))
	}

	t.Run("both layers' slide, theme and media resolve from the merged library", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLayerWithThemeAndMedia(localRoot, "local", "local-slide", "local-theme", "local.png")
		external := t.TempDir()
		writeLayerWithThemeAndMedia(external, "external", "ext-slide", "ext-theme", "ext.png")

		lib, err := LoadDeckLibrary(deckRoot, external)
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}

		for _, name := range []string{"ext-slide", "local-slide"} {
			if _, _, ok := lib.TemplateByName(name); !ok {
				t.Errorf("TemplateByName(%q) not found: the merged library must carry both layers' slides", name)
			}
		}
		for _, name := range []string{"ext-theme", "local-theme"} {
			if _, ok := lib.Themes[name]; !ok {
				t.Errorf("Themes[%q] not found: the merged library must carry both layers' themes", name)
			}
		}

		if len(lib.Layers) != 2 {
			t.Fatalf("len(Layers) = %d, want 2", len(lib.Layers))
		}
		if lib.Layers[0].RootPath != external {
			t.Errorf("Layers[0].RootPath = %q, want the external layer %q", lib.Layers[0].RootPath, external)
		}
		if lib.Layers[1].RootPath != localRoot {
			t.Errorf("Layers[1].RootPath = %q, want the local layer %q", lib.Layers[1].RootPath, localRoot)
		}

		// Each layer's own themes and media stay addressable through its own
		// layer, even though the merged Library exposes only the primary
		// (local, deck-root templates/) layer's media.
		if _, ok := lib.Layers[0].Themes["ext-theme"]; !ok {
			t.Error("Layers[0].Themes[ext-theme] not found")
		}
		if _, ok := lib.Layers[1].Themes["local-theme"]; !ok {
			t.Error("Layers[1].Themes[local-theme] not found")
		}
		if !lib.Layers[0].HasMedia {
			t.Error("Layers[0].HasMedia = false, want true (the external layer carries media/)")
		}
		if _, err := fs.Stat(lib.Layers[0].Media, "ext.png"); err != nil {
			t.Errorf("Layers[0].Media does not resolve the external layer's ext.png: %v", err)
		}
		if !lib.Layers[1].HasMedia {
			t.Error("Layers[1].HasMedia = false, want true (the local layer carries media/)")
		}
		if _, err := fs.Stat(lib.Layers[1].Media, "local.png"); err != nil {
			t.Errorf("Layers[1].Media does not resolve the local layer's local.png: %v", err)
		}
		if _, err := fs.Stat(lib.Media, "local.png"); err != nil {
			t.Errorf("merged.Media does not resolve the local (primary) layer's local.png: %v", err)
		}
		if _, err := fs.Stat(lib.Media, "ext.png"); err == nil {
			t.Error("merged.Media resolves the external layer's ext.png, want the local (primary) layer's media only")
		}
		if !lib.HasMedia {
			t.Error("HasMedia = false, want true")
		}
	})

	t.Run("a theme name carried by both layers errors naming both theme paths", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLayerWithThemeAndMedia(localRoot, "local", "local-slide", "dup", "local.png")
		external := t.TempDir()
		writeLayerWithThemeAndMedia(external, "external", "ext-slide", "dup", "ext.png")

		lib, err := LoadDeckLibrary(deckRoot, external)
		if lib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", lib)
		}
		// Both paths are named, so neither layer's theme silently wins: the
		// layers have equal standing (library-layer-uniqueness).
		assertLayerDuplicateError(t, err, "theme", "dup",
			filepath.Join(external, ThemesDir, "dup"), filepath.Join(localRoot, ThemesDir, "dup"))
	})

	t.Run("a media path carried by both layers errors naming both media paths", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLayerWithThemeAndMedia(localRoot, "local", "local-slide", "local-theme", "dup.png")
		external := t.TempDir()
		writeLayerWithThemeAndMedia(external, "external", "ext-slide", "ext-theme", "dup.png")

		lib, err := LoadDeckLibrary(deckRoot, external)
		if lib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", lib)
		}
		// Both media paths are named, so neither layer's file silently wins:
		// the layers have equal standing (library-layer-uniqueness).
		assertLayerDuplicateError(t, err, "media path", "dup.png",
			filepath.Join(external, MediaDir, "dup.png"), filepath.Join(localRoot, MediaDir, "dup.png"))
	})
}

// TestCheckLayoutReferencesRootSpellingStabilityInt proves layout-reference
// containment is decided on the layer-relative path, not on how the root is
// spelled: the same escaping reference (and the same contained reference)
// yields the same verdict whether the root is bare, trailing-separator or
// "./"-prefixed. Only the layer-relative path drives the accept/reject
// decision; the root appears solely in the message, so a differently-spelled
// root can never turn an escaping reference into an accepted one (or vice
// versa). This is the checkLayoutReferences half of the containment rewrite
// that task-17's theme-CSS case pins.
//
// checkLayoutReferences is the seam the loader calls per layout at step 6, so
// it is driven directly here; the verdict does not depend on a successful load.
func TestCheckLayoutReferencesRootSpellingStabilityInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	// Three spellings of one root (templates/): bare, trailing-separator and
	// "./"-prefixed. All denote the same templates/ tree.
	spellings := []string{
		TemplatesDir,
		TemplatesDir + string(os.PathSeparator),
		"." + string(os.PathSeparator) + TemplatesDir,
	}

	cases := []struct {
		name    string
		layout  string
		wantErr bool
		substr  string
	}{
		{
			// Three ".." from slides/hello leaves the templates/ tree, so the
			// reference escapes the layer regardless of how the root is spelled.
			name:    "an escaping literal src is rejected for every root spelling",
			layout:  `<img src="../../../secret.png">`,
			wantErr: true,
			substr:  "must stay inside",
		},
		{
			name:    "a contained literal src is accepted for every root spelling",
			layout:  `<img src="img/logo.svg">`,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, root := range spellings {
				lt := &LibraryTemplate{
					Dir:        SlidesDir + "/hello",
					LayoutText: tc.layout,
					LayoutPath: displaypath.Display(root, SlidesDir+"/hello/"+LayoutFile),
				}
				err := checkLayoutReferences(root, lt, nil)
				if tc.wantErr {
					if err == nil {
						t.Errorf("checkLayoutReferences(root=%q) = nil, want the containment rejection", root)
						continue
					}
					libErr := asLibraryError(t, err)
					if !strings.Contains(libErr.Message, tc.substr) {
						t.Errorf("root %q: Message = %q, want it to contain %q", root, libErr.Message, tc.substr)
					}
					continue
				}
				if err != nil {
					t.Errorf("checkLayoutReferences(root=%q) = %v, want nil", root, err)
				}
			}
		})
	}
}
