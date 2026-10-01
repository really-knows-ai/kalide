# Changelog

All notable changes to kalide are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.10.1] - 2026-10-01

### Fixed

- `kalide upgrade` again refreshes the deck and library `AGENTS.md` guides for
  projects created or last upgraded by v0.9.0. The known-version catalog had
  lost the v0.9.0 guide digests (they were overwritten by the v0.10.0 guide
  digests), so those unmodified guides were reported as author content and
  skipped. The v0.9.0 digests are restored so `refreshed` includes
  `AGENTS.md`, and a regression test guards them.
- The README now documents the v0.10 template features it had missed: the
  `section`/`dict`/`list` layout helpers and the reserved `.raw`/`.data`/`.item`
  contexts.

## [0.10.0] - 2026-10-01

### Added

- Layout helpers `section`, `dict` and `list`. `{{ section "name" (dict ...)
  [body] }}` renders another section template inline from a layout, passing
  field values built with `dict` (and `list`) and an optional body. The layout
  helper set is now `media`, `section`, `dict` and `list`.
- A reserved `.raw` context in every slide and section instance, carrying the
  author's original source field and body values, and a reserved `.data`
  context exposing the authored section tree as data, where each entry carries
  its own `.raw`, `.item` and `.data`. `raw` and `data` are now reserved names
  and cannot be declared as a field or section.
- Section-helper calls are checked at load time: the target template must
  resolve, literal `dict` fields must match its schema, and child-section and
  body rules are enforced, with errors path-qualified to the calling template.
  Helper-call cycles are rejected. Helper calls count towards cycle detection
  but not towards the six-heading nesting depth bound.

### Changed

- The load-time `example.md` context now carries `.raw`, `.data` and `.item`,
  so layouts using them validate the same way they render.
- An omitted required field that declares a default is now satisfied, and the
  default is applied at every site (load-time examples and rendering).
- Validation errors no longer repeat the template prefix.
- Both embedded authoring guides (deck and library `AGENTS.md`) document the
  layout helpers and the `.raw`/`.data` contexts.
- Internal: load time and render time share one `template.RawContext` /
  `DataContext` builder, and section-helper reachability uses a memoised
  `Registry.HelperTargets`.

## [0.9.0] - 2026-09-30

### Added

- `kalide upgrade`: brings an existing deck or library in the current directory
  forward to the running binary, entirely offline. It refreshes kalide-owned
  scaffold files (seed starters, default theme, `AGENTS.md` guides) that the
  author has not edited — identified against an embedded catalog of known
  scaffold versions — and applies kalide's ordered library-format migrations.
  It never overwrites author content: an edited file is left untouched and
  reported, and a deleted seed starter stays deleted. It takes no path argument
  and has no `--force`. If the upgraded library fails validation, both the
  refresh and the migration are undone.
- Nested sections: section headings now nest by depth (`#` through `######`). A
  heading one level deeper opens a child section declared by the parent
  instance's resolved template, and child instances are counted per parent
  against the declared `min`/`max`. `sections:` is accepted in any template
  manifest, so a section template can declare its own child sections, up to six
  levels of composition; over-deep chains and cycles are errors naming the
  offending template chain.
- Sections render bottom-up: a parent section's layout receives its children as
  already-rendered HTML lists.
- A new reserved `.item` context in every section instance: `.item.index`,
  `.item.number`, `.item.count`, `.item.first`, `.item.last`, `.item.section`,
  `.item.template` and `.item.parent`. `item` is now a reserved name and cannot
  be declared as a field or section.
- Theme CSS may reach another theme's directory with the reserved
  `theme:<name>/` prefix, so one theme can build on another. Both `media:` and
  `theme:<name>/` now work in `@import` as well as `url()`, and imports are
  followed across trees, bounded and cycle-safe; references are rewritten to
  served URLs at serve time.
- The demo project gains a nested `group` section template and a nested-sections
  slide.

### Changed

- Validation errors for sections, fields and body rules inside nested sections
  carry an indexed containment path locating the offending instance.
- `example.md` validation recurses into nested section examples, and the gallery
  and `kalide templates` label child-section relationships by usage.
- `##`/`###` headings are ordinary body subheadings only where the enclosing
  template declares no child sections; otherwise every heading is a section
  marker. `# notes` is recognised only as a top-level heading.
- An unprefixed relative reference in theme CSS resolves from the referencing
  stylesheet's own location and is confined to its own tree.
- Both embedded authoring guides (deck and library `AGENTS.md`) are rewritten to
  document `kalide upgrade`, nested sections, `.item`, and the `theme:`/`media:`
  prefixes.

## [0.8.0] - 2026-09-29

### Added

- Theme shared media: a theme's `theme.css` — and any CSS it `@import`s from
  within its own theme directory — may now reference the resolved library's
  shared `media/` tree with the reserved `media:` URL prefix, for example
  `url('media:fonts/Inter-Regular.woff2')`. Several themes can share one library
  copy of brand assets (fonts, backgrounds, logos) instead of duplicating them
  into each `themes/<name>/`. At serve time kalide rewrites every `media:`
  reference in the served theme CSS to the served media URL — the same URL a
  layout `{{ media "path" }}` reference resolves to — and a reference is
  validated at load time exactly like a layout `media` argument (missing file,
  absolute path or `..` escape is an error naming the theme CSS file and line).
  The prefix is valid only for a `url()` reference, never for an `@import`
  target; a theme can reach only its own theme directory and the library's
  `media/` tree.
- Both embedded authoring guides now document the reserved `media:` prefix: the
  deck-author guide written at a project root and the library/template-author
  guide written at a library root.

## [0.7.1] - 2026-09-28

### Fixed

- A template library that is also a git repository now loads: the loader
  validates only the library structure (`library.yaml`, `slides/`, `sections/`,
  `themes/`, `media/`) and ignores every other top-level entry — a `.git/`
  directory, a `README.md`, an editor lockfile — instead of erroring on
  anything outside the documented layout. This generalizes the previous
  `AGENTS.md`-only exception.
- The file watcher follows only the resolved library's structure — its
  `library.yaml` and its `slides/`, `sections/`, `themes/` and `media/`
  subtrees — rather than the whole resolved root, so a `git` operation inside a
  git-hosted library no longer triggers a deck reload.

## [0.7.0] - 2026-09-28

### Added

- Agent guides: every scaffolding command now writes an `AGENTS.md` — a static,
  unbranded Markdown reference to the deck and template format for coding
  agents. `kalide init` and `kalide init <path>` write a deck-author guide at
  the project root, and `kalide init-library <path>` writes a
  library/template-author guide at the library root. A guide is written only
  when none exists: a pre-existing `AGENTS.md` is never overwritten and never
  blocks scaffolding.
- A build/test-time self-test keeps each embedded guide in step with the
  implemented format, so the vocabulary a guide documents cannot drift from
  what `kalide` accepts.

### Changed

- The resolved template-library layout now permits a single inert `AGENTS.md`
  at its root (ignored by loading, validation, rendering and serving). Every
  other unknown top-level entry remains an error.

## [0.6.0] - 2026-09-27

### Added

- External template libraries: `kalide.yaml` gains an optional top-level
  `templates:` key naming a template-library directory outside the deck
  (relative to the deck root, or absolute). A configured path wins over a local
  `templates/`; the resolved library is used for loading, validation,
  rendering, serving, the `/templates` gallery and live reload, and there is no
  fallback when neither resolves.
- `kalide init <path>`: scaffold a deck that references an external template
  library. The library and the deck's theme are validated before anything is
  written, and no local `templates/` is created.
- `kalide init-library <path>`: create a new, deck-ready, empty template
  library (`library.yaml`, empty `slides/`, `sections/` and `media/`, and a
  minimal `default` theme).

### Changed

- Retargeting `templates:` while `kalide start` is running now serves a
  full-page "restart kalide start" error on the deck page instead of silently
  switching libraries. Editing the value back to the one captured at startup
  resumes serving, and restarting applies the new library. Live reload still
  applies to edits under the resolved library and to every other
  `kalide.yaml`, `slides/` and `assets/` change.

## [0.5.0] - 2026-09-27

### Added

- `kalide version` command (also `--version` and `-v`) that prints a single
  line `kalide <version>` with the version stamped into the binary at build
  time (`dev` for unbuilt/source runs).
- `LICENSE` (Apache License 2.0), `NOTICE` and `TRADEMARKS.md` at the
  repository root.
- Brand assets: `brand/logo_light.svg`, `brand/logo_dark.svg` and
  `brand/LICENSE`. The `brand/` folder is excluded from the Apache-2.0 grant
  and is not built into the binary.
- `properties:` in `kalide.yaml`: an arbitrary mapping of typed scalar values
  (string, number, boolean, `YYYY-MM-DD` date) exposed to templates as
  `.deck.properties`, alongside the reserved, read-only `.deck` (title,
  author, date) and `.slide` (`number`, `total`) template context, available
  in slide layouts and in section instances at every depth.
- `darwin/amd64` (Intel Mac), `linux/amd64` and `linux/arm64` as supported
  release targets, alongside the existing `darwin/arm64`, `windows/amd64`
  and `windows/arm64`. Homebrew now serves both Mac architectures.
- Linux install by direct download of `kalide-linux-<arch>` with checksum
  verification against `checksums.txt` (no package-manager channel).

### Changed

- README reworked: light/dark logo hero, the name's origin and pronunciation,
  a `kalide version` entry, and license, trademark and changelog links.
- `INSTALL.md`: a post-install `kalide version` check and links to
  `LICENSE`, `CHANGELOG.md` and `TRADEMARKS.md`; `bucket/README.md` gains the
  same links.
- Homebrew formula license set to `Apache-2.0`.
- Build, release and native-e2e CI matrix expanded from three to six
  supported targets (`darwin/{arm64,amd64}`, `windows/{amd64,arm64}`,
  `linux/{amd64,arm64}`).

[Unreleased]: https://github.com/really-knows-ai/kalide/compare/v0.10.1...HEAD
[0.10.1]: https://github.com/really-knows-ai/kalide/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/really-knows-ai/kalide/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/really-knows-ai/kalide/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/really-knows-ai/kalide/compare/v0.7.1...v0.8.0
[0.7.1]: https://github.com/really-knows-ai/kalide/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/really-knows-ai/kalide/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/really-knows-ai/kalide/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/really-knows-ai/kalide/releases/tag/v0.5.0
