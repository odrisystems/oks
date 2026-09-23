package cli

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/odrisystems/infrastructure/tools/oks/internal/vaultkv"
)

func clustersCommand(args []string) int {
	fs := flag.NewFlagSet("oks clusters", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	vaultAddr := fs.String("vault-addr", "", "Vault address (default VAULT_ADDR or https://vault.odrisystems.com)")
	mount := fs.String("mount", "clusters", "KV v2 mount that holds cluster secrets")
	useToken := fs.Bool("use-token", false, "Use VAULT_TOKEN instead of the token from oks auth login")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oks clusters [options]\n\n")
		fmt.Fprintf(os.Stderr, "List cluster names stored in Vault. Run oks auth login first, or pass -use-token.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	client, err := clientFromLogin(*vaultAddr, *useToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	names, err := vaultkv.ListKV2(client, *mount)
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
