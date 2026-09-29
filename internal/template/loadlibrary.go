package template

import (
	"errors"
	htmltemplate "html/template"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// This file implements LoadLibrary, the only way to build a *Library: it
// walks a project's templates/ directory and runs the templates-dir-
// validation checks in a fixed, fail-fast order, returning the first
// failure. Each step is documented at its call site below with its step
// number, matching the plan's ordering:
//
//  1. library.yaml present and valid;
//  2. slide/section names unique across kinds; any top-level entry other
//     than the documented library structure is ignored — only the library
//     itself is validated, so a library that is also a git repo (or carries
//     a README, editor files, …) still loads;
//  3. per template, kind then name: the three required files present,
//     template.yaml well-formed with no `usage:` key, and the layout parses
//     as html/template with only the documented func set;
//  4. (hook) checkLibraryBuild — parsing manifests into definitions and
//     running the template-build-checks; phase 1 leaves this a no-op,
//     phase-2 task-11 fills it in;
//  5. each theme has theme.css;
//  6. a theme's CSS references — theme.css and any CSS it @imports — stay
//     inside the tree that owns them (or reach the library media/ tree via
//     the reserved media: prefix, or a sibling theme via the reserved
//     theme:<name>/ prefix), a layout's literal src/href references stay
//     inside templates/, and every `media` call argument is valid and
//     exists;
//  7. (hook) checkLibraryExamples — each example.md validates against its
//     definition and its layout executes; phase 1 leaves this a no-op,
//     phase-2 task-12 fills it in.
//
// There is no built-in fallback when the library root is missing or is not a
// directory (no-built-in-fallback): the loader reports a *LibraryError naming
// the expected path and suggesting `kalide init`, and the caller must not
// substitute compiled-in content. ResolveLibraryRoot and LoadDeckLibrary
// (resolve.go) are the single point that picks the root — the configured
// `templates:` path, else the local templates/ (external-template-library).

// LoadLibrary loads and validates the templates/ library at root within
// fsys — typically root == TemplatesDir at a project's root directory — and
// returns it once every templates-dir-validation check has passed. It
// returns the first *LibraryError encountered, in the fixed step order
// documented on this file.
//
// root is both the library directory's path within fsys and the path errors
// are qualified with. LoadDeckLibrary (resolve.go) is the deck-level entry
// point: it resolves the root (the configured `templates:` path, else the
// local templates/) and calls loadLibrary with a display root that may differ
// from the path within fsys when the resolved library sits outside the deck.
func LoadLibrary(fsys fs.FS, root string) (*Library, error) {
	return loadLibrary(fsys, root, root)
}

// loadLibrary is LoadLibrary's shared body. fsRoot is the library directory's
// path within fsys; displayRoot is the path errors are qualified with and that
// is stored as Library.RootPath. The two differ when the loader validates a
// resolved external root (external-template-library): fsys is then
// os.DirFS(resolved), fsRoot is ".", and displayRoot is the resolved
// operating-system path. displayRoot is never empty.
func loadLibrary(fsys fs.FS, fsRoot, displayRoot string) (*Library, error) {
	info, err := fs.Stat(fsys, fsRoot)
	if err != nil {
		return nil, missingTemplatesDirError(displayRoot, displayRoot+" directory not found")
	}
	if !info.IsDir() {
		return nil, missingTemplatesDirError(displayRoot, displayRoot+" exists but is not a directory")
	}

	sub, err := fs.Sub(fsys, fsRoot)
	if err != nil {
		return nil, missingTemplatesDirError(displayRoot, displayRoot+" is not usable as a directory")
	}

	lib := &Library{
		Root:     sub,
		RootPath: displayRoot,
		Slides:   make(map[string]*LibraryTemplate),
		Sections: make(map[string]*LibraryTemplate),
		Themes:   make(map[string]*LibraryTheme),
	}

	// Step 1: library.yaml present and valid.
	if err := loadLibraryMeta(sub, displayRoot, lib); err != nil {
		return nil, err
	}

	// Step 2: slide vs section names unique across kinds. Top-level entries
	// other than the documented library structure (library.yaml, slides/,
	// sections/, themes/, media/) are ignored: only the library itself is
	// validated, so an extra entry such as .git/, README.md or an editor
	// lockfile never blocks the load.
	slideNames, err := templateDirNames(sub, displayRoot, SlidesDir)
	if err != nil {
		return nil, err
	}
	sectionNames, err := templateDirNames(sub, displayRoot, SectionsDir)
	if err != nil {
		return nil, err
	}
	if err := checkCrossKindNames(displayRoot, slideNames, sectionNames); err != nil {
		return nil, err
	}

	// media/ is optional; note its presence now so step 3's layout parse and
	// step 6's media-argument check can resolve against it.
	if mediaInfo, statErr := fs.Stat(sub, MediaDir); statErr == nil && mediaInfo.IsDir() {
		if mediaFS, subErr := fs.Sub(sub, MediaDir); subErr == nil {
			lib.HasMedia = true
			lib.Media = mediaFS
		}
	}

	// Step 3: per template, kind then name — files present, manifest
	// well-formed with no `usage:` key, layout parses with the documented
	// func set.
	funcMap := LayoutFuncMap(lib.Media, "/"+MediaDir)
	for _, name := range slideNames {
		t, err := loadLibraryTemplate(sub, displayRoot, SlidesDir, KindSlide, name, funcMap)
		if err != nil {
			return nil, err
		}
		lib.Slides[name] = t
	}
	for _, name := range sectionNames {
		t, err := loadLibraryTemplate(sub, displayRoot, SectionsDir, KindSection, name, funcMap)
		if err != nil {
			return nil, err
		}
		lib.Sections[name] = t
	}

	// Step 4 (hook): the build checks. Phase 1 leaves this a no-op;
	// phase-2 task-11 (checkLibraryBuild) fills it in.
	if err := checkLibraryBuild(lib); err != nil {
		return nil, err
	}

	// Step 5: every theme has theme.css.
	themeNames, err := templateDirNames(sub, displayRoot, ThemesDir)
	if err != nil {
		return nil, err
	}
	for _, name := range themeNames {
		th, err := loadLibraryTheme(sub, displayRoot, name)
		if err != nil {
			return nil, err
		}
		lib.Themes[name] = th
	}

	// Step 6: every theme CSS reference — in theme.css or any CSS it
	// @imports — must be a relative path inside the tree that owns it, or
	// reach the library's shared media/ tree (the reserved media: prefix) or
	// a sibling theme's directory (the reserved theme:<name>/ prefix) through
	// a url() or @import reference (a url() media: target is validated like a
	// layout `media` argument). A layout's literal src/href references stay
	// inside templates/, and every `media` call argument is valid and exists.
	// One scanner spans the whole load (newThemeCSSScanner), so the visiting/
	// scanned sets and the maxThemeCSSImports bound are graph-wide: a
	// stylesheet reachable from two themes is scanned once, and a cycle
	// spanning themes is caught, instead of resetting per theme's entry
	// stylesheet.
	orderedThemeNames := make([]string, 0, len(lib.Themes))
	for name := range lib.Themes {
		orderedThemeNames = append(orderedThemeNames, name)
	}
	sort.Strings(orderedThemeNames)
	themeScanner := newThemeCSSScanner(displayRoot, lib)
	for _, name := range orderedThemeNames {
		if err := themeScanner.scanTheme(lib.Themes[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range slideNames {
		if err := checkLayoutReferences(displayRoot, lib.Slides[name], lib.Media); err != nil {
			return nil, err
		}
	}
	for _, name := range sectionNames {
		if err := checkLayoutReferences(displayRoot, lib.Sections[name], lib.Media); err != nil {
			return nil, err
		}
	}

	// Step 7 (hook): example validation and layout execution. Phase 1
	// leaves this a no-op; phase-2 task-12 (checkLibraryExamples) fills it
	// in.
	if err := checkLibraryExamples(lib); err != nil {
		return nil, err
	}

	return lib, nil
}

// checkLibraryBuild is templates-dir-validation step 4: parsing manifests
// (parseManifest) into definitions and running the template-build-checks
// (Registry.Validate) over them. It builds a *Registry over lib
// (NewRegistryFromLibrary), runs Validate, and — on success — attaches each
// resulting *Template definition to its LibraryTemplate so later steps
// (checkLibraryExamples, step 7) can validate against it without
// re-registering. A failure is reported as a *LibraryError positioned at the
// offending template's template.yaml.
//
// Because a section template may declare sections of its own
// (template-composition), a container template's example.md may contain nested
// instances (template-build-checks, nested-section-validation). Every template
// a definition's declared sections accept must therefore have its own attached
// Definition, or step 7 could not resolve that nested child. This is checked
// here so the build fails fast, positioned at the offending container's
// template.yaml, rather than surfacing mid-recursion later.
func checkLibraryBuild(lib *Library) error {
	reg, err := NewRegistryFromLibrary(lib)
	if err != nil {
		return err
	}
	if err := reg.Validate(); err != nil {
		if name, ok := manifestTemplateErrorName(err); ok {
			if lt, _, found := lib.TemplateByName(name); found {
				return libraryErrorf(lt.ManifestPath, 0, "%s", err.Error())
			}
		}
		return err
	}
	for _, lt := range lib.Slides {
		def, _ := reg.Lookup(lt.Name)
		lt.Definition = def
	}
	for _, lt := range lib.Sections {
		def, _ := reg.Lookup(lt.Name)
		lt.Definition = def
	}
	if err := checkLibraryBuildNested(lib); err != nil {
		return err
	}
	return nil
}

// checkLibraryBuildNested verifies that every section template a loaded
// definition declares as an accepted child also has an attached Definition, so
// recursive example.md validation (step 7) can resolve nested container
// instances. It walks the composition from each loaded definition at every
// depth; a missing child is reported as a *LibraryError positioned at the
// declaring template's template.yaml.
func checkLibraryBuildNested(lib *Library) error {
	checked := map[string]bool{}
	var walk func(owner *LibraryTemplate, def *Template) error
	walk = func(owner *LibraryTemplate, def *Template) error {
		if def == nil || checked[def.Name] {
			return nil
		}
		checked[def.Name] = true
		for i := range def.Sections {
			for _, name := range def.Sections[i].Accepted {
				lt, _, ok := lib.TemplateByName(name)
				if !ok || lt == nil || lt.Definition == nil {
					return libraryErrorf(owner.ManifestPath, 0,
						"section %q accepts template %q, which has no parsed definition for example validation",
						def.Sections[i].Name, name)
				}
				if err := walk(lt, lt.Definition); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, name := range sortedLibraryTemplateNames(lib.Slides) {
		lt := lib.Slides[name]
		if err := walk(lt, lt.Definition); err != nil {
			return err
		}
	}
	for _, name := range sortedLibraryTemplateNames(lib.Sections) {
		lt := lib.Sections[name]
		if err := walk(lt, lt.Definition); err != nil {
			return err
		}
	}
	return nil
}

// libraryMetaKeys is the complete set of library.yaml keys loadLibraryMeta
// handles, in declaration order. It is both the accepted-key set and the
// candidate list for closest-match suggestions.
var libraryMetaKeys = []string{"name", "description", "format"}

// LibraryMetaKeys returns the sorted accepted library.yaml keys loadLibraryMeta
// handles — description, format and name. It is the single source of truth for
// the library metadata vocabulary. The returned slice is a fresh allocation.
func LibraryMetaKeys() []string {
	out := make([]string, len(libraryMetaKeys))
	copy(out, libraryMetaKeys)
	sort.Strings(out)
	return out
}

// loadLibraryMeta reads and parses root/library.yaml (step 1): name must
// match the library naming convention, description is optional, and format
// is a required integer whose only currently accepted value is 1.
func loadLibraryMeta(sub fs.FS, root string, lib *Library) error {
	metaPath := path.Join(root, LibraryFile)
	data, err := fs.ReadFile(sub, LibraryFile)
	if err != nil {
		return libraryErrorf(metaPath, 0, "%s not found", LibraryFile).
			withHint("run `kalide init` to create one")
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return libraryErrorf(metaPath, 0, "%s: %v", LibraryFile, err)
	}
	mapping := libraryDocumentMapping(&doc)
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return libraryErrorf(metaPath, nodeLineOr(mapping, 1), "expected a mapping of library.yaml keys")
	}

	haveName, haveFormat := false, false
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		val := mapping.Content[i+1]
		line := nodeLineOr(key, nodeLineOr(mapping, 1))

		switch key.Value {
		case "name":
			if val.Kind != yaml.ScalarNode || val.ShortTag() != "!!str" || val.Value == "" {
				return libraryErrorf(metaPath, line, "key %q: expected a non-empty string", key.Value)
			}
			if !validName(val.Value) {
				return libraryErrorf(metaPath, line,
					"key %q: %q is not a valid name (want [a-z0-9][a-z0-9-]*)", key.Value, val.Value)
			}
			lib.Meta.Name = val.Value
			haveName = true

		case "description":
			if val.Kind != yaml.ScalarNode || val.ShortTag() != "!!str" {
				return libraryErrorf(metaPath, line, "key %q: expected a string", key.Value)
			}
			lib.Meta.Description = val.Value

		case "format":
			if val.Kind != yaml.ScalarNode || val.ShortTag() != "!!int" {
				return libraryErrorf(metaPath, line, "key %q: expected an integer", key.Value)
			}
			var format int
			if err := val.Decode(&format); err != nil {
				return libraryErrorf(metaPath, line, "key %q: %v", key.Value, err)
			}
			if format != 1 {
				return libraryErrorf(metaPath, line, "key %q: unsupported library format %d (want 1)", key.Value, format)
			}
			lib.Meta.Format = format
			haveFormat = true

		default:
			e := libraryErrorf(metaPath, line, "unknown key %q", key.Value)
			return e.withSuggestion(key.Value, libraryMetaKeys)
		}
	}

	if !haveName {
		return libraryErrorf(metaPath, nodeLineOr(mapping, 1), `missing required key "name"`)
	}
	if !haveFormat {
		return libraryErrorf(metaPath, nodeLineOr(mapping, 1), `missing required key "format"`)
	}
	return nil
}

// templateDirNames lists the directory names directly under root/kindDir,
// sorted. kindDir is absent entirely for an optional directory (themes/,
// sections/, media/ are all optional at the library level, though every
// template still needs its own subdirectory); a non-directory entry with
// that name is an error.
func templateDirNames(sub fs.FS, root, kindDir string) ([]string, error) {
	info, err := fs.Stat(sub, kindDir)
	if err != nil {
		return nil, nil
	}
	if !info.IsDir() {
		return nil, libraryErrorf(path.Join(root, kindDir), 0, "%s exists but is not a directory", kindDir)
	}
	entries, err := fs.ReadDir(sub, kindDir)
	if err != nil {
		return nil, libraryErrorf(path.Join(root, kindDir), 0, "read directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// checkCrossKindNames rejects a name declared in both templates/slides and
// templates/sections, naming both paths (cross-kind name uniqueness).
func checkCrossKindNames(root string, slideNames, sectionNames []string) error {
	sections := make(map[string]struct{}, len(sectionNames))
	for _, name := range sectionNames {
		sections[name] = struct{}{}
	}
	for _, name := range slideNames {
		if _, dup := sections[name]; dup {
			return libraryErrorf(path.Join(root, SlidesDir, name), 0,
				"template %q is declared in both %s and %s",
				name, path.Join(root, SlidesDir, name), path.Join(root, SectionsDir, name))
		}
	}
	return nil
}

// usageKeyPattern matches a top-level `usage:` key in a template.yaml
// manifest — reserved because a template's usage is derived from its
// directory (templates/slides vs templates/sections), not declared.
var usageKeyPattern = regexp.MustCompile(`(?m)^usage\s*:`)

// loadLibraryTemplate loads and step-3-validates one template directory,
// root/kindDir/name: the three required files must be present, the manifest
// must be well-formed YAML with no `usage:` key, and the layout must parse as
// html/template with only the documented func set.
func loadLibraryTemplate(sub fs.FS, root, kindDir string, kind TemplateKind, name string, funcMap htmltemplate.FuncMap) (*LibraryTemplate, error) {
	dir := path.Join(kindDir, name)
	displayDir := path.Join(root, dir)

	manifestRel := path.Join(dir, ManifestFile)
	manifest, err := fs.ReadFile(sub, manifestRel)
	if err != nil {
		return nil, libraryErrorf(path.Join(root, manifestRel), 0, "%s not found", ManifestFile)
	}

	layoutRel := path.Join(dir, LayoutFile)
	layoutText, err := fs.ReadFile(sub, layoutRel)
	if err != nil {
		return nil, libraryErrorf(path.Join(root, layoutRel), 0, "%s not found", LayoutFile)
	}

	exampleRel := path.Join(dir, ExampleFile)
	example, err := fs.ReadFile(sub, exampleRel)
	if err != nil {
		return nil, libraryErrorf(path.Join(root, exampleRel), 0, "%s not found", ExampleFile)
	}

	var probe yaml.Node
	if err := yaml.Unmarshal(manifest, &probe); err != nil {
		return nil, libraryErrorf(path.Join(root, manifestRel), 0, "%s: %v", ManifestFile, err)
	}
	if loc := usageKeyPattern.FindIndex(manifest); loc != nil {
		line := 1 + strings.Count(string(manifest[:loc[0]]), "\n")
		return nil, libraryErrorf(path.Join(root, manifestRel), line,
			"%s must not declare a %q key; usage is derived from its directory (%s)", ManifestFile, "usage", kindDir)
	}

	layoutName := path.Join(root, layoutRel)
	parsed, err := htmltemplate.New(layoutName).Funcs(funcMap).Parse(string(layoutText))
	if err != nil {
		return nil, libraryErrorf(layoutName, 0, "%v", err)
	}

	return &LibraryTemplate{
		Name:          name,
		Kind:          kind,
		Dir:           dir,
		ManifestPath:  displayDir + "/" + ManifestFile,
		ManifestBytes: manifest,
		LayoutPath:    layoutName,
		LayoutText:    string(layoutText),
		Layout:        parsed,
		ExamplePath:   displayDir + "/" + ExampleFile,
		ExampleBytes:  example,
	}, nil
}

// loadLibraryTheme loads one theme directory, root/themes/name, checking
// step 5: theme.css must be present. Every other file the directory contains
// is loaded as an owned file.
func loadLibraryTheme(sub fs.FS, root, name string) (*LibraryTheme, error) {
	dir := path.Join(ThemesDir, name)
	stylesheetRel := path.Join(dir, ThemeStylesheet)
	css, err := fs.ReadFile(sub, stylesheetRel)
	if err != nil {
		return nil, libraryErrorf(path.Join(root, stylesheetRel), 0, "%s not found", ThemeStylesheet)
	}

	th := &LibraryTheme{
		Name:            name,
		Dir:             dir,
		StylesheetPath:  path.Join(root, stylesheetRel),
		StylesheetBytes: css,
		Files:           make(map[string][]byte),
	}
	err = fs.WalkDir(sub, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, dir+"/")
		if rel == ThemeStylesheet {
			return nil
		}
		data, readErr := fs.ReadFile(sub, p)
		if readErr != nil {
			return readErr
		}
		th.Files[rel] = data
		return nil
	})
	if err != nil {
		return nil, libraryErrorf(path.Join(root, dir), 0, "read theme directory: %v", err)
	}
	return th, nil
}

// cssURLPattern matches a CSS url(...) reference, capturing its argument.
var cssURLPattern = regexp.MustCompile(`url\(\s*['"]?([^'")]+)['"]?\s*\)`)

// cssLineAt returns the 1-based line number containing the byte at offset in
// css, by counting the newlines before it. A negative or out-of-range offset
// is clamped to line 1.
func cssLineAt(css []byte, offset int) int {
	if offset < 0 || offset > len(css) {
		return 1
	}
	line := 1
	for _, b := range css[:offset] {
		if b == '\n' {
			line++
		}
	}
	return line
}

// maxThemeCSSImports bounds how many distinct stylesheets the import graph may
// pull in through @import across one library load. A chain or fan-out longer
// than this is reported as a templates error rather than making loading do
// unbounded work; an actual import cycle is detected separately and reported
// as a cycle.
const maxThemeCSSImports = 64

// cssImportPattern matches an @import rule in both its quoted-string form
// (@import "x.css";) and its url() form (@import url("x.css");), capturing
// the import target. The target may be in any of the five alternatives: the
// url() form single-quoted, double-quoted or unquoted, or the bare
// single-quoted or double-quoted string. firstCSSGroup extracts whichever one
// matched.
var cssImportPattern = regexp.MustCompile(`@import\s+(?:url\(\s*(?:'([^']*)'|"([^"]*)"|([^'")\s]+))\s*\)|'([^']*)'|"([^"]*)")`)

// themeCSSRefKind distinguishes the two kinds of reference the step-6 scanner
// follows in a theme stylesheet.
type themeCSSRefKind int

const (
	// themeCSSImport is an @import target.
	themeCSSImport themeCSSRefKind = iota
	// themeCSSURL is a url() reference that is not an @import target.
	themeCSSURL
)

// themeCSSRef is one reference found in a theme CSS file: an @import target or
// a url() target, with the byte offset of the target within the file so an
// error can be located with cssLineAt.
type themeCSSRef struct {
	kind   themeCSSRefKind
	value  string
	offset int
}

// themeCSSRefs returns every @import and non-@import url() reference in css,
// sorted by source offset. A url() that appears as an @import target (the
// url() form of @import) is reported once, as an import, and is not also
// returned as a url() reference.
func themeCSSRefs(css []byte) []themeCSSRef {
	var refs []themeCSSRef
	var importSpans [][2]int
	for _, m := range cssImportPattern.FindAllSubmatchIndex(css, -1) {
		value, offset := firstCSSGroup(css, m)
		if value == "" {
			continue
		}
		importSpans = append(importSpans, [2]int{m[0], m[1]})
		refs = append(refs, themeCSSRef{kind: themeCSSImport, value: value, offset: offset})
	}
	for _, m := range cssURLPattern.FindAllSubmatchIndex(css, -1) {
		if insideSpans(importSpans, m[0]) {
			continue
		}
		refs = append(refs, themeCSSRef{kind: themeCSSURL, value: string(css[m[2]:m[3]]), offset: m[2]})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].offset < refs[j].offset })
	return refs
}

// firstCSSGroup returns the first non-empty capture group in the submatch
// index slice m (in group order) and the byte offset of that capture's start.
// It returns "", m[0] when no group matched.
func firstCSSGroup(css []byte, m []int) (string, int) {
	for g := 2; g+1 < len(m); g += 2 {
		if m[g] < 0 {
			continue
		}
		return string(css[m[g]:m[g+1]]), m[g]
	}
	return "", m[0]
}

// insideSpans reports whether offset falls inside any [start, end) span.
func insideSpans(spans [][2]int, offset int) bool {
	for _, s := range spans {
		if offset >= s[0] && offset < s[1] {
			return true
		}
	}
	return false
}

// themeCSSScanner walks a library's stylesheets exactly once each, across one
// whole library load: every theme's theme.css plus every CSS file they
// @import, wherever in the resolved library that CSS lives — the same theme,
// another theme reached through the reserved theme:<name>/ prefix, or the
// shared media/ tree reached through the reserved media: prefix. One scanner
// is built per library load (newThemeCSSScanner) and reused for every theme's
// entry stylesheet (scanTheme), so the visiting/scanned sets and the
// maxThemeCSSImports count below span the whole import graph instead of
// resetting per theme. visiting holds the stylesheets on the current recursion
// stack (so an import cycle is detected), and scanned holds stylesheets
// already fully validated (so a diamond import — or a stylesheet reachable
// from two themes — is validated once, not re-followed).
type themeCSSScanner struct {
	root string

	// th is the theme scanTheme is currently entering, used by themeByTree as
	// a fast path. References ARE followed across trees through the reserved
	// media:/theme:<name>/ prefixes (see checkImport and checkURL), so a
	// stylesheet imported from another tree resolves its unprefixed relative
	// references against the theme that actually owns it; themeByTree falls
	// back to the whole library's themes when that tree is not th's.
	th *LibraryTheme

	// themes is the whole loaded library's themes keyed by name
	// (Library.Themes), so the scanner can resolve any theme:<name>/
	// reference across the import graph.
	themes map[string]*LibraryTheme

	// mediaFS is the library's media/ sub-filesystem (Library.Media). It
	// backs every media: reference, and later a media: @import followed
	// into the shared media tree.
	mediaFS fs.FS

	// visiting and scanned are the graph-wide recursion bookkeeping, keyed
	// so the same stylesheet is never followed twice across trees.
	visiting map[string]bool
	scanned  map[string]bool

	// tree is the owning tree of the stylesheet currently being scanned: a
	// theme's directory (LibraryTheme.Dir, e.g. "themes/default"), or
	// MediaDir ("media") when the stylesheet was reached through the
	// reserved media: prefix. scanFile sets it so a stylesheet's unprefixed
	// relative references are confined to the tree that owns it.
	tree string
}

// newThemeCSSScanner builds the graph-wide scanner for one library load. root
// is the templates/ root the display paths are built under. lib supplies the
// whole loaded library: its themes (Library.Themes) let the scanner resolve
// any theme:<name>/ reference across the import graph, and its media/
// sub-filesystem (Library.Media) backs every media: reference (a nil lib.Media
// makes every media: reference fail, since there is nowhere to resolve it).
// One scanner is reused for every theme (scanTheme), so its visiting/scanned
// sets and maxThemeCSSImports count span the whole graph.
func newThemeCSSScanner(root string, lib *Library) *themeCSSScanner {
	return &themeCSSScanner{
		root:     root,
		themes:   lib.Themes,
		mediaFS:  lib.Media,
		visiting: map[string]bool{},
		scanned:  map[string]bool{},
	}
}

// checkThemeCSSReferences validates a theme's CSS references (templates-dir-
// validation step 6): every relative url() reference and every @import target
// must resolve to an existing file inside the referencing stylesheet's own
// tree. A reference carrying the reserved media: prefix must resolve like a
// layout `media "path"` argument against mediaFS (both url() and @import), and
// one carrying the reserved theme:<name>/ prefix must name a file inside an
// existing theme's directory (both url() and @import); an unknown <name> is a
// templates error. An absolute path, a `..` escape that leaves the resolved
// tree, and any other scheme (http:, https:, data:, //) are rejected.
// @imports are followed in both the quoted-string and url() forms, bounded and
// cycle-safe across the whole import graph, so a stylesheet reached through a
// prefix has its own references validated in its own tree. Every error is a
// *LibraryError qualified with the offending CSS file and its 1-based line.
//
// It builds one scanner for the call (newThemeCSSScanner) and scans th's entry
// stylesheet with graph-wide visiting/scanned sets (scanTheme). loadLibrary
// instead builds one scanner for the whole library load and scans every theme
// through it, so the sets and the maxThemeCSSImports bound span the entire
// import graph rather than resetting per theme.
//
// root is the templates/ root the display paths are built under. th is the
// theme whose stylesheet seeds the scan: th.Dir is the theme's directory
// within that root and th.Files holds every other file the theme directory
// owns. lib is the whole loaded library, so the scanner can resolve any
// theme:<name>/ reference across the import graph (lib.Themes) and reach the
// shared media/ tree behind every media: reference (lib.Media; a nil
// lib.Media makes every media: reference fail, since there is nowhere to
// resolve it).
func checkThemeCSSReferences(root string, th *LibraryTheme, lib *Library) error {
	return newThemeCSSScanner(root, lib).scanTheme(th)
}

// scanTheme validates one theme's theme.css and everything it @imports with
// the scanner's graph-wide visiting/scanned sets. th.Dir is the theme's
// directory within root and th.Files holds every other file the theme
// directory owns. The entry stylesheet is registered in the graph-wide sets
// under its full key, th.Dir/theme.css, not the bare theme.css: a stylesheet
// is identified by its owning tree joined with its relative path, so two
// themes' theme.css files are not confused with each other. A theme whose
// theme.css was already reached and validated through an earlier theme's
// @import is skipped, so no stylesheet is scanned twice in one load and the
// maxThemeCSSImports count is charged for it once.
func (s *themeCSSScanner) scanTheme(th *LibraryTheme) error {
	if th == nil {
		return nil
	}
	themeKey := path.Join(th.Dir, ThemeStylesheet)
	if s.scanned[themeKey] {
		return nil
	}
	s.th = th
	s.visiting[themeKey] = true
	err := s.scanFile(th.StylesheetBytes, th.StylesheetPath, th.Dir, ThemeStylesheet)
	delete(s.visiting, themeKey)
	if err != nil {
		return err
	}
	s.scanned[themeKey] = true
	return nil
}

// scanFile validates every reference in one CSS file, in source order. css is
// the file's bytes, displayPath is its project-root-relative path for errors,
// tree is the tree that owns the stylesheet (a theme directory such as
// "themes/default", or MediaDir "media" when the stylesheet was reached
// through the reserved media: prefix), and rel is the stylesheet's path within
// tree, which its relative references resolve against. scanFile sets s.tree to
// tree for the duration of the file's scan — restoring the caller's tree on
// return — so an unprefixed relative reference is confined to the stylesheet's
// own tree rather than the theme the scan was entered from.
func (s *themeCSSScanner) scanFile(css []byte, displayPath, tree, rel string) error {
	prevTree := s.tree
	s.tree = tree
	defer func() { s.tree = prevTree }()

	for _, ref := range themeCSSRefs(css) {
		line := cssLineAt(css, ref.offset)
		if ref.kind == themeCSSImport {
			if err := s.checkImport(displayPath, rel, ref, line); err != nil {
				return err
			}
			continue
		}
		if err := s.checkURL(displayPath, rel, ref, line); err != nil {
			return err
		}
	}
	return nil
}

// checkImport validates one @import target and follows it, wherever in the
// resolved library the stylesheet it names lives. An unprefixed relative
// target resolves from the importing file's own location and must stay inside
// the tree that owns it: its theme directory, or media/ for a stylesheet
// reached through the reserved media: prefix. A target carrying the reserved
// media: prefix names a file under the library's shared media/ tree, and one
// carrying the reserved theme:<name>/ prefix names a file inside the named
// theme's directory (an unknown <name> is an error); both prefixes are valid
// on @import, so a stylesheet is followed across trees. Any other scheme or an
// absolute path is rejected. The target is followed through scanFile keyed by
// its tree-qualified path — so a cycle across trees is detected and a diamond
// is scanned once — and bounded by maxThemeCSSImports across the whole graph.
// Every error is a *LibraryError naming the offending CSS file and line.
func (s *themeCSSScanner) checkImport(displayPath, rel string, ref themeCSSRef, line int) error {
	target := ref.value

	// Each target kind resolves to the bytes to follow plus the tree and
	// tree-relative path that identify it in the graph.
	var (
		data  []byte
		tree  string
		relIn string
	)

	switch {
	case strings.HasPrefix(target, MediaURLPrefix()):
		// The reserved media: prefix names a file under the shared media/
		// tree, exactly like a layout `media "path"` argument. It is valid
		// on @import, so the file is read and followed.
		clean, err := cleanMediaPath(strings.TrimPrefix(target, MediaURLPrefix()))
		if err != nil {
			return positionThemeCSSError(err, displayPath, line)
		}
		if s.mediaFS == nil {
			return libraryErrorf(displayPath, line,
				"@import %q: media file not found: no templates/media directory", target)
		}
		b, readErr := fs.ReadFile(s.mediaFS, clean)
		if readErr != nil {
			return libraryErrorf(displayPath, line,
				"@import %q: media file not found: %v", target, readErr)
		}
		data, tree, relIn = b, MediaDir, clean

	case strings.HasPrefix(target, ThemeURLPrefix()):
		// The reserved theme:<name>/ prefix names a file inside the named
		// theme's directory. The path after the name is relative to that
		// theme's directory and must stay inside it.
		name, sub, found := strings.Cut(strings.TrimPrefix(target, ThemeURLPrefix()), "/")
		if !found || name == "" || sub == "" {
			return libraryErrorf(displayPath, line,
				"@import %q: expected %s<theme>/<path>", target, ThemeURLPrefix())
		}
		owner, ok := s.themes[name]
		if !ok {
			return libraryErrorf(displayPath, line,
				"@import %q: unknown theme %q", target, name)
		}
		dir := path.Join(s.root, owner.Dir)
		if path.IsAbs(sub) || strings.HasPrefix(sub, "/") {
			return libraryErrorf(displayPath, line,
				"@import %q: path %q must be relative to theme directory %s, not absolute",
				target, sub, dir)
		}
		themeRel := path.Clean(sub)
		if themeRel == ".." || strings.HasPrefix(themeRel, "../") {
			return libraryErrorf(displayPath, line,
				"@import %q: path %q must not escape theme directory %s with \"..\"",
				target, sub, dir)
		}
		if themeRel == "." {
			return libraryErrorf(displayPath, line,
				"@import %q: path must not be empty", target)
		}
		if themeRel == ThemeStylesheet {
			data = owner.StylesheetBytes
		} else {
			b, exists := owner.Files[themeRel]
			if !exists {
				return libraryErrorf(displayPath, line,
					"@import %q: file not found inside theme directory %s", target, dir)
			}
			data = b
		}
		tree, relIn = owner.Dir, themeRel

	default:
		// An unprefixed relative target resolves within its own tree and must
		// stay inside it; cross-tree reach is only through the two reserved
		// prefixes.
		desc := "its theme directory"
		if s.tree == MediaDir {
			desc = MediaDir + "/"
		}
		if isExternalRef(target) {
			return libraryErrorf(displayPath, line,
				"@import %q must be a relative path inside %s", target, desc)
		}
		importRel := path.Clean(path.Join(path.Dir(rel), target))
		if importRel == ".." || strings.HasPrefix(importRel, "../") || path.IsAbs(importRel) {
			return libraryErrorf(displayPath, line,
				"@import %q escapes %s %s", target, desc, path.Join(s.root, s.tree))
		}
		if s.tree == MediaDir {
			if s.mediaFS == nil {
				return libraryErrorf(displayPath, line,
					"@import %q: media file not found: no templates/media directory", target)
			}
			b, readErr := fs.ReadFile(s.mediaFS, importRel)
			if readErr != nil {
				return libraryErrorf(displayPath, line,
					"@import %q: file not found inside %s %s", target, desc, path.Join(s.root, s.tree))
			}
			data = b
		} else {
			owner := s.themeByTree(s.tree)
			var exists bool
			if owner != nil {
				if importRel == ThemeStylesheet {
					data, exists = owner.StylesheetBytes, true
				} else {
					data, exists = owner.Files[importRel]
				}
			}
			if !exists {
				return libraryErrorf(displayPath, line,
					"@import %q: file not found inside %s %s", target, desc, path.Join(s.root, s.tree))
			}
		}
		tree, relIn = s.tree, importRel
	}

	// Follow the target, keyed by its tree-qualified path so a cycle spanning
	// trees is detected and an already-validated stylesheet is not re-followed.
	key := path.Join(tree, relIn)
	if s.visiting[key] {
		return libraryErrorf(displayPath, line, "@import %q: import cycle", target)
	}
	if s.scanned[key] {
		return nil
	}
	if len(s.scanned)+len(s.visiting) >= maxThemeCSSImports {
		return libraryErrorf(displayPath, line,
			"@import %q: too many imported stylesheets (limit %d)", target, maxThemeCSSImports)
	}
	s.visiting[key] = true
	err := s.scanFile(data, path.Join(s.root, tree, relIn), tree, relIn)
	delete(s.visiting, key)
	if err != nil {
		return err
	}
	s.scanned[key] = true
	return nil
}

// checkURL validates one url() reference that is not an @import target. A
// target carrying the reserved media: prefix resolves against mediaFS like a
// layout `media` argument; one carrying the reserved theme:<name>/ prefix
// names a file inside the named sibling theme's directory (an unknown <name>
// is a templates error naming this CSS file and line); any other target must
// be a relative path naming an existing file inside the stylesheet's own tree
// — its theme directory, or media/ for a stylesheet reached through the
// reserved media: prefix. A scheme (http:, https:, data:, //) or an absolute
// path, and a `..` that leaves the resolved tree, are rejected. Every error is
// a *LibraryError qualified with the offending CSS file and its 1-based line.
func (s *themeCSSScanner) checkURL(displayPath, rel string, ref themeCSSRef, line int) error {
	target := ref.value
	if target == "" {
		return nil
	}

	// The reserved media: prefix names a file under the shared media/ tree,
	// exactly like a layout `media "path"` argument.
	if mediaPath := strings.TrimPrefix(target, MediaURLPrefix()); mediaPath != target {
		clean, err := cleanMediaPath(mediaPath)
		if err != nil {
			return positionThemeCSSError(err, displayPath, line)
		}
		if s.mediaFS == nil {
			return libraryErrorf(displayPath, line,
				"url(%q): media file not found: no templates/media directory", target)
		}
		if _, statErr := fs.Stat(s.mediaFS, clean); statErr != nil {
			return libraryErrorf(displayPath, line,
				"url(%q): media file not found: %v", target, statErr)
		}
		return nil
	}

	// The reserved theme:<name>/ prefix names a file inside the named theme's
	// directory: the url() counterpart of the @import form. <name> must be a
	// loaded theme, and the path after it is relative to that theme's
	// directory and must stay inside it.
	if rest := strings.TrimPrefix(target, ThemeURLPrefix()); rest != target {
		name, sub, found := strings.Cut(rest, "/")
		if !found || name == "" || sub == "" {
			return libraryErrorf(displayPath, line,
				"url(%q): expected %s<theme>/<path>", target, ThemeURLPrefix())
		}
		owner, ok := s.themes[name]
		if !ok {
			return libraryErrorf(displayPath, line,
				"url(%q): unknown theme %q", target, name)
		}
		dir := path.Join(s.root, owner.Dir)
		if path.IsAbs(sub) || strings.HasPrefix(sub, "/") {
			return libraryErrorf(displayPath, line,
				"url(%q): path %q must be relative to theme directory %s, not absolute",
				target, sub, dir)
		}
		themeRel := path.Clean(sub)
		if themeRel == ".." || strings.HasPrefix(themeRel, "../") {
			return libraryErrorf(displayPath, line,
				"url(%q): path %q must not escape theme directory %s with \"..\"",
				target, sub, dir)
		}
		if themeRel == "." {
			return libraryErrorf(displayPath, line,
				"url(%q): path must not be empty", target)
		}
		if !themeOwnsFile(owner, themeRel) {
			return libraryErrorf(displayPath, line,
				"url(%q): file not found inside theme directory %s", target, dir)
		}
		return nil
	}

	// Any other scheme or an absolute path is external and rejected. The
	// tree the reference must stay inside is the stylesheet's own: its theme
	// directory, or media/ when it was reached through the reserved media:
	// prefix.
	desc := "its theme directory"
	if s.tree == MediaDir {
		desc = MediaDir + "/"
	}
	if isExternalRef(target) {
		return libraryErrorf(displayPath, line,
			"url(%q) must be a relative path inside %s", target, desc)
	}

	// An unprefixed relative target resolves from this stylesheet's own
	// location and must stay inside the tree that owns it — never the tree
	// the scan was entered from.
	urlRel := path.Clean(path.Join(path.Dir(rel), target))
	if urlRel == ".." || strings.HasPrefix(urlRel, "../") || path.IsAbs(urlRel) {
		return libraryErrorf(displayPath, line,
			"url(%q) escapes %s %s", target, desc, path.Join(s.root, s.tree))
	}
	if s.tree == MediaDir {
		if s.mediaFS == nil {
			return libraryErrorf(displayPath, line,
				"url(%q): media file not found: no templates/media directory", target)
		}
		if _, statErr := fs.Stat(s.mediaFS, urlRel); statErr != nil {
			return libraryErrorf(displayPath, line,
				"url(%q): media file not found: %v", target, statErr)
		}
		return nil
	}
	if !themeOwnsFile(s.themeByTree(s.tree), urlRel) {
		return libraryErrorf(displayPath, line,
			"url(%q): file not found inside theme directory %s", target, path.Join(s.root, s.tree))
	}
	return nil
}

// themeByTree returns the loaded theme whose directory is tree (a
// LibraryTheme.Dir, e.g. "themes/default"), or nil when no theme owns it. A
// stylesheet reached through an import resolves its unprefixed relative
// references against the theme that actually owns it, rather than the theme
// the scan was entered from, so the lookup goes through the whole library.
func (s *themeCSSScanner) themeByTree(tree string) *LibraryTheme {
	if s.th != nil && s.th.Dir == tree {
		return s.th
	}
	for _, th := range s.themes {
		if th.Dir == tree {
			return th
		}
	}
	return nil
}

// themeOwnsFile reports whether th owns rel, a path relative to th.Dir.
// theme.css itself counts as owned even though Files excludes it, mirroring
// loadLibraryTheme, which stores the required stylesheet separately from the
// other theme files.
func themeOwnsFile(th *LibraryTheme, rel string) bool {
	if th == nil {
		return false
	}
	if rel == ThemeStylesheet {
		return true
	}
	_, ok := th.Files[rel]
	return ok
}

// positionThemeCSSError re-points a *LibraryError that names a media path
// (from cleanMediaPath) at the offending CSS file and line, so a bad media:
// reference reads as a file:line-qualified templates error. A non-
// *LibraryError is wrapped into one at the same position.
func positionThemeCSSError(err error, displayPath string, line int) error {
	var libErr *LibraryError
	if errors.As(err, &libErr) {
		c := *libErr
		c.Path = displayPath
		c.Line = line
		return &c
	}
	return libraryErrorf(displayPath, line, "%v", err)
}

// htmlRefPattern matches a literal src="..." or href="..." attribute in a
// layout (not a {{ media "…" }} action, which checkLayoutReferences handles
// separately).
var htmlRefPattern = regexp.MustCompile(`(?:src|href)\s*=\s*"([^"]*)"`)

// checkLayoutReferences validates one template's layout source (step 6):
// every literal src/href reference must stay inside templates/, and every
// `media "path"` call argument must be valid and name an existing media
// file.
func checkLayoutReferences(root string, t *LibraryTemplate, mediaFS fs.FS) error {
	if t == nil {
		return nil
	}
	for _, m := range htmlRefPattern.FindAllStringSubmatch(t.LayoutText, -1) {
		ref := m[1]
		if ref == "" || isExternalRef(ref) || strings.HasPrefix(ref, "{{") {
			continue
		}
		resolved := path.Clean(path.Join(root, t.Dir, ref))
		if !strings.HasPrefix(resolved, root+"/") && resolved != root {
			return libraryErrorf(t.LayoutPath, 0,
				"layout reference %q must stay inside %s", ref, root)
		}
	}
	for _, m := range mediaCallPattern.FindAllStringSubmatch(t.LayoutText, -1) {
		arg := m[1]
		clean, err := cleanMediaPath(arg)
		if err != nil {
			var libErr *LibraryError
			if le, ok := err.(*LibraryError); ok {
				libErr = le
			}
			if libErr != nil {
				libErr.Path = t.LayoutPath
				return libErr
			}
			return libraryErrorf(t.LayoutPath, 0, "media %q: %v", arg, err)
		}
		if mediaFS == nil {
			return libraryErrorf(t.LayoutPath, 0, "media %q: no templates/media directory", arg)
		}
		if _, statErr := fs.Stat(mediaFS, clean); statErr != nil {
			return libraryErrorf(t.LayoutPath, 0, "media %q: file not found: %v", arg, statErr)
		}
	}
	return nil
}

// mediaCallPattern matches a `media "path"` action call in a layout's
// source, capturing its string-literal argument.
var mediaCallPattern = regexp.MustCompile(`\{\{-?\s*media\s+"([^"]*)"\s*-?\}\}`)

// isExternalRef reports whether ref names something outside the templates/
// tree entirely: an absolute path, a scheme (http:, https:, data:) or a
// protocol-relative reference.
func isExternalRef(ref string) bool {
	if strings.HasPrefix(ref, "//") {
		return true
	}
	if path.IsAbs(ref) {
		return true
	}
	if i := strings.Index(ref, ":"); i > 0 {
		return true
	}
	return false
}

// libraryDocumentMapping returns the root mapping of a parsed YAML document,
// or nil when the document is empty.
func libraryDocumentMapping(doc *yaml.Node) *yaml.Node {
	if doc == nil || doc.Kind == 0 {
		return nil
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		return doc.Content[0]
	}
	return doc
}

// nodeLineOr returns n's 1-based line, or fallback when n has none.
func nodeLineOr(n *yaml.Node, fallback int) int {
	if n != nil && n.Line > 0 {
		return n.Line
	}
	return fallback
}
