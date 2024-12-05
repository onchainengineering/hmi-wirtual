package wirtuald_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/provisioner/echo"
	"github.com/onchainengineering/hmi-wirtual/provisionersdk/proto"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
)

func TestProvisionerJobLogs(t *testing.T) {
	t.Parallel()
	t.Run("StreamAfterComplete", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse: echo.ParseComplete,
			ProvisionApply: []*proto.Response{{
				Type: &proto.Response_Log{
					Log: &proto.Log{
						Level:  proto.LogLevel_INFO,
						Output: "log-output",
					},
				},
			}, {
				Type: &proto.Response_Apply{
					Apply: &proto.ApplyComplete{},
				},
			}},
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		logs, closer, err := client.WorkspaceBuildLogsAfter(ctx, workspace.LatestBuild.ID, 0)
		require.NoError(t, err)
		defer closer.Close()
		for {
			log, ok := <-logs
			t.Logf("got log: [%s] %s %s", log.Level, log.Stage, log.Output)
			if !ok {
				return
			}
		}
	})

	t.Run("StreamWhileRunning", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse: echo.ParseComplete,
			ProvisionApply: []*proto.Response{{
				Type: &proto.Response_Log{
					Log: &proto.Log{
						Level:  proto.LogLevel_INFO,
						Output: "log-output",
					},
				},
			}, {
				Type: &proto.Response_Apply{
					Apply: &proto.ApplyComplete{},
				},
			}},
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		logs, closer, err := client.WorkspaceBuildLogsAfter(ctx, workspace.LatestBuild.ID, 0)
		require.NoError(t, err)
		defer closer.Close()
		for {
			_, ok := <-logs
			if !ok {
				return
			}
		}
	})
}
