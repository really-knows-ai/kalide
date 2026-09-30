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

The library is either:

- the deck's **local `templates/` directory** — what a no-argument
  `kalide init` creates; or
- an **external library** named by the deck's `templates:` key in
  `kalide.yaml` (an absolute path, or a path relative to the deck root such as
  `../shared-lib`) — what `kalide init <path>` creates.

The format is identical in both cases; only where the library lives differs.
Two consequences of the external form:

- `kalide init <path>` writes an **empty `slides/`** — no starter slide — and
  no local `templates/`.
- A configured `templates:` path is authoritative: when the key is present and
  non-empty the local `templates/` directory is ignored entirely. A relative
  path is resolved against the deck root; an absolute path is used as given.

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
  directory under the resolved library's `themes/`.
- `navigation` — `default`, `linear` or `grid`; omitted means `default`.
- `properties` — an arbitrary mapping of author keys to typed scalars (string,
  number, boolean, or a `YYYY-MM-DD` date). A mapping or list value is an
  error. The map is always available to templates.
- `templates` — path to the deck's external template library. Omitted means the
  local `templates/` directory. Relative paths resolve against the deck root;
  absolute paths are used as given.

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

```markdown
# people
```
name: Ada Lovelace
role: Mathematician
```
Ada is the first programmer.
```

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

The resolved library is a directory with this fixed layout:

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
  `slides/` and `sections/` together.
- Every template directory holds exactly `template.yaml`, `layout.html.tmpl`
  and `example.md`.
- `themes/<name>/` holds `theme.css` plus any fonts, logos or other files the
  theme owns.
- `media/` is optional shared media of any type.
- A root-level `AGENTS.md` (a library and template-author guide) is ignored, as
  is any other top-level entry that is not part of the library (`library.yaml`,
  `slides/`, `sections/`, `themes/`, `media/`) — for example a `.git/`
  directory or a `README.md`. A library that is also a git repo therefore still
  loads; ignored entries are never parsed, validated, rendered, listed or
  served.

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
sections:
  - name: people
    accepted: [person]
    min: 0
    max: 4
body:
  mode: optional
  max_words: 120
```

Accepted top-level keys: `description`, `fields`, `sections`, `body`. (`name`
is accepted but ignored — the name is the directory name.) Any other key is an
error with a suggestion.

### Field types

Every field entry has `name` and `type`; a field may also set `required`,
`default`, `description`, and the limits for its type.

| type | value written in the frontmatter | extra keys |
|---|---|---|
| `text` | inline Markdown (bold, italic, inline code, `[]()` links, hard breaks) | `max_length` (characters after Markdown is stripped), `plain` (strip styles silently) |
| `number` | a plain YAML number, e.g. `1250000` or `0.12`; a quoted value is a type error | `min`, `max`, `formats`, `default_format` |
| `date` | `YYYY-MM-DD` | `min_date`, `max_date`, `formats`, `default_format` |
| `boolean` | `true` or `false` | — |
| `enum` | one of the declared variants | `variants` (required) |
| `image` | a path under `assets/`, existence checked | — |
| `link` | a `#label` anchor (must exist) or an `http(s)` URL | — |
| `list` | a YAML sequence; every element has the single item type | `item` (one nested field descriptor), `min_items`, `max_items` |
| `section-template` | structured YAML data shaped by another section template | `section_template` (the section template name) |

- A **section template used as a field type** carries `type: section-template`
  and `section_template: <name>`; it takes YAML data only. There are no
  separate record types.
- A `list` has exactly one item type, declared in its `item:` descriptor (the
  same field keys as a top-level field).
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
    accepted: [person]      # one or more section-usage template names
    min: 0                  # 0 = no minimum
    max: 4                  # 0 or less = unbounded
```

`name` and `accepted` are required; a section may accept only section
templates, never slide templates.

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
  subheadings: false    # whether ## / ### are allowed
```

`mode` is required when the `body:` block is present: `required`, `optional` or
`disallowed`. A section template used as a field type takes data only, so its
body is `disallowed`.

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

Two reserved context names are always provided, as **data**, not helpers:

- `.deck` — the deck-wide configuration: `.deck.title` (always present),
  `.deck.author` and `.deck.date` (empty strings when omitted), and
  `.deck.properties` (always a non-nil map). Read a property with
  `{{ index .deck.properties "audience" }}`, or `{{ .deck.properties.audience }}`
  for a plain key. Property values keep their declared type.
- `.slide` — this slide's derived position: `.slide.number` (a **string**, for
  example `"1"` or `"1a"`) and `.slide.total` (an **integer**). Both are
  available in a slide layout and in every nested section template.

The v1 layout **helper** set is exactly `media`:

```
{{ media "logo.svg" }}
```

`media`'s argument is a path relative to the resolved library's `media/`
directory. It returns the URL the server serves that file under. The argument
must not be absolute and must not contain `..`; a missing file, an absolute
path or a `..` segment is an error naming the layout's file and line. `media`
never resolves into a theme directory.

The built-in number and date format functions (`compact`, `exact`, `percent`,
`long`, `short`) are also available to layouts for rendering.

`deck` and `slide` are reserved top-level names: a template may not declare a
field or section named `deck` or `slide`. `notes`, `body` and the `_format`
suffix are reserved too.

## Themes

- `theme` is deck-wide only, never per slide. Omitted means `default`.
- Available themes are exactly the directories under the resolved library's
  `themes/`. There are **no built-in themes**.
- A named (or defaulted) theme the library does not define is an error listing
  the available themes with a closest-match suggestion.
- A theme is a `theme.css` token stylesheet plus optional files it owns.
  Theme-owned files (fonts, logos) are referenced from that theme's CSS and are
  served from the theme directory.
- A theme's `theme.css` — and every CSS it `@import`s, wherever in the resolved
  library that CSS lives (its own theme directory, another theme, or `media/`) —
  may reference a servable library file with both `url()` and `@import`, in
  three forms:
  - an unprefixed relative path, resolved from the referencing file's own
    location and confined to that file's own tree: the theme directory for a
    stylesheet in a theme, or `media/` for a stylesheet reached through
    `media:`;
  - the reserved `media:` prefix → the resolved library's shared `media/` tree,
    for example `url('media:fonts/Inter-Regular.woff2')` or
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

- Shared media of any type lives under the resolved library's `media/` and is
  referenced from layouts with `{{ media "path" }}`.
- The path is relative to `media/` only: no absolute paths and no `..`.
- A theme's CSS — `theme.css` and every CSS it `@import`s — reaches the same
  `media/` tree through the reserved `media:` prefix and another theme's
  directory through the reserved `theme:<name>/` prefix; both work in `url()`
  and `@import`, so a theme may build on another. An unprefixed relative
  reference stays inside the stylesheet's own tree. See **Themes**.
- Theme-owned files are not reachable through `media`; reference them from
  `theme.css`.
- File types are not restricted; the content type is derived from the file
  extension.

## Validation and errors

`kalide start` validates the whole deck before serving anything, and
`kalide templates` loads the same library. Validation is **fail-fast**: exactly
one error is reported — the first one found. An error states the file, the line
when known, the full path, what is wrong and how to fix it, for example:

```
slides/3-team.md:12 › column[1] › people[0] › name: required — add a name: value
```

The same text appears in the terminal and on the browser error page. Unknown
names (templates, sections, themes, keys) get a closest-match "did you
mean …?" suggestion.

## Commands

```
kalide init [path]         Create a deck in the current directory.
kalide init-library <path> Create a new, empty template library at <path>.
kalide start [--port N] [--no-open]
                           Validate and serve the deck with live reload.
kalide templates [name]    List the resolved library's templates, or show one.
kalide upgrade             Refresh kalide-owned files and apply migrations.
kalide version             Print the version (also --version, -v).
kalide help                Show usage.
```

- `kalide init` with no path writes the deck, a starter slide and a minimal
  local `templates/` library. `kalide init <path>` writes a deck referencing
  the external library at `<path>` (an empty `slides/`, and no local
  `templates/`) after validating it fail-fast. Both forms write a project-root
  `AGENTS.md` agent guide **only when none exists**; a pre-existing `AGENTS.md`
  never blocks and is left untouched. `init` refuses when `slides/`, `assets/`
  or `kalide.yaml` already exists (a local `templates/` blocks only the no-path
  form), and there is no `--force`.
- `kalide init-library <path>` creates a new, deck-ready library
  (`library.yaml`, empty `slides/`, `sections/` and `media/`, and a minimal
  default theme), refusing if the target already holds one. It also writes a
  root-level `AGENTS.md` library guide only when none exists.
- `kalide start` is the validation gate: run it after every edit.
- `kalide templates` lists the templates in the resolved library;
  `kalide templates <name>` documents one template's fields, sections, body
  rules and example.
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
