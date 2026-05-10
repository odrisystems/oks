# OKS (Odri Kubernetes Service)

`oks` fetches Kubernetes kubeconfig material from HashiCorp Vault and safely merges it into your kubeconfig (similar to `gcloud ... get-credentials`).

## Install

From GitHub Releases (recommended):

```bash
curl -fsSL https://raw.githubusercontent.com/odrisystems/oks/main/install.sh | bash
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/odrisystems/oks/main/install.sh | bash -s -- --version v0.0.0-nightly.20260510.120605.acfb8d87a258
```

## Usage

Vault secret path defaults to:

- `secret/data/clusters/<cluster>`

Example:

```bash
export VAULT_ADDR="https://vault.example.com"
export VAULT_TOKEN="..."

oks -cluster kind-odri-cluster -namespace workspacepro-prod
kubectl config use-context kind-odri-cluster
kubectl get ns
```

### Vault secret formats

- **Full kubeconfig YAML** stored under one key:
  - `kubeconfig` (or `config`, `kube_config`, `content`)

- **Structured fields**:
  - `server`
  - `certificate_authority_data` (base64) or `certificate_authority` (PEM)
  - `token` (or `client_certificate_data` + `client_key_data`)
  - optional `namespace`

## Releasing

Create a tag like `v0.1.0` and push it. GitHub Actions will publish release assets:

- `oks_<version>_<os>_<arch>.tar.gz` (linux/darwin)
- `oks_<version>_<os>_<arch>.zip` (windows)

