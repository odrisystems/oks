package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/odrisystems/infrastructure/tools/oks/internal/auth"
	"github.com/odrisystems/infrastructure/tools/oks/internal/cluster"
	"github.com/odrisystems/infrastructure/tools/oks/internal/kube"
	"github.com/odrisystems/infrastructure/tools/oks/internal/vaultkv"
)

// Run parses args and writes the selected OKS kubeconfig. It returns a process exit code.
func Run(args []string) int {
	fs := flag.NewFlagSet("oks", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	clusterName := fs.String("cluster", "", "Cluster name (required). Vault path defaults to clusters/data/<cluster>")
	vaultAddr := fs.String("vault-addr", "", "Vault address (default VAULT_ADDR or https://vault.odrisystems.com)")
	authMount := fs.String("auth", "userpass", "Vault userpass mount to use for browser login")
	useToken := fs.Bool("use-token", false, "Skip browser login and use VAULT_TOKEN")
	mount := fs.String("mount", "", "KV v2 mount override (default clusters)")
	vaultPath := fs.String("path", "", "KV v2 API path override, for example clusters/data/kind-odri-cluster")
	namespace := fs.String("namespace", "", "Namespace to set on the kubeconfig context when the secret is structured fields")
	field := fs.String("field", "kubeconfig", "Secret field that holds the kubeconfig YAML. Empty assembles server/token/CA fields")
	outPath := fs.String("o", "", "Kubeconfig file to write. '-' prints YAML and does not merge. Empty uses KUBECONFIG or ~/.kube/config")
	overwrite := fs.Bool("overwrite", false, "Replace the kubeconfig file. Default merges this cluster in and leaves other contexts alone")
	setCurrent := fs.Bool("set-current-context", true, "After writing, make this cluster the current context")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oks -cluster <name> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Log in through Vault in the browser, fetch the OKS kubeconfig, and save it.\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	env, err := cluster.Lookup(*clusterName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fs.Usage()
		return 2
	}

	client, err := vaultkv.NewClient(*vaultAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vault client: %v\n", err)
		return 1
	}

	if *useToken {
		if client.Token() == "" {
			fmt.Fprintln(os.Stderr, "error: -use-token requires VAULT_TOKEN")
			return 1
		}
	} else {
		token, err := auth.UserpassLogin(client, *authMount)
		if err != nil {
			fmt.Fprintf(os.Stderr, "login: %v\n", err)
			return 1
		}
		client.SetToken(token)
	}

	path := strings.TrimSpace(*vaultPath)
	if path == "" {
		path = env.APIPath(*mount)
	}

	data, err := vaultkv.ReadKV2(client, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vault read %s: %v\n", path, err)
		return 1
	}

	ns := strings.TrimSpace(*namespace)
	if ns == "" {
		ns = strings.TrimSpace(data["namespace"])
	}

	kubeYAML, err := kube.Materialize(data, strings.TrimSpace(*field), env.Cluster, env.Cluster, env.Cluster, ns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kubeconfig: %v\n", err)
		return 1
	}

	out := strings.TrimSpace(*outPath)
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

	if *overwrite {
		if err := kube.WriteFile(out, kubeYAML); err != nil {
			fmt.Fprintf(os.Stderr, "write file: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "Wrote %s (%s) and replaced the file\n", out, env.Name)
		return 0
	}

	if err := kube.Merge(out, kubeYAML, *setCurrent); err != nil {
		fmt.Fprintf(os.Stderr, "merge kubeconfig: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "Updated %s with the %s context\n", out, env.Name)
	return 0
}
