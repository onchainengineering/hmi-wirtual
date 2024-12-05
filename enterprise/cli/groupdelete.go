package cli

import (
	"fmt"

	"golang.org/x/xerrors"

	"github.com/coder/pretty"
	"github.com/coder/serpent"
	agpl "github.com/onchainengineering/hmi-wirtual/cli"
	"github.com/onchainengineering/hmi-wirtual/cli/cliui"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func (r *RootCmd) groupDelete() *serpent.Command {
	orgContext := agpl.NewOrganizationContext()
	client := new(wirtualsdk.Client)
	cmd := &serpent.Command{
		Use:   "delete <name>",
		Short: "Delete a user group",
		Middleware: serpent.Chain(
			serpent.RequireNArgs(1),
			r.InitClient(client),
		),
		Handler: func(inv *serpent.Invocation) error {
			var (
				ctx       = inv.Context()
				groupName = inv.Args[0]
			)

			org, err := orgContext.Selected(inv, client)
			if err != nil {
				return xerrors.Errorf("current organization: %w", err)
			}

			group, err := client.GroupByOrgAndName(ctx, org.ID, groupName)
			if err != nil {
				return xerrors.Errorf("group by org and name: %w", err)
			}

			err = client.DeleteGroup(ctx, group.ID)
			if err != nil {
				return xerrors.Errorf("delete group: %w", err)
			}

			_, _ = fmt.Fprintf(inv.Stdout, "Successfully deleted group %s!\n", pretty.Sprint(cliui.DefaultStyles.Keyword, group.Name))
			return nil
		},
	}
	orgContext.AttachOptions(cmd)

	return cmd
}
