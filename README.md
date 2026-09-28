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

- [kalide](#kalide)
- [About the name](#about-the-name)
- [Install](#install)
- [Supported computers](#supported-computers)
- [Getting started](#getting-started)
- [Commands](#commands)
- [How a deck is laid out](#how-a-deck-is-laid-out)
- [The templates/ library](#the-templates-library)
  - [The `.deck` and `.slide` context](#the-deck-and-slide-context)
- [What `kalide init` refuses to do](#what-kalide-init-refuses-to-do)
- [Working offline](#working-offline)
- [Making a PDF](#making-a-pdf)
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

### Homebrew (macOS, Apple silicon and Intel)

```sh
brew tap really-knows-ai/kalide https://github.com/really-knows-ai/kalide
brew trust really-knows-ai/kalide
brew install kalide
```

Homebrew 6.0.0 and later requires explicitly trusting a third-party tap before
installing from it; no account or token is needed.

To upgrade: `brew update && brew upgrade kalide`.

### Scoop (Windows, amd64 and arm64)

```powershell
scoop bucket add kalide https://github.com/really-knows-ai/kalide
scoop install kalide
```

To upgrade: `scoop update; scoop update kalide`.

### Manual download (Linux, or any platform)

Download the file for your computer from the
[releases page](https://github.com/really-knows-ai/kalide/releases). This is
the route on Linux, and a fallback on macOS and Windows.

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

`AGENTS.md` is a plain-text guide for coding agents that documents the deck
and template format, so an agent can help author slides without guessing. It
is written only when no `AGENTS.md` already exists: a pre-existing one is left
untouched, and the file is inert — it is not part of the deck and does not
affect `kalide start`.

The `templates/` folder it creates is a minimal, unbranded starter
library — one `hello` slide template and one `default` theme — not a
finished design system. Run it once in an empty folder and then edit the
slides, or add templates and themes of your own (see
[The templates/ library](#the-templates-library)). `kalide init` takes no
options. See [what it refuses to do](#what-kalide-init-refuses-to-do) below.

### `kalide start [--port N] [--no-open]`

Validates the whole deck and then serves it with live reload.

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

### `kalide templates`

Lists every template in the project's **own `templates/` library** — its
name, whether it is a `slide` or a `section`, and a one-line description.
There is no built-in fallback of any kind: if the current folder has no
valid `templates/` directory, `kalide templates` (and `kalide start`)
reports the problem instead of listing anything.

### `kalide templates <name>`

Shows one template's full documentation: its fields (type, whether required,
default, limits, formats), its sections, the rules for its body text, and a
copyable example, exactly as loaded from the project's `templates/`
directory. For example:

```
kalide templates content
```

If the name is misspelled, `kalide` suggests the closest match.

### `kalide version`

Prints the version of the `kalide` you are running (stamped into the binary
when it was built). Use it to check an install or upgrade worked.

### `kalide help`

Prints a short reminder of the commands above.

## How a deck is laid out

A deck is a folder containing:

```
kalide.yaml    deck settings
templates/     the project's template & theme library
slides/        your slides, one Markdown file each
assets/        your images and other files
```

Slides are named with a number and a short label, for example
`slides/1-title.md`. The number sets the order (`10` comes after `9`). A slide
named with a letter, such as `slides/2a-detail.md`, becomes a vertical slide
that appears *under* slide `2`.

`kalide.yaml` holds the deck-wide settings. Only `title` is required:

```yaml
title: My presentation   # required
author: A. Presenter     # optional
date: 2026-09-25         # optional, YYYY-MM-DD
theme: default           # optional, a theme name from templates/themes
navigation: default      # optional: default, linear or grid
properties:              # optional, arbitrary deck-wide values for templates
  audience: Investors
  revision: 3
  confidential: true
```

`properties` is an arbitrary mapping: any key you like, each holding a
**typed scalar** — string, number, boolean, or a date written `YYYY-MM-DD`.
A key whose value is itself a mapping or a list is a config error naming the
key. `properties` is the one addition to the top-level keys above; any other
unrecognized top-level key is still an error, and `kalide` suggests the
closest match if it looks like a typo. These values are for templates to read
(see [The templates/ library](#the-templates-library)) — `kalide` itself does
nothing with them beyond loading and typing.

Each slide starts with a short header between `---` lines that names its
template and fills in the fields, followed by the slide body in Markdown. The
starter deck created by `kalide init` shows a working example you can copy.
Use `kalide templates` to see what each template in the project's
`templates/` library needs.

## The templates/ library

Every deck owns its own `templates/` library: the set of slide templates,
section templates, themes, fonts, logos and media files it renders against.
There is no built-in, embedded design system of any kind — `kalide init`
seeds a minimal unbranded starter library, and you extend or replace it as
the deck needs. Everything visual — every template, theme, font, logo and
piece of media — comes from this library; the binary supplies none of it.

```
templates/
  library.yaml               library metadata (name, description, format)
  AGENTS.md                  agent guide for library/template authors (inert)
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
  media/
    ...                       images and other files layouts can reference
```

- **`library.yaml`** is required at the root of `templates/` and carries the
  library's `name`, a short `description`, and a `format` version number.
- **`AGENTS.md`** is the library/template-author agent guide: a plain-text
  reference to the library and template format that `kalide init-library`
  writes, so a coding agent can author new templates and themes without
  guessing. It is inert — the loader ignores it, and no template may rely on
  it. The loader likewise ignores any other top-level entry that is not part of
  the library (for example a `.git/` directory or a `README.md`), so a library
  that is also a git repository loads normally.
- **`slides/<name>/`** and **`sections/<name>/`** each hold exactly three
  files: `template.yaml`, `layout.html.tmpl` and `example.md`. A template's
  name is always its directory name.
- **`template.yaml`** is the manifest: a `description`, a list of `fields`
  (each with a `type` — `text`, `number`, `date`, `boolean`, `enum`, `image`,
  `link`, `list` or `section-template` — plus rules like `required`,
  `max_length`, `min`/`max`, `variants`, `formats`), a list of `sections`
  (each naming which section templates it `accepted`s and a `min`/`max`
  count), and a `body` rule (`mode: required|optional|disallowed`, plus
  limits like `max_words`, `max_paragraphs` and `max_list_items`).
- **`layout.html.tmpl`** is the `html/template` layout that turns a filled-in
  slide (its fields and any nested sections) into the slide's HTML.
- **`themes/<name>/theme.css`** is a theme's stylesheet, selected in
  `kalide.yaml` by name.
- **`media/`** holds images and other files a layout can reference with the
  `media` template helper, for example `{{ media "logo.svg" }}`. `media`
  resolves its argument only inside `templates/media` (an absolute path or a
  path that escapes with `..` is rejected), checks the file exists, and
  returns the URL the running server serves it under.

`kalide templates` and `kalide templates <name>` read straight from this
library, so its documentation output can never drift from what actually
renders.

### The `.deck` and `.slide` context

Every slide layout, and every section instance nested inside it at any
depth, is executed with two reserved, read-only names alongside its own
fields: `.deck` and `.slide`. Neither is authored — they are supplied by
`kalide` at render time — and both are **data**, not helpers: the v1
template helper set stays exactly `media`.

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

Both are available identically in a slide layout and in every section
template instance it nests, at every level of composition — the motivating
case is a reusable footer section rendering:

```
{{ .slide.number }} / {{ .slide.total }}
```

which renders `2a / 12` on the second vertical slide of a twelve-slide
deck, regardless of how deeply the footer section is nested inside other
sections.

`examples/demo` in this repository is a small, deliberately non-EY reference
project that exercises the format end to end: a `demo` library with a
`title`/`content` slide, a `column`/`person` section pairing (including a
list of nested section-template instances), a vertical slide, a custom theme
and a media file. Read it alongside this section to see a complete
`templates/` library in context.

## What `kalide init` refuses to do

`kalide init` never overwrites your work. Before writing anything, it checks
whether any of these already exist in the current folder:

- `slides/`
- `templates/`
- `assets/`
- `kalide.yaml`

If **any** of them is present, it stops, prints which one it found, and writes
nothing. It keeps your existing deck intact.

There is **no `--force` option** and no way to make it overwrite. To start a new
deck, either run the command in a different (empty) folder, or move the existing
`slides/`, `templates/`, `assets/` and `kalide.yaml` out of the way first.
Unrelated files such as a `.git` folder or a `README` do not get in the way.

## Working offline

`kalide` needs no internet connection. The reveal.js runtime is embedded
inside the `kalide` file itself, and the deck's own `templates/` library
(including its templates, themes, fonts, logos and media) and slide files
live on disk next to it. The page it serves contains no links to external
websites and loads nothing from a CDN. You can author and present on a
machine with no network.

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
