# Architecture Decision Records

This project records significant architecture decisions as ADRs in the
[MADR](https://adr.github.io/madr/) (Markdown Any Decision Records) format.

## Index

| ADR | Title | Status |
|---|---|---|
| [0000](0000-record-architecture-decisions.md) | Record architecture decisions as MADRs | accepted |
| [0001](0001-complement-cilium-egress-gateway-policy.md) | Complement, don't replace, CiliumEgressGatewayPolicy | accepted |
| [0002](0002-daemonset-ip-pinning.md) | Pin the egress IP with a DaemonSet, not SSH or a node agent | accepted |
| [0003](0003-deterministic-node-selection.md) | Deterministic control-plane node selection for the egress label | accepted |
| [0004](0004-event-driven-monitoring.md) | Event-driven monitoring, no polling loops | accepted |
| [0005](0005-least-privilege-pinner.md) | Least-privilege pinner container (NET_ADMIN only) | accepted |
| [0006](0006-route-management.md) | Route management with default-gateway fallback | accepted |
| [0007](0007-configurable-egress-node-selector.md) | Configurable egress node selector | accepted |

## Creating a new ADR

1. Copy the most recent ADR as a template and take the next sequence number.
2. Keep the filename in the form `NNNN-short-kebab-title.md`.
3. Open a pull request; the ADR is `proposed` until the PR merges, then `accepted`.
4. Never delete an ADR. Supersede it: set the old one's status to
   `superseded by [ADR-XXXX](XXXX-....md)` and reference the old one from the new one.
