package template

import (
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
//  6. theme.css's relative url() references stay inside the theme
//     directory, a layout's literal src/href references stay inside
//     templates/, and every `media` call argument is valid and exists;
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

	// Step 6: theme.css url() references stay inside their theme directory,
	// a layout's literal src/href references stay inside templates/, and
	// every `media` call argument is valid and exists.
	for _, name := range themeNames {
		if err := checkThemeURLs(displayRoot, lib.Themes[name]); err != nil {
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

// checkThemeURLs rejects a theme.css url(...) reference that is not a
// relative path staying inside its own theme directory (step 6): an absolute
// path, a scheme (http:, data:, //) or a `..` escape is rejected.
func checkThemeURLs(root string, th *LibraryTheme) error {
	if th == nil {
		return nil
	}
	for _, m := range cssURLPattern.FindAllSubmatch(th.StylesheetBytes, -1) {
		ref := string(m[1])
		if ref == "" {
			continue
		}
		if isExternalRef(ref) {
			return libraryErrorf(th.StylesheetPath, 0,
				"%s: url(%q) must be a relative path inside its theme directory", ThemeStylesheet, ref)
		}
		clean := path.Clean(ref)
		if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return libraryErrorf(th.StylesheetPath, 0,
				"%s: url(%q) escapes its theme directory %s", ThemeStylesheet, ref, path.Join(root, th.Dir))
		}
	}
	return nil
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
