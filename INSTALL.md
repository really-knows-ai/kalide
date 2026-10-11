# Installing kalide

`kalide` is a small program that turns a folder of Markdown files into a slide
deck and shows it in your browser. This guide is for people who want to install
it with **Homebrew** (macOS) or **Scoop** (Windows), or download the single
binary directly from GitHub Releases. Every step is anonymous — no programming
knowledge, no account, no sign-in and no token is needed — so just follow the
steps in order.

If you would rather not use a package manager, you can instead download the
`kalide` file and run it directly. That simpler route is described in [Manual download](#manual-download)
below and in the [README](README.md#install).

- [Before you start](#before-you-start)
- [On macOS (Homebrew)](#on-macos-homebrew)
- [On Windows (Scoop)](#on-windows-scoop)
- [On Linux (manual download)](#on-linux-manual-download)
- [Manual download](#manual-download)
- [Upgrading kalide versus upgrading a project](#upgrading-kalide-versus-upgrading-a-project)
- [Supported computers](#supported-computers)
- [Check the installed version](#check-the-installed-version)
- [License, changes and trademarks](#license-changes-and-trademarks)
- [If something goes wrong](#if-something-goes-wrong)

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

### Tap, trust and install

Run these three commands:

```
brew tap really-knows-ai/kalide https://github.com/really-knows-ai/kalide
```

```
brew trust really-knows-ai/kalide
```

```
brew install kalide
```

Homebrew 6.0.0 and later require trusting a third-party tap before installing
from it; because `kalide`'s tap is added from a custom remote, you trust it by
name as above. No GitHub account or token is needed.

That is it. Check the install with:

```
kalide help
```

### Upgrading the kalide program (macOS)

```
brew update
```

```
brew upgrade kalide
```

If no newer version is listed, you are already up to date.

This upgrades the **program**. To bring an existing deck or library forward
instead, see
[Upgrading kalide versus upgrading a project](#upgrading-kalide-versus-upgrading-a-project).

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

### Upgrading the kalide program (Windows)

```
scoop update
```

```
scoop update kalide
```

This upgrades the **program**. To bring an existing deck or library forward
instead, see
[Upgrading kalide versus upgrading a project](#upgrading-kalide-versus-upgrading-a-project).

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

### Upgrading the kalide program (Linux)

Repeat the download, checksum-verification and `chmod +x` steps above with
the new release's asset.

This upgrades the **program**. To bring an existing deck or library forward
instead, see
[Upgrading kalide versus upgrading a project](#upgrading-kalide-versus-upgrading-a-project).

## Manual download

If you do not want to use Homebrew (macOS) or Scoop (Windows), you can instead
download the matching `kalide-<os>-<arch>` binary directly from GitHub Releases,
in a browser or with `curl -LO`. This is the macOS and Windows fallback; on
Linux it is the normal route (see [On Linux (manual download)](#on-linux-manual-download)
above).

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

## Upgrading kalide versus upgrading a project

There are two different things you can upgrade:

- **The `kalide` program** — the file you installed. Use your install channel:
  `brew upgrade kalide` on macOS, `scoop update kalide` on Windows, or download
  and replace the file on Linux (see the per-system "Upgrading" steps above).
  `kalide` never checks for updates on its own and never upgrades itself.
- **A project** — a deck or template library you made with `kalide`. Bring it
  forward to the running `kalide` with the `kalide upgrade` command, described
  next.

### `kalide upgrade`

Run the command from inside your project folder:

```
kalide upgrade
```

`kalide upgrade` takes **no path** and has **no `--force`**: any argument at all
is an error. It always works on the folder you are in and runs entirely
**offline** — it uses only what the running `kalide` already carries plus your
project's own files. It **never overwrites author content**: a file you have
edited is left untouched and reported, and a deleted starter file is not brought
back. It refreshes the files `kalide` itself owns, applies any project-format
updates, and prints every path it refreshed and every path it left alone. If the
project would not validate afterwards, the change is undone and the project is
left exactly as it was.

This is not the same as `brew upgrade kalide` or `scoop update kalide`, which
upgrade the **program** (see the per-system "Upgrading" steps above).

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
