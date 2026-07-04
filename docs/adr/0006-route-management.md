# Route management with default-gateway fallback

- Status: accepted
- Date: 2026-07-04

## Context and Problem Statement

After Cilium SNATs traffic to the egress IP, the egress node must know where to send
packets for the destination CIDR. On simple networks the node's default route covers this,
but on segmented on-prem fabrics the destination may require a specific next-hop. Manually
adding routes over SSH has the same problems as manual IP assignment. What should the
operator create, and from what configuration?

## Decision Drivers

- Must be configurable in the `EgressGateway` manifest (user requirement).
- Zero-config should do the right thing on the common topology: traffic exits along the
  node's existing default path.
- Idempotency guardrail: applying the same spec twice must not churn routes.
- Not every environment wants node routes at all (BGP fabric may handle everything).

## Considered Options

1. `createRoutes: false` by default; when enabled, one route per destination
   `ip route add <cidr> via <nextHop> dev <iface> src <egressIP>`, where `nextHop` is
   optional and defaults to the node's **current default gateway**, discovered at runtime.
2. Always create routes for every destination.
3. Require an explicit `nextHop` for every destination.
4. Manage routes from the controller via a separate privileged job.

## Decision Outcome

Chosen option: **1**. Route creation is opt-in (`createRoutes`), keeping the default
behavior identical to plain Cilium. The per-destination `nextHop` covers segmented fabrics;
the default-gateway fallback covers the common case without configuration. Routes are
managed by the same pinner DaemonSet ([ADR-0002](0002-daemonset-ip-pinning.md)) and checked
before being (re)applied, so repeated application is a no-op. The `src <egressIP>` hint is
included so node-originated traffic to the destination also uses the egress IP; forwarded
(SNAT'ed) traffic is unaffected by it.

### Consequences

- Good: zero-config correctness on flat networks, explicit control on segmented ones.
- Good: routes recover together with the IP on reboot and node replacement.
- Bad: a route toward a CIDR the fabric also advertises can shadow fabric routing —
  the operator trusts the user's spec; documented in the README considerations.
- Bad: default-gateway discovery reads node state at apply time; if the default route
  changes later, the route is re-evaluated only on the next netlink event
  ([ADR-0004](0004-event-driven-monitoring.md)).
