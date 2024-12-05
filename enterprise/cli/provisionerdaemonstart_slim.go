//go:build slim

package cli

import (
	"github.com/coder/serpent"
	agplcli "github.com/onchainengineering/hmi-wirtual/cli"
)

func (r *RootCmd) provisionerDaemonStart() *serpent.Command {
	cmd := &serpent.Command{
		Use:   "start",
		Short: "Run a provisioner daemon",
		// We accept RawArgs so all commands and flags are accepted.
		RawArgs: true,
		Hidden:  true,
		Handler: func(inv *serpent.Invocation) error {
			agplcli.SlimUnsupported(inv.Stderr, "provisionerd start")
			return nil
		},
	}

	return cmd
}
