# cilium-egress-operator

[![Tests](https://github.com/stefanlievers/cilium-egress-operator/actions/workflows/test.yml/badge.svg)](https://github.com/stefanlievers/cilium-egress-operator/actions/workflows/test.yml)
[![Lint](https://github.com/stefanlievers/cilium-egress-operator/actions/workflows/lint.yml/badge.svg)](https://github.com/stefanlievers/cilium-egress-operator/actions/workflows/lint.yml)
[![Go Version](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)
[![API](https://img.shields.io/badge/API-v1alpha1-orange)](api/v1alpha1/egressgateway_types.go)

> [!WARNING]
> This project is 'vibe' coded.
> I'm a Kubernetes engineer missing the features this project provides.
> However I'm not a developer, Claude Code created the code on my request.
> Please proceed carefully if you find this project and want to use it.
> Kind regards, Stefan
> 2026-07-04

A Kubernetes operator that automates everything open-source Cilium leaves **manual** around egress gateways — node labeling, egress IP assignment, and routing — purpose-built for on-premises environments with BGP fabric (native routing mode).

## The Problem

Cilium's `CiliumEgressGatewayPolicy` works well, but it only handles the SNAT policy. On bare-metal Kubernetes it silently assumes that somebody has already:

1. **Labeled an egress node** — the policy's `nodeSelector` matches a label that nothing sets or recovers.
2. **Assigned the egress IP** — the IP must physically exist on a network interface, or return traffic is dropped. Today that means SSH-ing into the node and running `ip addr add`.
3. **Ensured the destination is routable** — the node needs a route toward the destination CIDR.

None of this survives node replacement, OS reboots, or cluster rebuilds without manual intervention.

## The Solution

`cilium-egress-operator` introduces an `EgressGateway` custom resource that owns the operational lifecycle *around* your Cilium egress gateway policy:

```yaml
apiVersion: egress.cilium-egress-operator.io/v1alpha1
kind: EgressGateway
metadata:
  name: egress-external
spec:
  # The IP that selected traffic leaves the cluster with; pinned as /32
  # on the egress node's interface.
  egressIP: "10.255.26.10"

  # Interface on the egress node to pin the IP to (default: eth0).
  interface: "eth0"

  # Which kind of node to elect when no node matches the selector yet:
  # control-plane (default) or worker.
  nodeRole: worker

  # Labels identifying the egress node (default: egress-node: "true").
  # Applied to the elected node and used to schedule the IP pinner.
  # Gateways with different selectors get independent egress nodes.
  # Keep in sync with your CiliumEgressGatewayPolicy nodeSelector.
  nodeSelector:
    egress-zone: dmz

  # Pods that use this egress gateway...
  podSelector:
    matchLabels:
      egress-pool: external
  # ...optionally narrowed to namespaces matching this selector.
  namespaceSelector:
    matchLabels:
      environment: production

  # Destination CIDRs reached via the gateway. With createRoutes: true,
  # one route per destination is maintained on the egress node.
  destinations:
    - cidr: "10.20.30.0/24"
      nextHop: "10.255.26.1"   # optional; defaults to the node's default gateway
    - cidr: "192.168.40.0/22"  # no nextHop: routed via the node's default gateway
  createRoutes: true

  # Optional: pinner image override for air-gapped clusters; the image
  # should provide iproute2 for event-driven monitoring (default: alpine:3.19).
  pinnerImage: "registry.internal.example/network/alpine-iproute2:3.19"
```

The operator reconciles this into:

| Concern | Mechanism |
|---|---|
| Egress node selection | Deterministic `egress-node: "true"` label on a control-plane node, recovered on node events |
| Egress IP on the interface | Per-gateway **IP pinner DaemonSet** that idempotently pins the IP and restores it on loss |
| Destination routing | Optional per-destination routes on the egress node (`createRoutes: true`) |
| Observability | `status.egressNode`, `status.egressIPConfirmed`, `status.lastReconciled` |

> **Scope note:** the operator deliberately does **not** create the `CiliumEgressGatewayPolicy` itself (yet — see [Roadmap](#roadmap)). The policy works fine on its own; this operator fills the gaps around it. See [ADR-0001](docs/adr/0001-complement-cilium-egress-gateway-policy.md).

## How It Works

### Node selection

The operator watches all Node events. When no node matches the gateway's egress selector — `egress-node: "true"` by default, overridable per gateway via `spec.nodeSelector` ([ADR-0007](docs/adr/0007-configurable-egress-node-selector.md)) — the controller deterministically selects a candidate node (sorted by name) and applies the selector labels to it. No webhooks, no polling — pure event-driven reconciliation ([ADR-0003](docs/adr/0003-deterministic-node-selection.md)).

Which nodes are candidates is controlled by `spec.nodeRole`:

- `control-plane` (default) — nodes with the `node-role.kubernetes.io/control-plane` label
- `worker` — nodes *without* a control-plane (or legacy master) label

A node already matching the selector is always respected, regardless of `nodeRole` — pre-label any node yourself to override the automatic choice. Gateways with different `nodeSelector` values elect **independent** egress nodes; keep the selector in sync with the `nodeSelector` of your `CiliumEgressGatewayPolicy`.

```
Node event received
    ↓
Any node matching spec.nodeSelector (default egress-node: "true")?
    ├── Yes → do nothing
    └── No  → apply selector labels to candidates(nodeRole)[0] (sorted by name)
```

> [!WARNING]
> **Workloads on the egress node itself do not use the egress IP — by design.** Traffic from pods co-located with the gateway does not traverse the egress redirection path that traffic from other nodes takes through the BPF datapath, and can leave the node with its regular source addressing. Cilium's [egress gateway documentation](https://docs.cilium.io/en/stable/network/egress-gateway/egress-gateway/) does not cover the same-node case explicitly, so treat co-location as unsupported: keep egress-selected workloads off the egress node (taints, anti-affinity, or a dedicated node), and use `nodeRole` to steer the label toward a node class your workloads don't run on. See also the [egress gateway troubleshooting guide](https://docs.cilium.io/en/stable/network/egress-gateway/egress-gateway-troubleshooting/).

### IP lifecycle

A DaemonSet with `nodeSelector: egress-node: "true"` runs on the egress node and ensures the egress IP is present on the configured interface ([ADR-0002](docs/adr/0002-daemonset-ip-pinning.md)):

- **On startup** — pins the IP immediately.
- **On IP loss** — an `ip monitor` watch restores it the moment it disappears (event-driven; a periodic check is only used as fallback when iproute2 is unavailable, [ADR-0004](docs/adr/0004-event-driven-monitoring.md)).
- **On node replacement** — the DaemonSet reschedules automatically when the new node receives the label.
- **On OS reboot** — the pod restarts and repins the IP and routes.

The pod becomes **Ready** only when the IP is verifiably on the interface; the controller reflects this as `status.egressIPConfirmed`.

### Route management

With `createRoutes: true`, the pinner also maintains one route per destination:

```
ip route add <cidr> via <nextHop> dev <interface> src <egressIP>
```

When `nextHop` is omitted, the node's current default gateway is discovered and used — traffic follows the node's existing exit path ([ADR-0006](docs/adr/0006-route-management.md)).

### Ownership

All generated resources carry `ownerReferences` back to the `EgressGateway` CR. Deleting the CR cascades cleanup automatically — no dangling DaemonSets.

## Architecture

```
┌─────────────────────────────────────────┐
│            EgressGateway CR             │
└────────────────┬────────────────────────┘
                 │
       ┌─────────┴──────────────┐
       ▼                        ▼
  Node label              IP pinner DaemonSet
 (egress-node)          (egress IP + routes)
       │                        │
       └────────┬───────────────┘
                ▼
   CiliumEgressGatewayPolicy (yours)
   selects the labeled node & SNATs
   to the pinned egress IP
```

## Prerequisites

- Kubernetes **1.30+** (tested against 1.36)
- Cilium **1.14+** configured with:
  - `routingMode: native` (tunnel mode is unsupported — see [compatibility](docs/compatibility.md))
  - `egressGateway.enabled: true`
  - `kubeProxyReplacement: true` and `bpf.masquerade: true` (Cilium egress gateway requirements)
  - BGP peering (Cilium BGP Control Plane) so the egress IP is advertised to your fabric
- IPv4 egress IPs and destinations (IPv6 is not yet supported)

See [docs/compatibility.md](docs/compatibility.md) for the full version matrix and support policy.

## Installation

### From a release (recommended)

Every [release](https://github.com/stefanlievers/cilium-egress-operator/releases) ships a consolidated `install.yaml` (CRD, RBAC, and controller Deployment) with a pinned image from GHCR:

```bash
kubectl apply -f https://github.com/stefanlievers/cilium-egress-operator/releases/latest/download/install.yaml
```

Or pin a specific version:

```bash
kubectl apply -f https://github.com/stefanlievers/cilium-egress-operator/releases/download/v0.1.0/install.yaml
```

Releases follow [Semantic Versioning](https://semver.org) (`major.minor.patch`); see [docs/compatibility.md](docs/compatibility.md) for which platform versions each release line supports.

### Development / evaluation

```bash
git clone https://github.com/stefanlievers/cilium-egress-operator
cd cilium-egress-operator

# Install the CRD into the cluster of your current kubeconfig context
make install

# Run the controller locally against that cluster
make run
```

A Helm chart is on the [roadmap](#roadmap).

## Usage

Apply an `EgressGateway` resource:

```bash
kubectl apply -f config/samples/egress_v1alpha1_egressgateway.yaml
```

Check status:

```bash
$ kubectl get egressgateway
NAME              EGRESSIP       NODE         IPCONFIRMED   POLICYREADY   AGE
egress-external   10.255.26.10   rke2-cp-01   true                        2m
```

### Spec reference

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `egressIP` | string (IPv4) | yes | — | IP to pin on the egress node's interface (added as `/32`) |
| `interface` | string | no | `eth0` | Interface on the egress node; restricted to valid Linux interface names |
| `nodeRole` | `control-plane` \| `worker` | no | `control-plane` | Which kind of node to label as egress node when none is labeled yet |
| `nodeSelector` | map[string]string | no | `egress-node: "true"` | Labels identifying the egress node; applied on election, used by the pinner. Different selectors per gateway give independent egress nodes |
| `podSelector` | LabelSelector | yes | — | Pods that will use this egress gateway |
| `namespaceSelector` | LabelSelector | no | — | Limits selected pods to matching namespaces |
| `destinations` | list | yes (min 1) | — | Destination CIDRs reached via the gateway |
| `destinations[].cidr` | string (IPv4 CIDR) | yes | — | Destination network |
| `destinations[].nextHop` | string (IPv4) | no | node default gateway | Next-hop for the route (only with `createRoutes: true`) |
| `createRoutes` | bool | no | `false` | Manage a route per destination on the egress node |
| `pinnerImage` | string | no | `alpine:3.19` | Image for the pinner DaemonSet; override in air-gapped environments with an image that provides iproute2 |

### Status reference

| Field | Description |
|---|---|
| `egressNode` | Node currently labeled as egress gateway |
| `egressIPConfirmed` | `true` once the pinner pod's readiness probe verifies the IP is on the interface |
| `lastReconciled` | Timestamp of the last successful reconciliation |
| `ciliumPolicyReady` | Reserved for future `CiliumEgressGatewayPolicy` management (currently unset) |

## Security Model

Security is a first-class design constraint ([SECURITY.md](SECURITY.md), [ADR-0005](docs/adr/0005-least-privilege-pinner.md)):

- The pinner container runs **without `privileged`** — only `CAP_NET_ADMIN`, with all other capabilities dropped and `allowPrivilegeEscalation: false`.
- Every spec value interpolated into the pinner script is constrained by **CRD validation patterns** (IPv4 addresses, CIDRs, Linux interface names) and shell-quoted — no injection surface.
- The operator's RBAC is minimal: nodes (label patching), DaemonSets (owned), and its own CRD.
- CI runs `golangci-lint` and the codebase is scanned with `govulncheck`.
- CPU/memory requests and limits are set on the pinner (5m/16Mi requested, 50m/64Mi limit).

## Design Considerations & Limitations

Honest notes on current boundaries — most of these are tracked on the roadmap:

- **One egress node per selector.** Gateways sharing a `nodeSelector` (including the default) share one egress node — the first gateway to reconcile elects it. Use distinct selectors for independent egress nodes. If you manually label multiple nodes with the same selector, every one of them runs the pinner and would claim the IP — don't do that. Keep different selectors either identical or fully disjoint.
- **Don't co-locate egress-selected workloads with the gateway.** Pods on the egress node do not use the egress IP (see the warning under [Node selection](#node-selection)).
- **Keep destinations disjoint across gateways.** Two `EgressGateway` resources with `createRoutes: true`, the same destination CIDR, and different `nextHop` values will fight over the same node route.
- **The operator labels, but does not un-label.** If the egress node disappears, a new one is labeled automatically; a manually added second label is not removed.
- **IPv4 only.** CRD validation currently rejects IPv6 addresses and CIDRs.
- **Native routing / BGP only.** In tunnel mode (VXLAN/Geneve) Cilium's egress gateway is not supported by Cilium itself.
- **BGP advertisement is your fabric's job.** The operator pins the IP; your Cilium BGP configuration must advertise it (or your fabric must route it to the node).
- **Failover is disruptive by design.** When the egress node is replaced, the IP moves with the label; in-flight connections through the old node are reset.
- **The pinner tolerates control-plane, etcd, and `CriticalAddonsOnly` taints by default.** It is node-critical network infrastructure and must be able to run on tainted RKE2 server nodes. Nodes with other custom taints need those taints tolerated too — a `spec.tolerations` passthrough is planned.
- **The default pinner image needs a registry.** `alpine:3.19` installs iproute2 at startup when a registry is reachable; in air-gapped clusters, set `pinnerImage` to an internal image that ships iproute2 (busybox-only images fall back to a 60-second periodic check).

Deeper rationale lives in the [Architecture Decision Records](docs/adr/).

## Roadmap

- [x] **v0.1.0** — Core reconciliation (node labeling, IP pinning, routes, status), deployment manifests, GHCR image + `install.yaml` releases
- [x] **v0.2.0** — Configurable egress node selector: per-gateway independent egress nodes
- [ ] **v0.3.0** — Helm chart, status `conditions` (KEP-1623 compliant), optional `CiliumEgressGatewayPolicy` generation, egress node de-labeling / conflict resolution
- [ ] **v0.4.0** — IPv6 support, Prometheus metrics and alerting rules
- [ ] **v1.0.0** — Production-ready, stable `v1` API

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Significant design changes should come with an [ADR](docs/adr/).

## License

Apache 2.0 — see [LICENSE](LICENSE). Free to use for everyone, forever.

## Background

Built from production experience running Cilium in BGP/native-routing mode on on-premises RKE2 clusters within sovereign-boundary environments, where SSH-ing into nodes to `ip addr add` an egress IP is neither auditable nor acceptable.
