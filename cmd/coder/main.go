package main

import (
	"fmt"
	"os"
	_ "time/tzdata"

	"github.com/onchainengineering/hmi-wirtual/agent/agentexec"
	"github.com/onchainengineering/hmi-wirtual/cli"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "agent-exec" {
		err := agentexec.CLI()
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var rootCmd cli.RootCmd
	rootCmd.RunWithSubcommands(rootCmd.AGPL())
}
