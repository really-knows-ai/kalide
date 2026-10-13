# Cookbook

Copyable recipes for kalide template authors: each is a small layout or YAML
snippet plus the call that uses it. They assume a library at the deck root
(`templates/`); a two-layer deck puts the same files under the external
library's root. For the full reference — the library layout, the `template.yaml`
manifest, the layout context and the helper set — see
[docs/templates.md](templates.md); for how the two layers merge, see
[docs/layered-libraries.md](layered-libraries.md).

Every template directory holds exactly three files: `template.yaml`,
`layout.html.tmpl` and `example.md`. The loader validates each `example.md` like
real content and executes the layout against it, so keep it a minimal, working
sample.

## A footer from the `.slide` context

`.slide.number` (a string, for example `"1"` or `"1a"`) and `.slide.total` (the
integer slide-file count) are available in the slide layout and in every nested
section template. A reusable footer is a plain section template:

`sections/footer/template.yaml`:

```yaml
description: The slide footer.
```

`sections/footer/layout.html.tmpl`:

```html
<footer class="slide-footer">{{ .slide.number }} / {{ .slide.total }}</footer>
```

`sections/footer/example.md`:

```markdown
A slide footer.
```

Call it from any slide layout with the `section` helper:

```html
<section class="hello">
  <h1>{{ .title }}</h1>
  {{ .body }}
  {{ section "footer" }}
</section>
```

- `.slide.number` is a **string** and `.slide.total` is an **integer**; both
  render as text.
- A section rendered by the `section` helper runs with the calling slide's
  deck/slide context, so `.slide` is in scope inside `footer` even though
  `footer` is not a declared child of the slide.
- You can also read the context directly in a slide layout, with no helper:

  ```html
  <div class="position">{{ .slide.number }} / {{ .slide.total }}</div>
  ```

## A nested `child` composition

`child "<section>" [fields] [body] [child…]` renders one instance of a section
declared by the `section` call's target. Nested `child` arguments are the
child's own declared children, so a composition nests to any depth the declared
sections allow.

Declare a `columns` section that accepts two column templates, and have each
column template declare a `cell` child:

`sections/columns/template.yaml`:

```yaml
description: A row of columns.
sections:
  - name: column
    accepted: [half, full]
    min: 0
    max: 3
```

`sections/columns/layout.html.tmpl`:

```html
<div class="columns">{{ range .column }}{{ . }}{{ end }}</div>
```

`sections/half/template.yaml` (and `sections/full/template.yaml`, same shape):

```yaml
description: A half-width column.
fields:
  - name: title
    type: text
sections:
  - name: cell
    accepted: [cell]
    min: 0
    max: 6
```

`sections/half/layout.html.tmpl`:

```html
<div class="column half">
  {{ if .title }}<h2>{{ .title }}</h2>{{ end }}
  {{ .body }}
  {{ range .cell }}{{ . }}{{ end }}
</div>
```

`sections/cell/template.yaml`:

```yaml
description: One cell of content.
body:
  mode: optional
```

`sections/cell/layout.html.tmpl`:

```html
<div class="cell">{{ .body }}</div>
```

Compose them from any slide or section layout. Because `columns` accepts two
templates, each `column` child supplies the `template:` selector in its fields:

```html
{{ section "columns" nil nil
   (child "column" (dict "template" "half" "title" "Left") nil
      (child "cell" nil "Left cell"))
   (child "column" (dict "template" "full" "title" "Right") nil
      (child "cell" nil "Right cell")) }}
```

- The `template` key is the selector among the declaration's `accepted`
  templates; it is required only when a declaration accepts more than one, and
  it is consumed as a selector — it never reaches the instantiated template's
  fields.
- `nil` in the fields or body slot is an omitted argument. The trailing
  arguments are the children, so `{{ section "columns" nil nil (child …) }}`
  supplies no fields and no body.
- The nested `child "cell"` is resolved against the instantiated column
  template (`half` or `full`), which declares `cell`.
- Every instance rendered by the `section`, `child` and `children` helpers is a
  real section instance with its own `.item` descriptor: `.item.index`
  (zero-based), `.item.number`/`.item.count`, `.item.first`/`.item.last`,
  `.item.depth`, `.item.section`, `.item.template` and `.item.parent`. A
  `section` call is a one-item group, so a call from a slide reports `index` 0,
  `count` 1 and `first`/`last` true.
- `.item.depth` counts the enclosing instances whose resolved template repeats
  the instance's own — `0` at a template's outermost instance and `+1` for each
  enclosing same-template instance. A `child`/`children` composition that
  repeats a template — a declaration accepting its own template, directly or
  through a cycle — is **bounded self-nesting** and needs the declaration to
  carry a positive `max_depth: N`; see
  [A self-nesting `timeline-event` section](#a-self-nesting-timeline-event-section).

A minimal `example.md` for each template makes the library load: for `half`, a
section fragment with its field and a body:

````markdown
```
title: Left
```
Half a column.
````

## A wrapper layout forwarding its authored children

`children "<section>" .data.<section>` forwards the caller's **authored**
`.data.<section>` entries as that declared section's children, so a wrapper
layout hands its own authored children to the section it wraps without
re-listing them.

A slide template declares an `item` section (authored as `# item` headings):

`slides/agenda/template.yaml`:

```yaml
description: An agenda slide that wraps its items.
fields:
  - name: title
    type: text
    required: true
sections:
  - name: item
    accepted: [item]
    min: 1
    max: 8
```

`slides/agenda/layout.html.tmpl` forwards its authored items to `wrapper`:

```html
<section class="agenda">
  <h1>{{ .title }}</h1>
  {{ section "wrapper" nil nil (children "item" .data.item) }}
</section>
```

`sections/wrapper/template.yaml` declares the same `item` section, which the
forwarded entries fill:

```yaml
description: Wrap a set of item children.
sections:
  - name: item
    accepted: [item]
    min: 0
    max: 8
```

`sections/wrapper/layout.html.tmpl`:

```html
<div class="wrapper">{{ range .item }}{{ . }}{{ end }}</div>
```

`sections/item/template.yaml`:

```yaml
description: One agenda item.
fields:
  - name: title
    type: text
    required: true
body:
  mode: optional
```

`sections/item/layout.html.tmpl`:

```html
<article class="item"><h2>{{ .title }}</h2>{{ .body }}</article>
```

The author writes the items once, on the slide:

```markdown
---
template: agenda
title: Agenda
---

# item
```
title: Welcome
```

Each forwarded entry re-instantiates as a real child, keeping its own
`template`, its fields and body, its own `.item` — including the reserved
`.item.depth` — and its own nested `.data`. A `children` forwarding is not
inspected at load; it is checked against the target's declared sections when the
target renders, and the declaration's `min`/`max` is counted over the forwarded
set.

## Reading the `.item` context

Every section instance — authored as a section heading, or rendered by the
`section`, `child` or `children` helper — sees its own `.item` descriptor among
its same-name siblings. A numbered crumb layout stamps its position and depth:

`sections/crumb/template.yaml`:

```yaml
description: One numbered crumb in a trail.
fields:
  - name: label
    type: text
    required: true
```

`sections/crumb/layout.html.tmpl`:

```html
<span class="crumb" data-index="{{ .item.index }}" data-number="{{ .item.number }}" data-count="{{ .item.count }}" data-first="{{ .item.first }}" data-last="{{ .item.last }}" data-depth="{{ .item.depth }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}">{{ .label }}</span>
```

`sections/crumb/example.md`:

````markdown
```
label: Introduction
```
A crumb.
````

- `.item.index` (zero-based) and `.item.number` (one-based) are the instance's
  position among its same-name siblings, with `.item.count`, `.item.first` and
  `.item.last`.
- `.item.section` is the declared section name and `.item.template` the resolved
  template name; `.item.parent` is the enclosing section instance's field
  values, or nil at the top level, where the parent is the slide.
- `.item.depth` is the instance's self-nesting depth: how many enclosing
  instances resolved to the same template as this one. It is `0` for an ordinary
  instance and rises only where a template's resolved name repeats up the
  containment chain — see
  [A self-nesting `timeline-event` section](#a-self-nesting-timeline-event-section).

The authored tree carries the same descriptor under `.data`: `.data.<section>`
has one entry per authored instance, each with its own `.raw`, `.item` and
`.data`, and every entry's `.item.depth` matches that instance's rendered
`.item.depth`.

## A self-nesting `timeline-event` section

A section declaration may accept a template that leads back to the declaring
template — directly, or through a cycle of declarations — when it carries a
positive `max_depth: N`. The instance the declaration produces then has at most
`N` enclosing instances that resolve to its own template, and each instance
reads that count as `.item.depth`. A self-nesting (or cyclic) acceptance with no
`max_depth` is refused at load as a reference cycle.

`sections/timeline-event/template.yaml` declares a `loop` section accepting the
template's own name, bounded so an event may hold nested events two levels deep:

```yaml
description: One event on a timeline, nestable into sub-events.
fields:
  - name: date
    type: text
    required: true
  - name: title
    type: text
    required: true
body:
  mode: optional
sections:
  - name: loop
    accepted: [timeline-event]
    max_depth: 2
```

`sections/timeline-event/layout.html.tmpl` renders the event and its `loop`
children — each a `timeline-event` again — through the section the template
accepts, and tags the instance with its depth:

```html
<li class="event" data-depth="{{ .item.depth }}">
  <time class="event-date">{{ .date }}</time>
  <h3 class="event-title">{{ .title }}</h3>
  {{ .body }}
  {{ if .loop }}<ul class="event-nested">{{ range .loop }}{{ . }}{{ end }}</ul>{{ end }}
</li>
```

`sections/timeline-event/example.md` is a section fragment: its own fields and
body, then one nested `# loop` instance:

````markdown
```
date: 2026-01
title: Kickoff
```
The project starts.

# loop
```
date: 2026-02
title: Design freeze
```
````

A slide template declares an `event` section accepting `timeline-event`:

`slides/timeline/template.yaml`:

```yaml
description: A timeline of events.
fields:
  - name: title
    type: text
    required: true
sections:
  - name: event
    accepted: [timeline-event]
    min: 1
    max: 0
```

`slides/timeline/layout.html.tmpl`:

```html
<section class="timeline">
  <h1>{{ .title }}</h1>
  <ul class="timeline-list">{{ range .event }}{{ . }}{{ end }}</ul>
</section>
```

`slides/timeline/example.md` is a slide source like any slide template's:

````markdown
---
template: timeline
title: Example timeline
---
# event
```
date: 2026-01
title: Kickoff
```
````

The author nests events with headings: a `# event` section, then a `## loop`
child one level deeper, then a `### loop` grandchild:

````markdown
---
template: timeline
title: Release timeline
---
# event
```
date: 2026-01
title: Kickoff
```
## loop
```
date: 2026-02
title: Design freeze
```
### loop
```
date: 2026-03
title: Beta
```
````

- All three instances resolve to `timeline-event`, so their `.item.depth` is
  `0`, `1` and `2` — the count of enclosing instances whose resolved template
  repeats. `max_depth: 2` is the deepest an instance of the declaration may go,
  so a fourth-level `#### loop` is refused with
  `section "loop": nesting deeper than max_depth 2`.
- Each instance also reads `.item.section` (`event` or `loop`), its sibling
  `.item.index`/`.item.number`/`.item.count` and `.item.parent` (the enclosing
  instance's field values), and every `.data.loop` entry carries the same
  `depth`.
- Authored nesting is written as headings (`#` through `######`), so a
  self-nesting declaration's `max_depth` must leave room for the heading levels
  above it: the loader walks the longest chain the bound allows and refuses the
  declaration at load if that chain would exceed the six-heading-level syntax.

## A table in a body

A template opts into GFM tables in its body with the `tables` key of its
`body:` block; the body's non-table content stays under `{{ .body }}` and each
parsed table arrives under `.table`/`.tables`. A metrics slide accepts one table
of at most four columns and six body rows, and declares a `highlight` enum and a
`bold` boolean cell attribute:

`slides/metrics/template.yaml`:

```yaml
description: A slide whose body carries one table.
fields:
  - name: title
    type: text
    required: true
body:
  mode: optional
  tables:
    min_tables: 0        # fewest tables in a body; 0 = no minimum
    max_tables: 1        # most tables in a body; 0 = unbounded; default 1
    min_columns: 2       # fewest columns, header included
    max_columns: 4       # most columns, header included; 0 = unbounded
    min_rows: 1          # fewest body rows, the header excluded
    max_rows: 6          # most body rows, the header excluded; 0 = unbounded
    cell_attributes:
      - name: highlight  # an enum cell attribute
        type: enum
        variants: [none, green, amber]
      - name: bold       # a boolean cell attribute
        type: boolean
```

`slides/metrics/layout.html.tmpl` renders `.table` with no guard: the entry is
always present, nil only when the body carries no table.

```html
<section class="metrics">
  <h1>{{ .title }}</h1>
  <table>
    <thead>
      <tr>{{ range .table.headers }}<th>{{ .html }}</th>{{ end }}</tr>
    </thead>
    <tbody>
      {{ range .table.rows }}
      <tr>{{ range . }}<td{{ with index .attrs "highlight" }} class="highlight-{{ . }}"{{ end }}>{{ .html }}</td>{{ end }}</tr>
      {{ end }}
    </tbody>
  </table>
  {{ .body }}
</section>
```

`slides/metrics/example.md` is a real slide source; each cell is inline Markdown
plus the `{br}` and `{…}` extensions, and the table sits in the body where the
author writes it:

```markdown
---
template: metrics
title: Key metrics
---
| Metric | This week | Trend               |
| :----- | --------: | ------------------: |
| Reads  | 1,204     | {green} up          |
| Writes | 88        | Steady{br}no change |
```

- A table is an **opt-in**: without a `tables` mapping (or with `tables: false`)
  a body's table is refused with guidance naming the `tables` option, and a
  `tables:` value that is neither a mapping nor `false`, or an unknown sub-key,
  fails template load naming the offending `template.yaml`.
- `min_tables`/`max_tables` bound the tables in one body, and
  `min_columns`/`max_columns` (header included) and `min_rows`/`max_rows` (body
  rows only) bound one table. An explicit `0` on a `max_*` key is unbounded and a
  `0` on a `min_*` key is no minimum; a breach is an error naming the limit and
  the actual count.
- The accepted table is GFM's: a header row, a delimiter row of at least one `-`
  per column (written `:---`, `---:` or `:---:` for left, right or centre
  alignment), then the body rows. Leading and trailing `|` are optional and every
  cell is trimmed, `\|` is a literal pipe, and column counts are strict — a body
  or delimiter row whose cell count differs from the header's is an error, never
  a paragraph.
- `cell_attributes` declares each attribute's `name` and a `type` of `boolean` or
  `enum` (an enum carrying a non-empty `variants` list). `{bold}` is
  `{bold:true}`, `{highlight:green}` sets the enum, `{highlight:green, bold}`
  sets both, and the bare variant `{green}` sets the one declared enum carrying
  it. A `\{` is a literal brace and a brace inside an inline code span or a link
  is literal text; an undeclared attribute — or a value of the wrong type — is an
  error, the former with a closest-match suggestion. `br` is reserved and is
  never a declared cell attribute.
- `{br}` is a line break, rendered as `<br>`. The `{…}` group sits outside an
  inline code span and a link (by convention at the cell's end) and is stripped
  from the cell's visible content.
- Each cell is a map with `.html` — the cell's inline Markdown rendered, `{br}`
  as `<br>` — and `.attrs`, keyed by the declared names. An unset attribute is
  nil, so `{{ index .attrs "highlight" }}` is empty: guard it with `with`/`if` as
  above. Header cells carry the same `{html, attrs}` shape under
  `.table.headers`.
- `.tables` is every parsed table in source order (an empty list when the body
  carries none) and `.table` is the body's first table (nil when there is none);
  both are always present, so a layout reads them without a guard. Each table
  appears **only** through these entries: `{{ .body }}` is the body's non-table
  content, so the layout places the table itself.

## Theme `media:` and `theme:<name>/` references

A theme's `theme.css` — and every CSS it `@import`s — references servable
library files in three forms: an unprefixed relative path (inside the
stylesheet's own tree), the reserved `media:` prefix (the library's shared
`media/`), and the reserved `theme:<name>/` prefix (another theme's directory).

`themes/acme/theme.css`:

```css
@import url('theme:base/theme.css');

@font-face {
  font-family: "Acme";
  src: url('media:fonts/Acme-Regular.woff2') format('woff2');
}

.acme-logo {
  background-image: url('logo.svg');
}
```

- `url('logo.svg')` is the theme's own file, `themes/acme/logo.svg`. An
  unprefixed relative path never leaves the stylesheet's own tree — no absolute
  paths and no `..` out of that tree.
- `url('media:fonts/Acme-Regular.woff2')` reaches the library's shared
  `media/`, the same base the layout `{{ media "fonts/Acme-Regular.woff2" }}`
  helper uses.
- `@import url('theme:base/theme.css')` reaches another theme's directory,
  `themes/base/`, so a theme can build on another. An unknown `theme:<name>`
  is an error.
- Both prefixes work in `url()` and `@import`. An imported stylesheet's own
  references are checked too, and a bad reference or a cycle is an error naming
  the theme CSS file and its line.
- Theme-owned files are **not** reachable through the layout `media` helper; a
  theme references its own fonts and logos from its CSS.

In a two-layer deck, a theme's `media:` and `theme:<name>/` references resolve
in that theme's **own** layer, never in the other layer
([docs/layered-libraries.md](layered-libraries.md)).

## See also

- [docs/templates.md](templates.md) — the template-author reference.
- [docs/layered-libraries.md](layered-libraries.md) — the two-layer how-to.
