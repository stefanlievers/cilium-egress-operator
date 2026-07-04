# Contributing to cilium-egress-operator

Thank you for your interest in contributing. This project welcomes contributions of all
kinds — bug reports, documentation, tests, and code.

## Getting started

### Prerequisites

- Go 1.26+
- `make` and a container runtime (for envtest binaries and image builds)
- kubectl
- Optional: a Kubernetes cluster with Cilium installed (kind/k3s works for controller
  development; egress behavior itself needs Cilium in native routing mode)

### Local development

```bash
git clone https://github.com/stefanlievers/cilium-egress-operator
cd cilium-egress-operator

# Install CRDs into the cluster of your current kubeconfig context
make install

# Run the controller locally against that cluster
make run
```

### Before you push

```bash
make test     # unit + envtest integration tests (downloads envtest binaries on first run)
make lint     # golangci-lint; `make lint-fix` fixes what it can
make manifests generate   # after changing anything in api/ — commit the generated files
```

CI runs the same `make test` and `make lint` on every push and pull request, plus e2e
tests on kind.

## Project ground rules

These are non-negotiable design constraints; PRs that violate them will be asked to change:

- **Idempotent**: applying the same spec twice must be a no-op (no churn, no side effects).
- **Event-driven**: no polling loops, `RequeueAfter` timers, or CronJobs unless there is
  genuinely no event source to hook — see [ADR-0004](docs/adr/0004-event-driven-monitoring.md).
- **Least privilege**: no `privileged` containers; capability or RBAC expansions need an
  ADR — see [ADR-0005](docs/adr/0005-least-privilege-pinner.md) and [SECURITY.md](SECURITY.md).
- **Validated inputs**: every CR field that reaches a shell or the host must have a CRD
  validation pattern.
- **English only**: all code comments, log messages, and documentation are written in
  English.

## Architecture decisions

Significant design changes (new controllers, privilege changes, API shape, node-selection
behavior) should be proposed as an ADR in [`docs/adr/`](docs/adr/) using the
[MADR](https://adr.github.io/madr/) format — see the [ADR index](docs/adr/README.md) for
the process. Small fixes don't need one.

## Pull request process

1. Fork the repository and create a feature branch (`git checkout -b feat/my-feature`).
2. Commit with clear messages following [Conventional Commits](https://www.conventionalcommits.org/)
   (`feat:`, `fix:`, `docs:`, `test:`, ...).
3. Add or update tests for behavior changes; update the [CHANGELOG](CHANGELOG.md) under
   `Unreleased`.
4. Open a pull request against `main` and make sure CI passes.

## Reporting issues

Use the issue templates. For bug reports, please include:

- Kubernetes and Cilium versions (`kubectl version`, `cilium version`)
- Your `EgressGateway` spec (redact sensitive IPs if needed)
- Operator logs
- `kubectl get egressgateway -o yaml` output
- `kubectl describe ds -l app=egress-ip-pinner` output if the issue involves IP pinning

For security vulnerabilities, **do not open an issue** — see [SECURITY.md](SECURITY.md).

## Code of Conduct

This project follows the [CNCF Code of Conduct](CODE_OF_CONDUCT.md). Be kind.
