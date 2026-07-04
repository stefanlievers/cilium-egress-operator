# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Nothing yet.

## [0.1.0] - 2026-07-04

First release. Installable via the `install.yaml` attached to the
[release page](https://github.com/stefanlievers/cilium-egress-operator/releases/tag/v0.1.0);
the controller image is published as
`ghcr.io/stefanlievers/cilium-egress-operator:v0.1.0` (linux/amd64).

### Added
- Initial `EgressGateway` CRD (v1alpha1) with `egressIP`, `interface`, `podSelector`,
  `namespaceSelector`, and `destinations`
- Controller: deterministic control-plane node labeling (`egress-node: "true"`),
  recovered event-driven via a Node watcher
- Controller: IP pinner DaemonSet lifecycle management with owner references
- Route management: opt-in `createRoutes` with per-destination optional `nextHop`,
  falling back to the node's default gateway
- `pinnerImage` spec field to override the pinner container image (air-gapped support)
- `nodeRole` spec field (`control-plane` | `worker`) to choose which kind of node gets
  the egress label; workloads on the egress node itself do not use the egress IP
- Deployment manifests (`config/manager/`), Dockerfile, and a tag-triggered release
  workflow publishing the image to GHCR and `install.yaml` to the GitHub release
- Status writeback: `egressNode`, `egressIPConfirmed` (backed by a readiness probe on the
  pinner), and `lastReconciled`
- Architecture Decision Records (MADR) in `docs/adr/`, compatibility matrix in
  `docs/compatibility.md`, `SECURITY.md`, and `CODE_OF_CONDUCT.md`

### Changed
- Pinner container hardened: dropped `privileged` in favor of `CAP_NET_ADMIN` only, all
  other capabilities dropped, `allowPrivilegeEscalation: false`, resource requests/limits
- Pinner monitoring made resilient: event-driven `ip monitor` when iproute2 is available,
  periodic fallback on BusyBox-only images
- All code comments and log messages translated to English

### Fixed
- Egress IP presence check no longer substring-matches other addresses
  (e.g. `10.0.0.1` matching `10.0.0.10`)
- `spec.interface` is now validated as a Linux interface name, closing a shell-injection
  vector in the pinner script
- Node label patching no longer panics on nodes without labels

### Security
- Upgraded `golang.org/x/net` to v0.56.0 (GO-2026-5026, GO-2026-4918)

[Unreleased]: https://github.com/stefanlievers/cilium-egress-operator/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/stefanlievers/cilium-egress-operator/releases/tag/v0.1.0
