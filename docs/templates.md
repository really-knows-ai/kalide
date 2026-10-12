# Template library reference

A kalide **template library** is the set of slide templates, section templates,
themes and shared media files a deck renders against. This page is the
template-author reference for that library: its fixed directory layout, the
`template.yaml` manifest vocabulary, the layout execution context and helper
set, and the theme/media reference rules.

Every deck resolves a library. A deck may carry it at the deck's own
`templates/` directory (the deck's **local layer**), or name an external
library in its top-level `templates:` key (the deck's **external layer**), or
both. For how the two layers merge, see
[docs/layered-libraries.md](layered-libraries.md); for copyable recipes, see
[docs/cookbook.md](cookbook.md).

An agent-facing version of this reference ships inside every generated library
as `AGENTS_KALIDE.md` (with a short `AGENTS.md` pointer beside it). That file
is inert documentation — `kalide` never reads it — and this page tracks the
same rules.

## Library layout

A library is a directory with this fixed layout:

```
templates/
  library.yaml               # library metadata
  slides/<name>/             # one slide template
    template.yaml            # the template's schema manifest
    layout.html.tmpl         # the Go html/template layout
    example.md               # a validating example
  sections/<name>/           # one section template, the same three files
    template.yaml
    layout.html.tmpl
    example.md
  themes/<name>/             # one theme
    theme.css                # required stylesheet
    ...                      # optional theme-owned files (fonts, logos)
  media/                     # shared media referenced with media "..."
```

- `library.yaml` is required at the library root.
- `slides/`, `sections/`, `themes/` and `media/` are all optional. A library
  that defines no templates loads; a library intended to host a deck must
  provide the theme that deck resolves.
- Every slide or section template directory must contain the three files
  `template.yaml`, `layout.html.tmpl` and `example.md`.
- Every theme directory requires `theme.css` and may hold any number of other
  files it owns.
- Any other top-level entry is **not** part of the library and is ignored: a
  `.git/` directory, a `README.md`, an editor lockfile, `AGENTS.md` and
  `AGENTS_KALIDE.md`. Ignored entries are never parsed, validated, rendered,
  listed or served, so a library that is also a git repo still loads.

A template's **kind** comes only from its directory — `slides/` or
`sections/`. `template.yaml` must not carry a `usage:` key. A template's
**name** is its directory name. A slide name and a section name share one
namespace and may not collide across `slides/` and `sections/`; theme names
and `media/` paths each form their own namespace. When the library is a deck's
external layer, all three namespaces must also be disjoint from the deck's
local layer (see [docs/layered-libraries.md](layered-libraries.md)).

## library.yaml

`library.yaml` is required at the library root and accepts exactly three keys:

```yaml
name: hello
description: Minimal starter template library.
format: 1
```

- `name` (required) — the library's own name, matching `[a-z0-9][a-z0-9-]*`.
- `description` (optional) — one line.
- `format` (required) — the layout and manifest format version. Exactly `1` is
  accepted; anything else is an error.

Any other key is an error.

## template.yaml

`template.yaml` declares a template's schema as data. An empty manifest is
valid. Accepted top-level keys are `description`, `fields`, `sections` and
`body`; `name` is accepted but ignored, because the name is always the
directory name; any other key is an error with a suggestion.

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

### `description`

An optional one-line summary of the template.

### `fields`

`fields` is a sequence of field entries. Each entry requires `name` and
`type`, and may set the common keys `required`, `default` and `description`
plus the limits for its type:

| `type` | value the author writes | type-specific keys |
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

- `required: true` — the field must be supplied. A field that declares a
  `default` is satisfied when omitted and takes that value instead.
- `default` — the value applied when the author omits the field.
- `description` — the field's author-facing help text.
- `max_length` — the maximum character count of a `text` value, measured
  after Markdown is stripped; `0` is no limit. `plain: true` turns off the
  inline-Markdown checks for a `text` field.
- `min` / `max` — inclusive numeric bounds for a `number` field.
- `min_date` / `max_date` — inclusive `YYYY-MM-DD` bounds for a `date` field.
- `min_items` / `max_items` — length bounds for a `list` field; `0` is no
  bound.
- `variants` — the complete, required set of allowed values for an `enum`
  field. A declared `default` must be one of the variants. An enum field's
  variants are also a template's only layout-variant mechanism.
- `item` — the `list` field's single item descriptor, written with the same
  field keys (so a list of `number`s, a list of `section-template`s, and so on).
- `section_template` — names the section template a `section-template` field's
  value is shaped by; the value is data only, with no separate record types.
- `formats` / `default_format` — for `number` and `date` fields, the named
  display formats the field accepts and the one used when the author writes no
  selection. Number formats are `compact`, `exact` and `percent`; date formats
  are `long` and `short`. An author selects one in content with the reserved
  sibling key `<field>_format`; otherwise `default_format` applies.

### `sections`

`sections` declares the child sections a template accepts:

```yaml
sections:
  - name: people
    accepted: [person]     # section-usage template names
    min: 0                 # 0 = no minimum
    max: 4                 # 0 or less = unbounded
```

- `name` (required) — the declared section name.
- `accepted` (required, non-empty) — the section templates the section accepts.
  A section accepts only section-usage templates, never slide templates.
- `min` — the minimum number of instances; `0` means no minimum.
- `max` — the maximum number of instances; `0` or less means unbounded.

A section instance's `template:` key is required only when the section accepts
more than one template. **Any** template may declare `sections:` — a slide
template, and equally a section template, whose instances then accept nested
child sections. The child instances are authored as headings; see
`example.md` below.

### `body`

Every template has an implied `body` field: the Markdown after a slide's
frontmatter, or after a section heading. The `body:` block declares its rule:

```yaml
body:
  mode: optional         # required | optional | disallowed
  max_words: 120         # 0 = no limit
  max_paragraphs: 3
  max_list_items: 6
  subheadings: false     # whether ## / ### are allowed
```

`mode` is required when the block is present and is one of `required`,
`optional` or `disallowed`. `max_words`, `max_paragraphs` and `max_list_items`
are limits where `0` means no limit. `subheadings` controls whether `##` / `###`
subheadings are allowed in the body. A section template used as a field type
takes data only, so its body is `disallowed`. A `body:` key in content YAML is
an error — the body is written as Markdown, not as a key.

## Layout context

`layout.html.tmpl` is a standard Go `html/template`. There is no Markdown
helper and no third-party template engine. A layout reads:

- each declared field by name — `{{ .title }}`;
- the body as `{{ .body }}`;
- each section's instances by section name.

Values arrive in two forms:

- the **body**, **`text` fields** and **each rendered section** arrive as
  pre-rendered, trusted HTML — insert them unescaped;
- every **other field** (number, date, boolean, enum string, list,
  image/link URL) arrives as a typed value, and `html/template` auto-escapes
  strings.

### Reserved contexts

Five names are reserved as execution-context **data**, not helpers:

- `.deck` — `.deck.title` (always present), `.deck.author` and `.deck.date`
  (empty strings when omitted), and `.deck.properties` (always a non-nil map).
  Read a property with `{{ index .deck.properties "audience" }}`, or
  `{{ .deck.properties.audience }}` for a plain key.
- `.slide` — `.slide.number` (a string, for example `"1"` or `"1a"`) and
  `.slide.total` (the integer slide-file count).
- `.item` — present only inside a **section** instance, never in a slide
  layout. It describes the instance among its same-name siblings:
  `.item.index` (zero-based), `.item.number` (one-based), `.item.count`,
  `.item.first` and `.item.last` (booleans), `.item.section` (the declared
  section name), `.item.template` (the resolved section template name) and
  `.item.parent` (the enclosing section instance's field values, or nil at the
  top level). A slide layout and a `section-template`-typed field value carry
  no `item` entry.
- `.raw` — the author's **original source** values, before rendering:
  `.raw.<field>` is a text field's source Markdown exactly as authored, and
  `.raw.body` is the original body source. A declared default is not a source
  value, so a field the author did not supply is absent. `.raw` is data, not a
  helper.
- `.data` — the authored section tree as data, parallel to the pre-rendered
  section lists: `.data.<section>` is a list with one entry per authored
  instance of that declared section, in source order, and each entry carries
  its own `.raw`, `.item` and `.data` (its children, keyed the same way). Range
  over the section list to splice rendered HTML; read `.data.<section>` to
  inspect the same instances as source data. A `section` helper call is a
  direct render call, not an authored instance, so it appends no entry to
  `.data`.

### Reserved names

The reserved declared-name tokens are `body`, `notes`, `deck`, `slide`,
`item`, `raw` and `data`, plus the `_format` **suffix**. None of them may be
used as a field or section name: `body` is the implied body field, `notes` is
the parser's speaker-notes section, `deck`/`slide`/`item`/`raw`/`data` are the
reserved context names above, and a name ending in `_format` is reserved for
the format-selection sibling key.

## Layout helpers

The layout helper set is exactly `media`, `section`, `dict`, `list`, `child`
and `children`, plus the built-in number and date format functions (`compact`,
`exact`, `percent`, `long`, `short`).

`media "path"` returns the URL the server serves the library's `media/` file
under. The path is relative to `media/`; it must not be absolute and must not
contain `..`, and the file must exist. `media` never resolves into a theme
directory.

```
{{ media "logo.svg" }}
```

`section "name" [fields] [body] [child…]` renders a section template directly,
exactly as if an instance of it had been written in Markdown:

- `{{ section "footer" }}` renders the `footer` section template with its
  declared defaults and no body;
- `{{ section "number-heading" (dict "number" .item.number "heading" .raw.title) }}`
  renders it with those field values, built by `dict` from the call's context;
- `{{ section "callout" (dict "label" "Proposition") .raw.body }}` passes the
  slide's original body through as a **non-literal** body expression.

`section` takes the target template name, an optional `fields` map of source
values validated against the target's field schema, and an optional `body`
string validated against the target's body rule; omitted fields take the
target's defaults. The rendered instance is treated as a **one-item group**:
`.item` reports `index` 0, `number` and `count` 1, `first` and `last` true,
`section` and `template` the target name, and `parent` the calling instance's
fields (nil when called from a slide layout).

`child "section" [fields] [body] [child…]` names a **declared child section of
the `section` call's target** and renders one instance of it, exactly as if an
instance had been authored. It takes the section name, an optional `fields`
map validated against that section's accepted templates, an optional `body`
string validated against the instantiated template's body rule, and any number
of nested `child` arguments for that child's own declared children. The
optional `template` key in its `fields` selects which accepted template is
instantiated; it is required only when the section accepts more than one, and
it is consumed as a selector, never reaching the instantiated template's
fields. For example,
`{{ section "columns" nil nil (child "column" (dict "template" "half" "title" .raw.title)) }}`
renders one `column` child of `columns` using the `half` accepted template,
and `{{ child "column" nil nil (child "cell") }}` renders a nested grandchild.

`children "section" .data.<section>` forwards the caller's **authored**
`.data.<section>` entries as that declared section's children, so a wrapper
layout hands its own authored children to the section it wraps:

```
{{ section "wrapper" nil nil (children "item" .data.item) }}
```

Each forwarded entry re-instantiates as a real child keeping its own `template`,
its own `.raw` fields and body, and its own nested `.data`, with the
declaration's `min`/`max` counted over the forwarded set.

Every child — literal or forwarded — gets its own `.item`, `.data` and `.raw`
context exactly as an authored child does. Literal child arguments are checked
at load: the named section must be declared by the target and the instantiated
`template` must be one of its accepted templates; when every `child` argument
is literal, the parent's `min`/`max` is checked over the whole passed set, and
each child's literal fields and body are validated against the instantiated
template. A forwarded or otherwise non-literal child list is validated at
render instead. A `child`/`children` call contributes nothing to the caller's
own `.data` or rendered section lists.

`dict k1 v1 …` builds a `map[string]any` from alternating key/value arguments,
for passing structured source values to `section`. Keys must be strings; an odd
argument count, a non-string key or a repeated key is an error.

`list a b …` builds a sequence from its arguments — the layout syntax for a
literal list-typed value.

### Composing layouts

A layout may include another layout with the standard Go
`{{ template "<layout name>" . }}` action, so shared markup lives in one
layout that other slides and sections compose. The layout name is the included
layout's **layer-relative path inside its own layer**:

- `slides/<name>/layout.html.tmpl` for a slide layout, and
- `sections/<name>/layout.html.tmpl` for a section layout.

That name is identical on every OS and wherever the layer sits on disk: it has
no layer root, no `templates/` prefix, no backslashes and no absolute path. It is
exactly the name the loader registers each layout under, so the action names the
path literally, never the bare template name. For a `hello` slide layout the
call is `{{ template "slides/hello/layout.html.tmpl" . }}`.

A layout may include any layout in its own library by that layer-relative name.
A deck's local-layer layout may additionally include an external library's
layout by the external layout's layer-relative name. The reverse is not
allowed: an external library is validated standalone, so a layout there that
includes a layout existing only in the deck's local layer is a templates error
(`no such template`) reported at the invoking layout's own on-disk path and
line, quoting the target name exactly as written. The included layout executes
in the **invoking** layout's namespace, so an included layout's
`{{ media "logo.svg" }}` resolves in the layer of the layout that named it. A
`section` call rebinds to the called template's own layer instead, so the called
template's media resolves in that template's own library. Compose with the
`template` action to share markup; call with `section` when the target's own
library must supply its media.

## Themes

- Themes live at `themes/<name>/` and each requires a `theme.css`.
- A theme is a **token-only** stylesheet plus any files it owns (fonts, logos).
  There are **no built-in themes**.
- A deck selects a theme by name (the deck's `theme` defaults to `default`).
  A library that defines no theme is load-valid, but a library that hosts a
  deck must define the theme that deck resolves.
- Theme-owned files are referenced from that theme's CSS and are served from
  the theme directory. They are never reachable through the `media` helper.

## Media

- Shared media of any type lives under `media/` and is referenced from layouts
  with `{{ media "path" }}`.
- The path is relative to `media/` only: no absolute paths and no `..`. A
  missing file is an error, not a silent broken link.
- File types are unrestricted; the server derives the content type from the
  extension.

### Theme and media CSS references

A theme's `theme.css` — and every CSS it `@import`s, wherever in the library
that CSS lives — may reference a servable library file with both `url()` and
`@import`, in three forms:

- an **unprefixed relative path**, resolved from the referencing file's own
  location and confined to that file's own tree: the theme directory for a
  stylesheet in a theme, or `media/` for a stylesheet reached through `media:`;
- the reserved **`media:`** prefix → the library's shared `media/` tree, for
  example `url('media:fonts/Inter-Regular.woff2')` or
  `@import url('media:theme.css')`;
- the reserved **`theme:<name>/`** prefix → another theme's directory, for
  example `url('theme:default/theme.css')` or
  `@import url('theme:default/theme.css')`, so a theme may build on another.

Cross-tree reach happens only through the `media:` and `theme:<name>/`
prefixes. An unprefixed relative reference stays inside the stylesheet's own
tree: no absolute paths, no `..` leaving that tree, no other scheme, nothing
outside the library, and no references into non-servable entries (`slides/`,
`sections/`, `library.yaml`, `AGENTS.md`, `AGENTS_KALIDE.md`). The path after
`media:` is relative to the library's `media/` — the same base the layout
`{{ media "path" }}` helper uses — and the path after `theme:<name>/` is
relative to `themes/<name>/`; an unknown `theme:<name>` is an error. Both
prefixes work in both `url()` and `@import`, and an imported stylesheet's own
references are checked too. `@import` is followed, bounded and cycle-safe
across the whole import graph; a missing file, a bad reference or a cycle is an
error naming the theme CSS file and line.

## example.md

Every template's `example.md` is parsed and validated like real content, so
keep it a working, minimal sample. `kalide templates <name>` prints it for
copy.

- a **slide** template's example is a slide source: `---` frontmatter at line
  1, a body and optional `# name` section instances;
- a **section** template's example is a section fragment: an optional plain
  fence (three backticks, no language tag), a body and nested sections.

Child sections are authored as headings whose depth is the nesting depth: `#`
through `######` are heading levels, not Markdown subheading levels. A `# name`
heading opens a top-level section; a heading one level deeper opens a child of
the most recent shallower heading, naming a section declared by that parent
instance's resolved template. A heading at the same or a shallower depth closes
the open sections back to its parent. Where the enclosing template declares no
child sections, a deeper heading stays in the enclosing body as an ordinary
Markdown subheading — allowed `##`/`###` only where that template's body rule
permits subheadings.
