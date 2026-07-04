# Pin the egress IP with a DaemonSet, not SSH or a node agent

- Status: accepted
- Date: 2026-07-04

## Context and Problem Statement

The egress IP must physically exist on a network interface of the egress node, and must
survive OS reboots, node replacement, and accidental removal. How should the operator get
`ip addr add` executed on exactly the right node, continuously?

## Decision Drivers

- No SSH: sovereign environments require auditable, in-cluster mechanisms.
- Survive reboots and node replacement without human action.
- Low resource footprint; no periodic jobs (project guardrail).
- Cascade cleanup when the `EgressGateway` resource is deleted.

## Considered Options

1. A per-gateway **DaemonSet** with `nodeSelector: egress-node: "true"` and `hostNetwork`.
2. A CronJob that periodically reasserts the IP.
3. A privileged exec from the controller into a helper pod.
4. An out-of-band agent (systemd unit) installed on nodes.

## Decision Outcome

Chosen option: **1 — DaemonSet**. Scheduling *is* the failover mechanism: when the label
moves to a new node, kubelet starts the pinner there automatically; on reboot the pod
restarts and repins. The DaemonSet carries an ownerReference to the `EgressGateway`, so
deletion cascades. A readiness probe verifies the IP is actually on the interface, which the
controller reflects as `status.egressIPConfirmed`.

### Consequences

- Good: zero SSH, zero polling, automatic recovery on every node lifecycle event.
- Good: the pod's Ready condition doubles as verification signal.
- Bad: requires `hostNetwork` and `CAP_NET_ADMIN` on one pod (bounded by [ADR-0005](0005-least-privilege-pinner.md)).
- Bad: one pinner pod per `EgressGateway` on the same node; acceptable at expected scale
  (single-digit gateways per cluster).
