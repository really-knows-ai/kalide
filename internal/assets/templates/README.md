# Built-in template content

This directory is the `Templates` sub-tree embedded by `internal/assets`
(`assets.Templates()`). It carries the **content** of the minimal built-in
templates: their Go `html/template` layouts and one validating example each.

The Go schema definitions, one-line descriptions, structured example data and
registration live in `internal/template.Builtins`, which imports
`internal/assets`; this tree must stay dependency-free (no Go code, and it is
never imported by `internal/assets` in the other direction).

## Layout convention

One directory per template, named by the template name:

    templates/<name>/layout.html.tmpl    the Go html/template layout
    templates/<name>/example.md          the validating example

The minimal built-in set:

| template  | usage   | layout                     | example               |
|-----------|---------|----------------------------|-----------------------|
| `title`   | slide   | `title/layout.html.tmpl`   | `title/example.md`    |
| `content` | slide   | `content/layout.html.tmpl` | `content/example.md`  |
| `column`  | section | `column/layout.html.tmpl`  | `column/example.md`   |

Layouts are parsed into one `html/template` namespace keyed by the template
name, so a composed layout invokes a section template with
`{{ template "<name>" . }}` (the `content` layout renders each `# columns`
instance through the `column` layout this way).

## Example shapes

- A **slide** template's `example.md` is a complete slide source: YAML `---`
  frontmatter at line 1 (with `template:` naming the slide template), an
  optional body, then sections and an optional `# notes` last.
- A **section** template's `example.md` is a section fragment: a plain ```
  frontmatter fence (with `template:` and the section's fields) followed by the
  section body — the same shape a `# <section>` instance has inside a slide.

## Render context

A layout is executed with a `map[string]any`:

- each declared field name maps to its value. Number and date values arrive
  already formatted by the render layer (honouring the field's
  `default_format` and its `<field>_format` sibling); text values arrive as
  rendered inline HTML (or escaped plain text when the field is `plain`).
- `body` maps to the rendered body HTML, or is absent/nil when there is no
  body.
- each declared section name maps to a slice of instance maps in source order;
  an instance map carries that section template's field names plus `body`.

`notes` is never passed to a layout: the renderer emits the reveal.js
`<aside class="notes">` itself.

The `content` enum field `layout` selects a variant in that layout; no other
variant mechanism exists. Fragments, transitions and backgrounds are set only
in these layouts (controlled effects), never by deck authors.

## Field reference

### `title` — usage: slide

| field      | type | required | default | rules                                           | description |
|------------|------|----------|---------|-------------------------------------------------|-------------|
| `title`    | text | yes      | —       | `max_length: 80`                                | Deck title. |
| `subtitle` | text | no       | —       | `max_length: 140`                               | Supporting line under the title. |
| `date`     | date | no       | —       | `default_format: long`; formats `long`, `short` | Date under the subtitle. |

- Sections: none.
- Body: `disallowed`.
- Formats: `date_format` selects `long` (`25 September 2026`) or `short`
  (`25 Sep 2026`); the default is `long`.
- Effects: `data-transition="fade"`,
  `data-background-color="var(--ey-bg-dark)"`.
- Example (`title/example.md`): `title`, `subtitle`, `date: 2026-09-25`,
  `date_format: long`, then a `# notes` section.

### `content` — usage: slide

| field         | type    | required | default   | rules                                                              | description |
|---------------|---------|----------|-----------|--------------------------------------------------------------------|-------------|
| `heading`     | text    | yes      | —         | `max_length: 80`                                                   | Slide heading. |
| `layout`      | enum    | no       | `default` | variants `default`, `columns`                                      | Layout variant. |
| `metric`      | number  | no       | —         | `min: 0`; `default_format: compact`; formats `compact`, `exact`, `percent` | Headline number. |
| `show_metric` | boolean | no       | `false`   | —                                                                  | Whether to show `metric`. |
| `as_of`       | date    | no       | —         | `default_format: long`; formats `long`, `short`                    | Date the numbers are as of. |

- Sections: `columns` accepts the section template `column`; `min: 2`,
  `max: 4` (`column` accepts exactly one template, so a section instance may
  omit `template:` and resolves to `column`).
- Body: `optional`; `max_paragraphs: 2`.
- Formats: `metric_format` selects `compact` (`1.25M`), `exact` (`1,250,000`)
  or `percent` (`125%`); the default is `compact`. `as_of_format` selects
  `long` or `short`, default `long`.
- Effects: `metric` carries `class="fragment"`; each `column` instance carries
  `class="fragment"`.
- Example (`content/example.md`): `heading`, `layout: columns`,
  `metric: 1250000`, `metric_format: compact`, `show_metric: true`,
  `as_of: 2026-09-25`, one body paragraph, and two `# columns` sections (each
  with a `title` field and a body).

### `column` — usage: section

| field   | type | required | default | rules            | description |
|---------|------|----------|---------|------------------|-------------|
| `title` | text | no       | —       | `max_length: 60` | Column heading. |

- Sections: none. Composition is `content` → `columns` → `column`.
- Body: `optional`.
- Effects: the column `<div>` carries `class="fragment"`.
- Example (`column/example.md`): a section fragment with `template: column`,
  `title: Revenue` and a body.

## Notes for `internal/template.Builtins`

- Register exactly the three templates above with the usages shown.
- `title` declares no sections and `body: disallowed`; `content` declares the
  `columns` section (`accepted: ["column"]`, `min: 2`, `max: 4`) and
  `body: optional` with `max_paragraphs: 2`; `column` declares no sections and
  `body: optional`.
- The reserved `_format` siblings (`date_format`, `metric_format`,
  `as_of_format`) are not declared fields. The format names and their output
  used by the examples are listed per template above.
- The structured example data must match the Markdown examples exactly: field
  names, values and the declared `# columns` instances (with their `title`
  fields and bodies) as described above.
