# headquarters — Architecture

headquarters manages a collection of local Git repositories and hands
single-repository work over to lazygit. This page is a map of the code and
its architecture decisions; the ADRs are the source of truth for the
decisions, their rationale, and their constraints.

## Packages

| Package | Responsibility |
|---|---|
| `cmd/headquarters` | flags, configuration lookup, version check, `--check`, `init` |
| `internal/config` | TOML decoding, defaults, paths, validation, themes |
| `internal/inventory` | discovery, patterns, identity, precedence, validation states |
| `internal/initconfig` | one-time scan and generation of a starting configuration, atomic file writing |
| `internal/git` | isolated Git command runner, parsers, inspection, fetch, update |
| `internal/jobs` | scheduler, timeouts, store locks, event queue |
| `internal/app` | application services, operations, handover, filters, sorting |
| `internal/launcher` | lazygit, custom launchers, browser |
| `internal/clipboard` | native clipboard tools, value checks |
| `internal/domain` | repository and operation models |
| `internal/tui` | Bubble Tea model, views, layout, theme, icons, key map |
| `internal/testutil` | real Git repositories for tests |

## Architecture decision records

The [ADR index](adr/README.md) explains how the records are maintained.

| Topic | Record |
|---|---|
| Product boundaries, technology, and configuration | [ADR 0001](adr/0001-product-boundaries-and-technology.md) |
| Inventory, identity, and repository stores | [ADR 0002](adr/0002-inventory-and-repository-identity.md) |
| Git commands, parsing, and process isolation | [ADR 0003](adr/0003-git-cli-and-process-isolation.md) |
| Fetch and safe updates | [ADR 0004](adr/0004-batch-fetch-and-safe-update.md) |
| Terminal handover and job coordination | [ADR 0005](adr/0005-terminal-handover-and-job-coordination.md) |
| Domain state and error handling | [ADR 0006](adr/0006-domain-state-and-errors.md) |
| User interface principles | [ADR 0007](adr/0007-user-interface-principles.md) |
| Compatibility, performance, and testing | [ADR 0008](adr/0008-compatibility-performance-and-testing.md) |
| Generating a starting configuration | [ADR 0009](adr/0009-starting-configuration-generation.md) |
| Copying values to the clipboard | [ADR 0010](adr/0010-copying-to-the-clipboard.md) |

User-facing configuration and keys are documented in
[configuration.md](configuration.md) and [keybindings.md](keybindings.md).
