# Configuration

Git Dashboard reads a TOML file. The file is located in this order:

1. the `--config <path>` command-line option;
2. the `GIT_DASHBOARD_CONFIG` environment variable;
3. `$XDG_CONFIG_HOME/git-dashboard/config.toml`, falling back to
   `~/.config/git-dashboard/config.toml`.

If no file exists, Git Dashboard starts with an empty inventory and shows the
expected path. `git-dashboard --check` validates the file and prints the
resolved inventory without starting the user interface.

Syntax errors, type errors, and unknown fields prevent startup. Problems with
individual entries, such as a missing path, are shown as entry states.

A complete example is in [../examples/config.toml](../examples/config.toml).

## Paths

All path values (`path`, `root`, `[git].executable`, `[lazygit].command`, and
the first element of a launcher command) support:

- `~` and `~/` at the beginning;
- environment variables in the form `${NAME}`; an undefined variable is an
  error. `$NAME` without braces is rejected.

Relative paths are resolved against the directory of the configuration file.
A command without a slash, such as `lazygit`, is looked up in `PATH`.

## Top level

| Key | Type | Default | Description |
|---|---|---|---|
| `version` | integer | `1` | configuration format version; only `1` is supported |

## `[[repositories]]`

Explicit repositories. They are always included, even below directories that
a discovery rule excludes.

| Key | Type | Default | Description |
|---|---|---|---|
| `path` | string | required | worktree root of the repository; a subdirectory is reported as `config error` |
| `family` | string | discovery family or `Ungrouped` | family shown in the dashboard |
| `enabled` | boolean | `true` | `false` removes the repository, even if discovery finds it |
| `remote` | string | none | expected remote URL; a mismatch is shown as `≠remote` |
| `remote_name` | string | `origin` | remote that `remote` refers to; also selects the primary remote |
| `untracked` | `"normal"` or `"no"` | `[git].untracked` | `no` skips untracked-file detection for large repositories |

## `[[discovery]]`

Bounded discovery below a root directory.

| Key | Type | Default | Description |
|---|---|---|---|
| `root` | string | required | directory to scan; missing roots produce a warning |
| `max_depth` | integer | `3` | number of directory levels below `root` that are scanned |
| `family` | string | `Ungrouped` | family of every discovered repository |
| `include` | array of strings | `["**"]` | a repository must match at least one pattern |
| `exclude` | array of strings | `[]` | matching directories are skipped and not traversed |
| `follow_symlinks` | boolean | `false` | follow symbolic links; loops are detected |

Rules:

- Patterns use doublestar glob syntax (`*`, `**`, `?`, `[...]`, `{a,b}`) and
  are matched case-sensitively against the slash-separated path relative to
  `root`, for example `company/api`.
- A pattern ending in `/**` also matches the directory itself.
- Discovery never descends into a repository. Nested repositories, including
  submodules, must be configured explicitly.
- Bare repositories are never discovered.
- If several discovery entries find the same repository, the first entry in
  the file supplies the family, and a warning is shown.

### Precedence

| Explicit entry | Found by discovery | Result |
|---|---|---|
| none | yes | included with the discovery settings |
| `enabled = true` | no, excluded, or outside every root | included with the explicit settings |
| `enabled = true` | yes | included once; explicit fields override discovery fields |
| `enabled = false` | any | excluded |

## `[git]`

| Key | Type | Default | Description |
|---|---|---|---|
| `executable` | string | `git` | Git executable; version 2.34 or newer is required |
| `inspection_parallelism` | integer | `8` | concurrent inspections |
| `inspection_timeout` | duration | `"10s"` | timeout per inspection |
| `fetch_parallelism` | integer | `4` | concurrent fetches and updates |
| `fetch_timeout` | duration | `"120s"` | timeout per fetch or update |
| `fetch_prune` | boolean | `true` | add `--prune` to `git fetch --all` |
| `untracked` | `"normal"` or `"no"` | `"normal"` | default untracked-file detection |

Durations use Go syntax, for example `"500ms"`, `"10s"`, or `"2m"`.

## `[lazygit]`

| Key | Type | Default | Description |
|---|---|---|---|
| `command` | string | `lazygit` | executable name or path |
| `args` | array of strings | `[]` | additional arguments; no placeholders |
| `enabled` | boolean | `true` | `false` disables `Enter` |

lazygit starts in the worktree root of the selected repository.

## `[[launchers]]`

Launchers appear in the menu opened with `o`.

| Key | Type | Description |
|---|---|---|
| `name` | string | label in the menu |
| `command` | array of strings | executable and arguments |

No shell is involved. The only placeholder is `{path}`; it must be a complete
argument and is replaced by the worktree root. Launchers start in the worktree
root, take over the terminal like lazygit, and the repository is refreshed
afterward.

The built-in menu entry *Open remote in browser* converts the primary remote
to an HTTPS URL (HTTPS remotes, or SSH remotes on github.com, gitlab.com,
bitbucket.org, and codeberg.org) and opens it with `xdg-open` or `open`.
