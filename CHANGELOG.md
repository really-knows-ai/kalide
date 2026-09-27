# Changelog

All notable changes to kalide are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/really-knows-ai/kalide/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/really-knows-ai/kalide/releases/tag/v0.5.0
