package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/odrisystems/infrastructure/tools/oks/internal/auth"
	"github.com/odrisystems/infrastructure/tools/oks/internal/vaultkv"
)

func authCommand(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintf(os.Stderr, "Usage: oks auth login [options]\n\n")
		fmt.Fprintf(os.Stderr, "Log in to Vault and save the client token for later oks commands.\n")
		fmt.Fprintf(os.Stderr, "The default method is userpass.\n")
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	if args[0] != "login" {
		fmt.Fprintf(os.Stderr, "error: unknown auth command %q\n\n", args[0])
		fmt.Fprintf(os.Stderr, "Usage: oks auth login [options]\n")
		return 2
	}
	return loginCommand(args[1:])
}

func loginCommand(args []string) int {
	fs := flag.NewFlagSet("oks auth login", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	method := fs.String("method", "userpass", "Vault login method. userpass opens a browser. token stores VAULT_TOKEN.")
	authMount := fs.String("auth", "userpass", "Vault userpass mount used when -method is userpass")
	vaultAddr := fs.String("vault-addr", "", "Vault address (default VAULT_ADDR or https://vault.odrisystems.com)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oks auth login [options]\n\n")
		fmt.Fprintf(os.Stderr, "Log in to Vault and save the client token. The default method is userpass.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	client, err := vaultkv.NewClient(*vaultAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vault client: %v\n", err)
		return 1
	}

	var token string
	switch strings.ToLower(strings.TrimSpace(*method)) {
	case "userpass":
		token, err = auth.UserpassLogin(client, *authMount)
	case "token":
		token = client.Token()
		if token == "" {
			err = fmt.Errorf("-method token requires VAULT_TOKEN")
		}
	default:
		fmt.Fprintf(os.Stderr, "error: unsupported login method %q\n", *method)
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "login: %v\n", err)
		return 1
	}

	path, err := auth.SaveToken(token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "save token: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "Logged in to %s with %s. Token saved to %s\n", client.Address(), strings.ToLower(strings.TrimSpace(*method)), path)
	return 0
}
