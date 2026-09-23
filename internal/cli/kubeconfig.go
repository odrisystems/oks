package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hashicorp/vault/api"
	"github.com/odrisystems/infrastructure/tools/oks/internal/cluster"
	"github.com/odrisystems/infrastructure/tools/oks/internal/kube"
	"github.com/odrisystems/infrastructure/tools/oks/internal/vaultkv"
)

func getCredentials(client *api.Client, clusterName, vaultPath, namespace, field, outPath string, overwrite bool) int {
	env, err := cluster.Lookup(clusterName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}

	path := strings.TrimSpace(vaultPath)
	if path == "" {
		path = env.APIPath("")
	}

	data, err := vaultkv.ReadKV2(client, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vault read %s: %v\n", path, err)
		return 1
	}

	ns := strings.TrimSpace(namespace)
	if ns == "" {
		ns = strings.TrimSpace(data["namespace"])
	}

	kubeYAML, err := kube.Materialize(data, strings.TrimSpace(field), env.Cluster, env.Cluster, env.Cluster, ns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kubeconfig: %v\n", err)
		return 1
	}
	kubeYAML, contextName, err := kube.Activate(kubeYAML, env.Cluster, ns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kubeconfig: %v\n", err)
		return 1
	}

	out := strings.TrimSpace(outPath)
	if out == "" {
		out = kube.DefaultPath()
	}
	if out == "-" {
		if _, err := io.WriteString(os.Stdout, kubeYAML); err != nil {
			fmt.Fprintf(os.Stderr, "write stdout: %v\n", err)
			return 1
		}
		return 0
	}

	if overwrite {
		if err := kube.WriteFile(out, kubeYAML); err != nil {
			fmt.Fprintf(os.Stderr, "write file: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "Wrote %s and switched to context %s\n", out, contextName)
		return 0
	}

	if err := kube.Merge(out, kubeYAML); err != nil {
		fmt.Fprintf(os.Stderr, "merge kubeconfig: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "Updated %s and switched to context %s\n", out, contextName)
	return 0
}
