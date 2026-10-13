<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="brand/logo_dark.svg">
    <img src="brand/logo_light.svg" alt="kalide logo" width="112" height="112">
  </picture>
</p>

<h1 id="kalide" align="center">kalide</h1>

`kalide` turns a folder of Markdown files into a slide deck and shows it in your
browser. You write slides as plain text, run one command, and present — with the
browser updating live as you edit.

Everything needed to render and present is built into the program. It works
offline, and there is no separate tool to install or run alongside it.

- [About the name](#about-the-name)
- [Install](#install)
  - [Homebrew (macOS)](#homebrew-macos)
  - [Scoop (Windows)](#scoop-windows)
  - [Manual download](#manual-download)
  - [Upgrading the kalide binary](#upgrading-the-kalide-binary)
  - [Upgrading a project](#upgrading-a-project)
- [Supported computers](#supported-computers)
- [Getting started](#getting-started)
- [Commands](#commands)
  - [`kalide init`](#kalide-init)
  - [`kalide init-library`](#kalide-init-library)
  - [`kalide config`](#kalide-config)
  - [`kalide start`](#kalide-start)
  - [`kalide check`](#kalide-check)
  - [`kalide render`](#kalide-render)
  - [`kalide templates`](#kalide-templates)
  - [`kalide templates <name>`](#kalide-templates-name)
  - [`kalide upgrade`](#kalide-upgrade)
  - [`kalide version`](#kalide-version)
  - [`kalide help`](#kalide-help)
- [How a deck is laid out](#how-a-deck-is-laid-out)
- [The templates/ library](#the-templates-library)
  - [Layered libraries](#layered-libraries)
  - [The reserved template context](#the-reserved-template-context)
  - [Layout helpers](#layout-helpers)
  - [Composing layouts](#composing-layouts)
  - [Author guides](#author-guides)
- [What `kalide init` refuses to do](#what-kalide-init-refuses-to-do)
- [Working offline](#working-offline)
- [Making a PDF](#making-a-pdf)
- [Examples](#examples)
- [Install details](#install-details)
- [License](#license)
- [Changelog](#changelog)

## About the name

kalide comes from the Greek *kalón* (καλόν) — the ideal of the beautiful and
the good — joined with *slide*. It is pronounced like "collide"
(kuh-LIDE), and the name is always written in lowercase: kalide.

kalide is not quite as pedantic as the ancient Greeks, but slides look better
when they're consistent.

## Install

### Homebrew (macOS)

```sh
brew tap really-knows-ai/kalide https://github.com/really-knows-ai/kalide
brew trust really-knows-ai/kalide
brew install kalide
```

The tap lives inside the main `kalide` repository, so there is **no separate tap
repository** to add, and the same tap serves Apple silicon and Intel Macs.
Homebrew 6.0.0 and later requires explicitly trusting a third-party tap before
installing from it. No GitHub account or token is needed.

### Scoop (Windows)

```powershell
scoop bucket add kalide https://github.com/really-knows-ai/kalide
scoop install kalide
```

The bucket lives inside the main `kalide` repository, so there is **no separate
bucket repository** to add. It covers both 64-bit Intel/AMD and ARM Windows. No
GitHub account or token is needed.

### Manual download

Download the file for your computer from the
[releases page](https://github.com/really-knows-ai/kalide/releases). This is the
route on Linux (there is no package-manager channel), and a fallback on macOS
and Windows.

`kalide` is a **single file**. To install it, copy that one file somewhere on
your computer and run it. There is no installer, no runtime to install first,
and no setup step.

- On **macOS**: copy `kalide-darwin-arm64` (Apple silicon) or
  `kalide-darwin-amd64` (Intel) to a folder on your `PATH` (for example
  `/usr/local/bin/kalide`), then run it in a terminal.
- On **Windows**: copy `kalide-windows-amd64.exe` (or
  `kalide-windows-arm64.exe`) to a folder on your `PATH` and run it in a
  terminal (PowerShell or Command Prompt).
- On **Linux**: copy `kalide-linux-amd64` or `kalide-linux-arm64` to a
  folder on your `PATH` (for example `/usr/local/bin/kalide`), `chmod +x`
  it, then run it in a terminal.

The file is self-contained: the reveal.js runtime and everything needed to
serve a deck are inside it. Slide and section templates, themes, fonts, logos
and any other media are **not** built into the binary at all — there is no
built-in fallback of any kind. Every one of those comes from the project's
own `templates/` library (see [The templates/ library](#the-templates-library)
below); `kalide init` seeds a small unbranded starter library to get you
going.

### Upgrading the kalide binary

Upgrading the **program** is a different thing from upgrading a **project**
(below). Use your install channel:

- **Homebrew:** `brew update && brew upgrade kalide`
- **Scoop:** `scoop update; scoop update kalide`
- **Manual download:** download the new release asset for your platform and
  replace the old file.

`kalide` never checks for updates by itself, and it never upgrades itself.

### Upgrading a project

`kalide upgrade` is a different thing entirely: it brings an existing deck or
template library in the current directory forward to the running `kalide`. It
runs entirely **offline**, takes **no path** and has **no `--force`**, and it
**never overwrites author content** — a file you have edited is left untouched
and reported, and a deleted starter file stays deleted. See
[`kalide upgrade`](#kalide-upgrade) below, and [`INSTALL.md`](INSTALL.md) for
the full install and upgrade guide.

Full install, upgrade and uninstall steps for every platform are in
[`INSTALL.md`](INSTALL.md). Release notes for every version are in
[`CHANGELOG.md`](CHANGELOG.md); the license is in [`LICENSE`](LICENSE).

## Supported computers

| Your computer | Supported |
|---|---|
| macOS on Apple silicon (M-series) | Yes — `darwin/arm64` |
| macOS on Intel | Yes — `darwin/amd64` |
| Windows on 64-bit Intel/AMD | Yes — `windows/amd64` |
| Windows on ARM | Yes — `windows/arm64` |
| Linux on 64-bit Intel/AMD | Yes — `linux/amd64` |
| Linux on ARM64 | Yes — `linux/arm64` |

All six targets are built and released for every version. Each is published
only while its build machine is available, so any one of them may
occasionally be missing from a given release.

## Getting started

Open a terminal, change to the folder where you want your presentation to live,
and run:

```
kalide init
```

This creates a small starter deck. Then:

```
kalide start
```

`kalide start` checks the deck for problems, serves it on your own computer
(`127.0.0.1`), and opens it in your default browser. Edit any slide file and
save — the browser refreshes by itself. Press **Ctrl+C** in the terminal to stop
(Ctrl+Break also works on Windows).

## Commands

### `kalide init`

Creates a starter deck in the **current folder**:

```
kalide init [path] [--local]
```

With no arguments it writes the built-in starter deck and its local
`templates/` library:

```
AGENTS_KALIDE.md
AGENTS.md
kalide.yaml
slides/1-hello.md
templates/library.yaml
templates/slides/hello/template.yaml
templates/slides/hello/layout.html.tmpl
templates/slides/hello/example.md
templates/themes/default/theme.css
assets/
```

The created-files report `kalide init` prints uses your operating system's
native separators — for example `slides\1-hello.md` and
`templates\library.yaml` on Windows, and `slides/1-hello.md` and
`templates/library.yaml` on macOS and Linux.

`AGENTS_KALIDE.md` is a plain-text guide for coding agents that documents the
deck and template format, so an agent can help author slides without guessing.
`AGENTS.md` beside it is a short pointer to that guide. Both are written only
when absent, are preserved or extended rather than overwritten, and are inert:
they are not part of the deck and do not affect `kalide start`. See
[Author guides](#author-guides) for the exact scheme.

The `templates/` folder it creates is a minimal, unbranded starter
library — one `hello` slide template and one `default` theme — not a
finished design system. Run it once in an empty folder and then edit the
slides, or add templates and themes of your own (see
[The templates/ library](#the-templates-library)).

The template library the deck renders against is resolved **first match wins**:

1. an explicit `[path]` argument — an existing template library the deck
   references;
2. the configured `default-template-dir` (see
   [`kalide config`](#kalide-config) below);
3. the built-in starter deck, the minimal library embedded in `kalide`.

With an explicit `[path]` or a configured `default-template-dir`, `kalide init`
writes a deck that **references** that library rather than seeding a local one:
`kalide.yaml` carries `templates: <path>`, there is no local `templates/`, and
there is no starter slide. The library is validated **before anything is
written** — a path that is missing, is not a valid library, or does not define
the deck's `default` theme stops with an error naming the path and writes
nothing. An explicit relative `[path]` is stored as written and resolved against
the deck root; the configured default is stored as an absolute path. On success
`kalide init` names the library the deck now references.

`--local` forces the built-in starter deck even when a default is configured,
so it never reads the setting. `--local` together with a `[path]` is a usage
error (exit code 2, nothing written). There is no `--force`. See
[what it refuses to do](#what-kalide-init-refuses-to-do) below.

### `kalide init-library`

Creates a new, deck-ready template library at a required `<path>`:

```
kalide init-library <path>
```

```
Created a template library at <path>:
  library.yaml
  AGENTS_KALIDE.md
  AGENTS.md
  slides/
  sections/
  media/
  themes/default/theme.css
```

`kalide init-library` prints these paths with your operating system's native
separators — for example `themes\default\theme.css` on Windows, and
`themes/default/theme.css` on macOS and Linux.

The library is valid and ready to host a deck, but holds nothing beyond a
minimal `default` theme and the library-root author-guide pair (see
[Author guides](#author-guides)); add templates, themes and media yourself.
The library's `name` is derived from the target directory's base name and must
match `[a-z0-9][a-z0-9-]*`; an invalid name is an error and nothing is written.

`kalide init-library` refuses to overwrite: if the target already holds a
`library.yaml` or any of the standard layout entries (`slides/`, `sections/`,
`themes/`, `media/`), it fails and writes nothing. A target that is missing, or
that exists and holds only unrelated files (a `.git` directory, a `README`), is
allowed. A missing, extra or option-looking argument is a usage error (exit
code 2, nothing written).

### `kalide config`

Manages `kalide`'s user-level settings: the small `key=value` file kept in your
operating system's user config directory (Go's `os.UserConfigDir()` — that is
`~/Library/Application Support/kalide/config` on macOS,
`$XDG_CONFIG_HOME/kalide/config` or `~/.config/kalide/config` on Linux, and
`%AppData%\kalide\config` on Windows). The settings are per-user, not
per-project, and stay on your computer.

```
kalide config get <key>
kalide config set <key> <value>
kalide config unset <key>
kalide config list
```

There is exactly one recognised key:

- `default-template-dir` — the directory of the template library `kalide init`
  uses when it is given no `[path]` and no `--local` (see
  [`kalide init`](#kalide-init) above).

`set` normalises the value to an **absolute path** before storing it: a leading
`~` expands to your home directory and a relative path resolves against the
folder you are in when you run `set`. The stored path therefore does not depend
on where a later command runs. For example:

```
kalide config set default-template-dir ~/shared/kalide-library
kalide config get default-template-dir
kalide config list
kalide config unset default-template-dir
```

- `get <key>` prints the stored value and exits `0`; asking for a recognised key
  that is not set prints an error naming the key and exits non-zero.
- `set <key> <value>` writes the value and exits `0`.
- `unset <key>` removes the key when present, and succeeds even when it is
  absent.
- `list` prints one `key=value` line per set recognised key, and nothing when
  none is set.

Anything else is a **usage error**: a missing or unknown subcommand, a missing
`<key>`, a `set` missing its `<value>`, or a key outside the recognised set.
Each prints usage to stderr listing `default-template-dir`, exits `2`, and
writes nothing.

Only `kalide init` reads `default-template-dir`. `kalide start` and
`kalide templates` never consult the user config: a deck always renders against
its own `templates/` library, or the one `kalide.yaml` names.

### `kalide start`

Validates the whole deck and then serves it with live reload:

```
kalide start [--port N] [--no-open]
```

- The deck is checked **first**. If something is wrong, `kalide start` prints the
  single first problem and stops without serving anything. Fix it and run again.
- The page is served only on your own computer (`127.0.0.1`), never to the
  network.
- `--port N` picks a specific port (between 1 and 65535). Without it, `kalide`
  tries port `8080` and, if that is busy, the next free port up to `8099`. It
  prints the address it actually used. If you ask for a port that is already in
  use, it reports an error instead of falling back.
- `--no-open` skips opening the browser. Use it when you want to open the link
  yourself, or run headless.
- Press **Ctrl+C** to stop. `kalide` shuts down cleanly and releases the port.

### `kalide check`

Validates the whole deck — its slide files, its resolved library layers and its
theme — without serving it:

```
kalide check [--all] [--json]
```

By default it stops at the **first** problem and reports it exactly as
`kalide start` would. With `--all` it collects **every** problem, in the same
fixed order, instead of stopping at the first. `--json` writes a
machine-readable report to stdout (a `valid` boolean, a summary, and each
error's file, line, path and message) following the same collection mode;
without it the summary goes to stdout and the errors go to stderr. A usage
error is plain text on stderr even with `--json`, and no report is emitted.

Exit codes are `0` when the deck is valid, `1` when something is wrong, and `2`
for a usage error.

A **library or theme load failure** — a missing or invalid `templates/` library
layer, or a theme that fails to load — is reported as the first check error and
ends the report there even with `--all`, because no registry exists to collect
further errors from.

### `kalide render`

Writes a slide's rendered HTML without serving it — no server, no port, no
network:

```
kalide render [slide] [-o dir] [--full|--fragment]
```

With a slide it prints that slide's HTML to stdout; without one it writes one
`<number>[letter]-<label>.html` file per slide into the required `-o`
directory, in deck order. The slide is selected by its position label (`3` or
`3a`) first, then by its deck-unique label (the filename's `<label>`); an
argument matching no slide is an error with a closest-match suggestion.

`--full` (the default) writes a complete standalone page whose reveal.js
container holds that slide — the same page shell, head, embedded assets and
theme stylesheet the server serves. `--fragment` writes only the slide's
`<section>` markup, the fragment the deck page embeds. Both build the selected
slide from the **whole deck's** identity, so the `--full` page embeds exactly
the `--fragment` output and `.slide.number`/`.slide.total` count across the
whole deck. The two flags are mutually exclusive; giving both is a usage error.

A validation error exits `1` in the error-reporting format; a usage error exits
`2`.

### `kalide templates`

Lists every template in the project's resolved template library — its name,
whether it is a `slide` or a `section`, and a one-line description:

```
kalide templates [--all] [name]
```

Templates whose manifest declares `internal: true` are hidden from the default
listing; `--all` includes them. There is no built-in fallback of any kind: if
the current folder has no valid library, `kalide templates` (and
`kalide start`) reports the problem instead of listing anything.

### `kalide templates <name>`

Shows one template's full documentation, exactly as loaded from the project's
library:

```
kalide templates <name>
```

For example:

```
kalide templates content
```

The report covers the template's fields — each with its type, whether it is
required or optional, whether it is `nullable`, its default, its limits and its
formats — its sections (the accepted templates and repeat bounds), the rules
for its body text, and a copyable example. A field whose type is an inline
record has its nested fields listed beneath it, and an enum field's variants
are listed beneath the field rather than inline among its other attributes. If
the name is misspelled, `kalide` suggests the closest match.

### `kalide upgrade`

Brings the project in the **current directory** — a deck or a template
library — forward to the running `kalide`:

```
kalide upgrade
```

It takes **no path** and has **no `--force`**: any argument at all is a usage
error (exit code 2, nothing written). It always works on the current directory
and runs entirely **offline**.

`kalide upgrade` refreshes kalide-owned scaffold files to the running binary's
versions and applies kalide's library-format migrations. It **never overwrites
author content**: a file the author has edited is left untouched, and a deleted
starter file is never re-created. It prints every refreshed path and every
path it left alone, including deliberately skipped author-edited files. If the
upgraded project fails to validate, the work is undone and the project is left
exactly as it was.

This is not the same as `brew upgrade kalide` or `scoop update kalide`, which
upgrade the **program** (see [Upgrading the kalide binary](#upgrading-the-kalide-binary)).

### `kalide version`

Prints the version of the `kalide` you are running (stamped into the binary
when it was built):

```
kalide version
```

Also `kalide --version` and `kalide -v`. Use it to check that an install or
upgrade worked.

### `kalide help`

Prints a short reminder of the commands above:

```
kalide help
```

A bare `kalide` with no command prints the same text, as does `kalide --help`
and `kalide -h`. The help is also shown, on stderr, after a usage error.

## How a deck is laid out

A deck is a folder containing:

```
kalide.yaml    deck settings
templates/     the deck's local template & theme library
slides/        your slides, one Markdown file each
assets/        your images and other files
```

Slides are named with a number and a short label, for example
`slides/1-title.md`. The number sets the order (`10` comes after `9`). A slide
named with a letter, such as `slides/2a-detail.md`, becomes a vertical slide
that appears *under* slide `2`.

`kalide.yaml` holds the deck-wide settings. Only `title` is required:

```yaml
title: My presentation     # required
author: A. Presenter       # optional
date: 2026-09-25           # optional, YYYY-MM-DD
theme: default             # optional, a theme name from the library
navigation: default        # optional: default, linear or grid
properties:                # optional, arbitrary deck-wide values for templates
  audience: Investors
  revision: 3
  confidential: true
templates: ../shared-lib   # optional, an external template library layer
```

- `title` (required) — the deck title. Missing or blank is an error.
- `author` and `date` — the presenter and the presentation date (`YYYY-MM-DD`).
- `theme` — the theme name; omitted means `default`. It must be a theme in the
  resolved library.
- `navigation` — `default`, `linear` or `grid`; omitted means `default`.
- `properties` — an arbitrary mapping of author keys to **typed scalars** —
  string, number, boolean, or a date written `YYYY-MM-DD`. A key whose value is
  itself a mapping or a list is a config error naming the key. These values are
  for templates to read; `kalide` itself does nothing with them beyond loading
  and typing them.
- `templates` — names the deck's **external-library layer**: a template-library
  directory outside the deck. A **relative** value is resolved against the deck
  root; an **absolute** value is used as given. The deck-root `templates/`
  directory is the deck's **local layer**, loaded whenever it exists. Either,
  both or neither may be present. See [Layered libraries](#layered-libraries).

Any other unrecognized top-level key is an error, and `kalide` suggests the
closest match if it looks like a typo.

Each slide starts with a short header between `---` lines that names its
template and fills in the fields, followed by the slide body in Markdown. The
starter deck created by `kalide init` shows a working example you can copy.
Use `kalide templates` to see what each template in the library needs.

## The templates/ library

Every deck resolves a **template library**: the set of slide templates, section
templates, themes, fonts, logos and media files it renders against. There is no
built-in, embedded design system of any kind — `kalide init` seeds a minimal
unbranded starter library, and you extend or replace it as the deck needs.
Everything visual — every template, theme, font, logo and piece of media — comes
from a library; the binary supplies none of it.

A library is a directory with this fixed layout:

```
templates/
  library.yaml               library metadata (name, description, format)
  AGENTS_KALIDE.md           library/template-author full guide (inert)
  AGENTS.md                  short pointer to AGENTS_KALIDE.md (inert)
  slides/
    <name>/
      template.yaml           the template's manifest (fields, sections, body rule)
      layout.html.tmpl        the html/template layout that renders it
      example.md              a copyable example slide body
  sections/
    <name>/
      template.yaml
      layout.html.tmpl
      example.md
  themes/
    <name>/
      theme.css               the theme's stylesheet
      ...                     optional theme-owned files (fonts, logos)
  media/
    ...                       images and other files layouts can reference
```

- **`library.yaml`** is required at the root of the library and carries the
  library's `name` (matching `[a-z0-9][a-z0-9-]*`), a short `description`, and a
  `format` version number. Exactly `format: 1` is accepted.
- **`slides/<name>/`** and **`sections/<name>/`** each hold exactly three
  files: `template.yaml`, `layout.html.tmpl` and `example.md`. A template's
  name is always its directory name. A slide name and a section name share one
  namespace and may not collide; theme names and `media/` paths each form their
  own namespace.
- **`template.yaml`** is the manifest: a `description`, a list of `fields`
  (each with a `type` — `text`, `number`, `date`, `boolean`, `enum`, `image`,
  `link`, `list` or `section-template` — plus rules like `required`,
  `max_length`, `min`/`max`, `variants`, `formats`), a list of `sections`
  (each naming which section templates it `accepted`s and a `min`/`max`
  count), and a `body` rule (`mode: required|optional|disallowed`, plus
  limits like `max_words`, `max_paragraphs` and `max_list_items`).
- **`layout.html.tmpl`** is the standard Go `html/template` layout that turns a
  filled-in slide (its fields and any nested sections) into the slide's HTML.
  There is no Markdown helper and no third-party template engine.
- **`themes/<name>/theme.css`** is a theme's stylesheet, selected in
  `kalide.yaml` by name. Theme-owned files (fonts, logos) are referenced from
  that theme's CSS and are served from the theme directory.
- **`media/`** holds images and other files a layout can reference with the
  `media` template helper, for example `{{ media "logo.svg" }}`. `media`
  resolves its argument only inside the library's `media/` (an absolute path or
  a path that escapes with `..` is rejected), checks the file exists, and
  returns the URL the running server serves it under.
- **`AGENTS_KALIDE.md`** and **`AGENTS.md`** are the library/template-author
  guide and its pointer (see [Author guides](#author-guides)). They are inert —
  the loader ignores them — as is any other top-level entry that is not part of
  the library (for example a `.git/` directory or a `README.md`), so a library
  that is also a git repository loads normally.

`kalide templates` and `kalide templates <name>` read straight from the
resolved library, so their output can never drift from what actually renders.

For the full template-author reference — every `template.yaml` key, the field
types and limits, the layout context and the helper set — see
[`docs/templates.md`](docs/templates.md). For copyable recipes, see
[`docs/cookbook.md`](docs/cookbook.md).

### Layered libraries

A deck renders against one **merged library** built from up to two **layers**:

- the deck's **local layer** — the `templates/` directory at the deck root; and
- the deck's **external layer** — the library named by the top-level
  `templates:` key in `kalide.yaml` (a path relative to the deck root, or an
  absolute path).

Both layers load when both are present, with **equal standing**: there is **no
precedence** and no overriding. The two layers are merged over three
namespaces — template names (slide and section names share one namespace),
theme names, and `media/` paths. A name carried by both layers is a
**validation error naming the name and the path that carries it in each layer**,
never an override.

References resolve in the layer that owns them: a layout's
`{{ media "path" }}` and a theme's `media:` / `theme:<name>/` CSS references
stay inside the layer that owns the layout or theme. The `section` helper, by
contrast, resolves against the **merged** section namespace, so a local-layer
layout may call an external-layer section as if it were local (and that section
then executes with its own layer's `media`). The external library is
**self-contained and validated standalone**, so it can never call a section
that exists only in the deck's local layer.

Changing the configured `templates:` value while `kalide start` is running is
**not applied live**: the deck page shows a restart-required error, and the
server keeps serving the library it started with until you restart
`kalide start`. Edits under either resolved layer root still live-reload.

For the full two-layer how-to, see
[`docs/layered-libraries.md`](docs/layered-libraries.md).

### The reserved template context

Every slide layout, and every section instance nested inside it at any
depth, is executed with reserved, read-only names alongside its own fields:
`.deck`, `.slide`, `.item`, `.raw` and `.data`. None is authored — they are
supplied by `kalide` at render time — and all five are **data**, not helpers.

- **`.deck`** carries the deck-wide settings from `kalide.yaml`:
  - `.deck.title` — always present.
  - `.deck.author` and `.deck.date` — empty strings when omitted.
  - `.deck.properties` — always a non-nil map, even when `kalide.yaml`
    declares no `properties:` at all, so `{{ .deck.properties.audience }}`
    is safe to write unconditionally. Values keep their declared type — a
    number or boolean property stays a number or boolean — and any string
    value is escaped like the rest of an `html/template` layout: it is
    never treated as Markdown and never inserted as pre-rendered HTML.
- **`.slide`** carries this slide's position, derived at render time and
  never authored in a slide's frontmatter:
  - `.slide.number` — the slide's position label as a **string**, for
    example `"1"` for a horizontal slide or `"1a"` for the vertical slide
    beneath it.
  - `.slide.total` — the deck's total slide count, as an integer.
- **`.item`** describes this section instance among its same-name siblings
  under the same parent. It is present only inside a **section** instance's
  context — never in a slide layout, and never for a
  `section-template`-typed field value:
  - `.item.index` — the instance's zero-based position among its same-name
    siblings, and `.item.number` — the same position, one-based.
  - `.item.count` — how many same-name siblings there are.
  - `.item.first` and `.item.last` — whether this is the first or the last
    of them.
  - `.item.section` — the declared section name the instance was authored
    under, and `.item.template` — the resolved section template's name.
  - `.item.parent` — the enclosing section instance's field values, or nil
    at the top level, where the parent is the slide.
- **`.raw`** is the author's **original source** values, before conversion:
  - `.raw.<field>` — a declared field's value exactly as authored, so a
    text field's Markdown is the source as written, before inline-Markdown
    rendering.
  - `.raw.body` — the instance's original body source, before block
    rendering, when the author supplied one.
  A declared default is not a source value, so a field the author did not
  supply is absent; a body the author did not supply is likewise absent. A
  section instance rendered by the `section` helper sees `.raw` as the
  values the call supplied.
- **`.data`** is the authored section tree as data, parallel to the
  pre-rendered section lists:
  - `.data.<section>` — a list with one entry per authored instance of that
    declared section, in source order.
  - each entry carries its own `.raw`, `.item` and `.data` (its children,
    keyed the same way), so the whole authored tree is addressable as data.
  Range over the section name to splice rendered HTML; read
  `.data.<section>` to inspect the same instances as source data. A
  section-helper call is a direct render call, not an authored instance, so
  it appends no entry to `.data` and no element to the rendered section
  list.

`.deck` and `.slide` are available identically in a slide layout and in
every section template instance it nests, at every level of composition —
the motivating case is a reusable footer section rendering:

```
{{ .slide.number }} / {{ .slide.total }}
```

which renders `2a / 12` on the second vertical slide of a twelve-slide
deck, regardless of how deeply the footer section is nested inside other
sections.

`deck`, `slide`, `item`, `raw` and `data` are **reserved names**: a template
may not declare a field or section named `deck`, `slide`, `item`, `raw` or
`data`. `notes`, `body` and the `_format` suffix are reserved too.

### Layout helpers

The layout **helper** set is exactly `media`, `section`, `dict`, `list`,
`child` and `children` — functions a layout calls, unlike the reserved context
values above:

- **`media`** resolves a path relative to the library's `media/` directory
  and returns the URL the running server serves it under. The path must not
  be absolute and must not contain `..`; a missing file is an error. `media`
  never resolves into a theme directory. For example `{{ media "logo.svg" }}`.
- **`section`** renders a section template directly, exactly as if an
  instance of it had been written in Markdown. Its form is
  `{{ section "name" (dict ...) [body] [child…] }}`:
  - `{{ section "name" }}` renders the `name` section template with its
    declared defaults and no body.
  - the optional `(dict ...)` supplies the instance's field values.
  - the optional `[body]` supplies its body source, for example
    `{{ section "callout" (dict "label" "Proposition") .raw.body }}`.
  - any further arguments are the target's **child sections**, passed with
    `child` and `children` (below).
  Omitted fields take the target's defaults. The target's fields are
  validated against its schema and the body against its body rule. The
  rendered instance is treated as a **one-item group**: `.item` reports
  `index` 0, `number` and `count` 1, `first` and `last` true, `section` and
  `template` the target name, and `parent` the calling instance's fields. A
  section-helper call is a direct render call, not an authored instance, so
  it appends nothing to `.data` or to the rendered section list.
- **`child`** names one **declared section of the `section` call's target**
  and renders it, exactly as if an instance of it had been authored. Its form
  is `{{ child "<section>" (dict ...) [body] [child…] }}`: the section name,
  an optional `(dict ...)` of field values, an optional `[body]`, and any
  number of nested `child` arguments for that child's own declared children.
  The optional `template` key in the fields selects which accepted template
  is instantiated; it is required only when the declaration accepts more than
  one, and it is a selector, never a field of the template. A `child` call
  reads nothing outside its arguments. For example
  `{{ section "columns" nil nil (child "column" (dict "template" "half" "title" .raw.title)) }}`
  renders one `column` child of the `columns` section.
- **`children`** forwards the caller's **authored** `.data.<section>` entries
  as that declared section's children, so a wrapper layout hands its own
  authored children to the section it wraps, for example
  `{{ section "wrapper" nil nil (children "item" .data.item) }}`. Each
  forwarded entry re-instantiates as a real child, keeping its own `template`
  (its `.item.template`), its `.raw` fields and body, and its own nested
  `.data`.
- **`dict`** builds a map from alternating key/value arguments, the syntax
  for passing named fields to `section`, for example
  `{{ dict "number" .item.number "heading" .raw.title }}`. Keys must be
  strings; an odd argument count, a non-string key or a repeated key is an
  error.
- **`list`** returns its arguments as a sequence, the layout syntax for a
  literal list-typed field value, for example `{{ list "a" "b" }}`.

Each child — literal or forwarded — gets its own `.item`, `.raw` and `.data`
context exactly as an authored child does, and the target's own rendered
child-section lists and its `.data.<child>` are populated from the passed
children. Literal `child` arguments are checked at load: the named section
must be declared, the `template` accepted, the fields and body valid, and,
when every child is literal, the declaration's `min`/`max` over the whole
passed set; a forwarded or otherwise non-literal child is validated when the
target renders. A `child`/`children` call contributes nothing to the caller's
own `.data` or rendered section lists.

The built-in number and date format functions (`compact`, `exact`, `percent`,
`long` and `short`) are also available to layouts.

### Composing layouts

A layout may include another layout with the standard Go
`{{ template "<layout name>" . }}` action, so shared markup lives in one layout
that other slides and sections compose. The included layout is named by its
**layer-relative path inside its own layer**:

- `slides/<name>/layout.html.tmpl` for a slide layout, and
- `sections/<name>/layout.html.tmpl` for a section layout.

That name is identical on every OS and wherever the layer sits on disk: it
stays forward-slash on every OS, with no layer root, no `templates/` prefix, no
backslashes and no absolute path. It is
exactly the name the loader registers each layout under, so the action names the
path literally, never the bare template name. For the deck's local `hello` slide
layout the call is

```
{{ template "slides/hello/layout.html.tmpl" . }}
```

A layout may include any layout in its **own layer** by that name. A local-layer
layout may additionally include an **external-layer** layout by the external
layout's layer-relative name: the merged layout namespace registers every
layer's layouts, so the call resolves. The reverse is **not allowed**: the
external library is validated standalone, so an external-layer layout that
includes a layout existing only in the deck's local layer is a templates error
(`no such template`) reported at the invoking layout's own on-disk path and
line, quoting the target name exactly as written.

The included layout executes in the **invoking** layout's namespace, so its
`{{ media "path" }}` resolves in the invoking layer's `media/`. To have the
target's own layer supply its media, call it with `section` instead — a
`section` call rebinds to the called template's own layer. Compose with
`template` to share markup; call with `section` when the target's own library
must supply its media.

### Author guides

Every scaffolding command writes a pair of plain-text Markdown files for coding
agents that document the deck and template format, so an agent can help author
slides or templates without guessing:

- **`AGENTS_KALIDE.md`** is the full, kalide-owned guide — the complete format
  reference. It is written **only when absent**: a pre-existing
  `AGENTS_KALIDE.md` is treated as author content and left byte-for-byte
  untouched.
- **`AGENTS.md`** beside it holds only the pointer to that guide. Its reference
  line is:

  ```
  Kalide Reference: ./AGENTS_KALIDE.md
  ```

  When `AGENTS.md` is absent it is created carrying the pointer. When it
  already contains that line it is left byte-for-byte untouched. Otherwise —
  an author's own `AGENTS.md` — the reference line is appended while the
  author's bytes are preserved.

`kalide init` and `kalide init <path>` write the pair at the deck root;
`kalide init-library <path>` writes it at the library root. Both files are
inert: the loader ignores them, and they never affect validation, rendering or
serving.

`kalide upgrade` maintains the pair: it writes the current `AGENTS_KALIDE.md`
when absent, appends the reference line to an author `AGENTS.md` that lacks it,
and **migrates** an `AGENTS.md` that is byte-identical to a guide kalide
released in an older version to the short pointer (writing the current
`AGENTS_KALIDE.md` alongside it). It never rewrites author content.

## What `kalide init` refuses to do

`kalide init` never overwrites your work. Before writing anything, it checks
whether any of these already exist in the current folder:

- `slides/`
- `assets/`
- `kalide.yaml`

If **any** of them is present, it stops in every form of `kalide init`, prints
which one it found, and writes nothing. It keeps your existing deck intact.

A local `templates/` is the fourth deck-owned entry, and whether it blocks
depends on which library the deck uses:

- the built-in starter deck (no `[path]`, no `--local`) writes a local
  `templates/`, so a pre-existing `templates/` blocks it like the three above;
- an external-library deck (an explicit `[path]`, or the configured
  `default-template-dir`) references the external library and writes no local
  `templates/`, so a pre-existing `templates/` is **exempt**: it does not block
  and is left byte-for-byte untouched.

There is **no `--force` option** and no way to make it overwrite. To start a new
deck, either run the command in a different (empty) folder, or move the blocking
`slides/`, `assets/` and `kalide.yaml` (and, for the built-in deck,
`templates/`) out of the way first. Unrelated files such as a `.git` folder or a
`README` do not get in the way. A pre-existing `AGENTS_KALIDE.md` never blocks
and is left unchanged; a pre-existing `AGENTS.md` never blocks either — it is
left unchanged when it already points at the guide, and otherwise gains the
appended reference line with its content preserved.

## Working offline

`kalide` needs no internet connection. The reveal.js runtime is embedded
inside the `kalide` file itself, and the deck's own `templates/` library
(including its templates, themes, fonts, logos and media) and slide files
live on disk next to it. The page it serves contains no links to external
websites and loads nothing from a CDN. You can author and present on a
machine with no network.

`kalide upgrade` is offline too: it uses only the content the running binary
already carries and the project's own local files.

## Making a PDF

There is no export command, and none is needed. To produce a PDF, use the
printer built into the reveal.js page:

1. Run `kalide start` to serve the deck.
2. Open the printed address in your browser (or let `kalide` open it), then add
   `?print-pdf` to the end of the URL. For example:
   `http://127.0.0.1:8080/?print-pdf`
3. In the browser, choose **Print** and pick **Save as PDF** as the
   destination. Keep backgrounds enabled for the best result.

The print styles come from the reveal.js version bundled inside `kalide`.

## Examples

`examples/demo` in this repository is a small reference project that exercises
the format end to end: a `demo` library with a `title`/`content` slide, a
`column`/`person` section pairing (including a list of nested section-template
instances), a nested `group` section, a vertical slide, a custom theme and a
media file. Read it alongside
[The templates/ library](#the-templates-library) to see a complete library in
context.

## Install details

Full install, upgrade and uninstall steps for every platform are in
[`INSTALL.md`](INSTALL.md).

## License

kalide is licensed under the [Apache License 2.0](LICENSE) (Apache-2.0); see
also [`NOTICE`](NOTICE). The kalide name and logo are covered by
[`TRADEMARKS.md`](TRADEMARKS.md). The brand assets in [`brand/`](brand/) are
**excluded** from the Apache-2.0 grant and carry their own terms in
[`brand/LICENSE`](brand/LICENSE).

## Changelog

Release notes for every version are in [`CHANGELOG.md`](CHANGELOG.md).
