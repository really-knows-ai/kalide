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
var manifestTopLevelKeys = []string{"description", "fields", "sections", "body", "internal"}

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
// schema keys parseManifest decodes and dispatches — body, description, fields,
// internal and sections (template-manifest). It does not include the
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
//
// defs is the named definitions of the library layer that owns t
// (LibraryLayer.Definitions). It is threaded through to the field and section
// parsers so a field's `type:`/`include:` or a section's `@group` name is
// resolved against its own owning layer (named-library-definitions); a nil
// defs holds no definitions. A top-level `internal:` boolean is decoded into
// Template.Internal (internal-templates).
func parseManifest(t *LibraryTemplate, defs *LibraryDefinitions) (*Template, error) {
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
			fields, err := parseManifestFields(t.ManifestPath, val, defs)
			if err != nil {
				return nil, err
			}
			def.Fields = fields
		case "sections":
			sections, err := parseManifestSections(t.ManifestPath, val, defs)
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
		case "internal":
			b, err := scalarBool(val, t.ManifestPath, line, "internal")
			if err != nil {
				return nil, err
			}
			def.Internal = b
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

// parseManifestFields decodes the `fields:` sequence. An entry may be the
// named-field-set reference `- include: <name>`, which expands in place to the
// named field set's fields from the owning layer's definitions
// (named-library-definitions); every other entry is one field descriptor
// (parseManifestField). defs is the owning layer's named definitions
// (parseManifest), threaded to each field parser and used to resolve an
// `include:` name. A name no field set carries, or a malformed reference, is a
// positioned templates error; an unknown name carries a closest-match
// suggestion (closest-match-suggestions).
func parseManifestFields(manifestPath string, val *yaml.Node, defs *LibraryDefinitions) ([]Field, error) {
	if val.Kind != yaml.SequenceNode {
		return nil, libraryErrorf(manifestPath, nodeLineOr(val, 0), `key "fields": expected a sequence`)
	}
	fields := make([]Field, 0, len(val.Content))
	for _, item := range val.Content {
		// An `- include: <name>` entry is a mapping whose sole key is
		// `include`. It references a named field set, which expands to its
		// fields in place; the reference's line positions any error.
		if item.Kind == yaml.MappingNode && len(item.Content) == 2 && item.Content[0].Value == "include" {
			kl := nodeLineOr(item.Content[0], nodeLineOr(item, 0))
			name, err := scalarString(item.Content[1], manifestPath, kl, "include")
			if err != nil {
				return nil, err
			}
			def, err := resolveNamedDefinition(namedDefinitionFieldSet, name, defs, manifestPath, kl)
			if err != nil {
				return nil, err
			}
			fields = append(fields, def.([]Field)...)
			continue
		}
		f, err := parseManifestField(manifestPath, item, false, defs)
		if err != nil {
			return nil, err
		}
		fields = append(fields, *f)
	}
	return fields, nil
}

// parseManifestField decodes one field entry (or, recursively, a list
// field's `item:`). isItem marks an item descriptor: only there may a `type:`
// be the inline `record` form (`item: {type: record, fields: [...]}`), whose
// `fields:` recurse through parseManifestFields (record-item-type). A record
// item descriptor needs no `name`; a `record` anywhere else is rejected
// (record-list-item-only). defs is the owning layer's named definitions
// (parseManifest), threaded through the recursive `item:`/`fields:` decodes.
//
// An image field's path options `roots:`/`dir:`/`extensions:` decode into
// Roots/Dir/Extensions and an enum field's `variants_from:` into VariantsFrom
// (image-field-roots, enum-variants-from). Each is meaningful only on its own
// type: the path options on an image field and the variant source on an enum
// field, rejected on any other type just as max_items_from is on a non-list
// field. A `roots:` entry must name `assets` or `media`; an unrecognised entry
// is a templates error carrying a closest-match suggestion over those two.
//
// A `type:` that is not a built-in type name may instead name a type in defs
// (named-library-definitions): the named descriptor is expanded into this
// field, the referencing field's own sibling keys (description, required,
// default, …) overriding the named definition's and its Name being the
// referencing field's. A name no definition carries is a templates error
// carrying a closest-match suggestion that includes the built-in types
// (resolveNamedDefinition).
func parseManifestField(manifestPath string, node *yaml.Node, isItem bool, defs *LibraryDefinitions) (*Field, error) {
	if node.Kind != yaml.MappingNode {
		return nil, libraryErrorf(manifestPath, nodeLineOr(node, 0), "expected a mapping describing a field")
	}
	line := nodeLineOr(node, 0)
	f := &Field{}
	var haveName, haveType bool
	var typeStr string
	// present records the keys this descriptor itself declares, so that when
	// its `type:` names a definition the named descriptor can be expanded
	// underneath it: the referencing field's declared keys override the named
	// definition's, while the keys it omits keep the named definition's values
	// (named-library-definitions).
	present := make(map[string]bool, len(node.Content)/2)
	// typeLine positions a `type:` key, so an unresolvable named type is
	// reported at its reference rather than at the descriptor's first key.
	var typeLine int
	// A cross-field list bound is decoded into a local until the field's type
	// is known, so it can be rejected on a non-list field (list-only).
	var maxItemsFrom, minItemsFrom string
	var haveMaxItemsFrom, haveMinItemsFrom bool
	var maxItemsFromLine, minItemsFromLine int
	// The image path options (roots/dir/extensions) and the enum variant
	// source (variants_from) are likewise decoded with their key lines, so
	// each can be rejected on the wrong field type once the type is known
	// (image-field-roots, enum-variants-from).
	var rootsLine, dirLine, extensionsLine, variantsFromLine int
	var haveRoots, haveDir, haveExtensions, haveVariantsFrom bool

	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]
		kl := nodeLineOr(key, line)
		present[key.Value] = true

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
			typeLine = kl
		case "nullable":
			b, err := scalarBool(val, manifestPath, kl, "nullable")
			if err != nil {
				return nil, err
			}
			f.Nullable = b
		case "coerce":
			b, err := scalarBool(val, manifestPath, kl, "coerce")
			if err != nil {
				return nil, err
			}
			f.Coerce = b
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
		case "max_items_from":
			s, err := scalarString(val, manifestPath, kl, "max_items_from")
			if err != nil {
				return nil, err
			}
			maxItemsFrom = s
			maxItemsFromLine = kl
			haveMaxItemsFrom = true
		case "min_items_from":
			s, err := scalarString(val, manifestPath, kl, "min_items_from")
			if err != nil {
				return nil, err
			}
			minItemsFrom = s
			minItemsFromLine = kl
			haveMinItemsFrom = true
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
		case "variants_from":
			s, err := scalarString(val, manifestPath, kl, "variants_from")
			if err != nil {
				return nil, err
			}
			f.VariantsFrom = s
			variantsFromLine = kl
			haveVariantsFrom = true
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
		case "roots":
			ss, err := scalarStringList(val, manifestPath, kl, "roots")
			if err != nil {
				return nil, err
			}
			f.Roots = ss
			rootsLine = kl
			haveRoots = true
		case "dir":
			s, err := scalarString(val, manifestPath, kl, "dir")
			if err != nil {
				return nil, err
			}
			f.Dir = s
			dirLine = kl
			haveDir = true
		case "extensions":
			ss, err := scalarStringList(val, manifestPath, kl, "extensions")
			if err != nil {
				return nil, err
			}
			f.Extensions = ss
			extensionsLine = kl
			haveExtensions = true
		case "item":
			item, err := parseManifestField(manifestPath, val, true, defs)
			if err != nil {
				return nil, err
			}
			f.Item = item
		case "fields":
			fs, err := parseManifestFields(manifestPath, val, defs)
			if err != nil {
				return nil, err
			}
			f.Fields = fs
		default:
			e := libraryErrorf(manifestPath, kl, "unknown key %q in a field", key.Value)
			return nil, e.withSuggestion(key.Value, []string{
				"name", "type", "nullable", "coerce", "plain", "required",
				"default", "description", "max_length", "min_items", "max_items",
				"max_items_from", "min_items_from",
				"min", "max", "min_date", "max_date", "variants", "variants_from",
				"formats", "default_format", "section_template",
				"roots", "dir", "extensions",
				"item", "fields",
			})
		}
	}

	// A cross-field list bound (max_items_from/min_items_from) names another
	// list field and so is meaningful only on a list field; on any other type
	// the key is rejected, just as `fields` is rejected on a non-record
	// (cross-field-list-constraints). Likewise an image field's path options
	// (roots/dir/extensions) are meaningful only on an image field and an enum
	// field's variant source (variants_from) only on an enum field
	// (image-field-roots, enum-variants-from). A `roots:` entry, when the field
	// is an image field, must name one of the two known roots; an unrecognised
	// entry is a templates error carrying a closest-match suggestion over them.
	checkTypeOnlyKeys := func(ft FieldType, name string) error {
		if ft != FieldList {
			if haveMaxItemsFrom {
				return libraryErrorf(manifestPath, maxItemsFromLine, "field %q: key %q is only valid on a list field", name, "max_items_from")
			}
			if haveMinItemsFrom {
				return libraryErrorf(manifestPath, minItemsFromLine, "field %q: key %q is only valid on a list field", name, "min_items_from")
			}
		}
		if ft != FieldImage {
			if haveRoots {
				return libraryErrorf(manifestPath, rootsLine, "field %q: key %q is only valid on an image field", name, "roots")
			}
			if haveDir {
				return libraryErrorf(manifestPath, dirLine, "field %q: key %q is only valid on an image field", name, "dir")
			}
			if haveExtensions {
				return libraryErrorf(manifestPath, extensionsLine, "field %q: key %q is only valid on an image field", name, "extensions")
			}
		} else if haveRoots {
			for _, r := range f.Roots {
				if r == "assets" || r == "media" {
					continue
				}
				e := libraryErrorf(manifestPath, rootsLine, "field %q: unknown root %q", name, r)
				return e.withSuggestion(r, []string{"assets", "media"})
			}
		}
		if ft != FieldEnum && haveVariantsFrom {
			return libraryErrorf(manifestPath, variantsFromLine, "field %q: key %q is only valid on an enum field", name, "variants_from")
		}
		return nil
	}

	// A record item descriptor needs no `name`: it is described by its
	// `fields:` (record-item-type) — `item: {type: record, fields: [...]}`.
	// Every other descriptor (including a non-record item descriptor) still
	// requires one.
	recordItem := isItem && haveType && typeStr == string(FieldRecord)
	if (!haveName || f.Name == "") && !recordItem {
		return nil, libraryErrorf(manifestPath, line, "field: missing required key %q", "name")
	}
	if !haveType {
		return nil, libraryErrorf(manifestPath, line, "field %q: missing required key %q", f.Name, "type")
	}
	if typeStr == string(FieldRecord) {
		if !isItem {
			return nil, libraryErrorf(manifestPath, line, "field %q: type %q is only valid as a list item descriptor", f.Name, typeStr)
		}
		if f.Fields == nil {
			return nil, libraryErrorf(manifestPath, line, "field %q: type %q requires key %q", f.Name, typeStr, "fields")
		}
		if err := checkTypeOnlyKeys(FieldRecord, f.Name); err != nil {
			return nil, err
		}
		f.Type = FieldRecord
		return f, nil
	}
	if ft, ok := manifestFieldTypes[typeStr]; ok {
		if f.Fields != nil {
			return nil, libraryErrorf(manifestPath, line, "field %q: key %q is only valid on a record item descriptor", f.Name, "fields")
		}
		if err := checkTypeOnlyKeys(ft, f.Name); err != nil {
			return nil, err
		}
		f.MaxItemsFrom = maxItemsFrom
		f.MinItemsFrom = minItemsFrom
		f.Type = ft
		return f, nil
	}

	// Not a built-in type name: a field's `type: <name>` may name a type in
	// the owning layer's definitions (named-library-definitions), whose
	// descriptor is expanded into this field. resolveNamedDefinition reports a
	// name no definition carries as a templates error with a closest-match
	// suggestion over the built-in types and the declared named types.
	named, err := resolveNamedDefinition(namedDefinitionType, typeStr, defs, manifestPath, typeLine)
	if err != nil {
		return nil, err
	}
	if f.Fields != nil {
		return nil, libraryErrorf(manifestPath, line, "field %q: key %q is only valid on a record item descriptor", f.Name, "fields")
	}
	if haveMaxItemsFrom {
		f.MaxItemsFrom = maxItemsFrom
	}
	if haveMinItemsFrom {
		f.MinItemsFrom = minItemsFrom
	}
	merged := mergeNamedTypeField(named.(Field), *f, present)
	if err := checkTypeOnlyKeys(merged.Type, merged.Name); err != nil {
		return nil, err
	}
	return &merged, nil
}

// mergeNamedTypeField expands base, a named type's descriptor, into f, the
// field that referenced it as `type: <base's name>` (named-library-definitions):
// the expanded descriptor keeps the referencing field's Name, every sibling key
// the referencing field itself declared (tracked in present) overrides base's,
// and every key the referencing field omitted keeps base's value. The `type:`
// key is the reference itself, so the expanded descriptor's Type is always
// base's, never the referencing field's type name.
func mergeNamedTypeField(base, f Field, present map[string]bool) Field {
	out := base
	out.Name = f.Name
	if present["nullable"] {
		out.Nullable = f.Nullable
	}
	if present["coerce"] {
		out.Coerce = f.Coerce
	}
	if present["plain"] {
		out.Plain = f.Plain
	}
	if present["required"] {
		out.Required = f.Required
	}
	if present["default"] {
		out.Default = f.Default
	}
	if present["description"] {
		out.Description = f.Description
	}
	if present["max_length"] {
		out.MaxLength = f.MaxLength
	}
	if present["min_items"] {
		out.MinItems = f.MinItems
	}
	if present["max_items"] {
		out.MaxItems = f.MaxItems
	}
	if present["max_items_from"] {
		out.MaxItemsFrom = f.MaxItemsFrom
	}
	if present["min_items_from"] {
		out.MinItemsFrom = f.MinItemsFrom
	}
	if present["min"] {
		out.Min = f.Min
	}
	if present["max"] {
		out.Max = f.Max
	}
	if present["min_date"] {
		out.MinDate = f.MinDate
	}
	if present["max_date"] {
		out.MaxDate = f.MaxDate
	}
	if present["variants"] {
		out.Variants = f.Variants
	}
	if present["variants_from"] {
		out.VariantsFrom = f.VariantsFrom
	}
	if present["formats"] {
		out.Formats = f.Formats
	}
	if present["default_format"] {
		out.DefaultFormat = f.DefaultFormat
	}
	if present["section_template"] {
		out.SectionTemplate = f.SectionTemplate
	}
	if present["roots"] {
		out.Roots = f.Roots
	}
	if present["dir"] {
		out.Dir = f.Dir
	}
	if present["extensions"] {
		out.Extensions = f.Extensions
	}
	if present["item"] {
		out.Item = f.Item
	}
	if present["fields"] {
		out.Fields = f.Fields
	}
	return out
}

// namedDefinitionKind identifies which of the three named-definition sections
// of a library.yaml — groups, field_sets and types
// (named-library-definitions) — a bare definition name is resolved against.
type namedDefinitionKind int

const (
	// namedDefinitionGroup is a named accept-list: a section declaration
	// references it from its `accepted:` as `@<name>`, combinable with
	// literal template names.
	namedDefinitionGroup namedDefinitionKind = iota

	// namedDefinitionFieldSet is a named field group: a template references
	// it from its `fields:` as the sequence entry `- include: <name>`.
	namedDefinitionFieldSet

	// namedDefinitionType is a named field type: a field references it as
	// `type: <name>`, expanding the named descriptor into the field.
	namedDefinitionType
)

// String names the kind as a resolveNamedDefinition error reads it: "group",
// "field set" or "type".
func (k namedDefinitionKind) String() string {
	switch k {
	case namedDefinitionGroup:
		return "group"
	case namedDefinitionFieldSet:
		return "field set"
	case namedDefinitionType:
		return "type"
	default:
		return "definition"
	}
}

// resolveNamedDefinition resolves name against defs, the owning library layer's
// named definitions (LibraryLayer.Definitions), for the given kind
// (named-library-definitions). It returns the definition — a group's accept-list
// ([]string), a field set's fields ([]Field) or a named type's descriptor
// (Field) — and reports a name that no definition of that kind carries as a
// positioned templates error (a *LibraryError) carrying a closest-match
// suggestion (closest-match-suggestions).
//
// defs is the definitions of the layer that owns the referencing template; a
// nil defs holds no definitions, so every name is unknown. name is the bare
// definition name — the caller strips a reference's `@` sigil (and any
// `include:`/`type:` key) before calling — and supplies the position
// (manifestPath, line) of the reference. A type name's suggestion set is every
// valid field type, the built-in types (manifestFieldTypes) as well as the
// declared named types, so a misspelling of either is suggested.
func resolveNamedDefinition(kind namedDefinitionKind, name string, defs *LibraryDefinitions, manifestPath string, line int) (any, error) {
	switch kind {
	case namedDefinitionGroup:
		if defs != nil {
			if group, ok := defs.Groups[name]; ok {
				return group, nil
			}
		}
	case namedDefinitionFieldSet:
		if defs != nil {
			if set, ok := defs.FieldSets[name]; ok {
				return set, nil
			}
		}
	case namedDefinitionType:
		if defs != nil {
			if t, ok := defs.Types[name]; ok {
				return t, nil
			}
		}
	}
	e := libraryErrorf(manifestPath, line, "unknown %s %q", kind, name)
	return nil, e.withSuggestion(name, namedDefinitionCandidates(kind, defs))
}

// namedDefinitionCandidates returns the names a resolveNamedDefinition error
// suggests against: the names declared of the given kind, plus, for a type, the
// built-in field types. It returns nil when there are none.
func namedDefinitionCandidates(kind namedDefinitionKind, defs *LibraryDefinitions) []string {
	switch kind {
	case namedDefinitionGroup:
		if defs == nil {
			return nil
		}
		return sortedStringKeys(defs.Groups)
	case namedDefinitionFieldSet:
		if defs == nil {
			return nil
		}
		return sortedStringKeys(defs.FieldSets)
	case namedDefinitionType:
		out := sortedStringKeys(manifestFieldTypes)
		if defs != nil {
			out = append(out, sortedStringKeys(defs.Types)...)
		}
		return out
	default:
		return nil
	}
}

// parseManifestSections decodes the `sections:` sequence. defs is the owning
// layer's named definitions (parseManifest), threaded to each section parser.
func parseManifestSections(manifestPath string, val *yaml.Node, defs *LibraryDefinitions) ([]SectionDecl, error) {
	if val.Kind != yaml.SequenceNode {
		return nil, libraryErrorf(manifestPath, nodeLineOr(val, 0), `key "sections": expected a sequence`)
	}
	out := make([]SectionDecl, 0, len(val.Content))
	for _, item := range val.Content {
		d, err := parseManifestSection(manifestPath, item, defs)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, nil
}

// parseManifestSection decodes one `sections:` entry. defs is the owning
// layer's named definitions (parseManifest), threaded through for the
// section's `accepted:` entries: an `@<name>` entry resolves to the named
// accept-list (group) it references, expanding in place and combinable with
// literal section-template names, while an unknown group name is a positioned
// templates error carrying a closest-match suggestion
// (named-library-definitions, closest-match-suggestions).
//
// A `max_depth: N` key is the positive-integer bound a self-nesting/cyclic
// acceptance carries (bounded-self-nesting); a value that is not a positive
// integer is a positioned templates error naming the offending template.yaml.
func parseManifestSection(manifestPath string, node *yaml.Node, defs *LibraryDefinitions) (*SectionDecl, error) {
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
			// Each entry is a literal section-template name or an
			// `@<name>` reference to a named accept-list (group) in the
			// owning layer's definitions, which expands in place to the
			// group's template names and is combinable with literal names
			// (named-library-definitions). The sequence is walked here
			// rather than through scalarStringList so an `@<name>` entry is
			// resolved at its own line.
			if val.Kind != yaml.SequenceNode {
				return nil, libraryErrorf(manifestPath, kl, "key %q: expected a sequence of strings", "accepted")
			}
			names := make([]string, 0, len(val.Content))
			for _, item := range val.Content {
				if item.Kind != yaml.ScalarNode || item.ShortTag() != "!!str" {
					return nil, libraryErrorf(manifestPath, nodeLineOr(item, kl), "key %q: expected a sequence of strings", "accepted")
				}
				if strings.HasPrefix(item.Value, "@") {
					il := nodeLineOr(item, kl)
					def, err := resolveNamedDefinition(namedDefinitionGroup, item.Value[1:], defs, manifestPath, il)
					if err != nil {
						return nil, err
					}
					names = append(names, def.([]string)...)
					continue
				}
				names = append(names, item.Value)
			}
			d.Accepted = names
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
		case "max_depth":
			// The bound a self-nesting/cyclic acceptance carries
			// (bounded-self-nesting). It must be a positive integer; a
			// non-integer, zero or a negative value is a templates error
			// positioned at this key's line and naming the offending
			// template.yaml.
			n, err := scalarInt(val, manifestPath, kl, "max_depth")
			if err != nil || n <= 0 {
				return nil, libraryErrorf(manifestPath, kl, "key %q: expected a positive integer", "max_depth")
			}
			d.MaxDepth = n
		default:
			e := libraryErrorf(manifestPath, kl, "unknown key %q in a section", key.Value)
			return nil, e.withSuggestion(key.Value, []string{"name", "accepted", "min", "max", "max_depth"})
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

// checkCellAttributeAmbiguity rejects a decoded set of cell-attribute
// declarations whose short-hand names would not resolve by mutex
// (table-cell-attributes): a declared boolean attribute whose name is also a
// variant of a declared enum attribute, or an enum variant declared by more
// than one enum attribute, makes a bare `{name}` group ambiguous, so it is
// refused at template load. The returned error is a positioned templates
// error (*LibraryError) naming the ambiguous name and the offending
// template.yaml, or nil when every declared short-hand name resolves
// unambiguously.
//
// It inspects the whole decoded set and is order-independent. Duplicate
// declared names and the reserved name `br` are separate concerns of the
// manifest decoder that builds the declarations (parseManifestCellAttributes)
// and are not checked here.
func checkCellAttributeAmbiguity(manifestPath string, attrs []CellAttribute) error {
	// variantOwner records, for each declared enum variant, the index of the
	// first enum attribute that declared it. A variant another enum attribute
	// also declares is ambiguous: a bare `{variant}` would set either.
	variantOwner := make(map[string]int, len(attrs))
	for i, a := range attrs {
		if a.Kind != FieldEnum {
			continue
		}
		for _, v := range a.Variants {
			owner, ok := variantOwner[v]
			if !ok {
				variantOwner[v] = i
				continue
			}
			if owner != i {
				return libraryErrorf(manifestPath, 0,
					"cell attribute %q is ambiguous: it is a variant of more than one declared enum attribute",
					v)
			}
			// A repeated variant within one enum is not this check's
			// concern (duplicate declarations are parseManifestCellAttributes').
		}
	}
	// A declared boolean attribute whose name is also a declared enum variant
	// is ambiguous: a bare `{name}` could mean the boolean attribute or the
	// single enum variant that carries that name.
	for _, a := range attrs {
		if a.Kind != FieldBoolean {
			continue
		}
		if owner, ok := variantOwner[a.Name]; ok {
			return libraryErrorf(manifestPath, 0,
				"cell attribute %q is ambiguous: it is declared as a boolean attribute and as a variant of the enum attribute %q",
				a.Name, attrs[owner].Name)
		}
	}
	return nil
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
