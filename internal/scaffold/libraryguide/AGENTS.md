# kalide template-library authoring guide

This file is a complete reference for authoring a kalide **template library**:
the set of slide templates, section templates, themes and shared media files
that decks render against. It is inert documentation — `kalide` never reads
this file.

A library is a directory with this fixed layout:

```
templates/
  library.yaml               # library metadata
  slides/<name>/             # one slide template
    template.yaml            # the template's schema manifest
    layout.html.tmpl         # the Go html/template layout
    example.md               # a validating example
  sections/<name>/           # one section template, same three files
    template.yaml
    layout.html.tmpl
    example.md
  themes/<name>/             # one theme
    theme.css                # required stylesheet
    ...                      # optional theme-owned files (fonts, logos)
  media/                     # shared media referenced with media "..."
```

## library.yaml

`library.yaml` is required at the library root.

```yaml
name: hello
description: Minimal starter template library.
format: 1
```

- `name` (required) — an identifier matching `[a-z0-9][a-z0-9-]*`.
- `description` (optional) — one line.
- `format` (required) — the layout and manifest format version; exactly `1` is
  accepted.

Any other key is an error. A library that defines no templates is load-valid; a
library intended to **host a deck** must provide the deck's theme (the deck's
`theme` defaults to `default`, so a `themes/default/` directory is normally
required).

A root-level `AGENTS.md` (this guide) is ignored by the loader, as is any other
top-level entry that is not part of the library (`library.yaml`, `slides/`,
`sections/`, `themes/`, `media/`) — for example a `.git/` directory or a
`README.md`. A library that is also a git repo therefore still loads; ignored
entries are never parsed, validated, rendered, listed or served.

## Slides and sections

- `slides/<name>/` holds a slide-usage template; `sections/<name>/` holds a
  section-usage template.
- A template's **kind** comes only from its directory. `template.yaml` must not
  carry a `usage:` key.
- A template's **name** is its directory name, and must be unique across
  `slides/` and `sections/` together.
- Every template directory must contain exactly `template.yaml`,
  `layout.html.tmpl` and `example.md`.

## template.yaml

`template.yaml` declares the template's schema as data. An empty manifest is
valid.

```yaml
description: "Title slide with an optional body."
fields:
  - name: title
    type: text
    required: true
    max_length: 80
    description: Slide title.
  - name: subtitle
    type: text
    description: Optional subtitle.
body:
  mode: optional
  max_words: 120
  subheadings: false
```

Accepted top-level keys: `description`, `fields`, `sections`, `body`. (`name`
is accepted but ignored; the name is the directory name.) Any other key is an
error with a suggestion.

### Fields

A field entry has `name` and `type`, and may set `required`, `default`,
`description` plus the limits for its type.

| type | value in content | extra keys |
|---|---|---|
| `text` | inline Markdown (bold, italic, inline code, `[]()` links, hard breaks) | `max_length`, `plain` |
| `number` | a plain YAML number; a quoted value is a type error | `min`, `max`, `formats`, `default_format` |
| `date` | `YYYY-MM-DD` | `min_date`, `max_date`, `formats`, `default_format` |
| `boolean` | `true` or `false` | — |
| `enum` | one of the declared variants | `variants` (required) |
| `image` | a path under the deck's `assets/`, existence checked | — |
| `link` | a `#label` anchor (must exist) or an `http(s)` URL | — |
| `list` | a YAML sequence; every element has the single item type | `item`, `min_items`, `max_items` |
| `section-template` | structured YAML data shaped by another section template | `section_template` |

- A `list` has exactly one item type, declared in its `item:` descriptor (the
  same field keys).
- A `section-template` field names a section template with `section_template:`
  and takes YAML data only; there are no separate record types.
- An `enum` field's variants are also the template's only layout-variant
  mechanism.
- Number formats: `compact`, `exact`, `percent`. Date formats: `long`, `short`.
  An author selects one with the reserved sibling key `<field>_format`;
  otherwise the field's `default_format` applies.
- Reserved names: `body`, `notes`, `deck`, `slide`, `item`, `raw`, `data` and
  the `_format` suffix cannot be used as field or section names.

### Sections

```yaml
sections:
  - name: people
    accepted: [person]     # section-usage template names
    min: 0                 # 0 = no minimum
    max: 4                 # 0 or less = unbounded
```

`name` and `accepted` are required. A section accepts only section-usage
templates, never slide templates. A section instance's `template:` key is
required only when the section accepts more than one template.

**Any** template may declare `sections:` — a slide template, and equally a
section template, whose instances then accept nested child sections. Heading
depth is nesting depth: `#` through `######`, not a Markdown subheading level. A
`# name` heading opens a top-level section; a heading one level deeper opens a
child of the most recent shallower heading, naming a section declared by that
parent instance's resolved template (the slide template for a top-level
section); a heading at the same or a shallower depth closes the open sections
back to its parent. Each parent's child instances are counted per name against
the declaration's `min`/`max`, and an undeclared child name is an error with a
closest-match suggestion.

Where the enclosing template declares no child sections, a deeper heading stays
in the enclosing body as an ordinary Markdown subheading (allowed `##`/`###`
only where that template's body rule permits subheadings). The reserved
`# notes` section is top-level only.

### Body rules

Every template has an implied `body` field: the Markdown after the frontmatter
(for a slide) or after the section heading (for a section). A `body:` key in
content YAML is an error, not a way to set it.

```yaml
body:
  mode: optional         # required | optional | disallowed
  max_words: 120         # 0 = no limit
  max_paragraphs: 3
  max_list_items: 6
  subheadings: false     # whether ## / ### are allowed
```

`mode` is required when the block is present. A section template used as a
field type takes data only, so its body is `disallowed`.

### example.md

Every template's `example.md` is validated by the loader:

- a **slide** template's example is a slide source: `---` frontmatter at line
  1, a body and optional `# name` section instances;
- a **section** template's example is a section fragment: an optional plain
  fence (three backticks, no language tag), a body and nested sections.

The example is parsed and validated like real content, so keep it a working,
minimal sample. `kalide templates <name>` prints it for copy.

## layout.html.tmpl

Layouts are standard Go `html/template`. There is no Markdown helper and no
third-party template engine.

Context:

- declared fields by name: `{{ .title }}`;
- the body as `{{ .body }}`;
- each section's instances by section name.

Values arrive in two forms:

- the **body**, **`text` fields** and **each rendered section** arrive as
  pre-rendered, trusted HTML — insert them unescaped;
- every **other field** arrives as a typed value (number, date, boolean, enum
  string, list, image/link URL), and `html/template` auto-escapes strings.

Reserved context names, provided as **data**, not helpers:

- `.deck` — `.deck.title` (always present), `.deck.author` and `.deck.date`
  (empty strings when omitted), and `.deck.properties` (always a non-nil map).
  Read a property with `{{ index .deck.properties "audience" }}`, or
  `{{ .deck.properties.audience }}` for a plain key.
- `.slide` — `.slide.number` (a string, for example `"1"` or `"1a"`) and
  `.slide.total` (an integer).
- `.item` — present only inside a **section** instance, never in a slide
  layout. It describes the instance among its same-name siblings:
  `.item.index` (zero-based), `.item.number` (one-based), `.item.count`,
  `.item.first` and `.item.last` (booleans), `.item.section` (the declared
  section name), `.item.template` (the resolved section template name) and
  `.item.parent` (the enclosing section instance's field values, or nil at the
  top level). A slide layout and a `section-template`-typed field value carry no
  `item` entry.
- `.raw` — the author's **original source** values, before rendering:
  `.raw.<field>` is a text field's source Markdown exactly as authored (before
  inline-Markdown rendering), and `.raw.body` is the original body source
  (before block rendering). A declared default is not a source value, so a
  field the author did not supply is absent or empty. `.raw` is data, not a
  helper. A section instance the `section` helper renders sees `.raw` as the
  values the call supplied.
- `.data` — the authored section tree as data, parallel to the pre-rendered
  section lists: `.data.<section>` is a list with one entry per authored
  instance of that declared section, in source order, and each entry carries
  its own `.raw`, `.item` and `.data` (its children, keyed the same way). Range
  over `.sectionName` to splice rendered HTML; read `.data.<section>` to
  inspect the same instances as source data. A section-helper call is a direct
  render call, not an authored instance, so it appends no entry to `.data` and
  no element to the rendered section list. `.data` is data, not a helper.

The v1 layout **helper** set is exactly `media`, `section`, `dict` and `list`.

`media`'s argument is a path relative to the library's `media/` directory; it
returns the URL the server serves the file under. The argument must not be
absolute and must not contain `..`; a missing file, an absolute path or a `..`
segment is an error naming the layout's file and line. `media` never resolves
into a theme directory.

```
{{ media "logo.svg" }}
```

`section` renders a section template directly, exactly as if an instance of it
had been written in Markdown:

- `{{ section "footer" }}` renders the `footer` section template with its
  declared defaults and no body;
- `{{ section "number-heading" (dict "number" .item.number "heading" .raw.title) }}`
  renders it with those field values, built by `dict` from the call's context;
- `{{ section "callout" (dict "label" "Proposition") .raw.body }}` renders it
  with a literal field and the slide's original body passed through as a
  **non-literal** body expression.

`section` takes the target template name, an optional `fields` map of source
values validated against the target's field schema, and an optional `body`
string validated against the target's body rule; omitted fields take the
target's defaults. The rendered instance is treated as a **one-item group**:
`.item` reports `index` 0, `number` and `count` 1, `first` and `last` true,
`section` and `template` the target name, and `parent` the calling instance's
fields (nil when the call is made from a slide layout). **No child sections
pass through the helper in v1**: a target declaring a mandatory child section,
or a body whose heading names one of the target's child sections, is an error.

`dict k1 v1 …` builds a `map[string]any` from alternating key/value arguments,
for passing structured source values to `section`. Keys must be strings; an odd
argument count, a non-string key or a repeated key is an error.

`list a b …` builds a sequence from its arguments, the layout syntax for a
literal list-typed value.

The built-in number and date format functions (`compact`, `exact`, `percent`,
`long`, `short`) are also available.

`deck`, `slide`, `item`, `raw` and `data` are reserved top-level names and
cannot be declared as fields or sections. `notes`, `body` and the `_format`
suffix are reserved too.

## Themes

- Themes live at `themes/<name>/`, and each requires a `theme.css`.
- A theme is a **token-only** stylesheet plus any files it owns (fonts, logos).
  There are **no built-in themes**.
- A deck selects a theme by name; a library that defines no theme is load-valid,
  but a library that hosts a deck must define the theme that deck resolves (the
  deck's `theme` defaults to `default`).
- Theme-owned files are referenced from that theme's CSS and are served from
  the theme directory.
- A theme's `theme.css` — and every CSS it `@import`s, wherever in the library
  that CSS lives (its own theme directory, another theme, or `media/`) — may
  reference a servable library file with both `url()` and `@import`, in three
  forms:
  - an unprefixed relative path, resolved from the referencing file's own
    location and confined to that file's own tree: the theme directory for a
    stylesheet in a theme, or `media/` for a stylesheet reached through
    `media:`;
  - the reserved `media:` prefix → the library's shared `media/` tree, for
    example `url('media:fonts/Inter-Regular.woff2')` or
    `@import url('media:theme.css')`;
  - the reserved `theme:<name>/` prefix → another theme's directory, for
    example `url('theme:default/theme.css')` or
    `@import url('theme:default/theme.css')`, so a theme may build on another.
- Cross-tree reach happens only through the `media:` and `theme:<name>/`
  prefixes. An unprefixed relative reference stays inside the stylesheet's own
  tree: no absolute paths, no `..` leaving that tree, no other scheme, nothing
  outside the library, and no references into non-servable entries (`slides/`,
  `sections/`, `library.yaml`, `AGENTS.md`). The path after `media:` is relative
  to the library's `media/` — the same base the layout `{{ media "path" }}`
  helper uses — and the path after `theme:<name>/` is relative to
  `themes/<name>/`; an unknown `theme:<name>` is an error. Both prefixes work in
  both `url()` and `@import`, and an imported stylesheet's own references are
  checked too. `@import` is followed, bounded and cycle-safe across the whole
  import graph; a missing file, a bad reference or a cycle is an error naming
  the theme CSS file and line.

## Media

- Shared media of any type lives under `media/` and is referenced from layouts
  with `{{ media "path" }}`.
- The path is relative to `media/` only: no absolute paths and no `..`.
- A theme's CSS — `theme.css` and every CSS it `@import`s — reaches the same
  `media/` tree through the reserved `media:` prefix and another theme's
  directory through the reserved `theme:<name>/` prefix; both work in `url()`
  and `@import`, so a theme may build on another. An unprefixed relative
  reference stays inside the stylesheet's own tree. See **Themes**.
- Theme files are not reachable through `media`.
- File types are unrestricted; the server derives the content type from the
  extension.

## Validating the library

There is no library-only serve command. Bring the library forward, then point a
deck at it and run `kalide start`:

1. If the library was created by an older `kalide`, run `kalide upgrade` at the
   library root first. It brings the library forward: it refreshes the
   kalide-owned scaffold files to the running binary's versions and applies
   kalide's format migrations, entirely offline, with no network or `git`. It
   never overwrites author content — your `slides/`, `sections/`, every theme
   (including its `theme.css` and theme-owned files) and the shared `media/`
   tree are left byte-for-byte untouched, with one exception: a seeded default
   `themes/default/theme.css` that is still byte-identical to a known released
   kalide version (that is, unedited since `kalide init`) is refreshed to the
   running binary's version. A kalide-owned file the author has edited — an
   edited `themes/default/theme.css` included — is left alone and reported. An
   unknown or newer `library.yaml` `format:` is refused instead: `kalide upgrade`
   exits non-zero, names the format found and the format supported, and changes
   nothing. `kalide upgrade` takes no path argument and has no `--force`.
2. Create a deck whose `kalide.yaml` resolves your library — either put the
   library at the deck's `templates/` directory, or set the deck's `templates:`
   key to the library path (relative to the deck root, or absolute).
3. Run `kalide start`. It validates the library (`library.yaml`, the layout,
   every template manifest and layout, the themes and every media reference)
   and the deck, then serves it. Validation is fail-fast: exactly one error is
   reported, with the file, the line, the path, what is wrong and how to fix
   it, plus closest-match suggestions for unknown names.
4. Use `kalide templates` to list the loaded library, and
   `kalide templates <name>` to inspect one template's fields, sections, body
   rules and example.

A library with no templates loads but hosts no slides; make sure the deck's
theme exists in the library before serving.
