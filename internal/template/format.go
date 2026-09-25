package template

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file implements Format, the number/date display-format catalogue: the
// built-in named format functions a template author may select, the single
// source of truth for both the schema checker (which lists the valid names) and
// the renderer (which gets the function to apply).
//
// A template author declares which named formats a number or date field accepts
// (Field.Formats) and the name used when the author writes no selection
// (Field.DefaultFormat). A deck author then selects one with the reserved
// sibling key `<field>_format`; omitting it means the field's DefaultFormat.
//
// The format functions are rendering-only: they turn an already validated Go
// value into display text and never check, default or mutate a value. Format
// exposes them to phase 6's html/template through FuncMap so a layout may also
// call them directly.

// FormatKind is the value type a named format applies to: number or date. It
// is a defined string so an unknown kind is representable.
type FormatKind string

const (
	// NumberFormat names the number display formats (compact, exact, percent).
	NumberFormat FormatKind = "number"

	// DateFormat names the date display formats (long, short).
	DateFormat FormatKind = "date"
)

// FormatFunc renders one validated value as display text. It is rendering-only:
// it never validates, defaults or mutates the value. It returns an error only
// when the value has the wrong Go kind for the format, which the schema checker
// has already rejected.
type FormatFunc func(value any) (string, error)

// Format maps named format functions to their kinds, plus the order the names
// are reported in. The zero value is not usable; build one with NewFormat. It
// is the single source of truth for resolving a field and a chosen format name
// to the function that renders it.
type Format struct {
	number map[string]FormatFunc
	date   map[string]FormatFunc
}

// FormatError reports that a chosen format name is not one of a field's
// declared formats. Valid lists the names the author may use, in declaration
// (for a field) or catalogue order.
type FormatError struct {
	// Field is the field the selection was written for.
	Field string

	// Name is the chosen format name that was rejected.
	Name string

	// Valid is the closed set of accepted names, empty when the field declares
	// no formats at all.
	Valid []string
}

// Error renders the rejection, naming the field and, when there are any, the
// valid alternatives.
func (e *FormatError) Error() string {
	if len(e.Valid) == 0 {
		return fmt.Sprintf("field %q declares no formats; %q is not available", e.Field, e.Name)
	}
	return fmt.Sprintf("unknown format %q for field %q (valid: %s)", e.Name, e.Field, strings.Join(e.Valid, ", "))
}

// NewFormat returns the built-in catalogue of named number and date display
// formats. Number formats are compact, exact and percent; date formats are long
// and short.
func NewFormat() *Format {
	return &Format{
		number: map[string]FormatFunc{
			"compact": formatCompact,
			"exact":   formatExact,
			"percent": formatPercent,
		},
		date: map[string]FormatFunc{
			"long":  formatDateLong,
			"short": formatDateShort,
		},
	}
}

// BuiltinFormats is the package's built-in format catalogue. It is the source
// of truth the schema checker and the renderer share.
var BuiltinFormats = NewFormat()

// table returns the catalogue for a kind, or nil when the kind is unknown.
func (f *Format) table(kind FormatKind) map[string]FormatFunc {
	switch kind {
	case NumberFormat:
		return f.number
	case DateFormat:
		return f.date
	}
	return nil
}

// Names returns the catalogue's format names for kind, sorted, or nil for an
// unknown kind. These are the names valid for a field whose Formats is empty.
func (f *Format) Names(kind FormatKind) []string {
	m := f.table(kind)
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Lookup returns the format function registered under name for kind.
func (f *Format) Lookup(kind FormatKind, name string) (FormatFunc, bool) {
	m := f.table(kind)
	if m == nil {
		return nil, false
	}
	fn, ok := m[name]
	return fn, ok
}

// FuncMap returns every built-in format function keyed by name, as a plain
// map[string]any suitable for html/template's Funcs. Layouts use it to render
// only; it never validates.
func (f *Format) FuncMap() map[string]any {
	out := make(map[string]any, len(f.number)+len(f.date))
	for name, fn := range f.number {
		out[name] = fn
	}
	for name, fn := range f.date {
		out[name] = fn
	}
	return out
}

// Resolve returns the rendering function for one field and the author's chosen
// format name, which is the single source of truth shared by the checker and
// the renderer.
//
// An empty chosen means the field's DefaultFormat; an empty DefaultFormat too
// means the field has no effective format and Resolve returns (nil, nil), so
// the renderer leaves the value as it is. When the effective name is not one of
// the field's declared Formats — or of the catalogue for the field's kind when
// Formats is empty — Resolve returns a *FormatError listing the valid names.
// A field whose type carries no formats rejects any selection the same way.
func (f *Format) Resolve(field *Field, chosen string) (FormatFunc, error) {
	if field == nil {
		return nil, nil
	}
	kind := formatKindOf(field.Type)
	if kind == "" {
		if chosen == "" && field.DefaultFormat == "" {
			return nil, nil
		}
		return nil, &FormatError{Field: field.Name, Name: chosen}
	}

	effective := chosen
	if effective == "" {
		effective = field.DefaultFormat
	}
	if effective == "" {
		return nil, nil
	}

	valid := field.Formats
	if len(valid) == 0 {
		valid = f.Names(kind)
	}
	if !containsString(valid, effective) {
		return nil, &FormatError{Field: field.Name, Name: effective, Valid: valid}
	}
	fn, ok := f.Lookup(kind, effective)
	if !ok {
		return nil, &FormatError{Field: field.Name, Name: effective, Valid: valid}
	}
	return fn, nil
}

// formatKindOf maps a field type to the kind of formats it carries, or "" when
// the type has none.
func formatKindOf(t FieldType) FormatKind {
	switch t {
	case FieldNumber:
		return NumberFormat
	case FieldDate:
		return DateFormat
	}
	return ""
}

// containsString reports whether s is in list.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// formatExact renders a number with comma thousands grouping and no rounding:
// 1250000 becomes "1,250,000" and 1.5 stays "1.5".
func formatExact(value any) (string, error) {
	n, ok := asFloat(value)
	if !ok {
		return "", fmt.Errorf("exact: expected a number, got %s", describeValue(value))
	}
	return groupThousands(strconv.FormatFloat(n, 'f', -1, 64)), nil
}

// formatCompact renders a number with a one-letter magnitude suffix and up to
// two decimals, trailing zeros trimmed: 1250000 becomes "1.25M". Values below
// one thousand are rendered in full.
func formatCompact(value any) (string, error) {
	n, ok := asFloat(value)
	if !ok {
		return "", fmt.Errorf("compact: expected a number, got %s", describeValue(value))
	}
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	units := []struct {
		limit  float64
		suffix string
	}{
		{1e12, "T"},
		{1e9, "B"},
		{1e6, "M"},
		{1e3, "K"},
	}
	for _, u := range units {
		if n >= u.limit {
			return sign + trimNumber(n/u.limit) + u.suffix, nil
		}
	}
	return sign + strconv.FormatFloat(n, 'f', -1, 64), nil
}

// formatPercent renders a ratio as a percentage by scaling by 100: 1.25 becomes
// "125%" and 0.075 becomes "7.5%".
func formatPercent(value any) (string, error) {
	n, ok := asFloat(value)
	if !ok {
		return "", fmt.Errorf("percent: expected a number, got %s", describeValue(value))
	}
	return trimNumber(n*100) + "%", nil
}

// formatDateLong renders a date as "25 September 2026".
func formatDateLong(value any) (string, error) {
	t, err := asDate(value)
	if err != nil {
		return "", fmt.Errorf("long: %w", err)
	}
	return t.Format("2 January 2006"), nil
}

// formatDateShort renders a date as "25 Sep 2026".
func formatDateShort(value any) (string, error) {
	t, err := asDate(value)
	if err != nil {
		return "", fmt.Errorf("short: %w", err)
	}
	return t.Format("2 Jan 2006"), nil
}

// asDate returns value as a time.Time, accepting a time.Time or a YYYY-MM-DD
// string. The schema checker has already rejected any other form, so this only
// guards against a wrong Go kind.
func asDate(value any) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case string:
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return time.Time{}, fmt.Errorf("expected a YYYY-MM-DD date, got %q", v)
		}
		return t, nil
	default:
		return time.Time{}, fmt.Errorf("expected a date, got %s", describeValue(value))
	}
}

// trimNumber renders a float with at most two decimals, trimming trailing
// zeros: 1.250 → "1.25", 1.0 → "1", 125.0 → "125".
func trimNumber(n float64) string {
	s := strconv.FormatFloat(n, 'f', 2, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

// groupThousands inserts comma thousands separators into the integer part of a
// decimal string, preserving a leading sign and any fractional part.
func groupThousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	n := len(intPart)
	for i := 0; i < n; i++ {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(intPart[i])
	}
	b.WriteString(frac)
	return b.String()
}
