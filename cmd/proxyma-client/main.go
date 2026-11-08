package main

import (
	"os"

	"proxyma/internal/clientcli"
)

func main() {
	os.Exit(clientcli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
