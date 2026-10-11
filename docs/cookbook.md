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
`template`, its fields and body, and its own nested `.data`. A `children`
forwarding is not inspected at load; it is checked against the target's
declared sections when the target renders, and the declaration's `min`/`max` is
counted over the forwarded set.

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
