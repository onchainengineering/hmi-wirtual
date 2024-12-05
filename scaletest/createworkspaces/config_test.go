package createworkspaces_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/scaletest/agentconn"
	"github.com/onchainengineering/hmi-wirtual/scaletest/createworkspaces"
	"github.com/onchainengineering/hmi-wirtual/scaletest/reconnectingpty"
	"github.com/onchainengineering/hmi-wirtual/scaletest/workspacebuild"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpapi"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func Test_UserConfig(t *testing.T) {
	t.Parallel()

	id := uuid.New()

	cases := []struct {
		name        string
		config      createworkspaces.UserConfig
		errContains string
	}{
		{
			name: "OK",
			config: createworkspaces.UserConfig{
				OrganizationID: id,
				Username:       "test",
				Email:          "test@test.coder.com",
			},
		},
		{
			name: "NoOrganizationID",
			config: createworkspaces.UserConfig{
				OrganizationID: uuid.Nil,
				Username:       "test",
				Email:          "test@test.coder.com",
			},
			errContains: "organization_id must not be a nil UUID",
		},
		{
			name: "NoUsername",
			config: createworkspaces.UserConfig{
				OrganizationID: id,
				Username:       "",
				Email:          "test@test.coder.com",
			},
			errContains: "username must be set",
		},
		{
			name: "NoEmail",
			config: createworkspaces.UserConfig{
				OrganizationID: id,
				Username:       "test",
				Email:          "",
			},
			errContains: "email must be set",
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

func Test_Config(t *testing.T) {
	t.Parallel()

	id := uuid.New()

	userConfig := createworkspaces.UserConfig{
		OrganizationID: id,
		Username:       id.String(),
		Email:          id.String() + "@example.com",
	}

	workspaceConfig := workspacebuild.Config{
		OrganizationID: id,
		UserID:         id.String(),
		Request: wirtualsdk.CreateWorkspaceRequest{
			TemplateID: id,
		},
	}

	reconnectingPTYConfig := reconnectingpty.Config{
		AgentID: id,
	}

	agentConnConfig := agentconn.Config{
		AgentID:        id,
		ConnectionMode: agentconn.ConnectionModeDirect,
		HoldDuration:   httpapi.Duration(time.Minute),
	}

	cases := []struct {
		name        string
		config      createworkspaces.Config
		errContains string
	}{
		{
			name: "OK",
			config: createworkspaces.Config{
				User:            userConfig,
				Workspace:       workspaceConfig,
				ReconnectingPTY: &reconnectingPTYConfig,
				AgentConn:       &agentConnConfig,
			},
		},
		{
			name: "OKOptional",
			config: createworkspaces.Config{
				User:            userConfig,
				Workspace:       workspaceConfig,
				ReconnectingPTY: nil,
				AgentConn:       nil,
			},
		},
		{
			name: "BadUserConfig",
			config: createworkspaces.Config{
				User: createworkspaces.UserConfig{
					OrganizationID: uuid.Nil,
				},
			},
			errContains: "validate user",
		},
		{
			name: "BadWorkspaceConfig",
			config: createworkspaces.Config{
				User: userConfig,
				Workspace: workspacebuild.Config{
					Request: wirtualsdk.CreateWorkspaceRequest{
						TemplateID: uuid.Nil,
					},
				},
			},
			errContains: "validate workspace",
		},
		{
			name: "BadReconnectingPTYConfig",
			config: createworkspaces.Config{
				User:      userConfig,
				Workspace: workspaceConfig,
				ReconnectingPTY: &reconnectingpty.Config{
					Timeout: httpapi.Duration(-1 * time.Second),
				},
			},
			errContains: "validate reconnecting pty",
		},
		{
			name: "BadAgentConnConfig",
			config: createworkspaces.Config{
				User:      userConfig,
				Workspace: workspaceConfig,
				AgentConn: &agentconn.Config{
					ConnectionMode: "bad",
				},
			},
			errContains: "validate agent conn",
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
