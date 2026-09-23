# OKS (Odri Kubernetes Service)

`oks` logs you into Vault in the browser with a username, password, and authenticator code, downloads one cluster kubeconfig, and writes it into your kubeconfig.

## Layout

```
cmd/oks/            command entrypoint
internal/cli/       flags and the login-then-write flow
internal/auth/      Vault userpass browser login
internal/cluster/   cluster name and clusters/data/<cluster> path
internal/vaultkv/   Vault client and KV v2 reads
internal/kube/      merge, overwrite, and kubeconfig assembly
```

## Install

From GitHub Releases (recommended):

```bash
curl -fsSL https://raw.githubusercontent.com/odrisystems/oks/main/install.sh | bash
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/odrisystems/oks/main/install.sh | bash -s -- --version v1.0.0
```

## Usage

Vault secret path defaults to:

- `clusters/data/<cluster>`

Example:

```bash
oks -cluster kind-odri-cluster -namespace workspacepro-prod
kubectl config use-context kind-odri-cluster
kubectl get ns
```

Vault address defaults to `https://vault.odrisystems.com`. Browser login uses the `userpass` mount (`-auth userpass`) and asks for the username, password, and authenticator code. Pass `-use-token` to skip the browser and use `VAULT_TOKEN` instead.

### Vault secret formats

- **Full kubeconfig YAML** stored under one key:
  - `kubeconfig` (or `config`, `kube_config`, `content`)

- **Structured fields**:
  - `server`
  - `certificate_authority_data` (base64) or `certificate_authority` (PEM)
  - `token` (or `client_certificate_data` + `client_key_data`)
  - optional `namespace`

## Releasing

A push to `main` publishes the next release. Versions step the minor number: `v1.0.0`, then `v1.1.0`, then `v1.2.0`. Pushes of tags do not build a release.

GitHub Actions builds the archives with GoReleaser and creates the GitHub Release:

- `oks_<version>_<os>_<arch>.tar.gz` (linux/darwin)
- `oks_<version>_<os>_<arch>.zip` (windows)
- `checksums.txt`

