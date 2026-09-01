package main

import (
	"os"

	"proxyma/internal/agentcli"
)

func main() {
	os.Exit(agentcli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
