package template

import (
	"strconv"
	"strings"
)

// This file holds the shared template schema-DOCUMENTATION vocabulary: the
// helpers that render a template's schema rules as the human-readable strings
// both `kalide templates <name>` (internal/cli, indented text) and the
// /templates gallery (internal/server, HTML tables) print. Each surface keeps
// its own presentation and layout; only the rendered vocabulary is shared, so
// the CLI's text and the gallery's tables cannot drift from each other or from
// the loaded schema.

// FieldLimits renders the limits that apply to a field's type: text length and
// plainness, inclusive number bounds, inclusive date bounds or list item
// bounds. It returns "" when no limit applies.
//
// An enum field's closed set of variants is deliberately not rendered here:
// each surface presents the variants in its own form — the CLI prints them on a
// separate line beneath the field (printTemplateEnumVariants), the gallery in
// its own table cell — so FieldLimits returns "" for an enum field and the
// CLI's variants line stays the only place the CLI prints them.
func FieldLimits(f *Field) string {
	var parts []string
	switch f.Type {
	case FieldText:
		if f.MaxLength > 0 {
			parts = append(parts, "max "+strconv.Itoa(f.MaxLength)+" chars")
		}
		if f.Plain {
			parts = append(parts, "plain")
		}
	case FieldNumber:
		parts = append(parts, numberBounds(f.Min, f.Max)...)
	case FieldDate:
		parts = append(parts, dateBounds(f.MinDate, f.MaxDate)...)
	case FieldList:
		parts = append(parts, listBounds(f.MinItems, f.MaxItems)...)
	}
	return strings.Join(parts, ", ")
}

// FieldFormats renders a field's named formats and its default format, for
// example "compact, exact, percent (default compact)". It returns "" when the
// field takes no format.
func FieldFormats(f *Field) string {
	if len(f.Formats) == 0 && f.DefaultFormat == "" {
		return ""
	}
	out := strings.Join(f.Formats, ", ")
	if f.DefaultFormat != "" {
		if out != "" {
			out += " (default " + f.DefaultFormat + ")"
		} else {
			out = "default " + f.DefaultFormat
		}
	}
	return out
}

// SectionRepeats renders a section declaration's repeat bounds: "n–m", "≥ n"
// or "≤ n". It returns "" when the section is unrestricted, which each surface
// renders in its own form: `kalide templates` prints "any" and the gallery
// passes the "" through to the page template's "any" fallback.
func SectionRepeats(d *SectionDecl) string {
	switch {
	case d.Min > 0 && d.Max > 0:
		return strconv.Itoa(d.Min) + "–" + strconv.Itoa(d.Max)
	case d.Min > 0:
		return "≥ " + strconv.Itoa(d.Min)
	case d.Max > 0:
		return "≤ " + strconv.Itoa(d.Max)
	default:
		return ""
	}
}

// numberBounds renders inclusive number bounds: "≥ n", "≤ n" or "n–m".
func numberBounds(min, max *float64) []string {
	switch {
	case min != nil && max != nil:
		return []string{boundText(*min) + "–" + boundText(*max)}
	case min != nil:
		return []string{"≥ " + boundText(*min)}
	case max != nil:
		return []string{"≤ " + boundText(*max)}
	default:
		return nil
	}
}

// dateBounds renders inclusive date bounds: "from YYYY-MM-DD", "to YYYY-MM-DD"
// or "YYYY-MM-DD–YYYY-MM-DD".
func dateBounds(min, max string) []string {
	switch {
	case min != "" && max != "":
		return []string{min + "–" + max}
	case min != "":
		return []string{"from " + min}
	case max != "":
		return []string{"to " + max}
	default:
		return nil
	}
}

// listBounds renders list item bounds: "n–m items", "≥ n items" or
// "≤ m items". A zero minimum and a non-positive maximum mean unbounded.
func listBounds(min, max int) []string {
	switch {
	case min > 0 && max > 0:
		return []string{strconv.Itoa(min) + "–" + strconv.Itoa(max) + " items"}
	case min > 0:
		return []string{"≥ " + strconv.Itoa(min) + " items"}
	case max > 0:
		return []string{"≤ " + strconv.Itoa(max) + " items"}
	default:
		return nil
	}
}

// boundText renders a numeric bound without a trailing ".0": 0 becomes "0".
func boundText(n float64) string {
	if n == float64(int64(n)) {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}
