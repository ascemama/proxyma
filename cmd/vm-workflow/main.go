package main

import (
	"os"

	"proxyma/internal/vmworkflow"
)

func main() { os.Exit(vmworkflow.Run(os.Args[1:], os.Stdout, os.Stderr)) }
