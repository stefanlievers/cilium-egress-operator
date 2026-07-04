# Deterministic control-plane node selection for the egress label

- Status: accepted
- Date: 2026-07-04

## Context and Problem Statement

`CiliumEgressGatewayPolicy` selects the egress node by label, but nothing in OSS Cilium
sets or recovers that label. When the labeled node is drained, replaced, or lost, egress
traffic blackholes until a human re-labels a node. Which component should assign the label,
and to which node?

## Decision Drivers

- Recovery must be automatic and fast on node events.
- Selection must be deterministic — two reconciles must not pick different nodes.
- No new availability dependencies (a webhook that is down blocks node admission).
- Control-plane nodes are the most stable, least-drained nodes in the target RKE2 clusters.

## Considered Options

1. Controller labels the **alphabetically first control-plane node** when no node carries
   the label, triggered by Node watch events.
2. A MutatingWebhook that labels nodes at admission.
3. Manual labeling, documented as a prerequisite.
4. Leader-election-style annotation lease between candidate nodes.

## Decision Outcome

Chosen option: **1**. The controller watches Node events and, when no `egress-node: "true"`
node exists, sorts control-plane nodes by name and labels the first. Sorting makes the
choice reproducible; the Node watch makes recovery event-driven; an existing label is always
respected (users can pre-label any node, including workers, to override the choice).

### Consequences

- Good: self-healing with no polling and no webhook availability risk.
- Good: user override is trivial — label the node you want; the operator backs off.
- Bad: the label is cluster-global, so all gateways share one egress node for now
  (per-gateway labels are a roadmap item).
- Bad: the operator does not remove extra labels; if a user labels two nodes, both run the
  pinner. Documented as a limitation.
