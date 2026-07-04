# Event-driven monitoring, no polling loops

- Status: accepted
- Date: 2026-07-04

## Context and Problem Statement

The egress IP and routes can disappear at any time (interface flaps, NetworkManager or
wicked rewrites, manual `ip addr del`). The pinner must notice and repair drift, but a
project guardrail forbids constant loops and periodic jobs unless strictly necessary.
How does the pinner detect drift without burning CPU on polling?

## Decision Drivers

- Low CPU/RAM guardrail: no busy loops, no CronJobs.
- Repair latency should be near-instant, not "next poll interval".
- The default container image (`alpine:3.19`) ships BusyBox `ip`, which lacks `ip monitor`.
- Air-gapped clusters may not be able to install iproute2 at startup.

## Considered Options

1. Block on `ip monitor address route` (kernel netlink events) and re-apply on every event.
2. `sleep`-based check loop at a fixed interval.
3. Kubernetes liveness probe as the only drift detector (kubelet restarts the pod on failure).

## Decision Outcome

Chosen option: **1, with 2 as explicit fallback**. The pinner script attempts to install
iproute2 (silently skipped when no registry is reachable), detects whether real iproute2 is
present, and then blocks on `ip monitor address route` — the process sleeps in the kernel
until an address or route actually changes. Only when `ip monitor` is unavailable
(BusyBox-only image in an air-gapped cluster) does it fall back to a 60-second check, and
the `spec.pinnerImage` field lets operators supply an iproute2-capable image to stay fully
event-driven.

At the controller level the same principle applies: reconciles are triggered by watches
(`EgressGateway`, owned DaemonSets, Node events) — never by `RequeueAfter` polling.

### Consequences

- Good: near-zero steady-state CPU; sub-second repair when the IP is removed.
- Good: kubelet's readiness probe independently verifies the IP every 30s, feeding
  `status.egressIPConfirmed` without extra controller work.
- Bad: two code paths (monitor vs. fallback) in the pinner script.
- Bad: the startup `apk add` adds a registry dependency in the default image; documented,
  and avoidable via `pinnerImage`.
