package template

import (
	"fmt"

	"github.com/really-knows-ai/ey-present/internal/assets"
)

// This file declares the compiled-in built-in templates (Builtins): the minimal
// title/content/column set. Their Go schema — names, one-line descriptions,
// usages, fields, sections, body rules and enum/number/date formats — is
// authored here, while their html/template layouts and example slide Markdown
// live in the internal/assets Templates sub-tree. internal/template imports
// internal/assets, never the reverse.
//
// Each Template also carries its example slide as structured data
// (Example.Frontmatter and Example.Sections), authored here to match the
// example Markdown exactly. Register loads the layout and Markdown source from
// the assets FS; Validate then checks the whole registry, including that
// structured data, as a build-time failure. Parsing the example Markdown end to
// end is phase 5 (validate.ValidateBuiltinExamples).
//
// Field names, types, rules, formats and example values mirror
// internal/assets/templates/README.md precisely; that file is the reference and
// is not duplicated here.

// Builtins returns the registry of compiled-in built-in templates, ready for
// lookup by the slide parser and the schema engine. It builds a Registry over
// assets.Templates() (the embedded layout and example Markdown sub-tree),
// registers the title, content and column templates with their structured
// example data, and runs the registry's build-time checks.
//
// A failure is a compiled-in programming error — a schema, composition, format
// or structured-example contradiction — never a deck-author error, so it is
// returned for the caller to surface at startup rather than swallowed.
func Builtins() (*Registry, error) {
	r := NewRegistry(assets.Templates())
	for _, t := range builtinTemplates() {
		if err := r.Register(t); err != nil {
			return nil, fmt.Errorf("template: built-in %q: %w", t.Name, err)
		}
	}
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("template: built-ins: %w", err)
	}
	return r, nil
}

// builtinTemplates returns the three minimal built-in template definitions in
// declaration order. The structured examples are hand-written to match the
// example Markdown under internal/assets/templates/<name>/example.md; the
// `template:` selector keys are not field values and so are not part of the
// structured frontmatter, and the reserved notes section is excluded from
// Example.Sections.
func builtinTemplates() []*Template {
	zero := 0.0

	return []*Template{
		{
			Name:        "title",
			Description: "Title slide: the deck title with an optional subtitle and date.",
			Usage:       UsageSlide,
			Fields: []Field{
				{
					Name:        "title",
					Type:        FieldText,
					Required:    true,
					MaxLength:   80,
					Description: "Deck title.",
				},
				{
					Name:        "subtitle",
					Type:        FieldText,
					MaxLength:   140,
					Description: "Supporting line under the title.",
				},
				{
					Name:          "date",
					Type:          FieldDate,
					Formats:       []string{"long", "short"},
					DefaultFormat: "long",
					Description:   "Date under the subtitle.",
				},
			},
			Body: BodyRule{Mode: BodyDisallowed},
			Example: Example{
				Frontmatter: map[string]any{
					"title":       "Quarterly Business Review",
					"subtitle":    "Performance, outlook and priorities",
					"date":        "2026-09-25",
					"date_format": "long",
				},
			},
		},
		{
			Name:        "content",
			Description: "Content slide: a heading, an optional headline metric and date, and composed columns.",
			Usage:       UsageSlide,
			Fields: []Field{
				{
					Name:        "heading",
					Type:        FieldText,
					Required:    true,
					MaxLength:   80,
					Description: "Slide heading.",
				},
				{
					Name:        "layout",
					Type:        FieldEnum,
					Variants:    []string{"default", "columns"},
					Default:     "default",
					Description: "Layout variant.",
				},
				{
					Name:          "metric",
					Type:          FieldNumber,
					Min:           &zero,
					Formats:       []string{"compact", "exact", "percent"},
					DefaultFormat: "compact",
					Description:   "Headline number.",
				},
				{
					Name:        "show_metric",
					Type:        FieldBoolean,
					Default:     false,
					Description: "Whether to show `metric`.",
				},
				{
					Name:          "as_of",
					Type:          FieldDate,
					Formats:       []string{"long", "short"},
					DefaultFormat: "long",
					Description:   "Date the numbers are as of.",
				},
			},
			Sections: []SectionDecl{
				{
					Name:     "columns",
					Accepted: []string{"column"},
					Min:      2,
					Max:      4,
				},
			},
			Body: BodyRule{Mode: BodyOptional, MaxParagraphs: 2},
			Example: Example{
				Frontmatter: map[string]any{
					"heading":       "Where the growth is coming from",
					"layout":        "columns",
					"metric":        1250000,
					"metric_format": "compact",
					"show_metric":   true,
					"as_of":         "2026-09-25",
					"as_of_format":  "long",
				},
				Sections: []ExampleSection{
					{
						Name:        "columns",
						Template:    "column",
						Frontmatter: map[string]any{"title": "Revenue"},
						Body:        "Recurring revenue grew 18% year over year.",
					},
					{
						Name:        "columns",
						Template:    "column",
						Frontmatter: map[string]any{"title": "New customers"},
						Body:        "We added 1,250 new logos in the quarter.",
					},
				},
			},
		},
		{
			Name:        "column",
			Description: "Column section: an optional title and body inside a content slide's columns.",
			Usage:       UsageSection,
			Fields: []Field{
				{
					Name:        "title",
					Type:        FieldText,
					MaxLength:   60,
					Description: "Column heading.",
				},
			},
			Body: BodyRule{Mode: BodyOptional},
			Example: Example{
				Frontmatter: map[string]any{"title": "Revenue"},
			},
		},
	}
}
