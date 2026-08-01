package main

import (
	"os"

	"github.com/wechatloom/wechatloom/internal/cli"
)

func main() {
	os.Exit(cli.NewRunner(os.Stdout, os.Stderr).Run(os.Args[1:]))
}
