# OKS (Odri Kubernetes Service)

`oks` logs you into Vault, lists clusters, and writes one cluster kubeconfig.

## Layout

```
cmd/oks/            command entrypoint
internal/cli/       auth login, cluster list, and kubeconfig commands
internal/auth/      Vault userpass login and the saved token
internal/cluster/   cluster name and clusters/data/<cluster> path
internal/vaultkv/   Vault client and KV v2 list and read
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

Log in once. The browser asks for the username and password. The authenticator code is optional. The Vault token is saved to `~/.config/oks/token`, and later commands use it. A saved token that Vault still accepts does not open the browser again.

```bash
oks auth login
oks clusters --list
oks clusters --get --cluster kind-odri-cluster --namespace workspacepro-prod
kubectl get ns
```

`--get` writes the kubeconfig and switches kubectl to the cluster context. Optional flags on `--get` are `--namespace`, `--o`, `--path`, `--field`, and `--overwrite`.

```bash
oks clusters --add --cluster name --file ./kubeconfig
oks clusters --delete --cluster name
```

Vault address defaults to `https://vault.odrisystems.com`. The userpass mount is `userpass` (`oks auth login --auth userpass`).

Vault secret path defaults to `clusters/data/<cluster>`.

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

