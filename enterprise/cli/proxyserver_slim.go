//go:build slim

package cli

import (
	"github.com/coder/serpent"
	agplcli "github.com/onchainengineering/hmi-wirtual/cli"
)

func (r *RootCmd) proxyServer() *serpent.Command {
	root := &serpent.Command{
		Use:     "server",
		Short:   "Start a workspace proxy server",
		Aliases: []string{},
		// We accept RawArgs so all commands and flags are accepted.
		RawArgs: true,
		Hidden:  true,
		Handler: func(inv *serpent.Invocation) error {
			agplcli.SlimUnsupported(inv.Stderr, "workspace-proxy server")
			return nil
		},
	}

	return root
}
