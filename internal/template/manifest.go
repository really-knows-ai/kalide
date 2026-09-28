package template

import (
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// This file implements parseManifest, which decodes one template.yaml
// manifest (template-manifest) into a *Template definition: the schema data
// that used to be authored in Go (builtins.go) is now authored as YAML data
// per template. It never runs the cross-template build-time checks
// (Registry.Validate owns those) — only local, positioned decoding: an
// unknown key, a malformed value or an unknown field type is reported at its
// offending line in the manifest.
//
// The manifest vocabulary is exactly the existing schema vocabulary
// (field-types, field-rules, body-rules, template-composition,
// template-variants, number-date-formats): there is no `usage:` key (kind
// comes from the directory the manifest was loaded from, checked earlier by
// loadLibraryTemplate) and no author-facing controlled-effects key (a
// `transition`/`background`/`fragment` field or section name is rejected the
// same way any other unknown value is, by the schema checker at content-check
// time, not specially here).
//
// A `name:` key is tolerated and ignored: the template's name is always its
// directory name (templates-dir-layout), never a manifest value.

// manifestFieldTypes maps a manifest `type:` string to its FieldType. It is
// the closed set of author-facing type names (field-types).
var manifestFieldTypes = map[string]FieldType{
	"text":             FieldText,
	"number":           FieldNumber,
	"date":             FieldDate,
	"boolean":          FieldBoolean,
	"enum":             FieldEnum,
	"image":            FieldImage,
	"link":             FieldLink,
	"list":             FieldList,
	"section-template": FieldSectionTemplate,
}

// manifestBodyModes maps a manifest `body.mode:` string to its BodyMode.
var manifestBodyModes = map[string]BodyMode{
	"required":   BodyRequired,
	"optional":   BodyOptional,
	"disallowed": BodyDisallowed,
}

// manifestTopLevelKeys is the complete set of top-level template.yaml schema
// keys parseManifest decodes and dispatches (template-manifest), in
// declaration order. It deliberately excludes the tolerated-and-ignored `name`
// key: a template's name is always its directory name, never a manifest value.
var manifestTopLevelKeys = []string{"description", "fields", "sections", "body"}

// ManifestFieldTypes returns the sorted field-type names a template.yaml
// `type:` may take — the keys of manifestFieldTypes (field-types). It is the
// single source of truth for the field-type vocabulary: callers that must
// enumerate the implemented types (notably the agent-guide drift self-test)
// derive them from here rather than duplicating the list. The returned slice
// is a fresh allocation.
func ManifestFieldTypes() []string {
	out := make([]string, 0, len(manifestFieldTypes))
	for name := range manifestFieldTypes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// BodyModes returns the sorted body-mode names a template.yaml `body.mode:`
// may take — the keys of manifestBodyModes (body-rules). It is the single
// source of truth for the body-mode vocabulary. The returned slice is a fresh
// allocation.
func BodyModes() []string {
	out := make([]string, 0, len(manifestBodyModes))
	for name := range manifestBodyModes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ManifestTopLevelKeys returns the sorted accepted top-level template.yaml
// schema keys parseManifest decodes and dispatches — body, description, fields
// and sections (template-manifest). It does not include the
// tolerated-and-ignored `name` key: a template's name is always its directory
// name, never a manifest value. It is the single source of truth for the
// template.yaml top-level key vocabulary. The returned slice is a fresh
// allocation.
func ManifestTopLevelKeys() []string {
	out := make([]string, len(manifestTopLevelKeys))
	copy(out, manifestTopLevelKeys)
	sort.Strings(out)
	return out
}

// parseManifest decodes t's template.yaml (t.ManifestBytes) into a *Template
// definition named t.Name, with Usage derived from kind (kind-by-directory).
// It returns a positioned *LibraryError for any decoding problem: an unknown
// top-level key, a field or section missing its required name, an unknown
// field type or body mode, or a malformed value.
//
// It performs no cross-template checks (undefined references, cycles,
// reserved names, slide-as-field-type, …): those are Registry.Validate's job,
// run once every manifest in the library has been parsed
// (NewRegistryFromLibrary).
func parseManifest(t *LibraryTemplate) (*Template, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(t.ManifestBytes, &doc); err != nil {
		return nil, libraryErrorf(t.ManifestPath, 0, "%s: %v", ManifestFile, err)
	}
	mapping := libraryDocumentMapping(&doc)

	def := &Template{Name: t.Name, Usage: usageForKind(t.Kind)}
	if mapping == nil {
		// An empty manifest is valid: a template with no description, no
		// fields, no sections and the zero-value (optional) body rule.
		return def, nil
	}
	if mapping.Kind != yaml.MappingNode {
		return nil, libraryErrorf(t.ManifestPath, nodeLineOr(mapping, 1), "expected a mapping of %s keys", ManifestFile)
	}

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		val := mapping.Content[i+1]
		line := nodeLineOr(key, nodeLineOr(mapping, 1))

		switch key.Value {
		case "name":
			// Tolerated and ignored: the name is always the directory name.
		case "description":
			s, err := scalarString(val, t.ManifestPath, line, "description")
			if err != nil {
				return nil, err
			}
			def.Description = s
		case "fields":
			fields, err := parseManifestFields(t.ManifestPath, val)
			if err != nil {
				return nil, err
			}
			def.Fields = fields
		case "sections":
			sections, err := parseManifestSections(t.ManifestPath, val)
			if err != nil {
				return nil, err
			}
			def.Sections = sections
		case "body":
			body, err := parseManifestBody(t.ManifestPath, val)
			if err != nil {
				return nil, err
			}
			def.Body = body
		default:
			e := libraryErrorf(t.ManifestPath, line, "unknown key %q", key.Value)
			return nil, e.withSuggestion(key.Value, manifestTopLevelKeys)
		}
	}

	return def, nil
}

// usageForKind maps a Library template's directory-derived kind to the
// Usage its parsed Template carries.
func usageForKind(kind TemplateKind) Usage {
	if kind == KindSection {
		return UsageSection
	}
	return UsageSlide
}

// scalarString decodes val as a plain string scalar, positioned at line for
// an error.
func scalarString(val *yaml.Node, manifestPath string, line int, key string) (string, error) {
	if val.Kind != yaml.ScalarNode || val.ShortTag() != "!!str" {
		return "", libraryErrorf(manifestPath, line, "key %q: expected a string", key)
	}
	return val.Value, nil
}

// parseManifestFields decodes the `fields:` sequence.
func parseManifestFields(manifestPath string, val *yaml.Node) ([]Field, error) {
	if val.Kind != yaml.SequenceNode {
		return nil, libraryErrorf(manifestPath, nodeLineOr(val, 0), `key "fields": expected a sequence`)
	}
	fields := make([]Field, 0, len(val.Content))
	for _, item := range val.Content {
		f, err := parseManifestField(manifestPath, item)
		if err != nil {
			return nil, err
		}
		fields = append(fields, *f)
	}
	return fields, nil
}

// parseManifestField decodes one field entry (or, recursively, a list
// field's `item:`).
func parseManifestField(manifestPath string, node *yaml.Node) (*Field, error) {
	if node.Kind != yaml.MappingNode {
		return nil, libraryErrorf(manifestPath, nodeLineOr(node, 0), "expected a mapping describing a field")
	}
	line := nodeLineOr(node, 0)
	f := &Field{}
	var haveName, haveType bool
	var typeStr string

	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]
		kl := nodeLineOr(key, line)

		switch key.Value {
		case "name":
			s, err := scalarString(val, manifestPath, kl, "name")
			if err != nil {
				return nil, err
			}
			f.Name = s
			haveName = true
		case "type":
			s, err := scalarString(val, manifestPath, kl, "type")
			if err != nil {
				return nil, err
			}
			typeStr = s
			haveType = true
		case "plain":
			b, err := scalarBool(val, manifestPath, kl, "plain")
			if err != nil {
				return nil, err
			}
			f.Plain = b
		case "required":
			b, err := scalarBool(val, manifestPath, kl, "required")
			if err != nil {
				return nil, err
			}
			f.Required = b
		case "default":
			var v any
			if err := val.Decode(&v); err != nil {
				return nil, libraryErrorf(manifestPath, kl, "key %q: %v", "default", err)
			}
			f.Default = v
		case "description":
			s, err := scalarString(val, manifestPath, kl, "description")
			if err != nil {
				return nil, err
			}
			f.Description = s
		case "max_length":
			n, err := scalarInt(val, manifestPath, kl, "max_length")
			if err != nil {
				return nil, err
			}
			f.MaxLength = n
		case "min_items":
			n, err := scalarInt(val, manifestPath, kl, "min_items")
			if err != nil {
				return nil, err
			}
			f.MinItems = n
		case "max_items":
			n, err := scalarInt(val, manifestPath, kl, "max_items")
			if err != nil {
				return nil, err
			}
			f.MaxItems = n
		case "min":
			n, err := scalarFloat(val, manifestPath, kl, "min")
			if err != nil {
				return nil, err
			}
			f.Min = &n
		case "max":
			n, err := scalarFloat(val, manifestPath, kl, "max")
			if err != nil {
				return nil, err
			}
			f.Max = &n
		case "min_date":
			s, err := scalarString(val, manifestPath, kl, "min_date")
			if err != nil {
				return nil, err
			}
			f.MinDate = s
		case "max_date":
			s, err := scalarString(val, manifestPath, kl, "max_date")
			if err != nil {
				return nil, err
			}
			f.MaxDate = s
		case "variants":
			ss, err := scalarStringList(val, manifestPath, kl, "variants")
			if err != nil {
				return nil, err
			}
			f.Variants = ss
		case "formats":
			ss, err := scalarStringList(val, manifestPath, kl, "formats")
			if err != nil {
				return nil, err
			}
			f.Formats = ss
		case "default_format":
			s, err := scalarString(val, manifestPath, kl, "default_format")
			if err != nil {
				return nil, err
			}
			f.DefaultFormat = s
		case "section_template":
			s, err := scalarString(val, manifestPath, kl, "section_template")
			if err != nil {
				return nil, err
			}
			f.SectionTemplate = s
		case "item":
			item, err := parseManifestField(manifestPath, val)
			if err != nil {
				return nil, err
			}
			f.Item = item
		default:
			e := libraryErrorf(manifestPath, kl, "unknown key %q in a field", key.Value)
			return nil, e.withSuggestion(key.Value, []string{
				"name", "type", "plain", "required", "default", "description",
				"max_length", "min_items", "max_items", "min", "max",
				"min_date", "max_date", "variants", "formats", "default_format",
				"section_template", "item",
			})
		}
	}

	if !haveName || f.Name == "" {
		return nil, libraryErrorf(manifestPath, line, "field: missing required key %q", "name")
	}
	if !haveType {
		return nil, libraryErrorf(manifestPath, line, "field %q: missing required key %q", f.Name, "type")
	}
	ft, ok := manifestFieldTypes[typeStr]
	if !ok {
		valid := make([]string, 0, len(manifestFieldTypes))
		for name := range manifestFieldTypes {
			valid = append(valid, name)
		}
		e := libraryErrorf(manifestPath, line, "field %q: unknown type %q", f.Name, typeStr)
		return nil, e.withSuggestion(typeStr, valid)
	}
	f.Type = ft
	return f, nil
}

// parseManifestSections decodes the `sections:` sequence.
func parseManifestSections(manifestPath string, val *yaml.Node) ([]SectionDecl, error) {
	if val.Kind != yaml.SequenceNode {
		return nil, libraryErrorf(manifestPath, nodeLineOr(val, 0), `key "sections": expected a sequence`)
	}
	out := make([]SectionDecl, 0, len(val.Content))
	for _, item := range val.Content {
		d, err := parseManifestSection(manifestPath, item)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, nil
}

// parseManifestSection decodes one `sections:` entry.
func parseManifestSection(manifestPath string, node *yaml.Node) (*SectionDecl, error) {
	if node.Kind != yaml.MappingNode {
		return nil, libraryErrorf(manifestPath, nodeLineOr(node, 0), "expected a mapping describing a section")
	}
	line := nodeLineOr(node, 0)
	d := &SectionDecl{}
	var haveName bool

	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]
		kl := nodeLineOr(key, line)

		switch key.Value {
		case "name":
			s, err := scalarString(val, manifestPath, kl, "name")
			if err != nil {
				return nil, err
			}
			d.Name = s
			haveName = true
		case "accepted":
			ss, err := scalarStringList(val, manifestPath, kl, "accepted")
			if err != nil {
				return nil, err
			}
			d.Accepted = ss
		case "min":
			n, err := scalarInt(val, manifestPath, kl, "min")
			if err != nil {
				return nil, err
			}
			d.Min = n
		case "max":
			n, err := scalarInt(val, manifestPath, kl, "max")
			if err != nil {
				return nil, err
			}
			d.Max = n
		default:
			e := libraryErrorf(manifestPath, kl, "unknown key %q in a section", key.Value)
			return nil, e.withSuggestion(key.Value, []string{"name", "accepted", "min", "max"})
		}
	}

	if !haveName || d.Name == "" {
		return nil, libraryErrorf(manifestPath, line, "section: missing required key %q", "name")
	}
	if len(d.Accepted) == 0 {
		return nil, libraryErrorf(manifestPath, line, "section %q: missing required key %q", d.Name, "accepted")
	}
	return d, nil
}

// parseManifestBody decodes the `body:` mapping.
func parseManifestBody(manifestPath string, val *yaml.Node) (BodyRule, error) {
	if val.Kind != yaml.MappingNode {
		return BodyRule{}, libraryErrorf(manifestPath, nodeLineOr(val, 0), `key "body": expected a mapping`)
	}
	line := nodeLineOr(val, 0)
	var b BodyRule
	var haveMode bool
	var modeStr string

	for i := 0; i+1 < len(val.Content); i += 2 {
		key := val.Content[i]
		v := val.Content[i+1]
		kl := nodeLineOr(key, line)

		switch key.Value {
		case "mode":
			s, err := scalarString(v, manifestPath, kl, "mode")
			if err != nil {
				return BodyRule{}, err
			}
			modeStr = s
			haveMode = true
		case "max_words":
			n, err := scalarInt(v, manifestPath, kl, "max_words")
			if err != nil {
				return BodyRule{}, err
			}
			b.MaxWords = n
		case "max_paragraphs":
			n, err := scalarInt(v, manifestPath, kl, "max_paragraphs")
			if err != nil {
				return BodyRule{}, err
			}
			b.MaxParagraphs = n
		case "max_list_items":
			n, err := scalarInt(v, manifestPath, kl, "max_list_items")
			if err != nil {
				return BodyRule{}, err
			}
			b.MaxListItems = n
		case "subheadings":
			bv, err := scalarBool(v, manifestPath, kl, "subheadings")
			if err != nil {
				return BodyRule{}, err
			}
			b.Subheadings = bv
		default:
			e := libraryErrorf(manifestPath, kl, "unknown key %q in body", key.Value)
			return BodyRule{}, e.withSuggestion(key.Value, []string{"mode", "max_words", "max_paragraphs", "max_list_items", "subheadings"})
		}
	}

	if !haveMode {
		return BodyRule{}, libraryErrorf(manifestPath, line, `body: missing required key "mode"`)
	}
	mode, ok := manifestBodyModes[modeStr]
	if !ok {
		e := libraryErrorf(manifestPath, line, "body: unknown mode %q", modeStr)
		return BodyRule{}, e.withSuggestion(modeStr, []string{"required", "optional", "disallowed"})
	}
	b.Mode = mode
	return b, nil
}

// scalarBool decodes val as a plain YAML boolean.
func scalarBool(val *yaml.Node, manifestPath string, line int, key string) (bool, error) {
	if val.Kind != yaml.ScalarNode || val.ShortTag() != "!!bool" {
		return false, libraryErrorf(manifestPath, line, "key %q: expected a boolean", key)
	}
	var b bool
	if err := val.Decode(&b); err != nil {
		return false, libraryErrorf(manifestPath, line, "key %q: %v", key, err)
	}
	return b, nil
}

// scalarInt decodes val as a plain YAML integer.
func scalarInt(val *yaml.Node, manifestPath string, line int, key string) (int, error) {
	if val.Kind != yaml.ScalarNode || val.ShortTag() != "!!int" {
		return 0, libraryErrorf(manifestPath, line, "key %q: expected an integer", key)
	}
	var n int
	if err := val.Decode(&n); err != nil {
		return 0, libraryErrorf(manifestPath, line, "key %q: %v", key, err)
	}
	return n, nil
}

// scalarFloat decodes val as a plain YAML number.
func scalarFloat(val *yaml.Node, manifestPath string, line int, key string) (float64, error) {
	if val.Kind != yaml.ScalarNode || (val.ShortTag() != "!!int" && val.ShortTag() != "!!float") {
		return 0, libraryErrorf(manifestPath, line, "key %q: expected a number", key)
	}
	var n float64
	if err := val.Decode(&n); err != nil {
		return 0, libraryErrorf(manifestPath, line, "key %q: %v", key, err)
	}
	return n, nil
}

// scalarStringList decodes val as a sequence of plain string scalars.
func scalarStringList(val *yaml.Node, manifestPath string, line int, key string) ([]string, error) {
	if val.Kind != yaml.SequenceNode {
		return nil, libraryErrorf(manifestPath, line, "key %q: expected a sequence of strings", key)
	}
	out := make([]string, 0, len(val.Content))
	for _, item := range val.Content {
		if item.Kind != yaml.ScalarNode || item.ShortTag() != "!!str" {
			return nil, libraryErrorf(manifestPath, nodeLineOr(item, line), "key %q: expected a sequence of strings", key)
		}
		out = append(out, item.Value)
	}
	return out, nil
}

// manifestTemplateErrorName extracts the template name from a
// Registry.Validate error of the form `template "name": …`, used to
// path-qualify a build-check failure to its manifest. ok is false when the
// error is not in that shape.
func manifestTemplateErrorName(err error) (string, bool) {
	msg := err.Error()
	const prefix = `template "`
	if !strings.HasPrefix(msg, prefix) {
		return "", false
	}
	rest := msg[len(prefix):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}
