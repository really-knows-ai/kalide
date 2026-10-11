# Layered template libraries

A deck renders against one **merged library** built from up to two **layers**:

- the deck's **local layer** — the `templates/` directory at the deck root,
  loaded whenever that directory exists; and
- the deck's **external layer** — the library named by the top-level
  `templates:` key in `kalide.yaml` (an absolute path, or a path relative to
  the deck root such as `../shared-lib`).

Both layers load when both are present, with equal standing. This page is the
how-to for a deck that uses both. For the library layout, the `template.yaml`
manifest, the layout execution context and the helper set, see
[docs/templates.md](templates.md); for copyable recipes, see
[docs/cookbook.md](cookbook.md).

## The two layers

A deck that sets `templates: ../shared-lib` **and** carries its own
`templates/` directory has two layers:

```
my-deck/
  kalide.yaml              # templates: ../shared-lib
  slides/
   1-title.md
  templates/               # the deck's LOCAL layer
    library.yaml
    slides/<name>/...
    sections/<name>/...
    themes/<name>/...
    media/
../shared-lib/             # the deck's EXTERNAL layer
  library.yaml
  slides/<name>/...
  sections/<name>/...
  themes/<name>/...
  media/
```

```yaml
# my-deck/kalide.yaml
title: My presentation
templates: ../shared-lib
```

- A **relative** `templates:` path is resolved against the deck root; an
  **absolute** path is used as given.
- The format is identical in both layers — only where the library lives
  differs.
- A deck with a `templates:` key but no deck-root `templates/` directory has
  only the external layer. A deck with no `templates:` key has only the local
  layer, loaded from `templates/` when it exists.
- `kalide init <path>` validates the external library **alone** before writing
  anything, then writes a deck that references it (`templates: <path>`, an
  empty `slides/`, and no local `templates/`). A pre-existing local
  `templates/` is left untouched and becomes the deck's local layer.

## Merge: equal standing, no precedence

The merged library is the union of the two layers over three namespaces:

- the **template** namespace, shared by slide and section names (a slide name
  and a section name may not collide, in one layer or across the two);
- the **theme** namespace; and
- the **`media/` path** namespace.

There is **no precedence and no overriding**: neither layer wins, and no name
from one layer is silently replaced by the same name in the other. The two
layers are loaded and merged in a fixed order — the external `templates:` layer
first, then the deck-root local layer — but that order only fixes which
collision is reported first; it never resolves one.

A name carried by both layers is a **validation error naming the name and the
path that carries it in each layer**, never an override. The three namespaces
report separately:

- a slide or section name: `template "<name>" is declared in both <path A> and
  <path B>`;
- a theme name: `theme "<name>" is declared in both <path A> and <path B>`; and
- a shared `media/` path: `media path "<path>" is declared in both <path A>
  and <path B>`.

Each path is qualified with its own layer root, so the error always says where
the two copies live. `kalide start` validates the merged library before serving
anything, so a collision is caught before the deck is served.

## References resolve in the layer that owns them

- A layout's `{{ media "path" }}` resolves in the **owning layer's** `media/`
  tree — the layer the layout was loaded from.
- A theme's reserved `media:` and `theme:<name>/` CSS references resolve inside
  that theme's **own layer**.
- The `section` helper, by contrast, resolves against the **merged** section
  namespace, so a local-layer layout may call an external-layer section exactly
  as if it were local:

  ```
  {{ section "shared-badge" }}
  ```

  The called template then executes with its **own** layer's `media`, so its
  `{{ media "badge.svg" }}` resolves in the external layer, not the local one.

The external library is **self-contained and validated standalone**. `kalide
init <path>` validates it as its own root before writing a deck, and `kalide
start` validates it as a standalone root before the merge. Every reference
inside its templates and themes must therefore resolve within that external
library alone. A consequence runs one way only: a local-layer template may call
an external-layer section, but the external library **cannot** call a section
that exists only in the deck's local layer — that is a templates error even
though the name resolves in the merged deck.

Because the external library is validated standalone, its theme names must not
collide with the local layer, and its `media:` / `theme:<name>/` references
never reach the deck's local layer.

## Composing layouts across layers

A layout may include another library layout with the standard Go
`{{ template "<layout name>" . }}` action. The included layout is named by its
**root-qualified path inside its own layer**:

- `<layer root>/slides/<name>/layout.html.tmpl` for a slide layout, and
- `<layer root>/sections/<name>/layout.html.tmpl` for a section layout,

where `<layer root>` is the directory that layer was loaded from: the deck-root
`templates/` for the local layer, or the external library's own root. The
action names that path literally, never the bare template name. For the deck's
local `hello` slide layout the call is

```
{{ template "templates/slides/hello/layout.html.tmpl" . }}
```

- A layout may include any layout in its **own layer** by that root-qualified
  name.
- A **local-layer** layout may additionally include an **external-layer**
  layout, by the external layer's own root-qualified name: the merged layout
  namespace registers every layer's layouts, so the call resolves.
- The reverse is **not allowed**. The external library is validated standalone
  before the merge, so an external-layer layout whose `{{ template }}` names a
  layout that exists only in the deck's local layer is a templates error
  (`no such template`) reported at the external layout's own file.

The included layout executes in the **invoking** layout's namespace, so its
`{{ media "path" }}` resolves in the **invoking** layer's `media/` — the layer
of the layout that named it, not the layer that owns the included layout. To
have the target's own layer supply its media, call it with `section` instead:
a `section` call rebinds to the called template's own layer (see
[References resolve in the layer that owns them](#references-resolve-in-the-layer-that-owns-them)).
Compose with `template` to share markup; call with `section` when the target's
own library must supply its media.

## Changing `templates:` needs a restart

While `kalide start` is running, edits under either resolved layer root — the
external `templates:` layer and the deck-root `templates/` local layer —
live-reload on the next change, with no restart.

Changing the configured `templates:` value in `kalide.yaml` is **not applied
live**. The deck page shows the standard full-page error naming the `templates:`
key and reading "restart kalide start to use the new template library", and the
server keeps serving the library it started with until you restart
`kalide start`. A retarget is any difference in the raw value as written —
a different path, adding the key where it was absent, removing it where it was
set, or even two spellings of the same directory.

## See also

- [docs/templates.md](templates.md) — the template-author reference: library
  layout, `template.yaml`, layout context and helper set.
- [docs/cookbook.md](cookbook.md) — copyable layout and YAML recipes.

<!-- -->
