package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/hashicorp/vault/api"
	"github.com/odrisystems/infrastructure/tools/oks/internal/auth"
	"github.com/odrisystems/infrastructure/tools/oks/internal/vaultkv"
)

// Run dispatches oks auth login, oks clusters, and kubeconfig fetch.
func Run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "auth":
		return authCommand(args[1:])
	case "clusters":
		return clustersCommand(args[1:])
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: oks <command> [options]

Log in once. Later commands use the saved token.

Commands:
  auth login   Log in with userpass and save the token
  clusters     List, add, delete, or get cluster credentials

Examples:
  oks auth login
  oks clusters --list
  oks clusters --get --cluster kind-odri-cluster --namespace workspacepro-prod
  oks clusters --add --cluster name --file ./kubeconfig
  oks clusters --delete --cluster name

auth login:
  --method userpass   Login method. userpass opens a browser. token stores VAULT_TOKEN
  --auth userpass     Userpass mount
  --vault-addr        Vault address (default https://vault.odrisystems.com)

clusters:
  --list                     List cluster names
  --add                      Store a kubeconfig in Vault
  --delete                   Delete a cluster secret from Vault
  --get                      Fetch credentials and switch kubectl to that context
  --cluster <name>           Cluster name
  --file <path>              Kubeconfig file for --add
  --namespace <name>         Namespace written onto the context. Used with --get
  --o <path>                 Kubeconfig file. '-' prints YAML. Used with --get
  --path <vault-path>        Vault path override. Used with --get
  --field <name>             Secret field that holds the kubeconfig. Used with --get
  --overwrite                Replace the kubeconfig file instead of merging. Used with --get

Run oks auth login -h or oks clusters -h for the full flag list.
`)
}

func clientFromLogin(vaultAddr string) (*api.Client, error) {
	client, err := vaultkv.NewClient(vaultAddr)
	if err != nil {
		return nil, err
	}
	envToken := client.Token()
	if saved, err := auth.LoadToken(); err == nil {
		client.SetToken(saved)
		if _, err := client.Auth().Token().LookupSelf(); err == nil {
			return client, nil
		}
	}
	if envToken != "" {
		client.SetToken(envToken)
		if _, err := client.Auth().Token().LookupSelf(); err == nil {
			return client, nil
		}
	}
	return nil, errors.New("not logged in. Run: oks auth login")
}
