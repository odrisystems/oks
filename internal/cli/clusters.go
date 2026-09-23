package cli

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hashicorp/vault/api"
	"github.com/odrisystems/infrastructure/tools/oks/internal/vaultkv"
)

func clustersCommand(args []string) int {
	fs := flag.NewFlagSet("oks clusters", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	list := fs.Bool("list", false, "List cluster names")
	add := fs.Bool("add", false, "Store a cluster kubeconfig in Vault")
	del := fs.Bool("delete", false, "Delete a cluster secret from Vault")
	get := fs.Bool("get", false, "Fetch a cluster kubeconfig and switch kubectl to it")
	clusterName := fs.String("cluster", "", "Cluster name. Required for --add, --delete, and --get")
	file := fs.String("file", "", "Kubeconfig file to store. Required for --add")
	namespace := fs.String("namespace", "", "Namespace to set on the context. Used with --get")
	vaultPath := fs.String("path", "", "Vault KV v2 path override. Used with --get")
	field := fs.String("field", "kubeconfig", "Secret field that holds the kubeconfig. Used with --get")
	outPath := fs.String("o", "", "Kubeconfig file to write. '-' prints YAML. Used with --get")
	overwrite := fs.Bool("overwrite", false, "Replace the kubeconfig file instead of merging. Used with --get")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, clustersUsage)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	chosen := 0
	for _, on := range []bool{*list, *add, *del, *get} {
		if on {
			chosen++
		}
	}
	if chosen != 1 {
		fmt.Fprint(os.Stderr, clustersUsage)
		return 2
	}

	client, err := clientFromLogin("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	name := strings.TrimSpace(*clusterName)
	switch {
	case *list:
		return listClusters(client)
	case *add:
		if name == "" || strings.TrimSpace(*file) == "" {
			fmt.Fprintln(os.Stderr, "error: --add requires --cluster and --file")
			return 2
		}
		return addCluster(client, name, strings.TrimSpace(*file))
	case *del:
		if name == "" {
			fmt.Fprintln(os.Stderr, "error: --delete requires --cluster")
			return 2
		}
		return deleteCluster(client, name)
	default:
		if name == "" {
			fmt.Fprintln(os.Stderr, "error: --get requires --cluster")
			return 2
		}
		return getCredentials(client, name, strings.TrimSpace(*vaultPath), strings.TrimSpace(*namespace), strings.TrimSpace(*field), strings.TrimSpace(*outPath), *overwrite)
	}
}

func listClusters(client *api.Client) int {
	names, err := vaultkv.ListKV2(client, "clusters")
	if err != nil {
		fmt.Fprintf(os.Stderr, "vault list: %v\n", err)
		return 1
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Println(name)
	}
	return 0
}

func addCluster(client *api.Client, name, file string) int {
	body, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read kubeconfig: %v\n", err)
		return 1
	}
	if strings.TrimSpace(string(body)) == "" {
		fmt.Fprintf(os.Stderr, "error: %s is empty\n", file)
		return 1
	}
	path := "clusters/data/" + name
	if err := vaultkv.WriteKV2(client, path, map[string]string{"kubeconfig": string(body)}); err != nil {
		fmt.Fprintf(os.Stderr, "vault write %s: %v\n", path, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "Stored %s at %s\n", name, path)
	return 0
}

func deleteCluster(client *api.Client, name string) int {
	if err := vaultkv.DeleteKV2(client, "clusters", name); err != nil {
		fmt.Fprintf(os.Stderr, "vault delete %s: %v\n", name, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "Deleted %s\n", name)
	return 0
}

const clustersUsage = `Usage: oks clusters --list
       oks clusters --add --cluster <name> --file <kubeconfig>
       oks clusters --delete --cluster <name>
       oks clusters --get --cluster <name> [options]

Uses the token saved by oks auth login.

  --list                 List cluster names
  --add                  Store a kubeconfig in Vault
  --delete               Delete a cluster secret from Vault
  --get                  Fetch credentials and switch kubectl to that context
  --cluster <name>       Cluster name
  --file <path>          Kubeconfig file for --add

Options for --get:
  --namespace <name>     Namespace written onto the context
  --o <path>             Kubeconfig file. '-' prints YAML. Default is ~/.kube/config
  --path <vault-path>    Vault path override, for example clusters/data/kind-odri-cluster
  --field <name>         Secret field that holds the kubeconfig (default kubeconfig)
  --overwrite            Replace the kubeconfig file instead of merging
`
