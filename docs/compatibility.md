# Compatibility & Version Boundaries

This document defines which platform versions the operator supports, what "supported"
means, and the hard functional boundaries of the current release line.

## Version matrix

| cilium-egress-operator | Kubernetes | Cilium | Go (build) | Status |
|---|---|---|---|---|
| v0.2.x | 1.30 – 1.36 | 1.14 – 1.18 | 1.26 | current |
| v0.1.x | 1.30 – 1.36 | 1.14 – 1.18 | 1.26 | security fixes only |

**Tested** means covered by CI (envtest against Kubernetes 1.36) or verified on a real
cluster. **Expected** means within the supported skew of our client libraries
(`k8s.io/client-go v0.36`, `controller-runtime v0.24`) but not explicitly exercised.
Currently: Kubernetes 1.36 is tested; 1.30–1.35 are expected. Report deviations as bugs.

### Distribution notes

- **RKE2** is the primary target platform and where real-cluster verification happens.
  Any conformant Kubernetes distribution running Cilium as the CNI should behave
  identically; distribution-specific issues are welcome as bug reports.
- The operator has no dependency on Rancher components.

## Required Cilium configuration

The operator complements Cilium's egress gateway; Cilium itself imposes these requirements
(see the [Cilium egress gateway documentation](https://docs.cilium.io/en/stable/network/egress-gateway/)):

| Setting | Required value | Why |
|---|---|---|
| `routingMode` | `native` | Cilium's egress gateway does not support tunnel mode |
| `egressGateway.enabled` | `true` | Enables the datapath feature |
| `kubeProxyReplacement` | `true` | Cilium requirement for egress gateway |
| `bpf.masquerade` | `true` | SNAT to the egress IP happens in BPF |
| BGP Control Plane | peered with your fabric | The pinned egress IP must be routable from outside |

## Hard boundaries (v0.1.x)

These are enforced or assumed by the current release line; violating them is unsupported,
not merely untested:

- **IPv4 only.** CRD validation rejects IPv6 addresses and CIDRs in `egressIP`,
  `destinations[].cidr`, and `destinations[].nextHop`.
- **Native routing only.** Tunnel mode (VXLAN/Geneve) is unsupported by Cilium's egress
  gateway and therefore by this operator.
- **One egress node per cluster.** The `egress-node: "true"` label is cluster-global and
  shared by all `EgressGateway` resources.
- **Linux egress nodes.** The pinner uses `ip`(8) via a hostNetwork container.
- **The egress IP must be free.** The operator adds the IP as `/32`; it does not detect or
  resolve conflicts with IPs already claimed elsewhere on the network.

## API stability

The `EgressGateway` API is **v1alpha1**: fields may change between minor releases while in
alpha, though we avoid breaking changes where possible and document every change in the
[CHANGELOG](../CHANGELOG.md). The API graduates to `v1beta1` once `CiliumEgressGatewayPolicy`
generation (opt-in) and status conditions land, and to `v1` at operator v1.0.0.

## Support policy

- The latest minor release line receives bug and security fixes.
- Security fixes may be backported one minor version at the maintainers' discretion
  (see [SECURITY.md](../SECURITY.md)).
- New Kubernetes minors are picked up by dependency updates; the matrix above is updated
  with each operator release.
