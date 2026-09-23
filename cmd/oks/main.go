package main

import (
	"os"

	"github.com/odrisystems/infrastructure/tools/oks/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
