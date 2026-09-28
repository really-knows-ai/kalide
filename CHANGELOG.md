# Changelog

All notable changes to kalide are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/really-knows-ai/kalide/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/really-knows-ai/kalide/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/really-knows-ai/kalide/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/really-knows-ai/kalide/releases/tag/v0.5.0
