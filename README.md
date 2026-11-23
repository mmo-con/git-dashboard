<p align="center">
  <img src="docs/images/headquarters.png" alt="headquarters logo" width="160" />
</p>

<h1 align="center">headquarters</h1>

<p align="center">
  A CLI dashboard that groups local Git repositories in a table view, runs batch fetches and safe fast-forward updates, and launches <code>lazygit</code> per repo.
</p>

<p align="center">
  <a href="https://github.com/mmo-con/headquarters/actions/workflows/ci.yml"><img src="https://github.com/mmo-con/headquarters/actions/workflows/ci.yml/badge.svg?branch=main" alt="Build status" /></a>
  <a href="https://github.com/mmo-con/headquarters/releases/latest"><img src="https://img.shields.io/github/v/release/mmo-con/headquarters?style=flat-square" alt="Release" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue?style=flat-square" alt="License: MIT" /></a>
</p>

---

headquarters is a terminal application for managing a curated collection of
local Git repositories. It shows a cross-repository overview, groups
repositories into families, runs safe batch operations such as fetch and
fast-forward updates, and hands all detailed work within one repository over
to [lazygit](https://github.com/jesseduffield/lazygit).

> headquarters manages the repository collection. lazygit manages the
> individual repository.

![headquarters in the tree view: repositories grouped by family with health
symbols, family summaries, and the detail panel of the selected
repository](docs/images/headquarters-screenshot.png)

## Quick start

[Install headquarters](#installation), then point it at a directory containing
your repositories. No configuration file is needed:

```sh
headquarters ~/src
```

Use `j` / `k` to navigate, `Enter` to open a repository in lazygit, `f` to
fetch, and `u` for a safe update after confirmation. Press `?` for help or `q`
to quit. See [key bindings](docs/keybindings.md) for all shortcuts.

The architecture and the decisions behind it are documented in
[docs/architecture.md](docs/architecture.md), with individual
[architecture decision records](docs/adr/README.md).

## Features

- Explicit repositories plus bounded discovery below root directories, with
  include and exclude patterns and clear precedence rules.
- Status per repository: branch, detached or unborn HEAD, operations in
  progress, staged/modified/deleted/renamed/conflicting/untracked entries,
  upstream state, stash entries, remotes, latest commit, and last fetch time;
  the detail panel lists the most recent commits (three by default).
  Stash entries are only reported, as a count and with a quick filter;
  applying or dropping them stays with lazygit.
- Other local branches that need attention, such as a stale `main` or a
  branch with unpushed work, shown as a count with details in the side panel;
  old and ignored branches are filtered out, pinned branches always count.
- Families as a filter or as a collapsible tree, text and state filters
  (`is:dirty`, `is:behind`, …), and sorting.
- `Enter` opens the selected repository in lazygit; the dashboard refreshes
  the repository afterward.
- Concurrent fetch of the selected or visible repositories with progress,
  cancellation, retry, and an interactive retry in the terminal for
  repositories that need a passphrase.
- Safe update after one confirmation: the dashboard fetches first, then
  fast-forwards only branches that can be updated safely, without merge
  commits, rebases, or stashes, and lists every skipped repository with the
  reason. The operations view then serves as a worklist: open blocked
  repositories in lazygit, see which blockers are resolved, and continue.
- Configurable launchers for a shell, an editor, or a file manager.
- lazygit-style panels with titled borders, one column per count (staged,
  modified, untracked, stashed, ahead, behind) plus a State column for everything
  else, per-cell colors, spinners and progress bars, dialogs as overlays, and
  header counters that double as quick filters. Colors, built-in theme presets
  (such as high contrast, Catppuccin, Dracula, and Gruvbox), and the symbol
  set (Unicode, Nerd Font, ASCII) are configurable; `NO_COLOR` is honored.

headquarters uses the official Git CLI for everything. Background Git
processes run without a controlling terminal, so they can never prompt in or
write into the user interface.

## Requirements

- Linux or macOS
- Git 2.34 or newer
- lazygit (optional, for `Enter`)
- Go 1.25 or newer to build from source

## Installation

### Quick install

On Linux or macOS (amd64 or arm64), download the installer and run it:

```sh
curl -fsSL https://github.com/mmo-con/headquarters/releases/latest/download/install.sh -o install.sh &&
  sh install.sh
```

You can inspect `install.sh` before running it. It installs to `~/.local/bin`
without `sudo` and tells you how to add that directory to `PATH` if needed.
It verifies the archive against the SHA-256 checksum from the same release;
the checksum is not an independent signature. If the selected release does
not include `install.sh`, use [manual installation](#manual-installation).

### Manual installation

Download the archive for your platform and `checksums.txt` from the same
[release](https://github.com/mmo-con/headquarters/releases), verify the
archive's SHA-256 checksum, then extract it and copy the `headquarters`
binary to a directory on your `PATH`.

The macOS binaries are not notarized. `curl` downloads run as they are; if
you download an archive with a browser, macOS blocks the binary until you
remove the quarantine attribute with
`xattr -d com.apple.quarantine headquarters`.

### Build from source

```sh
make build            # builds ./bin/headquarters
make install          # installs into $(go env GOPATH)/bin
```

## Usage

```sh
headquarters                      # uses ~/.config/headquarters/config.toml
headquarters ~/src                # repositories below ~/src, no inventory configuration needed
headquarters --config path.toml   # explicit configuration file
headquarters --check              # validate the configuration and print the inventory
headquarters --version
```

With a path, headquarters shows the repositories found below that directory
(or the directory itself if it is a repository) instead of the configured
ones. All other settings of the configuration file, such as the theme,
lazygit, and launchers, still apply. Options must precede the path.

The configuration file is looked up in this order: `--config`, the
`HEADQUARTERS_CONFIG` environment variable, and
`$XDG_CONFIG_HOME/headquarters/config.toml` (falling back to
`~/.config/headquarters/config.toml`).
After upgrading from the previous project name, move your existing
configuration to the new location and update any environment variable or
scripts that refer to the old name.

Start with [examples/config.toml](examples/config.toml). All options are
described in [docs/configuration.md](docs/configuration.md), and all keys in
[docs/keybindings.md](docs/keybindings.md).

## Development

```sh
make test             # unit and integration tests (requires git)
make test-installer   # offline tests for the release installer
make test-race        # tests with the race detector
make coverage         # race-detector tests with a Markdown coverage report
make perf             # local benchmark: 200 repositories within 2 seconds
make lint             # go vet and gofmt check
make release          # cross-compiled archives, checksums, and installer in ./dist
```

Integration tests create temporary Git repositories and ignore the user's
global Git configuration.

### Project layout

| Path | Responsibility |
|---|---|
| `cmd/headquarters` | command-line entry point |
| `internal/config` | configuration lookup, parsing, path expansion, validation |
| `internal/domain` | domain models without dependencies on UI or Git output |
| `internal/inventory` | discovery, identity, and precedence rules |
| `internal/git` | typed wrapper around the Git CLI with process isolation |
| `internal/jobs` | bounded scheduler with per-store locks and event queue |
| `internal/launcher` | lazygit, launchers, and browser handover commands |
| `internal/app` | application services: refresh, fetch, update, filter, sort |
| `internal/tui` | Bubble Tea user interface |
| `internal/testutil` | helpers for tests with real Git repositories |
| `scripts` | development scripts, such as the coverage report used in CI |

## License

Licensed under the [MIT License](LICENSE).
