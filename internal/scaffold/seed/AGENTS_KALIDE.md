# kalide deck authoring guide

This file is a complete reference to the kalide deck and template format as
implemented by the `kalide` binary you are using. Read it before writing or
editing a deck. It is inert documentation: `kalide` never reads this file, and
it does not affect validation, rendering or serving.

## What a deck is

A deck is a directory containing:

```
kalide.yaml      deck-wide settings
slides/          one Markdown file per slide
assets/          images and other files your slides reference
```

and a **template library** that supplies every slide layout, section layout,
theme and shared media file. There is no built-in fallback: a deck without a
resolvable library does not load.

The library is one merged set drawn from up to two **layers**:

- the deck's **local `templates/` directory** — loaded whenever that directory
  exists (what a no-argument `kalide init` creates); and
- an **external library** named by the deck's `templates:` key in
  `kalide.yaml` (an absolute path, or a path relative to the deck root such as
  `../shared-lib`) — loaded whenever the key is present (what
  `kalide init <path>` creates).

Both layers are loaded when both are present, with equal standing: there is no
precedence between them and no overriding. A relative `templates:` path is
resolved against the deck root; an absolute path is used as given. The format
is identical in both layers; only where the library lives differs. Two
consequences of the layered library:

- `kalide init <path>` validates the external library **alone** before writing
  anything — a missing or invalid library, or one that does not resolve the
  deck's theme, aborts with an error naming the path and nothing written — then
  writes an **empty `slides/`** (no starter slide) and no local `templates/`.
  A pre-existing local `templates/` is left untouched and participates as the
  deck's local layer.
- A name carried by both layers — a slide or section name (the two share one
  template namespace), a theme name, or a shared `media/` path — is a
  validation error naming the name and both paths, never an override. A local
  template's `section` helper call resolves against the merged section
  namespace, so it invokes an external section like a local one; every other
  reference (`media`, `media:`, `theme:<name>/`) resolves inside the
  referencing template's or theme's own layer, and the external library is
  self-contained, so it validates standalone.

## kalide.yaml

`kalide.yaml` holds deck-wide configuration. Only `title` is required.

```yaml
title: My presentation     # required, string
author: A. Presenter       # optional, string
date: 2026-09-25           # optional, YYYY-MM-DD
theme: default             # optional, a theme directory name from the library
navigation: default        # optional: default, linear or grid
properties:                # optional, arbitrary deck-wide values for templates
  audience: Investors
  revision: 3
  confidential: true
templates: ../shared-lib   # optional, path to an external template library
```

Every key:

- `title` (required) — the deck title, a string. Missing or blank is an error.
- `author` — the presenter, a string.
- `date` — the presentation date written `YYYY-MM-DD`; any other form is an
  error.
- `theme` — the theme name; omitted means `default`. The theme must be a
  directory under the merged library's `themes/` (a theme may live in either
  layer, but the name must be unique across both).
- `navigation` — `default`, `linear` or `grid`; omitted means `default`.
- `properties` — an arbitrary mapping of author keys to typed scalars (string,
  number, boolean, or a `YYYY-MM-DD` date). A mapping or list value is an
  error. The map is always available to templates.
- `templates` — path to the deck's external template library layer. Omitted
  means there is no external layer; the deck-root `templates/` is still loaded
  as the local layer whenever it exists. Relative paths resolve against the
  deck root; absolute paths are used as given.

Every other top-level key is an error, with a closest-match suggestion.

## Slides

Slides live in `slides/`, one Markdown file per slide. A slide file **must
begin with YAML frontmatter on line 1**, opened and closed by a `---` line:

```markdown
---
template: hello
title: Hello, world
---

Welcome to your first slide.

# notes
Speaker notes go here; they are never shown to the audience.
```

- The frontmatter must start on line 1 (no leading blank lines) and must name a
  slide-usage `template:`.
- The frontmatter supplies the template's field values. Discover the declared
  fields with `kalide templates <name>`.
- Everything after the closing `---` is the slide **body**, up to the first
  `# name` heading.
- A `body:` key inside the frontmatter is not how you write a body — body text
  comes from the Markdown after the frontmatter — and is an error.

### File naming, ordering and positions

A slide filename is `<number>[letter]-<label>.md`, for example `1-title.md` or
`2a-detail.md`:

- `number` is a run of digits and sets the horizontal order; ordering is
  numeric, so `10-x.md` follows `9-x.md`. Gaps are allowed.
- `letter` (optional, one letter) makes the slide a **vertical slide** nested
  under the numbered slide of the same number.
- `label` becomes the slide's anchor id and must be **unique across the whole
  deck**.

A letter slide with no matching numbered slide is an error; two slides sharing
a position, or two slides sharing a label, are errors.

### Sections

A `# name` heading starts a **section** inside the slide. Headings nest by
depth: `#` through `######` is nesting depth, not a Markdown subheading level. A
`# name` heading opens a top-level section; a heading one level deeper opens a
child of the most recent shallower heading, naming a section declared by that
parent instance's resolved template; a heading at the same or a shallower depth
closes the open sections back to its parent. Section names and their repeat
limits are declared by the enclosing template — the slide template for a
top-level section, the parent instance's resolved template for a child — and an
undeclared name is an error with a closest-match suggestion. Each parent's child
instances are counted per name against the declared `min`/`max`.

Where the enclosing template declares no child sections, a deeper heading stays
in the enclosing body as an ordinary Markdown subheading — allowed `##`/`###`
only where that template's body rule permits subheadings.

A section's frontmatter is an optional plain fence (three backticks, no
language tag) immediately after its heading, at any depth:

````markdown
# people
```
name: Ada Lovelace
role: Mathematician
```
Ada is the first programmer.
````

- `template:` is required on a section instance **only when** the section
  accepts more than one section template; with exactly one accepted template it
  resolves automatically.
- A section may repeat up to the declared `max` (and as few as its `min`).

### Speaker notes

A reserved `# notes` section holds speaker notes: it is recognised only as a
top-level (`#`) heading. At most one is allowed, it must be the **last**
section, and it does not count toward any section's repeat limits. `notes` is
reserved and cannot be declared as a section name. Notes are never shown to the
audience.

## The template library

Each resolved layer is a directory with this fixed layout (shown below as the
deck's local `templates/`):

```
templates/
  library.yaml               # library metadata: name, description, format: 1
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
    ...                      # optional theme-owned fonts, logos, ...
  media/                     # shared media layouts reference with media "..."
```

- `library.yaml` is required at the library root. Keys: `name` (identifier
  `[a-z0-9][a-z0-9-]*`), optional `description`, and required `format: 1`.
- A template's **kind** (`slide` or `section`) comes from its directory:
  `slides/` or `sections/`. `template.yaml` must not carry a `usage:` key.
- A template's **name** is its directory name, and must be unique across
  `slides/` and `sections/` together, and across the merged layers: a name
  carried by two layers is an error naming the name and both paths. Theme names
  and `media/` paths each form their own namespace, unique across the merged
  layers the same way.
- Every template directory holds exactly `template.yaml`, `layout.html.tmpl`
  and `example.md`.
- `themes/<name>/` holds `theme.css` plus any fonts, logos or other files the
  theme owns.
- `media/` is optional shared media of any type.
- A root-level `AGENTS.md` and `AGENTS_KALIDE.md` (the library author guide and
  the pointer that references it) are ignored, as is any other top-level entry
  that is not part of the library (`library.yaml`, `slides/`, `sections/`,
  `themes/`, `media/`) — for example a `.git/` directory or a `README.md`. A
  library that is also a git repo therefore still loads; ignored entries are
  never parsed, validated, rendered, listed or served.

### Named library definitions

Besides `name`, `description` and `format`, a `library.yaml` may declare
reusable **named definitions** — named accept-lists (`groups`), named field
groups (`field_sets`) and named field types (`types`) — which templates in the
same layer reference by name. They are additive to `format: 1` (the `format`
value does not change):

```yaml
name: shared
format: 1
groups:                   # named accept-lists, referenced as `@name`
  callouts: [note, warning]
field_sets:               # named field groups, referenced as `- include: name`
  meta:
    - name: subtitle
      type: text
      nullable: true
types:                    # named field types, referenced as `type: name`
  colour:
    type: enum
    variants: [red, green, blue]
```

- `groups:` maps a name to a list of section-template names. A section's
  `accepted:` entry `@<name>` expands that list in place and is combinable with
  literal template names, for example `accepted: [@callouts, person]`.
- `field_sets:` maps a name to a list of field descriptors written with the same
  field keys as a `template.yaml` field. A `fields:` entry `- include: <name>`
  expands the named set's fields in place.
- `types:` maps a name to a single field descriptor, referenced from a field as
  `type: <name>` (for example the `colour` enum above). The descriptor may set
  any field key except its name, which is the `type:` name.
- A definition resolves in the layer of the library that owns the template that
  references it, so a name declared in one layer is not visible to a template in
  the other.
- An unknown `@name`, `include:` or `type:` name is an error with a
  closest-match suggestion.

## template.yaml

`template.yaml` declares the template's schema as data. An empty manifest is
valid: a template with no description, no fields, no sections and an optional
body.

```yaml
description: "Hello slide: a required title and an optional body."
fields:
  - name: title
    type: text
    required: true
    max_length: 80
    description: Slide title.
  - name: subtitle
    type: text
    nullable: true
    coerce: true
    description: Optional subtitle; a number or boolean is accepted as text.
  - name: logo
    type: image
    roots: [assets, media]
    dir: logos
    extensions: [svg, png]
    description: Optional logo, resolved under assets/ then the owning layer's media/.
  - name: tags
    type: list
    item:
      name: tag
      type: text
  - name: selected
    type: list
    max_items_from: /tags
    item:
      name: tag
      type: text
  - name: members
    type: list
    item:
      type: record
      fields:
        - name: name
          type: text
          required: true
        - name: role
          type: text
          nullable: true
sections:
  - name: people
    accepted: [person]
    min: 0
    max: 4
body:
  mode: optional
  max_words: 120
```

Accepted top-level keys: `description`, `fields`, `sections`, `body` and
`internal`. (`name` is accepted but ignored — the name is the directory name.)
Any other key is an error with a suggestion. `internal: true` hides the
template from the default `kalide templates` listing — it is shown only with
`kalide templates --all` — while leaving it usable as a `section`-helper target
and as a `section-template` field type; an internal template cannot be named as
a slide's or a section instance's `template:`.

### Field types

Every field entry has `name` and `type`; a field may also set `required`,
`default`, `description`, `nullable`, and the rules for its type.

| type | value written in the frontmatter | extra keys |
|---|---|---|
| `text` | inline Markdown (bold, italic, inline code, `[]()` links, hard breaks) | `max_length` (characters after Markdown is stripped), `plain` (strip styles silently), `coerce` |
| `number` | a plain YAML number, e.g. `1250000` or `0.12`; a quoted value is a type error | `min`, `max`, `formats`, `default_format` |
| `date` | `YYYY-MM-DD` | `min_date`, `max_date`, `formats`, `default_format` |
| `boolean` | `true` or `false` | — |
| `enum` | one of the declared variants | `variants` (required unless `variants_from` expands the set), `variants_from` |
| `image` | a path resolved under the declared `roots`, existence checked | `roots`, `dir`, `extensions` |
| `link` | a `#label` anchor (must exist) or an `http(s)` URL | — |
| `list` | a YAML sequence; every element has the single item type | `item` (one nested field descriptor, which may be an inline `record`), `min_items`, `max_items`, `max_items_from`, `min_items_from` |
| `section-template` | structured YAML data shaped by another section template | `section_template` (the section template name) |

- A **section template used as a field type** carries `type: section-template`
  and `section_template: <name>`; it takes YAML data only. There is no separate
  named record type: an inline record is a list's `item:` descriptor, not a
  field's own `type:`.
- A `list` has exactly one item type, declared in its `item:` descriptor (the
  same field keys as a top-level field). When that descriptor declares
  `type: record` with its own `fields:`, the list is a list of **inline
  records**, validated recursively: `item: {type: record, fields: [...]}`. A
  record's fields use the same descriptors as a template's top-level fields
  (including `nullable`, `coerce` and a nested `list`/`record`), so a record
  field may itself be a list of records.
- `nullable: true` lets any field accept an explicit YAML `null` — or, on a
  record item descriptor, a null list entry. A null value skips every bound,
  format and membership check. Null is **distinct from absent**: a `required`
  nullable field's key must still be present (it may be null), and an explicit
  null never triggers the field's `default`.
- `coerce: true`, on a `text` field, accepts a YAML number or boolean where a
  string is expected and converts it to its string form (for example `42` →
  `"42"`, `true` → `"true"`). A mapping or list is still a type error.
- `max_items_from` and `min_items_from`, on a `list` field, bound its length
  against another list field's item count — at most (respectively at least) the
  named list's count. A leading `/name` names a top-level field of the template;
  a bare name names a sibling field within the same inline record. The named
  peer must itself be a `list` field, and the keys are valid only on a `list`.
- An `image` field resolves its authored path against `roots`, in the written
  order. A root is `assets`, the deck's `assets/` tree (the path is written with
  its `assets/` prefix), or `media`, the `media/` tree of the library layer that
  owns the field's template (the path is written media-relative, with no
  `media/` prefix). With no `roots` the path resolves under `assets/`. `dir`
  optionally restricts the accepted path to a subdirectory within a root and
  `extensions` to a set of file-name extensions (for example `[jpg, png]`). A
  missing file is an error carrying a closest-match suggestion.
- An `enum` field may declare its value set as a glob with `variants_from`
  instead of an inline `variants` set. The glob is written relative to the root
  of the library layer that owns the field's template and names files under that
  layer's `media/` tree (for example `media/bgs/*.jpg`); the field's valid
  values are the matching files, each written media-relative (for example
  `bgs/right-8.jpg`), in sorted order. Declaring `variants` and `variants_from`
  together, or a glob that matches no file, is an error.
- Number formats: `compact` (e.g. `1.25M`), `exact` (e.g. `1,250,000`) and
  `percent` (e.g. `125%`). Date formats: `long` (e.g. `25 September 2026`) and
  `short` (e.g. `25 Sep 2026`). An author selects a format with the reserved
  sibling key `<field>_format`; otherwise the field's `default_format` applies.
- `_format` is a reserved suffix and cannot be used as a field or section name.

### Sections

A `sections:` entry declares a named section:

```yaml
sections:
  - name: people
    accepted: [person]      # section-usage template names, or `@group` references
    min: 0                  # 0 = no minimum
    max: 4                  # 0 or less = unbounded
```

`name` and `accepted` are required; a section may accept only section
templates, never slide templates. An `accepted:` entry may also reference a
named accept-list from the owning layer's `library.yaml` with `@<name>`,
combinable with literal template names (see **Named library definitions**).

A section that accepts its own template — directly, or through a cycle of
declarations — is a **self-nesting** declaration and must carry a positive
`max_depth: N` bound. `max_depth` gives the most times the declaration's
section may recur in one containment chain, so an instance it produces has at
most `N` enclosing instances that resolve to its own template, and each
instance of the chain reads that count as `.item.depth`. A self-nesting or
cyclic acceptance with no `max_depth` is refused at load as a reference cycle,
and a `max_depth` that is not a positive integer is an error naming the
offending `template.yaml`.

`sections:` is accepted in **any** template's manifest — a slide template and,
equally, a section template. A section template that declares `sections:` gives
its own instances nested child sections: a heading one level deeper than the
section heading resolves against that section template's declarations rather
than the slide's (see **Sections**).

### Body rules

Every template has an implied `body` field whose content is the Markdown after
the frontmatter (for a slide) or after the section heading (for a section). A
`body:` key in the content YAML is an error, not a way to set it.

```yaml
body:
  mode: optional        # required | optional | disallowed
  max_words: 120        # 0 = no limit
  max_paragraphs: 3
  max_list_items: 6
  subheadings: false    # whether ##/### subheadings are allowed
```

`mode` is required when the `body:` block is present: `required`, `optional` or
`disallowed`. A section template used as a field type takes data only, so its
body is `disallowed`.

`subheadings` governs the body only where the enclosing template declares no
child sections: there a `##` or deeper heading is an ordinary Markdown
subheading. Where the template declares child sections, every heading at any
depth is a section marker instead (see **Sections**), so no heading is a body
subheading. `#` is always reserved for top-level sections and the reserved
`# notes`.

### example.md

`example.md` is the template's validating example. For a slide template it is a
slide source (frontmatter at line 1, a body and optional `# name` sections);
for a section template it is a section fragment. `kalide templates <name>`
prints it for you to copy.

## layout.html.tmpl

`layout.html.tmpl` is a standard Go `html/template`. It is not Markdown and
there is no Markdown helper.

Context:

- Each declared field is available by name: `{{ .title }}`.
- The body is available as `{{ .body }}`.
- Each section's instances are available by section name.

Values arrive in two forms:

- The **body**, **`text` fields** and **each rendered section** are
  pre-rendered, trusted HTML — insert them unescaped.
- Every **other field** is a typed value (number, date, boolean, enum string,
  list, image/link URL), and `html/template` auto-escapes string values.

Five reserved context names are provided, as **data**, not helpers:

- `.deck` — the deck-wide configuration: `.deck.title` (always present),
  `.deck.author` and `.deck.date` (empty strings when omitted), and
  `.deck.properties` (always a non-nil map). Read a property with
  `{{ index .deck.properties "audience" }}`, or `{{ .deck.properties.audience }}`
  for a plain key. Property values keep their declared type.
- `.slide` — this slide's derived position: `.slide.number` (a **string**, for
  example `"1"` or `"1a"`) and `.slide.total` (an **integer**). Both are
  available in a slide layout and in every nested section template.
- `.item` — present only inside a **section** instance's context, never in a
  slide layout. It describes the instance among its same-name siblings under
  the same parent: `.item.index` (zero-based), `.item.number` (one-based),
  `.item.count`, `.item.first` and `.item.last` (booleans), `.item.depth` (the
  instance's self-nesting depth: how many enclosing instances resolved to the
  same section template, `0` at the outermost instance of that template),
  `.item.section` (the declared section name), `.item.template` (the resolved
  section template name) and `.item.parent` (the enclosing section instance's
  field values, or nil at the top level). A slide layout and a
  `section-template`-typed field value carry no `item` entry. A bounded
  self-nesting declaration (a section that accepts its own template with a
  `max_depth`) reads `.item.depth` at every level, and the same `depth` is on
  each `.data.<section>` entry's `.item`.
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

`media`'s argument is a path relative to the `media/` directory of the layer
that owns the layout. It returns the URL the server serves that file under. The
argument must not be absolute and must not contain `..`; a missing file, an
absolute path or a `..` segment is an error naming the layout's file and line.
`media` never resolves into a theme directory.

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
one of the child's declared sections rejected. A non-literal or forwarded child
is validated at render instead.

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
so a child template living in either library layer is invoked like a local one;
the child's own `media` reference resolves in that child template's own owning
layer. A `child`/`children` call contributes nothing to the caller's own
`.data` or rendered section lists.

`dict k1 v1 …` builds a `map[string]any` from alternating key/value arguments,
for passing structured source values to `section`. Keys must be strings; an odd
argument count, a non-string key or a repeated key is an error.

`list a b …` builds a sequence from its arguments, the layout syntax for a
literal list-typed value.

The built-in number and date format functions (`compact`, `exact`, `percent`,
`long`, `short`) are also available to layouts for rendering.

`deck`, `slide`, `item`, `raw` and `data` are reserved top-level names: a
template may not declare a field or section named `deck`, `slide`, `item`,
`raw` or `data`. `notes`, `body` and the `_format` suffix are reserved too.

### Composing layouts

A layout may include another library layout with the standard Go
`{{ template "<layout name>" . }}` action, so shared markup lives in one layout
that the slides and sections which need it compose. The layout name is the
included layout's **layer-relative path inside its own layer**:

- `slides/<name>/layout.html.tmpl` for a slide layout, and
- `sections/<name>/layout.html.tmpl` for a section layout.

That name is identical on every OS and wherever the layer sits on disk: it has
no layer root, no `templates/` prefix, no backslashes and no absolute path. It is
exactly the name the loader builds and registers each layout under, so the
action names the path literally, never the bare template name. For the deck's
local `hello` slide layout the call is
`{{ template "slides/hello/layout.html.tmpl" . }}`.

A layout may include any layout in its **own layer** by that name. In a
two-layer deck a local-layer layout may also include an **external-layer**
layout by the external layout's layer-relative name: the merged layout namespace
keys every layer's layouts, so the call resolves. The reverse is not allowed —
the external library is validated standalone before the merge, so an
external-layer layout that includes a local-layer layout is a templates error
("no such template") reported at the invoking layout's own on-disk path and
line, quoting the target name exactly as written. An external library may never
depend on the deck's local layer.

The included layout executes in the **invoking** layout's namespace, so its
`media` helper binds to the invoking layer's `media/`: an included layout's
`{{ media "logo.svg" }}` resolves in the layer of the layout that named it, not
in the layer that owns the included layout. A `section` call rebinds to the
called template's own layer instead, so the called template's
`{{ media "logo.svg" }}` resolves in that template's own owning layer. Compose
with the `template` action to share markup; call with `section` when the
target's own layer must supply its media.

## Themes

- `theme` is deck-wide only, never per slide. Omitted means `default`.
- Available themes are exactly the directories under the merged library's
  `themes/` — the `themes/` of every resolved layer. There are **no built-in
  themes**.
- A named (or defaulted) theme the library does not define is an error listing
  the available themes with a closest-match suggestion.
- A theme is a `theme.css` token stylesheet plus optional files it owns.
  Theme-owned files (fonts, logos) are referenced from that theme's CSS and are
  served from the theme directory.
- A theme's `theme.css` — and every CSS it `@import`s, wherever in that layer
  that CSS lives (its own theme directory, another theme, or `media/`) — may
  reference a servable file in that same layer with both `url()` and `@import`,
  in three forms:
  - an unprefixed relative path, resolved from the referencing file's own
    location and confined to that file's own tree: the theme directory for a
    stylesheet in a theme, or `media/` for a stylesheet reached through
    `media:`;
  - the reserved `media:` prefix → that layer's shared `media/` tree,
    for example `url('media:fonts/Inter-Regular.woff2')` or
    `@import url('media:theme.css')`;
  - the reserved `theme:<name>/` prefix → another theme's directory in that
    layer, for example `url('theme:default/theme.css')` or
    `@import url('theme:default/theme.css')`, so a theme may build on another.
- Cross-tree reach happens only through the `media:` and `theme:<name>/`
  prefixes. An unprefixed relative reference stays inside the stylesheet's own
  tree: no absolute paths, no `..` leaving that tree, no other scheme, nothing
  outside that layer, and no references into non-servable entries (`slides/`,
  `sections/`, `library.yaml`, `AGENTS.md`, `AGENTS_KALIDE.md`). The path after
  `media:` is relative
  to that layer's `media/` — the same base the layout `{{ media "path" }}`
  helper uses — and the path after `theme:<name>/` is relative to
  `themes/<name>/`; an unknown `theme:<name>` is an error. Both prefixes work in
  both `url()` and `@import`, and an imported stylesheet's own references are
  checked too. `@import` is followed, bounded and cycle-safe across the whole
  import graph; a missing file, a bad reference or a cycle is an error naming
  the theme CSS file and line.

## Media

- Shared media of any type lives under each resolved layer's `media/` and is
  referenced from layouts with `{{ media "path" }}`.
- The path is relative to that layer's `media/` only: no absolute paths and no
  `..`.
- A theme's CSS — `theme.css` and every CSS it `@import`s — reaches that
  layer's `media/` tree through the reserved `media:` prefix and another
  theme's directory in that layer through the reserved `theme:<name>/` prefix;
  both work in `url()` and `@import`, so a theme may build on another. An
  unprefixed relative reference stays inside the stylesheet's own tree. See
  **Themes**.
- Theme-owned files are not reachable through `media`; reference them from
  `theme.css`.
- File types are not restricted; the content type is derived from the file
  extension.

## Validation and errors

`kalide start` validates the whole deck before serving anything, and
`kalide templates` loads the same merged library. Validation is **fail-fast**:
exactly one error is reported — the first one found. An error states the file,
the line when known, the full path, what is wrong and how to fix it, for
example:

```
slides/3-team.md:12 › column[1] › people[0] › name: required — add a name: value
```

The same text appears in the terminal and on the browser error page. Unknown
names (templates, sections, themes, keys) get a closest-match "did you
mean …?" suggestion.

The resolved layers are merged and checked before anything is served: a slide
or section name (the two share one template namespace), a theme name, or a
shared `media/` path carried by both layers is an error naming the name and
both paths — never an override. The external library is self-contained: every
reference inside an external-layer template or theme must resolve within that
external library alone, so an external library validates standalone. An error
is path-qualified with the layer root it comes from.

## Commands

```
kalide init [path]         Create a deck in the current directory.
kalide init-library <path> Create a new, empty template library at <path>.
kalide config              Read or write a recognised per-user setting.
kalide start [--port N] [--no-open]
                           Validate and serve the deck with live reload.
kalide check [--all] [--json]
                           Validate the deck without serving it.
kalide render [slide] [-o dir] [--full|--fragment]
                           Write a slide's rendered HTML without serving it.
kalide templates [--all] [name]
                           List the merged library's templates, or show one.
kalide upgrade             Refresh kalide-owned files and apply migrations.
kalide version             Print the version (also --version, -v).
kalide help                Show usage.
```

- `kalide init` with no path writes the deck, a starter slide and a minimal
  local `templates/` library. `kalide init <path>` validates the external
  library at `<path>` **alone** (fail-fast) before writing anything, then
  writes a deck referencing it — an empty `slides/`, and no local `templates/`;
  a pre-existing local `templates/` is left untouched and becomes the deck's
  local layer. Both forms write a project-root `AGENTS.md` agent guide **only
  when none exists**; a pre-existing `AGENTS.md` never blocks and is left
  untouched. `init` refuses when `slides/`, `assets/` or `kalide.yaml` already
  exists (a local `templates/` blocks only the no-path form), and there is no
  `--force`.
- `kalide init-library <path>` creates a new, deck-ready library
  (`library.yaml`, empty `slides/`, `sections/` and `media/`, and a minimal
  default theme), refusing if the target already holds one. It also writes a
  root-level `AGENTS.md` library guide only when none exists.
- `kalide start` is the validation gate: run it after every edit. While it
  runs, edits under either resolved layer live-reload; changing the configured
  `templates:` value in `kalide.yaml` does not — the deck page shows a
  restart-required error and keeps serving the startup library until you
  restart `kalide start`.
- `kalide check` validates the whole deck — its resolved library, its theme and
  every slide — **without serving it**: it loads exactly what `kalide start`
  loads and reports the same first error, or every error with `--all` in the
  fixed validation order, exiting 0 when the deck is valid and 1 otherwise.
  `--json` prints a machine-readable report. A library or theme that cannot be
  loaded is reported as the single check error; with `--all` the report still
  ends there, because there is no registry to collect further errors from. Use
  it to validate after every edit when you do not want to start a server.
- `kalide render` writes a slide's rendered HTML **without serving it**: name a
  slide by its position label (`3`, `3a`) or its deck-unique label to print it
  to stdout, or pass `-o <dir>` with no slide to write one
  `<number>[letter]-<label>.html` file per slide into that directory, in deck
  order. `--full` (the default) writes the complete standalone page, whose
  embedded `<section>` is byte-identical to the `--fragment` output;
  `--fragment` writes only the slide's `<section>` markup. The two flags are
  mutually exclusive. Output is deterministic, and `.slide.number` /
  `.slide.total` count across the whole deck.
- `kalide config` reads or writes kalide's recognised per-user settings (the
  sole recognised key is `default-template-dir`); it is a machine setting, not
  deck content.
- `kalide templates` lists the templates in the merged library (both layers);
  `kalide templates --all` additionally lists the templates whose manifest
  declares `internal: true`, which the default listing omits. `kalide templates
  <name>` documents one template whatever its `internal` flag: its fields
  (marking every nullable field `nullable`, with an enum field's variants
  printed beneath the field and any inline-record fields nested under their
  list), its sections, body rules and example.
- `kalide upgrade` brings an existing deck or library in the current directory
  forward: it refreshes the kalide-owned scaffold files to the running binary's
  versions and applies kalide's format migrations, entirely offline. It never
  overwrites author content — a file the author has edited is left untouched and
  reported — takes no path argument and has no `--force`.

## How to work on a deck

1. If the deck was created by an older `kalide`, run `kalide upgrade` first: it
   refreshes kalide-owned scaffold files and applies any format migrations,
   offline, without overwriting your content.
2. Discover the real templates before writing slides: run `kalide templates`,
   then `kalide templates <name>` for the fields, sections, body rules and a
   copyable example. Do not guess template names, field names or themes.
3. Write or edit slide files under `slides/`.
4. Validate with `kalide start`. If it reports a problem, fix exactly that
   first error and run it again.
