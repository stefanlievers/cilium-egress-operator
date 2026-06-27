# cilium-egress-operator

A Kubernetes operator that abstracts `CiliumEgressGatewayPolicy` into a simple, self-healing custom resource — purpose-built for on-premises environments with BGP fabric.

## The Problem

Running Cilium egress gateways on bare-metal Kubernetes with BGP requires coordinating multiple low-level resources:

- `CiliumEgressGatewayPolicy` — which pods leave via which node
- Egress IP assignment — the IP must physically exist on a network interface
- BGP advertisement — the IP must be announced to the upstream fabric
- Node lifecycle — when a node is replaced, labels and IPs must recover automatically

There is no single resource that manages this end-to-end. Until now.

## The Solution

`cilium-egress-operator` introduces a single `EgressGateway` custom resource that owns the full egress lifecycle:

```yaml
apiVersion: egress.cilium-egress-operator.io/v1alpha1
kind: EgressGateway
metadata:
  name: crown-jewels
spec:
  egressIP: "10.255.26.10"
  interface: "egress0"
  podSelector:
    matchLabels:
      egress-pool: crown-jewels
  namespaceSelector:
    matchLabels:
      egress-pool: crown-jewels
  destinations:
    - cidr: "10.20.30.0/24"
```

The operator reconciles this into:

| Resource | Description |
|---|---|
| `CiliumEgressGatewayPolicy` | Cilium policy with correct selectors |
| Node label | Deterministic egress node selection |
| DaemonSet | Pins the egress IP to the interface on boot/recovery |

## How It Works

### Node Selection

The operator watches all Node events. When no node carries the `egress-node: "true"` label, the controller deterministically selects the first control-plane node (sorted by name) and labels it. No webhooks, no polling — pure event-driven reconciliation.

```
Node event received
    ↓
Any node with egress-node: "true"?
    ├── Yes → do nothing
    └── No  → label controlPlaneNodes[0] (sorted by name)
```

### IP Lifecycle

A `DaemonSet` with `nodeSelector: egress-node: "true"` runs on the egress node and ensures the egress IP is present on the configured interface:

- **On startup** — sets the IP immediately
- **On node replacement** — DaemonSet reschedules automatically when the new node receives the label
- **On OS reboot** — DaemonSet pod restarts and repins the IP

No CronJob. No periodic polling. Event-driven at every layer.

### Ownership

All generated resources carry `ownerReferences` back to the `EgressGateway` CR. Deleting the CR cascades cleanup automatically — no orphaned Cilium policies or dangling IPs.

## Architecture

```
┌─────────────────────────────────────────┐
│            EgressGateway CR             │
└────────────────┬────────────────────────┘
                 │ owns
       ┌─────────┼──────────────┐
       ▼         ▼              ▼
 CiliumEgress  Node label    DaemonSet
 GatewayPolicy (egress-node)  (IP pinner)
```

## Prerequisites

- Kubernetes 1.28+
- Cilium 1.14+ with egress gateway enabled
- BGP peering configured (Cilium BGP Control Plane)
- Control-plane nodes accessible by the operator

## Installation

### Helm (recommended)

```bash
helm install cilium-egress-operator ./chart \
  --namespace cilium-egress-operator \
  --create-namespace
```

### kubectl

```bash
kubectl apply -f config/crd/egressgateway.yaml
kubectl apply -f config/rbac/
kubectl apply -f config/manager/manager.yaml
```

## Usage

Apply an `EgressGateway` resource:

```bash
kubectl apply -f config/samples/egress_v1alpha1_egressgateway.yaml
```

Check status:

```bash
kubectl get egressgateway crown-jewels -o yaml
```

Expected status:

```yaml
status:
  egressNode: "rke2-cp-01"
  egressIPConfirmed: true
  ciliumPolicyReady: true
  lastReconciled: "2026-06-27T10:00:00Z"
```

## Compatibility

| cilium-egress-operator | Cilium | Kubernetes |
|---|---|---|
| v0.1.x | 1.14 – 1.16 | 1.28 – 1.31 |

## Design Decisions

**Why a DaemonSet instead of a CronJob?**
CronJobs poll on a fixed interval regardless of actual drift. A DaemonSet starts immediately on node scheduling, pins the IP on boot, and requires zero periodic overhead.

**Why deterministic node selection instead of a webhook?**
A MutatingWebhookConfiguration introduces an availability dependency — if the webhook is down, nodes cannot join. Level-triggered reconciliation in the controller achieves the same result without that risk.

**Why not just use Cilium's built-in node selection?**
Cilium's `CiliumEgressGatewayPolicy` requires the egress node label to already exist. It provides no mechanism to set or recover that label. This operator fills exactly that gap.

## Roadmap

- [ ] v0.1.0 — Core reconciliation loop, node labeling, DaemonSet, CiliumEgressGatewayPolicy
- [ ] v0.2.0 — Status conditions (KEP-1623 compliant)
- [ ] v0.3.0 — Multi-egress-IP support (multiple EgressGateway CRs per cluster)
- [ ] v0.4.0 — Metrics (Prometheus) and alerting rules
- [ ] v1.0.0 — Production-ready, full test coverage

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](LICENSE).

## Background

Built from production experience running Cilium BGP on bare-metal Kubernetes with Cisco ACI fabric in a Dutch government sovereign cloud environment. Presented at [KubeCon + CloudNativeCon](https://events.linuxfoundation.org/kubecon-cloudnativecon-europe/).
