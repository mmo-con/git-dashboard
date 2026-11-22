# Agent instructions

Git Dashboard is a Go terminal application (Bubble Tea) that shows the state
of many local Git repositories, runs safe batch fetches and fast-forward
updates, and hands detailed work over to lazygit. The reasons behind the rules
below are in [docs/architecture.md](docs/architecture.md); read the relevant
section before changing Git integration, jobs, or safety behavior.

## Commands

```sh
make build              # ./bin/git-dashboard
make lint               # go vet and gofmt check
go test -race ./...     # all tests; CI also runs make coverage
make perf               # local benchmark: 200 repositories within 2 seconds
```

Run `make lint` and `go test -race ./...` before every commit.

## Hard rules

- Use only the official Git CLI through the typed client in `internal/git`.
  Never call `git` from other packages, never use a shell, and never build
  commands by string concatenation.
- Parse only machine-readable Git output (`--porcelain=v2`, `-z`, explicit
  `--format`). Never parse localized messages, colors, or column spacing.
- Background Git processes must never reach the terminal: keep the isolation
  in `internal/git` (`command.go`, `environment.go`, `process_unix.go`): no
  controlling terminal, no prompts, own process group, sanitized `GIT_*`
  environment.
- Never add an automatic stash, merge, rebase, reset, force-push, clone,
  remote rewrite, or Git configuration change. Operations that change local
  branches need a preview, a confirmation, and a recheck under the store lock
  right before execution.
- Unknown state stays unknown (`domain.Known`); never fall back to a clean or
  up-to-date value.
- Locks and fetches are per repository store (common Git directory), not per
  worktree.
- Workers never block on the user interface; results flow through the
  scheduler's event queue.
- Asynchronous completion must never open a dialog that a key can confirm.
  Report with a message and wait for an explicit key.
- Color is never the only carrier of information; every state needs a symbol
  with a count or text in all icon sets (`unicode`, `nerd`, `ascii`).
- Text from repositories (subjects, names, URLs, stderr) goes through
  `domain.Printable` or `domain.RedactURL` before display.
- Only Linux and macOS are supported; Git 2.34 is the minimum version.

## Tests

- Add focused tests for new or fixed behavior to the existing test files.
- Git behavior is tested against real temporary repositories built with
  `internal/testutil` (`IsolateGit`, `InitRepo`, `Commit`, `Git`), not mocks.
- TUI tests drive the model with `newModel`, `keys`, and `pump` (see
  `internal/tui/model_test.go` and `workflow_test.go`) and check rendered
  views with `plain` or `m.View()`.

## Documentation

Update the documentation in the same commit as the behavior:

| Change | Update |
|---|---|
| configuration option | `docs/configuration.md`, `examples/config.toml` |
| key, symbol, or filter | `docs/keybindings.md`, help view in `internal/tui/keymap.go` and the legend in `internal/tui/icons.go` |
| Git commands, safety, concurrency, or a design decision | `docs/architecture.md` |
| user-visible feature | `README.md` |

Documentation and code comments are written in English.

## Commits

- Subject: `<area>: <summary>` in lowercase imperative, without a period, for
  example `tui: group the operations view by status` or
  `config: add built-in theme presets`. Areas are package or topic names such
  as `tui`, `config`, `app`, `git`, `inventory`, `jobs`, `docs`, and `ci`.
- Body: wrapped at 72 characters; describes the new behavior and why, not the
  diff.
- One logical change per commit.
