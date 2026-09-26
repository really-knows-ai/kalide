package validate

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/really-knows-ai/ey-present/internal/deck"
	"github.com/really-knows-ai/ey-present/internal/template"
	"github.com/really-knows-ai/ey-present/internal/theme"
)

// This file implements ValidateBuiltinExamples, the proof that every
// registered template's example slide is valid end to end
// (whole-deck-validation, template-build-checks). Templates now come from a
// project's loaded template.Library (template.NewRegistryFromLibrary), not
// from embedded compiled-in builtins; ValidateBuiltinExamples validates
// whichever registry its caller passes, so it equally proves a project's
// templates/slides and templates/sections example.md files, wherever the
// registry that reg's templates were registered from came from.
//
// Phase 4 checks each example as STRUCTURED data only (frontmatter/field values
// and declared section instances, via CheckValues and the section repeat
// limits) and never parses Markdown. This unit closes the other half: it takes
// the example slide SOURCE that Register loaded (from a project's
// templates/<kind>/<name>/example.md) and runs it through the same pipeline a
// deck author's slide would take — slide.Parse, mdcheck on every body and
// inline text field, template.CheckValues and CheckBody, and the inter-slide
// link pass — so the two representations of the example cannot drift.
//
// The pipeline is the real deck validator (Validate), not a re-implementation:
// each example is placed in a synthetic single-slide deck and handed to
// Validate unchanged. A failure here is a template-authoring error in the
// project's templates/ library that must fail the load or the build/test
// suite; it is never shown to a deck author, who can neither see nor fix a
// template definition.
//
// The function is called at library-load time (checkLibraryExamples) and from
// tests. Any invalid example fails that call; an author-facing deck never
// reaches it.

// ExampleError is one registered template whose example slide did not survive
// the full validation pipeline: the template's name and the first
// ValidationError its example produced. Failures are returned in
// template-name order, so the result is deterministic.
type ExampleError struct {
	// Template is the registered template the failing example belongs to.
	Template string

	// Err is the first ValidationError the example produced. Its File is the
	// synthetic deck's slide path, `slides/1-example.md`; the template name is
	// in Template.
	Err ValidationError
}

// ValidateBuiltinExamples runs every registered template's example slide
// through the full validation pipeline and returns one ExampleError per failing
// template — the first error each example produced — in template-name order
// (the order Registry.Templates reports). It returns nil when every example is
// valid, so a caller fails the build or test on any non-empty result:
//
//	if errs := validate.ValidateBuiltinExamples(reg); len(errs) > 0 {
//		for _, e := range errs {
//			log.Fatalf("template %q example: %s", e.Template, validate.Format(e.Err))
//		}
//	}
//
// A slide-usage template's example is a complete slide source: it is written
// verbatim into the synthetic deck and validated against the registry as it is,
// using the `template:` its own frontmatter names.
//
// A section-usage template's example is a section fragment (a frontmatter fence
// and a body), not a whole slide. It is wrapped in a synthesized slide template
// whose single declared section accepts exactly that section template, and the
// fragment is placed under the section heading; the synthesized template is
// registered into a copy of the registry for the duration of the check, so the
// real registry is never mutated.
//
// The synthetic deck is a minimal read-only fs.FS (exampleFS) holding a valid
// eypres.yaml and exactly one slide, slides/1-example.md. It touches neither
// the disk nor the network. Because it has one slide, the only known anchor id
// is `example`; a `#label` link inside an example to any other label is
// reported by the link pass, exactly as it would be for an author's deck.
//
// A registered template with no example slide source is reported as a failure:
// the built-ins are expected to prove themselves end to end.
func ValidateBuiltinExamples(reg *template.Registry) []ExampleError {
	if reg == nil {
		return []ExampleError{{
			Template: "",
			Err: New("", 0, nil,
				"no template registry given",
				"pass template.Builtins()' registry"),
		}}
	}

	var failures []ExampleError
	for _, t := range reg.Templates() {
		if t == nil {
			continue
		}
		if verr, invalid := validateExample(reg, t); invalid {
			failures = append(failures, ExampleError{Template: t.Name, Err: verr})
		}
	}
	return failures
}

// validateExample validates one template's example source through Validate in a
// synthetic single-slide deck context. It returns the first error and whether
// one was found.
func validateExample(reg *template.Registry, t *template.Template) (ValidationError, bool) {
	if strings.TrimSpace(t.Example.Markdown) == "" {
		return New(path.Join(t.Name, "example.md"), 0, nil,
			fmt.Sprintf("template %q has no example slide", t.Name),
			"add an example.md so the built-in template is proven end to end"), true
	}

	vreg := reg
	slideSrc := t.Example.Markdown
	if t.Usage == template.UsageSection {
		synthetic, wrapped := wrapSectionExample(reg, t)
		rebuilt, err := registryWithSlide(reg, synthetic)
		if err != nil {
			return New(path.Join(t.Name, "example.md"), 0, nil,
				fmt.Sprintf("template %q: cannot build the synthetic slide context: %v", t.Name, err),
				"fix the compiled-in template registry"), true
		}
		vreg = rebuilt
		slideSrc = wrapped
	}

	return Validate(exampleFS{slide: []byte(slideSrc)}, vreg, exampleThemeRegistry())
}

// exampleThemeRegistry returns a minimal theme registry holding just the
// default theme, so the synthetic deck's config (which never sets a `theme`
// key) resolves. It is built fresh on each call rather than shared, in
// keeping with this file's rule that nothing here touches shared mutable
// state.
func exampleThemeRegistry() *theme.Registry {
	reg := theme.NewRegistry()
	// A programming error only: the default theme's name is a fixed
	// constant, so registration into a fresh, empty registry cannot fail.
	_ = reg.Register(theme.Default())
	return reg
}

// wrapSectionExample turns a section-usage template's example fragment into a
// complete slide source plus the synthesized slide template that uses it. The
// slide declares exactly one section accepting t, so the fragment resolves to t
// and every one of t's rules applies to it.
func wrapSectionExample(reg *template.Registry, t *template.Template) (*template.Template, string) {
	slideName := freeTemplateName(reg, exampleSlideTemplateName)
	synthetic := &template.Template{
		Name:        slideName,
		Description: "Synthetic slide template wrapping one section template's example.",
		Usage:       template.UsageSlide,
		Sections: []template.SectionDecl{{
			Name:     exampleSectionName,
			Accepted: []string{t.Name},
			Min:      1,
			Max:      1,
		}},
		Body: template.BodyRule{Mode: template.BodyOptional},
	}
	src := "---\ntemplate: " + slideName + "\n---\n# " + exampleSectionName + "\n" + t.Example.Markdown
	return synthetic, src
}

// registryWithSlide returns a copy of reg with extra registered into it, so a
// synthesized slide template can be resolved without mutating the caller's
// registry. The copy shares the registered *Template values; they are treated
// as immutable. Registration is the same local check Register always runs —
// Validate re-runs the deck-level pipeline over the rebuilt registry anyway.
func registryWithSlide(reg *template.Registry, extra *template.Template) (*template.Registry, error) {
	rebuilt := template.NewRegistry(nil)
	for _, t := range reg.Templates() {
		if t == nil {
			continue
		}
		if err := rebuilt.Register(t); err != nil {
			return nil, err
		}
	}
	if err := rebuilt.Register(extra); err != nil {
		return nil, err
	}
	return rebuilt, nil
}

// freeTemplateName returns base when it is unused in reg, else base with
// underscores appended until it is, so a synthesized name can never shadow a
// registered template.
func freeTemplateName(reg *template.Registry, base string) string {
	name := base
	for {
		if _, ok := reg.Lookup(name); !ok {
			return name
		}
		name += "_"
	}
}

// The synthetic deck's fixed names.
const (
	// exampleConfigYAML is the synthetic deck's eypres.yaml. Every valid deck
	// needs a title; the synthetic deck is never rendered, so the value is
	// arbitrary.
	exampleConfigYAML = "title: ey-present template example\n"

	// exampleSlideFile is the filename of the synthetic example slide. Its
	// label ("example") is the synthetic deck's only anchor id.
	exampleSlideFile = "1-example.md"

	// exampleSlideTemplateName and exampleSectionName name the synthesized
	// slide template and its single section that wrap a section template's
	// example fragment. The slide name is made unique against the registry at
	// run time (freeTemplateName).
	exampleSlideTemplateName = "__eypres_example_slide"
	exampleSectionName       = "__eypres_example_section"
)

// exampleFS is a minimal read-only fs.FS holding one synthetic deck — a valid
// eypres.yaml and exactly one slide file — so an example slide can be run
// through Validate without touching the disk or the network. It implements the
// FS ReadFile/ReadDir/Stat fast paths the deck loaders use.
type exampleFS struct {
	slide []byte
}

var (
	_ fs.FS         = exampleFS{}
	_ fs.ReadFileFS = exampleFS{}
	_ fs.ReadDirFS  = exampleFS{}
	_ fs.StatFS     = exampleFS{}
)

// Open returns the synthetic file at name, satisfying fs.FS. The pipeline's
// ReadFile/ReadDir/Stat calls use the methods below; Open is a fallback for any
// other fs helper.
func (f exampleFS) Open(name string) (fs.File, error) {
	if data, ok := f.contents(name); ok {
		return &exampleFile{name: name, data: data}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadFile returns the content of a synthetic file.
func (f exampleFS) ReadFile(name string) ([]byte, error) {
	if data, ok := f.contents(name); ok {
		return data, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadDir lists the synthetic deck's slide directory.
func (f exampleFS) ReadDir(name string) ([]fs.DirEntry, error) {
	switch name {
	case ".":
		return []fs.DirEntry{
			exampleDirEntry{name: deck.ConfigFile},
			exampleDirEntry{name: deck.SlidesDir, dir: true},
		}, nil
	case deck.SlidesDir:
		return []fs.DirEntry{exampleDirEntry{name: exampleSlideFile}}, nil
	}
	return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
}

// Stat reports the synthetic file or directory at name.
func (f exampleFS) Stat(name string) (fs.FileInfo, error) {
	if data, ok := f.contents(name); ok {
		return exampleFileInfo{name: name, size: int64(len(data))}, nil
	}
	if name == deck.SlidesDir {
		return exampleFileInfo{name: name, dir: true}, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

// contents returns the bytes of a synthetic file, or ok=false when name is not
// one of them.
func (f exampleFS) contents(name string) ([]byte, bool) {
	switch name {
	case deck.ConfigFile:
		return []byte(exampleConfigYAML), true
	case path.Join(deck.SlidesDir, exampleSlideFile):
		return f.slide, true
	}
	return nil, false
}

// exampleFile is a synthetic regular file.
type exampleFile struct {
	name string
	data []byte
	off  int
}

// Stat reports the file's fixed metadata.
func (f *exampleFile) Stat() (fs.FileInfo, error) {
	return exampleFileInfo{name: f.name, size: int64(len(f.data))}, nil
}

// Read returns the file's bytes, then io.EOF.
func (f *exampleFile) Read(p []byte) (int, error) {
	if f.off >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.off:])
	f.off += n
	return n, nil
}

// Close is a no-op for an in-memory file.
func (f *exampleFile) Close() error { return nil }

// exampleFileInfo is the fs.FileInfo of a synthetic file or directory.
type exampleFileInfo struct {
	name string
	size int64
	dir  bool
}

func (i exampleFileInfo) Name() string { return path.Base(i.name) }
func (i exampleFileInfo) Size() int64  { return i.size }

func (i exampleFileInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

func (i exampleFileInfo) ModTime() time.Time { return time.Time{} }
func (i exampleFileInfo) IsDir() bool        { return i.dir }
func (i exampleFileInfo) Sys() any           { return nil }

// exampleDirEntry is one fs.DirEntry of a synthetic directory.
type exampleDirEntry struct {
	name string
	dir  bool
}

func (e exampleDirEntry) Name() string      { return e.name }
func (e exampleDirEntry) IsDir() bool       { return e.dir }
func (e exampleDirEntry) Type() fs.FileMode { return exampleFileInfo{dir: e.dir}.Mode() &^ 0o777 }
func (e exampleDirEntry) Info() (fs.FileInfo, error) {
	return exampleFileInfo{name: e.name, dir: e.dir}, nil
}
