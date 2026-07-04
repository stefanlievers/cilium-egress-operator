<!-- Thanks for contributing! Please fill in what applies and delete the rest. -->

## What does this PR do?

<!-- Short description; link the issue it fixes: Fixes #123 -->

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Documentation
- [ ] Refactor / cleanup
- [ ] Security hardening

## Checklist

- [ ] `make test` and `make lint` pass locally
- [ ] Generated files are up to date (`make manifests generate`) if `api/` changed
- [ ] Behavior changes are covered by tests
- [ ] `CHANGELOG.md` updated under `Unreleased`
- [ ] Reconciliation stays idempotent and event-driven (no polling — [ADR-0004](../docs/adr/0004-event-driven-monitoring.md))
- [ ] No privilege/RBAC expansion, or an ADR is included ([ADR-0005](../docs/adr/0005-least-privilege-pinner.md))
