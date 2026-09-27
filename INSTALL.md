# Installing kalide

`kalide` is a small program that turns a folder of Markdown files into a slide
deck and shows it in your browser. This guide is for people who want to install
it with **Homebrew** (macOS) or **Scoop** (Windows), or download the single
binary directly from GitHub Releases. No programming knowledge is needed —
follow the steps in order.

If you would rather not use a package manager, you can instead download the
`kalide` file and run it directly. That simpler route is described in [Manual download](#manual-download)
below and in the [README](README.md#install).

- [Before you start](#before-you-start)
- [Step 1 — Ask for access to the repository](#step-1--ask-for-access-to-the-repository)
- [Step 2 — Create a GitHub token](#step-2--create-a-github-token)
- [On macOS (Homebrew)](#on-macos-homebrew)
- [On Windows (Scoop)](#on-windows-scoop)
- [Manual download](#manual-download)
- [Supported computers](#supported-computers)
- [Check the installed version](#check-the-installed-version)
- [License, changes and trademarks](#license-changes-and-trademarks)
- [If something goes wrong](#if-something-goes-wrong)
- [For maintainers: how a release is published](#for-maintainers-how-a-release-is-published)

## Before you start

You need three things:

1. **A supported computer** — see
   [Supported computers](#supported-computers) below. On a Mac this means an
   Apple silicon (M-series) machine; Intel Macs are not supported.
2. **A GitHub account that can see the private repository**
   `really-knows-ai/kalide` — see step 1.
3. **A GitHub token** — a long password-like string that lets your computer
   download the private release — see step 2.

You also need the package manager for your system. If you do not have it yet:

- **macOS:** install [Homebrew](https://brew.sh) and make sure the `brew`
  command works in Terminal (the app is in `/Applications/Utilities`).
- **Windows:** install [Scoop](https://scoop.sh) and make sure the `scoop`
  command works in PowerShell.

You do **not** need to be a developer, and `kalide` itself needs no internet
connection once installed. The token is only needed for downloading and
upgrading.

## Step 1 — Ask for access to the repository

`kalide` is distributed from a **private** GitHub repository, so your GitHub
account must be allowed to read it before anything can be downloaded.

Ask whoever manages the `really-knows-ai/kalide` repository to add your
GitHub username as a collaborator (or to a team with read access).

To check that it worked, sign in to GitHub and open:

<https://github.com/really-knows-ai/kalide>

If you can see the repository, you have access. If you get a "404" or "not
found" page, either you are signed in to the wrong account or your access has
not been granted yet.

## Step 2 — Create a GitHub token

Homebrew, Scoop and direct release downloads need a token to fetch assets from the private repository.
A token is a secret — treat it like a password. Never share it or paste it into a
chat, email or issue.

There are two kinds of token. **The fine-grained token is recommended** because
you can limit it to this one repository.

### Recommended: a fine-grained token

1. Sign in to GitHub and go to
   <https://github.com/settings/tokens?type=beta>.
2. Click **Generate new token**.
3. Give it a name such as `kalide install`.
4. Under **Expiration**, choose a period you are comfortable with (for example
   90 days). You will repeat this step when it expires.
5. Under **Repository access**, choose **Only select repositories**, then pick
   **really-knows-ai/kalide**.
6. Under **Permissions → Repository permissions**, find **Contents** and set it
   to **Read-only**. Leave everything else as-is. (GitHub adds the required
   **Metadata: Read-only** permission automatically.)
7. Click **Generate token** and copy the value that starts with `github_pat_…`.
   You will not be able to see it again — copy it now.

### Alternative: a classic token

1. Go to <https://github.com/settings/tokens>.
2. Click **Generate new token (classic)**.
3. Name it `kalide install`, choose an expiration, and tick the **`repo`**
   checkbox (the top-level one that includes all its sub-entries).
4. Click **Generate token** and copy the value that starts with `ghp_…`.

A classic `repo` token can read **all** of your repositories, so the
fine-grained token above is safer. Either works for `kalide`.

> Keep the token somewhere safe until the next step. If you lose it, just
> generate a new one — the old one can be deleted on the same settings page.

## On macOS (Homebrew)

`kalide` is installed from a Homebrew "tap" that lives inside the same source
repository (`really-knows-ai/kalide`). There is **no separate tap
repository** to add.

### Set the token

Homebrew reads the token from an environment variable named
`HOMEBREW_GITHUB_API_TOKEN`.

For the current Terminal window only, run (paste your own token in place of
`YOUR_TOKEN`):

```
export HOMEBREW_GITHUB_API_TOKEN=YOUR_TOKEN
```

To make it persist, add that same line to the end of your shell profile
(`~/.zshrc` on modern macOS) and open a new Terminal window.

### Tap and install

Run these two commands:

```
brew tap really-knows-ai/kalide https://github.com/really-knows-ai/kalide.git
```

```
brew install kalide
```

`brew tap` clones the private repository, so git may ask for a username and
password. Enter your GitHub username and paste the token **as the password**.

That is it. Check the install with:

```
kalide help
```

### Upgrading (macOS)

Set `HOMEBREW_GITHUB_API_TOKEN` again if you opened a new Terminal window, then:

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

### Set the token

Scoop reads the token from an environment variable named
`KALIDE_GITHUB_TOKEN`, or `GITHUB_TOKEN` if the first is not set.

For the current PowerShell window only, run (paste your own token in place of
`YOUR_TOKEN`):

```
$env:KALIDE_GITHUB_TOKEN = "YOUR_TOKEN"
```

To make it persist, run this once and then open a new PowerShell window:

```
setx KALIDE_GITHUB_TOKEN "YOUR_TOKEN"
```

### Add the bucket and install

Run these two commands:

```
scoop bucket add kalide https://github.com/really-knows-ai/kalide.git
```

```
scoop install kalide
```

`scoop bucket add` clones the private repository, so git may ask for a username
and password. Enter your GitHub username and paste the token **as the password**.

That is it. Check the install with:

```
kalide help
```

### Upgrading (Windows)

Set `KALIDE_GITHUB_TOKEN` again if you opened a new PowerShell window, then:

```
scoop update
```

```
scoop update kalide
```

The token is needed again on every upgrade, because the new version is
downloaded from the private release just like the first install.

### Removing kalide (Windows)

```
scoop uninstall kalide
```

## Manual download

If you do not want to use Homebrew or Scoop, you can download the matching
`kalide-<os>-<arch>` binary directly from GitHub releases:

<https://github.com/really-knows-ai/kalide/releases>

Because the repository is private, download using a browser signed in to your
GitHub account or with `gh release download` using your GitHub token.

1. Download the binary for your platform:
   - **macOS Apple silicon:** `kalide-darwin-arm64`
   - **Windows 64-bit:** `kalide-windows-amd64.exe`
   - **Windows ARM:** `kalide-windows-arm64.exe` (when included in the release)
2. Rename the downloaded file to `kalide` (or `kalide.exe` on Windows).
3. On macOS, make it executable if needed (`chmod +x kalide`).
4. Move it to a folder on your `PATH` (such as `/usr/local/bin` on macOS).
5. Open a terminal and check:

```
kalide help
```

## Supported computers

| Your computer | Supported | Notes |
|---|---|---|
| macOS on Apple silicon (M-series) | Yes | Installed with Homebrew (`darwin/arm64`) |
| Windows on 64-bit Intel/AMD | Yes | Installed with Scoop (`windows/amd64`) |
| Windows on ARM | Only when a release includes it | `windows/arm64` may be missing from a release |
| macOS on Intel | No | No `darwin/amd64` build exists |
| Linux | No | Not built |

Windows on ARM is published only while a build machine for it is available, so
the ARM installer may not be present in every release. If it is missing,
`scoop install kalide` says so clearly on an ARM machine. The Homebrew formula
also refuses to run on an Intel Mac with a plain-language message.

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

**"no GitHub token found" / "HOMEBREW_GITHUB_API_TOKEN is not set" / "KALIDE_GITHUB_TOKEN is not set"**

The token is not set in the window you are using. Repeat the *Set the token*
step for your system, then run the install or upgrade command again. On Windows,
remember that `setx` only affects **new** windows.

**GitHub rejected the token (HTTP 401 or 403)**

The token is wrong, expired, or does not have access to
`really-knows-ai/kalide`. Check step 1 (repository access) and step 2 (token
scope), then set the token again. For a fine-grained token, make sure
**Contents** is set to **Read-only** and the repository is selected.

**"could not read release" / HTTP 404**

The token is valid but cannot see the release, or the version has not been
published. Confirm you can open
<https://github.com/really-knows-ai/kalide> in a browser, then try again.

**`brew tap` or `scoop bucket add` asks for a password**

That is git asking for access to the private repository. Use your GitHub
username, and paste the token as the password.

**`kalide is Apple silicon only`**

You are on an Intel Mac. `kalide` runs only on Apple silicon (M-series) Macs.

**The install works but the version is old**

Run the upgrade commands for your system above. `kalide` never checks for
updates by itself.

If none of these helps, ask the person who granted you repository access.

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

In short: humans push `vX.Y.Z` tags, and CI publishes the release and pushes
only the package-manifest commit.
