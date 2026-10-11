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

## A library as a deck's external layer

A library is deck-ready on its own and can also be the **external library** of a
deck whose `kalide.yaml` names it with `templates:` (a path relative to the deck
root, or an absolute path). The deck's own `templates/` directory, when it
exists, loads as the deck's **local layer**. Both layers are loaded when both
are present, with equal standing: there is no precedence between them and no
overriding. The format is identical in both layers; only where the library lives
differs. This guide describes an **external library**, which is therefore
self-contained:

- It **validates standalone**. Every reference inside its templates and themes —
  the layout `media` helper, the reserved `media:` prefix and the reserved
  `theme:<name>/` prefix (see **Themes** and **Media**) — resolves within this
  library alone, never in the deck's local layer.
- Its templates **cannot call a section that lives only in the deck's local
  layer**. The `section` helper resolves against the merged section namespace, so
  a call naming a section this library does not define is a validation error even
  when the deck's local layer declares it. The reverse works: the merged
  namespace lets a deck's local template invoke an external section like a local
  one.
- Its **names must not collide with the deck's local layer**. A slide or section
  name (the two share one template namespace), a theme name, or a shared `media/`
  path carried by both layers is a validation error naming the name and both
  paths — never an override. Give the library names distinct from the deck's
  local templates.

`kalide init <path>` validates the external library **alone** (fail-fast) before
writing anything: a missing or invalid library, or one that does not resolve the
deck's theme, aborts with an error naming the path and nothing is written. It
then writes a deck referencing the library — an empty `slides/` and no local
`templates/`; a pre-existing local `templates/` is left untouched and becomes
the deck's local layer.

When a deck serves this library, `kalide start` validates the merged library
(both layers) before serving anything. While it runs, an edit under the library
root live-reloads; changing the deck's configured `templates:` value does not —
the deck page shows a restart-required error and keeps serving the startup
library until you restart `kalide start`.

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

A root-level `AGENTS.md` and `AGENTS_KALIDE.md` (this guide and the pointer that
references it) are ignored by the loader, as is any other top-level entry that
is not part of the library (`library.yaml`, `slides/`, `sections/`, `themes/`,
`media/`) — for example a `.git/` directory or a `README.md`. A library that is
also a git repo therefore still loads; ignored entries are never parsed,
validated, rendered, listed or served.

## Slides and sections

- `slides/<name>/` holds a slide-usage template; `sections/<name>/` holds a
  section-usage template.
- A template's **kind** comes only from its directory. `template.yaml` must not
  carry a `usage:` key.
- A template's **name** is its directory name, and must be unique across
  `slides/` and `sections/` together — and, when the library is a deck's external
  layer, across the merged layers: a name carried by both layers is an error
  naming the name and both paths, never an override (see **A library as a deck's
  external layer**). Theme names and `media/` paths each form their own namespace,
  unique across the merged layers the same way.
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

The layout **helper** set is exactly `media`, `section`, `dict`, `list`, `child`
and `children`.

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
fields (nil when the call is made from a slide layout). A `section` call may
also pass the target's child sections, after its fields and body, as `child`
arguments and `children` forwardings; a body whose heading names one of the
target's declared child sections is still an error — children pass only through
child arguments, never as headings inside the call's body.

`child "<section>" [fields] [body] [child…]` names a **declared section of the
`section` call's target** and renders one instance of it, exactly as if an
instance of it had been authored. It takes the section name, an optional
`fields` map of source values validated against that section's accepted
templates, an optional `body` string validated against the instantiated
template's body rule, and any number of nested `child` arguments for that
child's own declared children. The optional `template` key in its `fields`
selects which accepted template is instantiated; it is required only when the
section accepts more than one, and it is consumed as a selector — it is never a
field of the instantiated template. A `child` call reads nothing outside its own
arguments. For example,
`{{ section "columns" nil nil (child "column" (dict "template" "half" "title" .raw.title)) }}`
renders one `column` child of the `columns` section using the `half` accepted
template, and `{{ child "column" nil nil (child "cell") }}` renders a nested
grandchild.

The literal `child` arguments are checked at load: the named section must be
declared by the target and the instantiated `template` must be one of its
accepted templates; when **every** `child` argument is literal, the parent's
`min`/`max` is checked over the whole passed set; and each child's literal
fields and body are validated against the instantiated template — defaults
applied, the field rules and body rule enforced, and a body heading that names
one of the child's declared sections rejected. A forwarded or otherwise
non-literal child list is validated at render instead.

`children "<section>" .data.<section>` forwards the caller's **authored**
`.data.<section>` entries as that declared section's children, so a wrapper
layout hands its own authored children to the section it wraps, for example
`{{ section "wrapper" nil nil (children "item" .data.item) }}`. Each forwarded
entry re-instantiates as a real child keeping its own `template` (its
`.item.template`), its own `.raw` fields and body, and its own nested `.data`,
with the declaration's `min`/`max` counted over the forwarded set.

Each child — literal or forwarded — gets its own `.item`, `.data` and `.raw`
context exactly as an authored child does: `.item.section` is the named
section, `.item.template` the instantiated template, `.item.parent` the
target's field values, and `.raw` and `.data` that child's own source values and
nested children. The target's own rendered child-section lists and its
`.data.<child>` are populated from the passed children. A child names a section
of the `section` call's target and resolves in the **merged** section namespace,
so a child template living in either layer is invoked like a local one — but an
external library validates standalone, so a child naming a section this library
does not define is a validation error even when the deck's local layer declares
it, exactly as for a `section` call. The child's own `media` reference resolves
in that child template's own owning layer. A `child`/`children` call contributes
nothing to the caller's own `.data` or rendered section lists.

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

### Composing layouts

A layout may include another library layout with the standard Go
`{{ template "<layout name>" . }}` action, so shared markup lives in one layout
that the slides and sections which need it compose. The layout name is the
included layout's **root-qualified path inside its own layer**:

- `<layer root>/slides/<name>/layout.html.tmpl` for a slide layout, and
- `<layer root>/sections/<name>/layout.html.tmpl` for a section layout,

where `<layer root>` is the directory the library was loaded from — the
deck-root `templates/` when the library is the deck's local layer, or the
external library's root when it is the deck's external layer. That is exactly
the name the loader builds and registers each layout under, so the action names
the path literally, never the bare template name. For a `hello` slide layout the
call is `{{ template "slides/hello/layout.html.tmpl" . }}`, with `<layer root>`
elided only here for brevity.

A layout may include any layout in its **own library** by that root-qualified
name. A deck's local-layer layout may additionally include an external library's
layout by the external layer's own root-qualified name (see **A library as a
deck's external layer**). The reverse is not allowed: this library is validated
standalone before the merge, so a layout here that includes a layout existing
only in the deck's local layer is a templates error ("no such template")
reported at this library's own layout file. Keep an external library
self-contained.

The included layout executes in the **invoking** layout's namespace, so its
`media` helper binds to the invoking layer's `media/`: an included layout's
`{{ media "logo.svg" }}` resolves in the layer of the layout that named it, not
in the layer that owns the included layout. A `section` call rebinds to the
called template's own layer instead, so the called template's
`{{ media "logo.svg" }}` resolves in that template's own library. Compose with
the `template` action to share markup; call with `section` when the target's own
library must supply its media.

## Themes

- Themes live at `themes/<name>/`, and each requires a `theme.css`.
- A theme is a **token-only** stylesheet plus any files it owns (fonts, logos).
  There are **no built-in themes**.
- A deck selects a theme by name; a library that defines no theme is load-valid,
  but a library that hosts a deck must define the theme that deck resolves (the
  deck's `theme` defaults to `default`).
- When the library is a deck's external layer, its theme names must not collide
  with the deck's local layer, and every theme's `media:`/`theme:<name>/`
  reference resolves in this library alone — a theme reference never reaches the
  deck's local layer (see **A library as a deck's external layer**).
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
  `sections/`, `library.yaml`, `AGENTS.md`, `AGENTS_KALIDE.md`). The path
  after `media:` is relative to the library's `media/` — the same base the
  layout `{{ media "path" }}` helper uses — and the path after `theme:<name>/`
  is relative to `themes/<name>/`; an unknown `theme:<name>` is an error. Both
  prefixes work in both `url()` and `@import`, and an imported stylesheet's own
  references are checked too. `@import` is followed, bounded and cycle-safe
  across the whole import graph; a missing file, a bad reference or a cycle is
  an error naming the theme CSS file and line.

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
   library at the deck's `templates/` directory (the deck's local layer), or set
   the deck's `templates:` key to the library path (relative to the deck root, or
   absolute), which loads the library as the deck's external layer, layered over
   the deck's own `templates/` when that exists. `kalide init <path>` validates
   the external library alone and writes such a deck for you.
3. Run `kalide start`. It validates the library (`library.yaml`, the layout,
   every template manifest and layout, the themes and every media reference) —
   validating an external library as its own root, so it must be self-contained —
   and then the merged library (both layers) and the deck, before serving.
   Validation is fail-fast: exactly one error is reported, with the file, the
   line, the path, what is wrong and how to fix it, plus closest-match
   suggestions for unknown names. An error is path-qualified with the layer root
   it comes from.
4. Use `kalide templates` to list the loaded library, and
   `kalide templates <name>` to inspect one template's fields, sections, body
   rules and example.

A library with no templates loads but hosts no slides; make sure the deck's
theme exists in the library before serving.
