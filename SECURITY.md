# Security Policy

Security is a first-class design constraint of this project: the operator targets
sovereign, on-premises environments and runs a capability-bearing pod on control-plane
nodes. It must never endanger the clusters it runs in.

## Reporting a vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/stefanlievers/cilium-egress-operator/security/advisories/new)
("Report a vulnerability"). You will receive an acknowledgement within **7 days**. Please
include reproduction steps, affected versions, and the impact you foresee.

Coordinated disclosure: we ask for a reasonable embargo while a fix is prepared; credit is
given in the release notes unless you prefer otherwise.

## Supported versions

| Version | Security fixes |
|---|---|
| latest minor (v0.x) | yes |
| older minors | at maintainers' discretion, max one minor back |

## Security model

What the operator does — and deliberately does not — hold in terms of privilege:

- **Controller**: unprivileged Deployment. RBAC limited to the `EgressGateway` CRD, Node
  `get/list/watch/update/patch` (label management), and DaemonSet lifecycle.
- **IP pinner DaemonSet**: `hostNetwork` with `CAP_NET_ADMIN` **only** — no `privileged`,
  all other capabilities dropped, `allowPrivilegeEscalation: false`, CPU/memory limits set.
  See [ADR-0005](docs/adr/0005-least-privilege-pinner.md).
- **Input validation**: every spec value that reaches the pinner's shell script is
  constrained by CRD validation patterns (IPv4, CIDR, Linux interface name) at admission
  and shell-quoted by the controller. Unvalidated input never reaches a shell.
- **No SSH, no exec, no external endpoints**: all node configuration happens through
  Kubernetes-scheduled workloads; the operator makes no network calls beyond the API server
  (the default pinner image optionally contacts a package registry at startup — override
  `spec.pinnerImage` to remove that, see [docs/compatibility.md](docs/compatibility.md)).

## Development practices

- `golangci-lint` runs in CI on every push and pull request.
- Dependencies are scanned with `govulncheck`; known-vulnerable modules are upgraded before
  release.
- Changes touching privileges, RBAC, or the pinner script require an ADR and explicit
  review.
