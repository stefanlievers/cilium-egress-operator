# Least-privilege pinner container (NET_ADMIN only)

- Status: accepted
- Date: 2026-07-04

## Context and Problem Statement

The pinner must modify host network state (`ip addr`, `ip route`), which requires elevated
privileges on the node. The project guardrail says the operator must never endanger
clusters. What is the minimal privilege set, and how do we keep user input from becoming an
escalation path?

## Decision Drivers

- Runs on control-plane nodes — the most sensitive machines in the cluster.
- Spec values (`interface`, `egressIP`, CIDRs) are interpolated into a shell script.
- Reviewability for security-conscious adopters: the entire privileged surface should be
  readable in one place.

## Considered Options

1. `privileged: true` (simple, maximal).
2. `hostNetwork` + `CAP_NET_ADMIN` only, all other capabilities dropped.
3. A host-level agent outside Kubernetes (out of scope per [ADR-0002](0002-daemonset-ip-pinning.md)).

## Decision Outcome

Chosen option: **2**. The pinner runs with:

- `capabilities: { drop: [ALL], add: [NET_ADMIN] }` — sufficient for `ip addr`/`ip route`
- `allowPrivilegeEscalation: false`
- `hostNetwork: true` (required to see host interfaces)
- CPU/memory requests and limits (5m/16Mi — 50m/64Mi)

Injection is prevented one layer earlier, at the API: every value that reaches the script is
constrained by CRD validation patterns (IPv4 addresses `^(\d{1,3}\.){3}\d{1,3}$`, CIDRs,
Linux interface names `^[a-zA-Z0-9_.-]{1,15}$`) and additionally shell-quoted by the
controller. Values that fail validation never reach etcd, let alone a shell.

### Consequences

- Good: no `privileged` pod anywhere in the product; the threat model is "what can
  NET_ADMIN on the egress node do", not "root everywhere".
- Good: validation-at-admission means a compromised or buggy client cannot smuggle shell
  metacharacters through the CR.
- Bad: NET_ADMIN is still a powerful capability on a control-plane node; mitigated by the
  minimal, reviewable script and pinned resource limits.
