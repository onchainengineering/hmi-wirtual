package wirtuald_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/wirtualsdk/agentsdk"
	"github.com/coder/coder/v2/provisioner/echo"
	"github.com/coder/coder/v2/provisionersdk/proto"
	"github.com/coder/coder/v2/testutil"
)

func TestPostWorkspaceAuthAzureInstanceIdentity(t *testing.T) {
	t.Parallel()
	instanceID := "instanceidentifier"
	certificates, metadataClient := wirtualdtest.NewAzureInstanceIdentity(t, instanceID)
	client := wirtualdtest.New(t, &wirtualdtest.Options{
		AzureCertificates:        certificates,
		IncludeProvisionerDaemon: true,
	})
	user := wirtualdtest.CreateFirstUser(t, client)
	version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
		Parse: echo.ParseComplete,
		ProvisionApply: []*proto.Response{{
			Type: &proto.Response_Apply{
				Apply: &proto.ApplyComplete{
					Resources: []*proto.Resource{{
						Name: "somename",
						Type: "someinstance",
						Agents: []*proto.Agent{{
							Auth: &proto.Agent_InstanceId{
								InstanceId: instanceID,
							},
						}},
					}},
				},
			},
		}},
	})
	template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
	workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)
	wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	client.HTTPClient = metadataClient
	agentClient := &agentsdk.Client{
		SDK: client,
	}
	_, err := agentClient.AuthAzureInstanceIdentity(ctx)
	require.NoError(t, err)
}

func TestPostWorkspaceAuthAWSInstanceIdentity(t *testing.T) {
	t.Parallel()
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		instanceID := "instanceidentifier"
		certificates, metadataClient := wirtualdtest.NewAWSInstanceIdentity(t, instanceID)
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			AWSCertificates:          certificates,
			IncludeProvisionerDaemon: true,
		})
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse: echo.ParseComplete,
			ProvisionApply: []*proto.Response{{
				Type: &proto.Response_Apply{
					Apply: &proto.ApplyComplete{
						Resources: []*proto.Resource{{
							Name: "somename",
							Type: "someinstance",
							Agents: []*proto.Agent{{
								Auth: &proto.Agent_InstanceId{
									InstanceId: instanceID,
								},
							}},
						}},
					},
				},
			}},
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		client.HTTPClient = metadataClient
		agentClient := &agentsdk.Client{
			SDK: client,
		}
		_, err := agentClient.AuthAWSInstanceIdentity(ctx)
		require.NoError(t, err)
	})
}

func TestPostWorkspaceAuthGoogleInstanceIdentity(t *testing.T) {
	t.Parallel()
	t.Run("Expired", func(t *testing.T) {
		t.Parallel()
		instanceID := "instanceidentifier"
		validator, metadata := wirtualdtest.NewGoogleInstanceIdentity(t, instanceID, true)
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			GoogleTokenValidator: validator,
		})

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		agentClient := &agentsdk.Client{
			SDK: client,
		}
		_, err := agentClient.AuthGoogleInstanceIdentity(ctx, "", metadata)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode())
	})

	t.Run("InstanceNotFound", func(t *testing.T) {
		t.Parallel()
		instanceID := "instanceidentifier"
		validator, metadata := wirtualdtest.NewGoogleInstanceIdentity(t, instanceID, false)
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			GoogleTokenValidator: validator,
		})

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		agentClient := &agentsdk.Client{
			SDK: client,
		}
		_, err := agentClient.AuthGoogleInstanceIdentity(ctx, "", metadata)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusNotFound, apiErr.StatusCode())
	})

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		instanceID := "instanceidentifier"
		validator, metadata := wirtualdtest.NewGoogleInstanceIdentity(t, instanceID, false)
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			GoogleTokenValidator:     validator,
			IncludeProvisionerDaemon: true,
		})
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse: echo.ParseComplete,
			ProvisionApply: []*proto.Response{{
				Type: &proto.Response_Apply{
					Apply: &proto.ApplyComplete{
						Resources: []*proto.Resource{{
							Name: "somename",
							Type: "someinstance",
							Agents: []*proto.Agent{{
								Auth: &proto.Agent_InstanceId{
									InstanceId: instanceID,
								},
							}},
						}},
					},
				},
			}},
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		agentClient := &agentsdk.Client{
			SDK: client,
		}
		_, err := agentClient.AuthGoogleInstanceIdentity(ctx, "", metadata)
		require.NoError(t, err)
	})
}
