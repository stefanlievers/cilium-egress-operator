# Complement, don't replace, CiliumEgressGatewayPolicy

- Status: accepted
- Date: 2026-07-04

## Context and Problem Statement

Cilium OSS provides `CiliumEgressGatewayPolicy`, which SNATs selected pod traffic to an
egress IP via a selected node. The policy itself works reliably. What OSS Cilium does *not*
provide is everything around it: nothing labels the egress node, nothing puts the egress IP
on a network interface, and nothing creates routes toward the destination. Should this
operator own the Cilium policy too, or only the surrounding gaps?

## Decision Drivers

- The core pain in production is the manual, un-auditable work (SSH + `ip addr add`,
  manual node labels) — not the policy definition.
- Owning the policy couples the operator to Cilium's CRD schema and version lifecycle.
- Users may already manage `CiliumEgressGatewayPolicy` via GitOps and should not be forced
  to migrate it.

## Considered Options

1. Operator generates and owns the `CiliumEgressGatewayPolicy` from the `EgressGateway` spec.
2. Operator only manages the surroundings (label, IP, routes); the user keeps owning the policy.
3. Hybrid: surroundings always, policy generation opt-in.

## Decision Outcome

Chosen option: **2 — manage only the surroundings** for the first releases. The
`EgressGateway` spec already carries `podSelector`/`namespaceSelector`/`destinations` so that
option 3 (opt-in policy generation) can be added later without an API break; the
`status.ciliumPolicyReady` field is reserved for that future.

### Consequences

- Good: no dependency on Cilium's Go modules or CRD versioning; smaller attack and
  compatibility surface.
- Good: adoptable incrementally next to existing GitOps-managed policies.
- Bad: users must keep the policy's `nodeSelector` (`egress-node: "true"`) and `egressIP`
  consistent with the `EgressGateway` resource by hand until opt-in generation lands.
