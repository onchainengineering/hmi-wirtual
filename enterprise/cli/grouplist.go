package cli

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/google/uuid"
	"golang.org/x/xerrors"

	"github.com/coder/serpent"
	agpl "github.com/onchainengineering/hmi-wirtual/cli"
	"github.com/onchainengineering/hmi-wirtual/cli/cliui"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func (r *RootCmd) groupList() *serpent.Command {
	formatter := cliui.NewOutputFormatter(
		cliui.TableFormat([]groupTableRow{}, nil),
		cliui.JSONFormat(),
	)
	orgContext := agpl.NewOrganizationContext()

	client := new(wirtualsdk.Client)
	cmd := &serpent.Command{
		Use:   "list",
		Short: "List user groups",
		Middleware: serpent.Chain(
			serpent.RequireNArgs(0),
			r.InitClient(client),
		),
		Handler: func(inv *serpent.Invocation) error {
			ctx := inv.Context()

			org, err := orgContext.Selected(inv, client)
			if err != nil {
				return xerrors.Errorf("current organization: %w", err)
			}

			groups, err := client.GroupsByOrganization(ctx, org.ID)
			if err != nil {
				return xerrors.Errorf("get groups: %w", err)
			}

			if len(groups) == 0 {
				_, _ = fmt.Fprintf(inv.Stderr, "%s No groups found in %s! Create one:\n\n", agpl.Caret, color.HiWhiteString(org.Name))
				_, _ = fmt.Fprintln(inv.Stderr, color.HiMagentaString("  $ coder groups create <name>\n"))
				return nil
			}

			rows := groupsToRows(groups...)
			out, err := formatter.Format(inv.Context(), rows)
			if err != nil {
				return xerrors.Errorf("display groups: %w", err)
			}

			_, _ = fmt.Fprintln(inv.Stdout, out)
			return nil
		},
	}

	formatter.AttachOptions(&cmd.Options)
	orgContext.AttachOptions(cmd)
	return cmd
}

type groupTableRow struct {
	// For json output:
	Group wirtualsdk.Group `table:"-"`

	// For table output:
	Name           string    `json:"-" table:"name,default_sort"`
	DisplayName    string    `json:"-" table:"display name"`
	OrganizationID uuid.UUID `json:"-" table:"organization id"`
	Members        []string  `json:"-" table:"members"`
	AvatarURL      string    `json:"-" table:"avatar url"`
}

func groupsToRows(groups ...wirtualsdk.Group) []groupTableRow {
	rows := make([]groupTableRow, 0, len(groups))
	for _, group := range groups {
		members := make([]string, 0, len(group.Members))
		for _, member := range group.Members {
			members = append(members, member.Email)
		}
		rows = append(rows, groupTableRow{
			Name:           group.Name,
			DisplayName:    group.DisplayName,
			OrganizationID: group.OrganizationID,
			AvatarURL:      group.AvatarURL,
			Members:        members,
		})
	}

	return rows
}
