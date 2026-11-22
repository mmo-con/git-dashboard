# Git Dashboard

Git Dashboard is a terminal application for managing a curated collection of
local Git repositories. It shows a cross-repository overview, groups
repositories into families, runs safe batch operations such as fetch and
fast-forward updates, and hands all detailed work within one repository over
to [lazygit](https://github.com/jesseduffield/lazygit).

> Git Dashboard manages the repository collection. lazygit manages the
> individual repository.

![Git Dashboard in the tree view: repositories grouped by family with health
symbols, family summaries, and the detail panel of the selected
repository](docs/images/dashboard-tree.png)

*Shown with `icons = "nerd"` and `preset = "dracula"`; all repositories are
fictional.*

The architecture and the decisions behind it are documented in
[docs/architecture.md](docs/architecture.md).

## Features

- Explicit repositories plus bounded discovery below root directories, with
  include and exclude patterns and clear precedence rules.
- Status per repository: branch, detached or unborn HEAD, operations in
  progress, staged/modified/deleted/renamed/conflicting/untracked entries,
  upstream state, stash entries, remotes, latest commit, and last fetch time.
  Stash entries are only reported, as a count and with a quick filter;
  applying or dropping them stays with lazygit.
- Other local branches that need attention, such as a stale `main` or a
  branch with unpushed work, shown as a count with details in the side panel;
  old and ignored branches are filtered out, pinned branches always count.
- Families as a filter or as a collapsible tree, text and state filters
  (`is:dirty`, `is:behind`, …), and sorting.
- `Enter` opens the selected repository in lazygit; the dashboard refreshes
  the repository afterward.
- Concurrent fetch of the visible or marked repositories with progress,
  cancellation, retry, and an interactive retry in the terminal for
  repositories that need a passphrase.
- Safe update: `git merge --ff-only --no-autostash --no-overwrite-ignore
  <upstream commit>` for eligible repositories only, with a preview that
  explains every skip and applies exactly the upstream commit it showed. The
  operations view then serves as a worklist: open blocked repositories in
  lazygit, see which blockers are resolved, and continue with a new preview.
- Configurable launchers for a shell, an editor, or a file manager.
- lazygit-style panels with titled borders, one column per count (staged,
  modified, untracked, stashes, ahead, behind) plus a State column for everything
  else, per-cell colors, spinners and progress bars, dialogs as overlays, and
  header counters that double as quick filters. Colors, built-in theme presets
  (such as high contrast, Catppuccin, Dracula, and Gruvbox), and the symbol
  set (Unicode, Nerd Font, ASCII) are configurable; `NO_COLOR` is honored.

Git Dashboard uses the official Git CLI for everything. Background Git
processes run without a controlling terminal, so they can never prompt in or
write into the user interface.

## Requirements

- Linux or macOS
- Git 2.34 or newer
- lazygit (optional, for `Enter`)
- Go 1.25 or newer to build from source

## Installation

Prebuilt archives for Linux and macOS (amd64 and arm64) are attached to every
[release](https://github.com/mmo-con/git-dashboard/releases), together with
`checksums.txt`. Set `VERSION` to the current release. Each step runs only
if the previous one succeeded, so nothing is unpacked or installed when a
download fails or the checksum does not match:

```sh
VERSION=v0.3.0
NAME=git-dashboard_${VERSION}_linux_amd64   # or _linux_arm64, _darwin_amd64, _darwin_arm64
BASE=https://github.com/mmo-con/git-dashboard/releases/download/$VERSION
curl -fL -O "$BASE/$NAME.tar.gz" -O "$BASE/checksums.txt" &&
  grep " $NAME.tar.gz\$" checksums.txt | sha256sum -c - &&   # macOS: shasum -a 256 -c -
  tar xzf "$NAME.tar.gz" &&
  install -m 755 "$NAME/git-dashboard" ~/.local/bin/        # any directory in PATH
```

The macOS binaries are not notarized. `curl` downloads run as they are; if
you download an archive with a browser, macOS blocks the binary until you
remove the quarantine attribute with
`xattr -d com.apple.quarantine git-dashboard`.

To build from source:

```sh
make build            # builds ./bin/git-dashboard
make install          # installs into $(go env GOPATH)/bin
```

A first look needs no configuration: `git-dashboard ~/src` shows the
repositories below `~/src`.

## Usage

```sh
git-dashboard                      # uses ~/.config/git-dashboard/config.toml
git-dashboard ~/src                # repositories below ~/src, no inventory configuration needed
git-dashboard --config path.toml   # explicit configuration file
git-dashboard --check              # validate the configuration and print the inventory
git-dashboard --version
```

With a path, Git Dashboard shows the repositories found below that directory
(or the directory itself if it is a repository) instead of the configured
ones. All other settings of the configuration file, such as the theme,
lazygit, and launchers, still apply. Options must precede the path.

The configuration file is looked up in this order: `--config`, the
`GIT_DASHBOARD_CONFIG` environment variable, and
`$XDG_CONFIG_HOME/git-dashboard/config.toml` (falling back to
`~/.config/git-dashboard/config.toml`).

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
| `cmd/git-dashboard` | command-line entry point |
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
