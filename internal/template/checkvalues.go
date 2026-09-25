package template

import (
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/really-knows-ai/ey-present/internal/mdcheck"
	"github.com/really-knows-ai/ey-present/internal/suggest"
)

// This file implements CheckValues, the schema value-checking engine: it
// validates one map of YAML frontmatter or section data against one compiled
// Template's field schema, reporting positioned, structured ValueErrors and
// exposing the inline links it finds for the phase-5 link pass.
//
// CheckValues is deliberately schema-only. It never resolves section
// composition or repeats (that is Section/Registry), never applies a Field's
// Default, never selects a `<field>_format` format (that is Format) and never
// resolves a `#label` link (that is the phase-5 validator). It uses
// internal/mdcheck for inline text rules and internal/suggest for
// "did you mean …?" hints.

// PathSegment is one step in a value's location: a field or section name,
// optionally indexed because the value is a list element or a section
// instance. Index is meaningful only when HasIndex is set, so an unindexed
// field is distinct from index 0.
type PathSegment struct {
	// Name is the field or section name at this step.
	Name string

	// Index is the zero-based element/instance index, meaningful only when
	// HasIndex is set.
	Index int

	// HasIndex says whether Index applies.
	HasIndex bool
}

// PathString renders a value path the way the phase-5 formatter does, joining
// segments with " › " and appending "[i]" to indexed ones, for example
// `column[1] › people[0] › name`.
func PathString(path []PathSegment) string {
	parts := make([]string, len(path))
	for i, seg := range path {
		if seg.HasIndex {
			parts[i] = seg.Name + "[" + strconv.Itoa(seg.Index) + "]"
			continue
		}
		parts[i] = seg.Name
	}
	return strings.Join(parts, " › ")
}

// ValueError is one positioned schema violation.
//
// It is the single error shape CheckValues produces. The phase-5 validator
// adapts it into its own ValidationError, carrying Path and Line across and
// mapping What to its "what" and Fix to its "fix".
type ValueError struct {
	// Path locates the offending value within the data, for example
	// `column[1] › people[0] › name`.
	Path []PathSegment

	// Line is the 1-based line of the value, or 0 when the caller passed no
	// position source.
	Line int

	// Rule names the violated rule: "type", "required", "date", "enum",
	// "image", "link", "max_length", "min_items", "max_items", "min", "max",
	// "unknown-field", "section-template", or a mdcheck kind for inline text
	// errors.
	Rule string

	// Value is the actual offending Go value (a string, number, bool, list,
	// map or nil), so "state the rule and the actual value" is always
	// possible.
	Value any

	// What is the author-facing "what is wrong" text, including the rule and
	// the actual value.
	What string

	// Fix is the author-facing "how to fix it" text. It is empty when there
	// is nothing useful to add.
	Fix string
}

// PathString renders the error's path.
func (e ValueError) PathString() string { return PathString(e.Path) }

// Error renders the violation as a positioned message. The final author-facing
// format belongs to the phase-5 formatter; this is a convenience for tests,
// logs and callers that want a single string.
func (e ValueError) Error() string {
	what := e.What
	if e.Fix != "" {
		what += " — " + e.Fix
	}
	if p := e.PathString(); p != "" {
		if e.Line > 0 {
			return fmt.Sprintf("%d › %s: %s", e.Line, p, what)
		}
		return fmt.Sprintf("%s: %s", p, what)
	}
	if e.Line > 0 {
		return fmt.Sprintf("%d: %s", e.Line, what)
	}
	return what
}

// LinkRef is one `#label` or http(s) link found while checking, exposed so the
// phase-5 link pass can resolve `#label` targets once every label in the deck
// is known. CheckValues never resolves them itself.
type LinkRef struct {
	// Path locates the field the link was written in.
	Path []PathSegment

	// Line is the 1-based line of the link, or 0 when unknown.
	Line int

	// Text is the link's visible label text, flattened to plain text.
	Text string

	// Destination is the link destination as written.
	Destination string

	// Label is the `#label` target without the leading '#', empty for an
	// http(s) link.
	Label string
}

// Result is the outcome of CheckValues: the positioned violations, plus the
// links collected for the phase-5 link pass.
type Result struct {
	// Errors holds every schema violation found, in declaration then source
	// order.
	Errors []ValueError

	// Links holds every inline link found in non-plain text fields and every
	// `#label` target of a link field, in the same order.
	Links []LinkRef
}

// checkConfig holds the optional callbacks an Option supplies.
type checkConfig struct {
	lineFor     func(path []PathSegment) int
	imageExists func(path string) bool
}

// Option configures a CheckValues call.
type Option func(*checkConfig)

// WithLineSource supplies the 1-based line for a value path, or 0 when
// unknown. CheckValues attaches that line to every ValueError it reports. When
// no source is passed, errors carry line 0.
func WithLineSource(fn func(path []PathSegment) int) Option {
	return func(c *checkConfig) { c.lineFor = fn }
}

// WithImageExists supplies the existence check for image fields. It receives
// the path exactly as the author wrote it (always already under assets/). When
// no callback is passed, CheckValues still enforces the assets/ prefix but
// does not check that the file exists.
func WithImageExists(fn func(path string) bool) Option {
	return func(c *checkConfig) { c.imageExists = fn }
}

// CheckValues validates one map of YAML frontmatter or section data against
// tmpl's field schema and returns every violation plus the inline links it
// found.
//
// resolve looks up a section template by name for a FieldSectionTemplate
// field (or a list item of that type); it is called at CheckValues time so no
// dependency on the registry is needed. A nil resolve, or one that reports
// false, yields a section-template error.
//
// The check is recursive and uniform at every depth: a section-template value
// is checked against the nested template's fields, a list's elements against
// its single item type, and the same type/rule/unknown-field rules apply
// everywhere. Checks are deterministic: unknown keys are reported in sorted
// order and declared fields in declaration order.
func CheckValues(data map[string]any, tmpl *Template, resolve func(name string) (*Template, bool), opts ...Option) Result {
	var cfg checkConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if tmpl == nil {
		return Result{}
	}
	c := &checker{cfg: cfg, resolve: resolve}
	c.mapValues(nil, tmpl, data, 0)
	return Result{Errors: c.errors, Links: c.links}
}

// maxDepth bounds recursion so a malformed cyclic schema cannot loop forever;
// reference cycles are rejected at build time, so this is defensive only.
const maxDepth = 64

type checker struct {
	cfg     checkConfig
	resolve func(name string) (*Template, bool)
	errors  []ValueError
	links   []LinkRef
}

// mapValues checks one mapping against tmpl's declared fields: unknown keys
// first (sorted, deterministic), then each declared field in declaration
// order. It recurses into section-template values.
func (c *checker) mapValues(path []PathSegment, tmpl *Template, data map[string]any, depth int) {
	if depth > maxDepth {
		c.add(path, "section-template", data, "section-template nesting is too deep", "check the template for a reference cycle")
		return
	}

	declared := make(map[string]struct{}, len(tmpl.Fields))
	fieldsByName := make(map[string]*Field, len(tmpl.Fields))
	for i := range tmpl.Fields {
		declared[tmpl.Fields[i].Name] = struct{}{}
		fieldsByName[tmpl.Fields[i].Name] = &tmpl.Fields[i]
	}

	// The reserved `<field>_format` sibling of a declared field is owned by
	// Format: CheckValues verifies only that its selection names one of the
	// field's valid formats (Format.Resolve is the single source of truth).
	var unknown, formatKeys []string
	for key := range data {
		if _, ok := declared[key]; ok {
			continue
		}
		if _, ok := formatBase(key, declared); ok {
			formatKeys = append(formatKeys, key)
			continue
		}
		unknown = append(unknown, key)
	}
	sort.Strings(unknown)
	sort.Strings(formatKeys)

	candidates := declaredCandidates(tmpl)
	for _, key := range unknown {
		c.unknownKey(path, key, data[key], candidates)
	}
	for _, key := range formatKeys {
		base, _ := formatBase(key, declared)
		c.checkFormatKey(path, key, fieldsByName[base], data[key])
	}

	for i := range tmpl.Fields {
		f := &tmpl.Fields[i]
		value, present := data[f.Name]
		fieldPath := appendSeg(path, PathSegment{Name: f.Name})
		if !present {
			if f.Required {
				c.add(fieldPath, "required", nil,
					fmt.Sprintf("required: field %q is required but missing", f.Name),
					fmt.Sprintf("add a %s: value", f.Name))
			}
			continue
		}
		c.validateValue(fieldPath, f, value, depth)
	}
}

// checkFormatKey verifies a reserved `<field>_format` selection against the
// field's declared formats, using Format.Resolve so the checker and renderer
// share one source of truth. An unknown name is reported with the valid names;
// a selection on a field that carries no formats is an error too.
func (c *checker) checkFormatKey(path []PathSegment, key string, f *Field, value any) {
	if f == nil {
		return
	}
	chosen, ok := value.(string)
	if !ok {
		c.add(formatPath(path, f.Name), "format", value,
			fmt.Sprintf("type: expected a format name, got %s", describeValue(value)),
			"name one of the field's formats with a string")
		return
	}
	if _, err := BuiltinFormats.Resolve(f, chosen); err != nil {
		valid := f.Formats
		if len(valid) == 0 {
			valid = BuiltinFormats.Names(formatKindOf(f.Type))
		}
		fix := "remove the key or check the template definition"
		if len(valid) > 0 {
			fix = "use one of: " + strings.Join(valid, ", ")
		}
		c.add(formatPath(path, f.Name), "format", value, err.Error(), fix)
	}
}

// formatPath locates a `<field>_format` key within the data.
func formatPath(path []PathSegment, field string) []PathSegment {
	return appendSeg(path, PathSegment{Name: field + "_format"})
}

// validateValue checks one value against one field's type and rules.
func (c *checker) validateValue(path []PathSegment, f *Field, value any, depth int) {
	switch f.Type {
	case FieldText:
		c.validateText(path, f, value)
	case FieldNumber:
		c.validateNumber(path, f, value)
	case FieldDate:
		c.validateDate(path, value)
	case FieldBoolean:
		c.validateBoolean(path, value)
	case FieldEnum:
		c.validateEnum(path, f, value)
	case FieldImage:
		c.validateImage(path, value)
	case FieldLink:
		c.validateLink(path, value)
	case FieldList:
		c.validateList(path, f, value, depth)
	case FieldSectionTemplate:
		c.validateSection(path, f, value, depth)
	default:
		c.add(path, "type", value,
			fmt.Sprintf("type: unknown field type %q", string(f.Type)),
			"fix the template definition")
	}
}

// validateText checks a text field: the inline Markdown rules (unless Plain),
// the collected links, and MaxLength after Markdown is stripped.
func (c *checker) validateText(path []PathSegment, f *Field, value any) {
	s, ok := value.(string)
	if !ok {
		c.add(path, "type", value,
			fmt.Sprintf("type: expected a text field, got %s", describeValue(value)),
			"quote the value as a string")
		return
	}

	if !f.Plain {
		start := c.line(path)
		if start < 1 {
			start = 1
		}
		opts := mdcheck.Options{Mode: mdcheck.InlineMode, StartLine: start}

		for _, issue := range mdcheck.Check("", []byte(s), opts) {
			c.addLine(issueLine(issue.Line, c.line(path)), path, string(issue.Kind), s, issue.Message, issue.Guidance)
		}
		links, issues := mdcheck.ExtractLinks("", []byte(s), opts)
		for _, issue := range issues {
			c.addLine(issueLine(issue.Line, c.line(path)), path, string(issue.Kind), s, issue.Message, issue.Guidance)
		}
		for _, link := range links {
			c.links = append(c.links, LinkRef{
				Path:        clonePath(path),
				Line:        link.Line,
				Text:        link.Text,
				Destination: link.Destination,
				Label:       link.Label,
			})
		}
	}

	if f.MaxLength > 0 {
		if n := utf8.RuneCountInString(stripMarkdown(s)); n > f.MaxLength {
			c.add(path, "max_length", s,
				fmt.Sprintf("max_length: text is %d characters, maximum is %d", n, f.MaxLength),
				"shorten the value")
		}
	}
}

// validateNumber checks a plain YAML number and its inclusive Min/Max bounds.
// A quoted/string value is the type error the field-types spec calls out.
func (c *checker) validateNumber(path []PathSegment, f *Field, value any) {
	n, ok := asFloat(value)
	if !ok {
		what := fmt.Sprintf("type: expected a number, got %s", describeValue(value))
		if _, isString := value.(string); isString {
			what = fmt.Sprintf("type: expected a number, got the quoted value %s", strconv.Quote(value.(string)))
		}
		c.add(path, "type", value, what, "write the number without quotes")
		return
	}
	if f.Min != nil && n < *f.Min {
		c.add(path, "min", value,
			fmt.Sprintf("min: %s is below the minimum %s", formatNumber(n), formatNumber(*f.Min)),
			fmt.Sprintf("use a value of at least %s", formatNumber(*f.Min)))
	}
	if f.Max != nil && n > *f.Max {
		c.add(path, "max", value,
			fmt.Sprintf("max: %s exceeds the maximum %s", formatNumber(n), formatNumber(*f.Max)),
			fmt.Sprintf("use a value of at most %s", formatNumber(*f.Max)))
	}
}

// validateDate checks that a date is a YYYY-MM-DD value.
func (c *checker) validateDate(path []PathSegment, value any) {
	switch d := value.(type) {
	case time.Time:
		return
	case string:
		if isISODate(d) {
			return
		}
		c.add(path, "date", value,
			fmt.Sprintf("date: %s is not a YYYY-MM-DD date", strconv.Quote(d)),
			"write the date as YYYY-MM-DD, for example 2024-01-02")
		return
	default:
		c.add(path, "type", value,
			fmt.Sprintf("type: expected a YYYY-MM-DD date, got %s", describeValue(value)),
			"write the date as YYYY-MM-DD")
	}
}

// validateBoolean checks a plain YAML boolean.
func (c *checker) validateBoolean(path []PathSegment, value any) {
	if _, ok := value.(bool); ok {
		return
	}
	c.add(path, "type", value,
		fmt.Sprintf("type: expected a boolean, got %s", describeValue(value)),
		"write true or false without quotes")
}

// validateEnum checks that an enum value is one of the field's variants.
func (c *checker) validateEnum(path []PathSegment, f *Field, value any) {
	s, ok := value.(string)
	if !ok {
		c.add(path, "type", value,
			fmt.Sprintf("type: expected one of the enum variants, got %s", describeValue(value)),
			"use one of: "+strings.Join(f.Variants, ", "))
		return
	}
	for _, v := range f.Variants {
		if s == v {
			return
		}
	}
	c.add(path, "enum", value,
		fmt.Sprintf("enum: %s is not one of the allowed variants (%s)", strconv.Quote(s), strings.Join(f.Variants, ", ")),
		"use one of: "+strings.Join(f.Variants, ", "))
}

// validateImage checks that an image path is under assets/ and, when the
// caller supplied an existence callback, that it exists.
func (c *checker) validateImage(path []PathSegment, value any) {
	s, ok := value.(string)
	if !ok {
		c.add(path, "type", value,
			fmt.Sprintf("type: expected an image path, got %s", describeValue(value)),
			"write the path as a string")
		return
	}
	if !strings.HasPrefix(s, "assets/") {
		c.add(path, "image", value,
			fmt.Sprintf("image: %s is not under assets/", strconv.Quote(s)),
			"move the image into the deck's assets/ directory and reference it as assets/…")
		return
	}
	if c.cfg.imageExists != nil && !c.cfg.imageExists(s) {
		c.add(path, "image", value,
			fmt.Sprintf("image: %s does not exist", strconv.Quote(s)),
			"add the file under assets/ or fix the path")
	}
}

// validateLink checks that a link field is a `#label` anchor or an http(s)
// URL, and exposes a `#label` target for the phase-5 link pass.
func (c *checker) validateLink(path []PathSegment, value any) {
	s, ok := value.(string)
	if !ok {
		c.add(path, "type", value,
			fmt.Sprintf("type: expected a link, got %s", describeValue(value)),
			"write the link as a string")
		return
	}
	if strings.HasPrefix(s, "#") {
		label := strings.TrimPrefix(s, "#")
		if label == "" {
			c.add(path, "link", value,
				`link: "#" has an empty label`,
				"use a non-empty #label, for example #summary")
			return
		}
		c.links = append(c.links, LinkRef{
			Path:        clonePath(path),
			Destination: s,
			Label:       label,
		})
		return
	}
	if isHTTPDestination(s) {
		return
	}
	c.add(path, "link", value,
		fmt.Sprintf("link: %s is neither a #label anchor nor an http(s) URL", strconv.Quote(s)),
		"use a #label anchor or an http(s) URL")
}

// validateList checks a list's size bounds and every element against the
// field's single item type.
func (c *checker) validateList(path []PathSegment, f *Field, value any, depth int) {
	items, ok := asSlice(value)
	if !ok {
		c.add(path, "type", value,
			fmt.Sprintf("type: expected a list, got %s", describeValue(value)),
			"write the value as a YAML sequence")
		return
	}
	if f.MinItems > 0 && len(items) < f.MinItems {
		c.add(path, "min_items", value,
			fmt.Sprintf("min_items: %d item(s) is below the minimum %d", len(items), f.MinItems),
			fmt.Sprintf("add at least %d item(s)", f.MinItems))
	}
	if f.MaxItems > 0 && len(items) > f.MaxItems {
		c.add(path, "max_items", value,
			fmt.Sprintf("max_items: %d item(s) exceeds the maximum %d", len(items), f.MaxItems),
			fmt.Sprintf("remove items until at most %d remain", f.MaxItems))
	}
	if f.Item == nil {
		return
	}
	for i, elem := range items {
		itemPath := appendSeg(path, PathSegment{Index: i, HasIndex: true})
		c.validateValue(itemPath, f.Item, elem, depth)
	}
}

// validateSection checks a section-template-as-type value against the nested
// template's field schema.
func (c *checker) validateSection(path []PathSegment, f *Field, value any, depth int) {
	m, ok := asMap(value)
	if !ok {
		c.add(path, "type", value,
			fmt.Sprintf("type: expected a mapping for section template %s, got %s", strconv.Quote(f.SectionTemplate), describeValue(value)),
			"write the section data as YAML key: value pairs")
		return
	}
	if f.SectionTemplate == "" {
		c.add(path, "section-template", value,
			"section-template: field declares no section template",
			"fix the template definition")
		return
	}
	if c.resolve == nil {
		c.add(path, "section-template", value,
			fmt.Sprintf("section-template: cannot resolve %s", strconv.Quote(f.SectionTemplate)),
			"pass a section template resolver to CheckValues")
		return
	}
	nested, found := c.resolve(f.SectionTemplate)
	if !found || nested == nil {
		c.add(path, "section-template", value,
			fmt.Sprintf("section-template: unknown section template %s", strconv.Quote(f.SectionTemplate)),
			"check the template name against the built-in templates")
		return
	}
	c.mapValues(path, nested, m, depth+1)
}

// unknownKey reports an unknown key, with guidance for the reserved effect and
// body keys and a closest-match suggestion against the template's declared
// field names and their `_format` siblings.
func (c *checker) unknownKey(path []PathSegment, key string, value any, candidates []string) {
	p := appendSeg(path, PathSegment{Name: key})

	switch key {
	case "body":
		c.add(p, "unknown-field", value,
			`unknown field "body"`,
			"body content comes from the Markdown after the frontmatter, not a body: key; body rules are declared in the template")
		return
	case "transition", "background", "fragment":
		c.add(p, "unknown-field", value,
			fmt.Sprintf("unknown field %q", key),
			fmt.Sprintf("fragments, transitions and backgrounds are set in the template layout, not in content; remove %q", key))
		return
	}

	what := fmt.Sprintf("unknown field %q", key)
	fix := fmt.Sprintf("remove %q or use one of the template's declared fields", key)
	if closest := suggest.Closest(key, candidates); closest != "" {
		fix = fmt.Sprintf("did you mean %q?", closest)
	}
	c.add(p, "unknown-field", value, what, fix)
}

// add records an error with the caller-supplied line source.
func (c *checker) add(path []PathSegment, rule string, value any, what, fix string) {
	c.addLine(c.line(path), path, rule, value, what, fix)
}

// addLine records an error at an explicit line.
func (c *checker) addLine(line int, path []PathSegment, rule string, value any, what, fix string) {
	c.errors = append(c.errors, ValueError{
		Path:  clonePath(path),
		Line:  line,
		Rule:  rule,
		Value: value,
		What:  what,
		Fix:   fix,
	})
}

// line returns the line for path from the configured source, or 0.
func (c *checker) line(path []PathSegment) int {
	if c.cfg.lineFor == nil {
		return 0
	}
	return c.cfg.lineFor(path)
}

// declaredCandidates is the suggestion set for a template: every declared field
// name plus its reserved `<field>_format` sibling.
func declaredCandidates(t *Template) []string {
	out := make([]string, 0, len(t.Fields)*2)
	for i := range t.Fields {
		out = append(out, t.Fields[i].Name, t.Fields[i].Name+"_format")
	}
	return out
}

// formatBase reports whether key is the reserved `<field>_format` sibling of a
// declared field, which Format owns, and returns that field's name.
func formatBase(key string, declared map[string]struct{}) (string, bool) {
	if !strings.HasSuffix(key, "_format") {
		return "", false
	}
	base := strings.TrimSuffix(key, "_format")
	if _, ok := declared[base]; !ok {
		return "", false
	}
	return base, true
}

// appendSeg returns a new path with seg appended, so paths never share backing
// storage.
func appendSeg(path []PathSegment, seg PathSegment) []PathSegment {
	out := make([]PathSegment, len(path)+1)
	copy(out, path)
	out[len(path)] = seg
	return out
}

// clonePath copies path.
func clonePath(path []PathSegment) []PathSegment {
	if path == nil {
		return nil
	}
	out := make([]PathSegment, len(path))
	copy(out, path)
	return out
}

// issueLine prefers a mdcheck-reported line and falls back to the field line.
func issueLine(issueLine, fieldLine int) int {
	if issueLine > 0 {
		return issueLine
	}
	return fieldLine
}

// stripMarkdown returns src's visible text with all inline Markdown styles
// removed, using the same CommonMark core mdcheck parses with. It is used only
// to measure a text field's MaxLength in characters.
func stripMarkdown(src string) string {
	md := goldmark.New()
	doc := md.Parser().Parse(text.NewReader([]byte(src)))
	var b strings.Builder
	appendStripped(doc, []byte(src), &b)
	return b.String()
}

// appendStripped concatenates the text content of n and its descendants.
func appendStripped(n ast.Node, src []byte, b *strings.Builder) {
	switch t := n.(type) {
	case *ast.Text:
		b.Write(t.Segment.Value(src))
	case *ast.String:
		b.Write(t.Value)
	}
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		appendStripped(child, src, b)
	}
}

// isISODate reports whether s is a strict YYYY-MM-DD calendar date.
func isISODate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == 4 || i == 7 {
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// isHTTPDestination reports whether s is an http or https URL.
func isHTTPDestination(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	}
	return false
}

// asFloat returns v as a float64 for any Go numeric kind.
func asFloat(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return 0, false
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// asSlice returns v as a []any for any slice or array kind.
func asSlice(v any) ([]any, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil, false
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	if rv.Kind() == reflect.Slice && rv.IsNil() {
		return []any{}, true
	}
	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// asMap returns v as a map[string]any, accepting both the string-keyed and the
// interface-keyed maps YAML decoders produce.
func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = val
		}
		return out, true
	}
	return nil, false
}

// describeValue names the kind of an actual value for a type error.
func describeValue(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "a quoted string"
	case bool:
		return "a boolean"
	case time.Time:
		return "a date"
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		return "a list"
	case reflect.Map:
		return "a mapping"
	}
	return fmt.Sprintf("%T", v)
}

// formatNumber renders an actual or bound number without a trailing ".0" for
// whole values.
func formatNumber(n float64) string {
	if n == float64(int64(n)) {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'g', -1, 64)
}
