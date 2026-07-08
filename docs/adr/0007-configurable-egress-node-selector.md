# Configurable egress node selector

- Status: accepted
- Date: 2026-07-08

## Context and Problem Statement

The egress node was identified by the hardcoded label `egress-node: "true"`, making the
label cluster-global: every `EgressGateway` shared one egress node, and users could not
align the operator with existing labeling conventions or their `CiliumEgressGatewayPolicy`
nodeSelectors. How should the label become configurable without breaking the zero-config
default?

## Decision Drivers

- Users' `CiliumEgressGatewayPolicy` resources already carry a `nodeSelector`; the operator
  should be able to match whatever convention is in use.
- Different gateways should be able to use different egress nodes (roadmap item).
- The default must stay `egress-node: "true"` so existing installs upgrade unchanged.

## Considered Options

1. `spec.nodeSelector` map (native Kubernetes shape), defaulted to `{egress-node: "true"}`,
   used for discovery, election labeling, and the pinner DaemonSet's nodeSelector.
2. A single `spec.egressLabelKey` / `spec.egressLabelValue` string pair.
3. Operator-wide flag (one override for all gateways).

## Decision Outcome

Chosen option: **1**. A label map is the shape users already know from pod specs and from
`CiliumEgressGatewayPolicy` itself, and it composes: all labels in the map are applied to
the elected node and all must match during discovery. The CRD defaults the field, and the
controller falls back to the same default in code for objects created before the default
existed. Selector values never reach a shell — they only flow into Kubernetes API objects,
which the API server validates.

### Consequences

- Good: gateways with distinct selectors elect and use independent egress nodes.
- Good: drop-in alignment with existing Cilium policy selectors.
- Bad: changing the selector on a live gateway elects a new node but does not remove the
  old labels (consistent with [ADR-0003](0003-deterministic-node-selection.md): the
  operator labels, it does not un-label); the pinner moves with the selector, so the old
  node stops pinning the IP.
- Bad: two gateways with overlapping-but-different selectors can elect the same node twice
  with different labels; harmless, but users should keep selectors either identical or
  disjoint.
