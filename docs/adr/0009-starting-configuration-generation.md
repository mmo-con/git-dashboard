# ADR 0009: Generating a starting configuration

Status: Accepted

## Context

`headquarters <path>` works without configuration, but every repository is
`Ungrouped`. Writing a maintained configuration by hand means guessing
families, roots, and depths. [ADR 0002](0002-inventory-and-repository-identity.md)
requires families to be assigned explicitly and discovery to be bounded.

## Decision

`headquarters init DIR...` scans the directories once and generates a
configuration that the user reviews. It does not read or merge an existing
configuration, generates only `[[discovery]]` and `[[repositories]]`
entries, and never changes repositories.

Families are proposed from the first directory level below each root and
written as explicit `family` values. At runtime, families are still never
derived from directory names; ADR 0002 remains unchanged.

The scan uses the discovery rules of ADR 0002 with a bound of
`--max-depth N` (default `3`). The generated entries cover the same area,
not only the repositories found, so later clones are discovered too: each
grouped first-level directory becomes a discovery root with
`max_depth = N - 1`, and the root keeps an `Ungrouped` discovery entry with
`max_depth = N` that excludes the grouped directories. No repository is
found by two roots, so the generated file starts without inventory
warnings. Overlapping input directories are skipped for the same reason.

Nested repositories are written only as commented-out entries, because
discovery deliberately does not descend into repositories. Submodules are
skipped; they belong to their superproject.

Grouping by remote owner was rejected for the first version: it needs an
explicit entry for every repository, later clones would still be
`Ungrouped`, and remote URLs would have to be read and redacted.

## Consequences

- A flat directory of repositories yields a single `Ungrouped` entry.
- The configuration cannot escape `$`, and TOML requires UTF-8, so paths
  that contain `$` or invalid UTF-8 are skipped or stay `Ungrouped`, with a
  warning in the output and in the file.
- An existing file is replaced only with `--force`, atomically through a
  temporary file; a symbolic link keeps pointing to the replaced target.
