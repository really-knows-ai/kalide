package template

import (
	htmltemplate "html/template"
	"io/fs"
	"regexp"
	"sort"
)

// This file declares Library, the loaded shape of a project's templates/
// directory: its library.yaml metadata, its slide and section templates, its
// themes and its media root. Building one from an fs.FS is LoadLibrary
// (loadlibrary.go); the errors it reports are LibraryError (libraryerror.go);
// the html/template func map its layouts execute with is MediaFunc and
// LayoutFuncMap (media.go).
//
// Library is a plain data model: it carries what was found and parsed, not
// how it was validated. LoadLibrary is where the templates-dir-validation
// rules live.

// TemplatesDir is the fixed name of a project's template library directory,
// at the root of a project alongside eypres.yaml and slides/
// (templates-dir-required).
const TemplatesDir = "templates"

// LibraryFile is the fixed name of the library's metadata file, at the root
// of the templates/ directory.
const LibraryFile = "library.yaml"

// SlidesDir, SectionsDir, ThemesDir and MediaDir are the fixed top-level
// entries a templates/ directory may contain besides LibraryFile
// (templates-dir-layout). Any other top-level entry is rejected.
const (
	SlidesDir   = "slides"
	SectionsDir = "sections"
	ThemesDir   = "themes"
	MediaDir    = "media"
)

// ManifestFile, LayoutFile and ExampleFile are the fixed three files every
// slide or section template directory must contain.
const (
	ManifestFile = "template.yaml"
	LayoutFile   = "layout.html.tmpl"
	ExampleFile  = "example.md"
)

// ThemeStylesheet is the fixed required file inside a theme directory.
const ThemeStylesheet = "theme.css"

// libraryNamePattern is the accepted form for a library name, a template
// name, a section name or a theme name: it must start with a lowercase letter
// or digit and continue with lowercase letters, digits or hyphens.
var libraryNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validName reports whether name matches the library naming convention
// (project-templates): [a-z0-9][a-z0-9-]*.
func validName(name string) bool {
	return libraryNamePattern.MatchString(name)
}

// TemplateKind says whether a Library template directory came from
// templates/slides or templates/sections (kind by directory).
type TemplateKind string

const (
	// KindSlide marks a template loaded from templates/slides/<name>.
	KindSlide TemplateKind = "slide"

	// KindSection marks a template loaded from templates/sections/<name>.
	KindSection TemplateKind = "section"
)

// LibraryMeta is the parsed templates/library.yaml metadata
// (templates-dir-layout).
type LibraryMeta struct {
	// Name is the library's own name: [a-z0-9][a-z0-9-]*.
	Name string

	// Description is an optional one-line summary.
	Description string

	// Format is the library manifest format version. Exactly one version
	// (1) is currently accepted; the key is required.
	Format int
}

// LibraryTemplate is one slide or section template directory,
// templates/<slides|sections>/<name>/, holding its three required files
// (templates-dir-layout).
type LibraryTemplate struct {
	// Name is the template's directory name, and the name an author writes
	// in a slide or section's `template:` key.
	Name string

	// Kind says whether this came from templates/slides or
	// templates/sections.
	Kind TemplateKind

	// Dir is the template's directory path within the templates/ root, e.g.
	// "slides/hello".
	Dir string

	// ManifestPath is Dir/template.yaml.
	ManifestPath string

	// ManifestBytes is template.yaml's raw, unparsed content. Parsing it
	// into a Template definition is parseManifest (phase 2).
	ManifestBytes []byte

	// LayoutPath is Dir/layout.html.tmpl.
	LayoutPath string

	// LayoutText is the layout's raw html/template source.
	LayoutText string

	// Layout is the parsed html/template, ready to execute. It is parsed
	// with only the documented func set (LayoutFuncMap); an undocumented
	// function name fails to parse with a positioned error.
	Layout *htmltemplate.Template

	// ExamplePath is Dir/example.md.
	ExamplePath string

	// ExampleBytes is example.md's raw content. Validating it against the
	// template's parsed definition and executing the layout against it is
	// checkLibraryExamples (phase 2).
	ExampleBytes []byte

	// Definition is the *Template built from ManifestBytes by parseManifest
	// and checked by Registry.Validate. It is nil until checkLibraryBuild
	// (templates-dir-validation step 4) fills it in; checkLibraryExamples
	// (step 7) is the first consumer.
	Definition *Template
}

// LibraryTheme is one theme directory, templates/themes/<name>/, holding its
// required theme.css and any other files it owns (templates-dir-layout).
type LibraryTheme struct {
	// Name is the theme's directory name, and the name a deck's `theme:` key
	// selects.
	Name string

	// Dir is the theme's directory path within the templates/ root, e.g.
	// "themes/default".
	Dir string

	// StylesheetPath is Dir/theme.css.
	StylesheetPath string

	// StylesheetBytes is theme.css's raw content.
	StylesheetBytes []byte

	// Files lists every other file the theme directory owns (fonts, images,
	// …), keyed by their path relative to Dir. It excludes theme.css itself.
	Files map[string][]byte
}

// Library is a project's loaded templates/ directory: its metadata, its
// slide and section templates (kind by directory), its themes and its media
// root. LoadLibrary is the only way to build one; a Library that exists has
// already passed the templates-dir-validation checks LoadLibrary runs before
// returning it.
type Library struct {
	// Root is the templates/ directory, rooted so that "library.yaml",
	// "slides", "sections", "themes" and "media" are its direct entries.
	Root fs.FS

	// RootPath is Root's path within the project filesystem LoadLibrary was
	// given, typically TemplatesDir ("templates"). It is used to
	// path-qualify errors and to locate the media root for serving.
	RootPath string

	// Meta is the parsed library.yaml metadata.
	Meta LibraryMeta

	// Slides holds the templates loaded from templates/slides, keyed by
	// name.
	Slides map[string]*LibraryTemplate

	// Sections holds the templates loaded from templates/sections, keyed by
	// name. A slide template and a section template may not share a name
	// (cross-kind name uniqueness); LoadLibrary enforces this before
	// returning.
	Sections map[string]*LibraryTemplate

	// Themes holds the themes loaded from templates/themes, keyed by name.
	Themes map[string]*LibraryTheme

	// HasMedia reports whether the templates/media directory exists. It may
	// be empty or absent; there is no media/ requirement (media is optional
	// content, not a required top-level entry).
	HasMedia bool

	// Media is the templates/media sub-filesystem, rooted so that a media
	// path an author writes ("logo.svg", "video/intro.mp4") is a direct
	// fs.FS path within it. It is nil when HasMedia is false.
	Media fs.FS
}

// TemplateByName returns the slide or section template registered under
// name, and its kind. ok is false when neither Slides nor Sections has that
// name — the two maps are kept name-disjoint by LoadLibrary, so at most one
// can match.
func (l *Library) TemplateByName(name string) (*LibraryTemplate, TemplateKind, bool) {
	if l == nil {
		return nil, "", false
	}
	if t, ok := l.Slides[name]; ok {
		return t, KindSlide, true
	}
	if t, ok := l.Sections[name]; ok {
		return t, KindSection, true
	}
	return nil, "", false
}

// Names returns every slide and section template name in the library,
// sorted. It is the candidate list for a closest-match "did you mean …?"
// suggestion over template names.
func (l *Library) Names() []string {
	if l == nil {
		return nil
	}
	names := make([]string, 0, len(l.Slides)+len(l.Sections))
	for name := range l.Slides {
		names = append(names, name)
	}
	for name := range l.Sections {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ThemeNames returns every theme name in the library, sorted.
func (l *Library) ThemeNames() []string {
	if l == nil {
		return nil
	}
	names := make([]string, 0, len(l.Themes))
	for name := range l.Themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
