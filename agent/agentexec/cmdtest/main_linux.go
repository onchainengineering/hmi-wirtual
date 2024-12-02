//go:build linux
// +build linux

package main

import (
	"fmt"
	"os"

	"github.com/onchainengineering/hmi-wirtual/agent/agentexec"
)

func main() {
	err := agentexec.CLI()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
