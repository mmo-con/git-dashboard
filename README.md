<p align="center">
  <img src="docs/images/headquarters.png" alt="headquarters logo" width="160" />
</p>

<h1 align="center">headquarters</h1>

<p align="center">
  A CLI dashboard that groups local Git repositories in a table view, runs batch fetches &amp; pulls, and launches <code>lazygit</code> per repo.
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

The architecture and the decisions behind it are documented in
[docs/architecture.md](docs/architecture.md).

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

Prebuilt archives for Linux and macOS (amd64 and arm64) are attached to every
[release](https://github.com/mmo-con/headquarters/releases), together with
`checksums.txt`. Set `VERSION` to the current release. Each step runs only
if the previous one succeeded, so nothing is unpacked or installed when a
download fails or the checksum does not match:

```sh
VERSION=vX.Y.Z                            # a release containing headquarters archives
NAME=headquarters_${VERSION}_linux_amd64   # or _linux_arm64, _darwin_amd64, _darwin_arm64
BASE=https://github.com/mmo-con/headquarters/releases/download/$VERSION
curl -fL -O "$BASE/$NAME.tar.gz" -O "$BASE/checksums.txt" &&
  grep " $NAME.tar.gz\$" checksums.txt | sha256sum -c - &&   # macOS: shasum -a 256 -c -
  tar xzf "$NAME.tar.gz" &&
  install -m 755 "$NAME/headquarters" ~/.local/bin/        # any directory in PATH
```

The macOS binaries are not notarized. `curl` downloads run as they are; if
you download an archive with a browser, macOS blocks the binary until you
remove the quarantine attribute with
`xattr -d com.apple.quarantine headquarters`.

To build from source:

```sh
make build            # builds ./bin/headquarters
make install          # installs into $(go env GOPATH)/bin
```

A first look needs no configuration: `headquarters ~/src` shows the
repositories below `~/src`.

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
make test-race        # tests with the race detector
make coverage         # race-detector tests with a Markdown coverage report
make perf             # local benchmark: 200 repositories within 2 seconds
make lint             # go vet and gofmt check
make release          # cross-compiled archives and checksums in ./dist
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
