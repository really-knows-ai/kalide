# eypres

`eypres` turns a folder of Markdown files into a slide deck and shows it in your
browser. You write slides as plain text, run one command, and present — with the
browser updating live as you edit.

Everything needed to render and present is built into the program. It works
offline, and there is no separate tool to install or run alongside it.

- [Install](#install)
- [Supported computers](#supported-computers)
- [Getting started](#getting-started)
- [Commands](#commands)
- [How a deck is laid out](#how-a-deck-is-laid-out)
- [What `eypres init` refuses to do](#what-eypres-init-refuses-to-do)
- [Working offline](#working-offline)
- [Making a PDF](#making-a-pdf)
- [More install options](#more-install-options)

## Install

`eypres` is a **single file**. To install it, copy that one file somewhere on
your computer and run it. There is no installer, no runtime to install first, no
package manager requirement, and no setup step.

- On **macOS**: copy `eypres-darwin-arm64` to a folder on your `PATH` (for
  example `/usr/local/bin/eypres`), then run it in a terminal.
- On **Windows**: copy `eypres-windows-amd64.exe` (or
  `eypres-windows-arm64.exe`) to a folder on your `PATH` and run it in a
  terminal (PowerShell or Command Prompt).

The file is self-contained: all of the reveal.js code, the EY theme, fonts,
logos and built-in templates are inside it.

## Supported computers

| Your computer | Supported |
|---|---|
| macOS on Apple silicon (M-series) | Yes — `darwin/arm64` |
| Windows on 64-bit Intel/AMD | Yes — `windows/amd64` |
| Windows on ARM | Only when a release includes it — `windows/arm64` |
| macOS on Intel | No (`darwin/amd64` is not built) |
| Linux | No |

Windows on ARM (`windows/arm64`) is published only while a build machine for it
is available, so it may be missing from a release. Everything else in the table
is fixed.

## Getting started

Open a terminal, change to the folder where you want your presentation to live,
and run:

```
eypres init
```

This creates a small starter deck. Then:

```
eypres start
```

`eypres start` checks the deck for problems, serves it on your own computer
(`127.0.0.1`), and opens it in your default browser. Edit any slide file and
save — the browser refreshes by itself. Press **Ctrl+C** in the terminal to stop
(Ctrl+Break also works on Windows).

## Commands

### `eypres init`

Creates a starter deck in the **current folder**:

```
eypres.yaml
slides/1-title.md
slides/2-content.md
assets/
```

Run it once in an empty folder and then edit the slides. `eypres init` takes no
options. See [what it refuses to do](#what-eypres-init-refuses-to-do) below.

### `eypres start [--port N] [--no-open]`

Validates the whole deck and then serves it with live reload.

- The deck is checked **first**. If something is wrong, `eypres start` prints the
  single first problem and stops without serving anything. Fix it and run again.
- The page is served only on your own computer (`127.0.0.1`), never to the
  network.
- `--port N` picks a specific port (between 1 and 65535). Without it, `eypres`
  tries port `8080` and, if that is busy, the next free port up to `8099`. It
  prints the address it actually used. If you ask for a port that is already in
  use, it reports an error instead of falling back.
- `--no-open` skips opening the browser. Use it when you want to open the link
  yourself, or run headless.
- Press **Ctrl+C** to stop. `eypres` shuts down cleanly and releases the port.

### `eypres templates`

Lists every built-in template with its name, whether it is a `slide` or a
`section`, and a one-line description.

### `eypres templates <name>`

Shows one template's full documentation: its fields (type, whether required,
default, limits, formats), its sections, the rules for its body text, and a
copyable example. For example:

```
eypres templates content
```

If the name is misspelled, `eypres` suggests the closest match.

### `eypres help`

Prints a short reminder of the commands above.

## How a deck is laid out

A deck is a folder containing:

```
eypres.yaml          deck settings
slides/              your slides, one Markdown file each
assets/              your images and other files
```

Slides are named with a number and a short label, for example
`slides/1-title.md`. The number sets the order (`10` comes after `9`). A slide
named with a letter, such as `slides/2a-detail.md`, becomes a vertical slide
that appears *under* slide `2`.

`eypres.yaml` holds the deck-wide settings. Only `title` is required:

```yaml
title: My presentation   # required
author: A. Presenter     # optional
date: 2026-09-25         # optional, YYYY-MM-DD
theme: default           # optional
navigation: default      # optional: default, linear or grid
```

Each slide starts with a short header between `---` lines that names its
template and fills in the fields, followed by the slide body in Markdown. The
starter deck created by `eypres init` shows working examples you can copy. Use
`eypres templates` to see what each built-in template needs.

## What `eypres init` refuses to do

`eypres init` never overwrites your work. Before writing anything, it checks
whether any of these already exist in the current folder:

- `slides/`
- `assets/`
- `eypres.yaml`

If **any** of them is present, it stops, prints which one it found, and writes
nothing. It keeps your existing deck intact.

There is **no `--force` option** and no way to make it overwrite. To start a new
deck, either run the command in a different (empty) folder, or move the existing
`slides/`, `assets/` and `eypres.yaml` out of the way first. Unrelated files
such as a `.git` folder or a `README` do not get in the way.

## Working offline

`eypres` needs no internet connection. The reveal.js runtime, the EY theme,
fonts, logos, built-in templates and starter files are all embedded inside the
`eypres` file itself. The page it serves contains no links to external websites
and loads nothing from a CDN. You can author and present on a machine with no
network.

## Making a PDF

There is no export command, and none is needed. To produce a PDF, use the
printer built into the reveal.js page:

1. Run `eypres start` to serve the deck.
2. Open the printed address in your browser (or let `eypres` open it), then add
   `?print-pdf` to the end of the URL. For example:
   `http://127.0.0.1:8080/?print-pdf`
3. In the browser, choose **Print** and pick **Save as PDF** as the
   destination. Keep backgrounds enabled for the best result.

The print styles come from the reveal.js version bundled inside `eypres`.

## More install options

The steps above cover copying the single file by hand. Managed installs —
Homebrew on macOS and Scoop on Windows, including the one-time access and token
setup — are documented in [`INSTALL.md`](INSTALL.md).
