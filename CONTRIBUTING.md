# Contributing to cilium-egress-operator

Thank you for your interest in contributing. This project welcomes contributions of all kinds — bug reports, documentation, tests, and code.

## Getting Started

### Prerequisites

- Go 1.22+
- kubebuilder v3+
- kubectl
- Access to a Kubernetes cluster with Cilium installed (or a local k3s/kind cluster)

### Local Development

```bash
git clone https://github.com/stefanvangastel/cilium-egress-operator
cd cilium-egress-operator

# Install CRDs into your cluster
make install

# Run the controller locally (uses your current kubeconfig context)
make run
```

### Running Tests

```bash
make test
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Commit with clear messages following [Conventional Commits](https://www.conventionalcommits.org/)
4. Open a pull request against `main`
5. Ensure CI passes

## Reporting Issues

Please include:
- Kubernetes version
- Cilium version
- `EgressGateway` CR spec (redact sensitive IPs if needed)
- Operator logs (`kubectl logs -n cilium-egress-operator deploy/cilium-egress-operator`)
- Output of `kubectl get egressgateway -o yaml`

## Code of Conduct

Be kind. This project follows the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md).
