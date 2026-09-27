# Installing kalide

`kalide` is a small program that turns a folder of Markdown files into a slide
deck and shows it in your browser. This guide is for people who want to install
it with **Homebrew** (macOS) or **Scoop** (Windows), or download the single
binary directly from GitHub Releases. No programming knowledge, GitHub account
or token is needed — follow the steps in order.

If you would rather not use a package manager, you can instead download the
`kalide` file and run it directly. That simpler route is described in [Manual download](#manual-download)
below and in the [README](README.md#install).

- [Before you start](#before-you-start)
- [On macOS (Homebrew)](#on-macos-homebrew)
- [On Windows (Scoop)](#on-windows-scoop)
- [On Linux (manual download)](#on-linux-manual-download)
- [Manual download](#manual-download)
- [Supported computers](#supported-computers)
- [Check the installed version](#check-the-installed-version)
- [License, changes and trademarks](#license-changes-and-trademarks)
- [If something goes wrong](#if-something-goes-wrong)
- [For maintainers: how a release is published](#for-maintainers-how-a-release-is-published)

## Before you start

You need **a supported computer** — see
[Supported computers](#supported-computers) below. `kalide` supports macOS
(Apple silicon and Intel), Windows (64-bit Intel/AMD and ARM), and Linux
(64-bit Intel/AMD and ARM), for each target a release exists for.

You also need the package manager for your system. If you do not have it yet:

- **macOS:** install [Homebrew](https://brew.sh) and make sure the `brew`
  command works in Terminal (the app is in `/Applications/Utilities`).
- **Windows:** install [Scoop](https://scoop.sh) and make sure the `scoop`
  command works in PowerShell.

You do **not** need to be a developer, and `kalide` itself needs no internet
connection once installed. Downloading and upgrading need a normal internet
connection only.

## On macOS (Homebrew)

`kalide` is installed from a Homebrew "tap" that lives inside the same source
repository (`really-knows-ai/kalide`). There is **no separate tap
repository** to add. The same steps work on both Apple silicon (M-series) and
Intel Macs — Homebrew picks the matching binary for your Mac automatically.

### Tap and install

Run these two commands:

```
brew tap really-knows-ai/kalide https://github.com/really-knows-ai/kalide
```

```
brew install kalide
```

That is it. Check the install with:

```
kalide help
```

### Upgrading (macOS)

```
brew update
```

```
brew upgrade kalide
```

If no newer version is listed, you are already up to date.

### Removing kalide (macOS)

```
brew uninstall kalide
```

## On Windows (Scoop)

`kalide` is installed from a Scoop "bucket" that lives inside the same source
repository (`really-knows-ai/kalide`). There is **no separate bucket
repository** to add.

### Add the bucket and install

Run these two commands:

```
scoop bucket add kalide https://github.com/really-knows-ai/kalide
```

```
scoop install kalide
```

That is it. Check the install with:

```
kalide help
```

### Upgrading (Windows)

```
scoop update
```

```
scoop update kalide
```

### Removing kalide (Windows)

```
scoop uninstall kalide
```

## On Linux (manual download)

There is no package-manager channel for Linux (no Homebrew tap or Scoop
bucket equivalent). Install by downloading the matching release asset
directly:

1. Download the binary for your architecture from
   <https://github.com/really-knows-ai/kalide/releases> in your browser, or
   with `curl`, for example:

   ```
   curl -LO https://github.com/really-knows-ai/kalide/releases/latest/download/kalide-linux-amd64
   ```

   - **64-bit Intel/AMD:** `kalide-linux-amd64`
   - **ARM64:** `kalide-linux-arm64`
2. Also download `checksums.txt` from the same release
   (`curl -LO https://github.com/really-knows-ai/kalide/releases/latest/download/checksums.txt`)
   and verify the binary's SHA-256 matches the entry for your file:

   ```
   sha256sum kalide-linux-amd64
   ```

   Compare the printed hash against the matching line in `checksums.txt`.
3. Rename the downloaded file to `kalide` and make it executable:

   ```
   chmod +x kalide
   ```
4. Move it to a folder on your `PATH` (such as `/usr/local/bin`).
5. Check the install:

   ```
   kalide help
   ```

### Upgrading (Linux)

Repeat the download, checksum-verification and `chmod +x` steps above with
the new release's asset.

## Manual download

If you do not want to use Homebrew or Scoop, you can download the matching
`kalide-<os>-<arch>` binary directly from GitHub releases, in a browser or with
`curl -LO`:

<https://github.com/really-knows-ai/kalide/releases>

1. Download the binary for your platform, plus `checksums.txt` from the same
   release:
   - **macOS Apple silicon:** `kalide-darwin-arm64`
   - **macOS Intel:** `kalide-darwin-amd64`
   - **Windows 64-bit:** `kalide-windows-amd64.exe`
   - **Windows ARM:** `kalide-windows-arm64.exe`
   - **Linux 64-bit Intel/AMD:** `kalide-linux-amd64`
   - **Linux ARM64:** `kalide-linux-arm64`
2. Verify its SHA-256 against the matching line in `checksums.txt`
   (`shasum -a 256 <file>` on macOS, `sha256sum <file>` on Linux,
   `Get-FileHash <file>` in PowerShell).
3. Rename the downloaded file to `kalide` (or `kalide.exe` on Windows).
4. On macOS and Linux, make it executable if needed (`chmod +x kalide`).
5. Move it to a folder on your `PATH` (such as `/usr/local/bin` on macOS and
   Linux).
6. Open a terminal and check:

```
kalide help
```

## Supported computers

| Your computer | Supported | Notes |
|---|---|---|
| macOS on Apple silicon (M-series) | Yes | Installed with Homebrew (`darwin/arm64`) |
| macOS on Intel | Yes | Installed with Homebrew (`darwin/amd64`) |
| Windows on 64-bit Intel/AMD | Yes | Installed with Scoop (`windows/amd64`) |
| Windows on ARM | Yes | Installed with Scoop (`windows/arm64`) |
| Linux on 64-bit Intel/AMD | Yes | Manual download (`linux/amd64`); no package-manager channel |
| Linux on ARM64 | Yes | Manual download (`linux/arm64`); no package-manager channel |

All six targets are supported when a release for them exists: publishing is
gated on runner availability at release time, not on a fixed required/optional
split. If a target's asset is missing from a given release, the corresponding
install command (Scoop, or the manual-download instructions) will say so
clearly rather than silently failing.

## Check the installed version

However you installed `kalide`, you can confirm which version you have. Open a
terminal and run:

```
kalide version
```

It prints one line such as `kalide vX.Y.Z`. Compare it with the latest release
listed in the [changelog](CHANGELOG.md). If yours is older, run the upgrade
commands for your system above.

## License, changes and trademarks

- [LICENSE](LICENSE): `kalide` is licensed under the Apache License 2.0. The
  `brand/` folder is not covered by that license.
- [CHANGELOG.md](CHANGELOG.md): what changed in each release.
- [TRADEMARKS.md](TRADEMARKS.md): how you may use the "kalide" name and logo.

## If something goes wrong

**Download fails with HTTP 404**

The version, or the file for your computer, has not been published. Check
<https://github.com/really-knows-ai/kalide/releases> in a browser, then try
again.

**The checksum does not match**

Delete the downloaded file and download it again; do not run a file whose
checksum does not match `checksums.txt`.

**The install works but the version is old**

Run the upgrade commands for your system above. `kalide` never checks for
updates by itself.

If none of these helps, open an issue at
<https://github.com/really-knows-ai/kalide/issues>.

## For maintainers: how a release is published

This section is background only — you never need it to install or use `kalide`.

- **People push tags.** A release starts when a human pushes a tag named
  `vX.Y.Z` (for example `v1.2.0`). Nothing else creates tags.
- **CI builds and checks.** The release workflow builds the binaries for the
  supported targets, runs the native end-to-end checks on each target's own
  build machine, and only then publishes the GitHub Release with the binaries
  and a `checksums.txt`.
- **CI pushes only the manifest commit.** After publishing, CI updates
  `Formula/kalide.rb` and `bucket/kalide.json` (new version, download URLs and
  checksums) and pushes a single commit containing **only** files under
  `Formula/` and `bucket/`. The workflow fails if anything else is in that
  commit. It never creates tags and never pushes other code.
- **CI checks the public install.** After the manifest commit is pushed, the
  `package-install-e2e` job installs the new release anonymously (no token)
  with Homebrew on macOS (Apple silicon and Intel) and Scoop on Windows (64-bit
  and ARM), and checks that `kalide version` reports the pushed tag. It runs
  after publishing, so it flags a broken public install rather than blocking
  the release.

In short: humans push `vX.Y.Z` tags, and CI publishes the release, pushes
only the package-manifest commit, and then verifies the anonymous install.
