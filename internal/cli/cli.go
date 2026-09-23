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
		return kubeconfigCommand(args)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: oks <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  auth login   Log in to Vault. The default method is userpass.\n")
	fmt.Fprintf(os.Stderr, "  clusters     List clusters stored in Vault.\n")
	fmt.Fprintf(os.Stderr, "  -cluster     Fetch one cluster kubeconfig. Run oks auth login first, or pass -use-token.\n\n")
	fmt.Fprintf(os.Stderr, "Run oks <command> -h for command flags.\n")
}

func clientFromLogin(vaultAddr string, useToken bool) (*api.Client, error) {
	client, err := vaultkv.NewClient(vaultAddr)
	if err != nil {
		return nil, err
	}
	if useToken {
		if client.Token() == "" {
			return nil, errors.New("-use-token requires VAULT_TOKEN")
		}
		return client, nil
	}
	token, err := auth.LoadToken()
	if err != nil {
		return nil, err
	}
	client.SetToken(token)
	return client, nil
}
