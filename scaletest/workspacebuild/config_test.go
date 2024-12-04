package workspacebuild_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/scaletest/workspacebuild"
	"github.com/coder/coder/v2/wirtualsdk"
)

func Test_Config(t *testing.T) {
	t.Parallel()

	id := uuid.Must(uuid.NewRandom())

	cases := []struct {
		name        string
		config      workspacebuild.Config
		errContains string
	}{
		{
			name: "NoOrganizationID",
			config: workspacebuild.Config{
				OrganizationID: uuid.Nil,
				UserID:         id.String(),
				Request: wirtualsdk.CreateWorkspaceRequest{
					TemplateID: id,
				},
				NoWaitForAgents: true,
			},
			errContains: "organization_id must be set",
		},
		{
			name: "NoUserID",
			config: workspacebuild.Config{
				OrganizationID: id,
				UserID:         "",
				Request: wirtualsdk.CreateWorkspaceRequest{
					TemplateID: id,
				},
			},
			errContains: "user_id must be set",
		},
		{
			name: "UserIDNotUUID",
			config: workspacebuild.Config{
				OrganizationID: id,
				UserID:         "blah",
				Request: wirtualsdk.CreateWorkspaceRequest{
					TemplateID: id,
				},
			},
			errContains: "user_id must be \"me\" or a valid UUID",
		},
		{
			name: "NoTemplateID",
			config: workspacebuild.Config{
				OrganizationID: id,
				UserID:         id.String(),
				Request: wirtualsdk.CreateWorkspaceRequest{
					TemplateID: uuid.Nil,
				},
			},
			errContains: "request.template_id must be set",
		},
		{
			name: "UserMe",
			config: workspacebuild.Config{
				OrganizationID: id,
				UserID:         "me",
				Request: wirtualsdk.CreateWorkspaceRequest{
					TemplateID: id,
				},
			},
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := c.config.Validate()
			if c.errContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), c.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
