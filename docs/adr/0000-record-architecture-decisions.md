# Record architecture decisions as MADRs

- Status: accepted
- Date: 2026-07-04

## Context and Problem Statement

This operator makes opinionated choices about node selection, privilege boundaries, and
network configuration mechanics. Contributors and operators of sovereign, security-sensitive
clusters need to understand *why* those choices were made, not just what the code does today.

## Decision Outcome

We record every significant architecture decision as a Markdown Any Decision Record
([MADR](https://adr.github.io/madr/)) in `docs/adr/`, numbered sequentially and indexed in
`docs/adr/README.md`. ADRs are immutable once accepted; changes are made by superseding.

### Consequences

- Good: design rationale survives contributor turnover and long release gaps.
- Good: pull requests that change architecture have a natural documentation artifact.
- Bad: minor overhead per significant change; mitigated by keeping ADRs short.
