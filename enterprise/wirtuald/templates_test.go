package wirtuald_test

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/slogtest"

	"github.com/coder/coder/v2/cryptorand"
	"github.com/coder/coder/v2/enterprise/wirtuald/license"
	"github.com/coder/coder/v2/enterprise/wirtuald/schedule"
	"github.com/coder/coder/v2/enterprise/wirtuald/wirtualdenttest"
	"github.com/coder/coder/v2/provisioner/echo"
	"github.com/coder/coder/v2/provisionersdk/proto"
	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtuald/audit"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/notifications"
	"github.com/coder/coder/v2/wirtuald/notifications/notificationstest"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtuald/util/ptr"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtualsdk"
)

func TestTemplates(t *testing.T) {
	t.Parallel()

	logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)

	t.Run("Deprecated", func(t *testing.T) {
		t.Parallel()

		notifyEnq := &notificationstest.FakeEnqueuer{}
		owner, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
				NotificationsEnqueuer:    notifyEnq,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAccessControl: 1,
				},
			},
		})
		client, secondUser := wirtualdtest.CreateAnotherUser(t, owner, user.OrganizationID, rbac.RoleTemplateAdmin())
		otherClient, otherUser := wirtualdtest.CreateAnotherUser(t, owner, user.OrganizationID, rbac.RoleTemplateAdmin())

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)

		_ = wirtualdtest.CreateWorkspace(t, owner, template.ID)
		_ = wirtualdtest.CreateWorkspace(t, client, template.ID)

		// Create another template for testing that users of another template do not
		// get a notification.
		secondVersion := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		secondTemplate := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, secondVersion.ID)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, secondVersion.ID)

		_ = wirtualdtest.CreateWorkspace(t, otherClient, secondTemplate.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		updated, err := client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			DeprecationMessage: ptr.Ref("Stop using this template"),
		})
		require.NoError(t, err)
		assert.Greater(t, updated.UpdatedAt, template.UpdatedAt)
		// AGPL cannot deprecate, expect no change
		assert.True(t, updated.Deprecated)
		assert.NotEmpty(t, updated.DeprecationMessage)

		notifs := []*notificationstest.FakeNotification{}
		for _, notif := range notifyEnq.Sent() {
			if notif.TemplateID == notifications.TemplateTemplateDeprecated {
				notifs = append(notifs, notif)
			}
		}
		require.Equal(t, 2, len(notifs))

		expectedSentTo := []string{user.UserID.String(), secondUser.ID.String()}
		slices.Sort(expectedSentTo)

		sentTo := []string{}
		for _, notif := range notifs {
			sentTo = append(sentTo, notif.UserID.String())
		}
		slices.Sort(sentTo)

		// Require the notification to have only been sent to the expected users
		assert.Equal(t, expectedSentTo, sentTo)

		// The previous check should verify this but we're double checking that
		// the notification wasn't sent to users not using the template.
		for _, notif := range notifs {
			assert.NotEqual(t, otherUser.ID, notif.UserID)
		}

		_, err = client.CreateWorkspace(ctx, user.OrganizationID, wirtualsdk.Me, wirtualsdk.CreateWorkspaceRequest{
			TemplateID: template.ID,
			Name:       "foobar",
		})
		require.ErrorContains(t, err, "deprecated")

		// Unset deprecated and try again
		updated, err = client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{DeprecationMessage: ptr.Ref("")})
		require.NoError(t, err)
		assert.False(t, updated.Deprecated)
		assert.Empty(t, updated.DeprecationMessage)

		_, err = client.CreateWorkspace(ctx, user.OrganizationID, wirtualsdk.Me, wirtualsdk.CreateWorkspaceRequest{
			TemplateID: template.ID,
			Name:       "foobar",
		})
		require.NoError(t, err)
	})

	t.Run("MaxPortShareLevel", func(t *testing.T) {
		t.Parallel()

		cfg := wirtualdtest.DeploymentValues(t)
		cfg.Experiments = []string{"shared-ports"}
		owner, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
				DeploymentValues:         cfg,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureControlSharedPorts: 1,
				},
			},
		})
		client, _ := wirtualdtest.CreateAnotherUser(t, owner, user.OrganizationID, rbac.RoleTemplateAdmin())
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:         echo.ParseComplete,
			ProvisionPlan: echo.PlanComplete,
			ProvisionApply: []*proto.Response{{
				Type: &proto.Response_Log{
					Log: &proto.Log{
						Level:  proto.LogLevel_INFO,
						Output: "example",
					},
				},
			}, {
				Type: &proto.Response_Apply{
					Apply: &proto.ApplyComplete{
						Resources: []*proto.Resource{{
							Name: "some",
							Type: "example",
							Agents: []*proto.Agent{{
								Id: "something",
								Auth: &proto.Agent_Token{
									Token: uuid.NewString(),
								},
								Name: "test",
							}},
						}, {
							Name: "another",
							Type: "example",
						}},
					},
				},
			}},
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.MaxPortShareLevel = ptr.Ref(wirtualsdk.WorkspaceAgentPortShareLevelPublic)
		})
		require.Equal(t, template.MaxPortShareLevel, wirtualsdk.WorkspaceAgentPortShareLevelPublic)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		ws := wirtualdtest.CreateWorkspace(t, client, template.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
		ws, err := client.Workspace(context.Background(), ws.ID)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		// OK
		var level wirtualsdk.WorkspaceAgentPortShareLevel = wirtualsdk.WorkspaceAgentPortShareLevelPublic
		updated, err := client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			MaxPortShareLevel: &level,
		})
		require.NoError(t, err)
		assert.Equal(t, level, updated.MaxPortShareLevel)

		// Invalid level
		level = "invalid"
		_, err = client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			MaxPortShareLevel: &level,
		})
		require.ErrorContains(t, err, "invalid max port sharing level")

		// Create public port share
		_, err = client.UpsertWorkspaceAgentPortShare(ctx, ws.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
			AgentName:  ws.LatestBuild.Resources[0].Agents[0].Name,
			Port:       8080,
			ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevelPublic,
			Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocolHTTP,
		})
		require.NoError(t, err)

		// Reduce max level to authenticated
		level = wirtualsdk.WorkspaceAgentPortShareLevelAuthenticated
		_, err = client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			MaxPortShareLevel: &level,
		})
		require.NoError(t, err)

		// Ensure previously public port is now authenticated
		wpsr, err := client.GetWorkspaceAgentPortShares(ctx, ws.ID)
		require.NoError(t, err)
		require.Len(t, wpsr.Shares, 1)
		assert.Equal(t, wirtualsdk.WorkspaceAgentPortShareLevelAuthenticated, wpsr.Shares[0].ShareLevel)

		// reduce max level to owner
		level = wirtualsdk.WorkspaceAgentPortShareLevelOwner
		_, err = client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			MaxPortShareLevel: &level,
		})
		require.NoError(t, err)

		// Ensure previously authenticated port is removed
		wpsr, err = client.GetWorkspaceAgentPortShares(ctx, ws.ID)
		require.NoError(t, err)
		require.Empty(t, wpsr.Shares)
	})

	t.Run("SetAutostartRequirement", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
				},
			},
		})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		require.Equal(t, []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}, template.AutostartRequirement.DaysOfWeek)

		ctx := testutil.Context(t, testutil.WaitLong)
		updated, err := anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			Name:        template.Name,
			DisplayName: template.DisplayName,
			Description: template.Description,
			Icon:        template.Icon,
			AutostartRequirement: &wirtualsdk.TemplateAutostartRequirement{
				DaysOfWeek: []string{"monday", "saturday"},
			},
		})
		require.NoError(t, err)
		require.Equal(t, []string{"monday", "saturday"}, updated.AutostartRequirement.DaysOfWeek)

		template, err = anotherClient.Template(ctx, template.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"monday", "saturday"}, template.AutostartRequirement.DaysOfWeek)

		// Ensure a missing field is a noop
		updated, err = anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			Name:        template.Name,
			DisplayName: template.DisplayName,
			Description: template.Description,
			Icon:        template.Icon + "something",
		})
		require.NoError(t, err)
		require.Equal(t, []string{"monday", "saturday"}, updated.AutostartRequirement.DaysOfWeek)

		template, err = anotherClient.Template(ctx, template.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"monday", "saturday"}, template.AutostartRequirement.DaysOfWeek)
		require.Empty(t, template.DeprecationMessage)
		require.False(t, template.Deprecated)
	})

	t.Run("SetInvalidAutostartRequirement", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
				},
			},
		})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		require.Equal(t, []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}, template.AutostartRequirement.DaysOfWeek)

		ctx := testutil.Context(t, testutil.WaitLong)
		_, err := anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			Name:        template.Name,
			DisplayName: template.DisplayName,
			Description: template.Description,
			Icon:        template.Icon,
			AutostartRequirement: &wirtualsdk.TemplateAutostartRequirement{
				DaysOfWeek: []string{"foobar", "saturday"},
			},
		})
		require.Error(t, err)
		require.Empty(t, template.DeprecationMessage)
		require.False(t, template.Deprecated)
	})

	t.Run("SetAutostopRequirement", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
				},
			},
		})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		require.Empty(t, 0, template.AutostopRequirement.DaysOfWeek)
		require.EqualValues(t, 1, template.AutostopRequirement.Weeks)

		ctx := context.Background()
		updated, err := anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			Name:                         template.Name,
			DisplayName:                  template.DisplayName,
			Description:                  template.Description,
			Icon:                         template.Icon,
			AllowUserCancelWorkspaceJobs: template.AllowUserCancelWorkspaceJobs,
			DefaultTTLMillis:             time.Hour.Milliseconds(),
			AutostopRequirement: &wirtualsdk.TemplateAutostopRequirement{
				DaysOfWeek: []string{"monday", "saturday"},
				Weeks:      3,
			},
		})
		require.NoError(t, err)
		require.Equal(t, []string{"monday", "saturday"}, updated.AutostopRequirement.DaysOfWeek)
		require.EqualValues(t, 3, updated.AutostopRequirement.Weeks)

		template, err = anotherClient.Template(ctx, template.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"monday", "saturday"}, template.AutostopRequirement.DaysOfWeek)
		require.EqualValues(t, 3, template.AutostopRequirement.Weeks)
		require.Empty(t, template.DeprecationMessage)
		require.False(t, template.Deprecated)
	})

	t.Run("CleanupTTLs", func(t *testing.T) {
		t.Run("OK", func(t *testing.T) {
			t.Parallel()

			ctx := testutil.Context(t, testutil.WaitMedium)
			client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
				Options: &wirtualdtest.Options{
					IncludeProvisionerDaemon: true,
				},
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					Features: license.Features{
						wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
					},
				},
			})
			anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
			template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
			require.EqualValues(t, 0, template.TimeTilDormantMillis)
			require.EqualValues(t, 0, template.FailureTTLMillis)
			require.EqualValues(t, 0, template.TimeTilDormantAutoDeleteMillis)

			var (
				failureTTL    = 1 * time.Minute
				inactivityTTL = 2 * time.Minute
				dormantTTL    = 3 * time.Minute
			)

			updated, err := anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
				Name:                           template.Name,
				DisplayName:                    template.DisplayName,
				Description:                    template.Description,
				Icon:                           template.Icon,
				AllowUserCancelWorkspaceJobs:   template.AllowUserCancelWorkspaceJobs,
				TimeTilDormantMillis:           inactivityTTL.Milliseconds(),
				FailureTTLMillis:               failureTTL.Milliseconds(),
				TimeTilDormantAutoDeleteMillis: dormantTTL.Milliseconds(),
			})
			require.NoError(t, err)
			require.Equal(t, failureTTL.Milliseconds(), updated.FailureTTLMillis)
			require.Equal(t, inactivityTTL.Milliseconds(), updated.TimeTilDormantMillis)
			require.Equal(t, dormantTTL.Milliseconds(), updated.TimeTilDormantAutoDeleteMillis)

			// Validate fetching the template returns the same values as updating
			// the template.
			template, err = anotherClient.Template(ctx, template.ID)
			require.NoError(t, err)
			require.Equal(t, failureTTL.Milliseconds(), updated.FailureTTLMillis)
			require.Equal(t, inactivityTTL.Milliseconds(), updated.TimeTilDormantMillis)
			require.Equal(t, dormantTTL.Milliseconds(), updated.TimeTilDormantAutoDeleteMillis)
		})

		t.Run("BadRequest", func(t *testing.T) {
			t.Parallel()

			ctx := testutil.Context(t, testutil.WaitMedium)
			client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
				Options: &wirtualdtest.Options{
					IncludeProvisionerDaemon: true,
				},
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					Features: license.Features{
						wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
					},
				},
			})
			anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
			template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

			type testcase struct {
				Name                string
				TimeTilDormantMS    int64
				FailureTTLMS        int64
				DormantAutoDeleteMS int64
			}

			cases := []testcase{
				{
					Name:                "NegativeValue",
					TimeTilDormantMS:    -1,
					FailureTTLMS:        -2,
					DormantAutoDeleteMS: -3,
				},
				{
					Name:                "ValueTooSmall",
					TimeTilDormantMS:    1,
					FailureTTLMS:        999,
					DormantAutoDeleteMS: 500,
				},
			}

			for _, c := range cases {
				c := c

				// nolint: paralleltest // context is from parent t.Run
				t.Run(c.Name, func(t *testing.T) {
					_, err := anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
						Name:                           template.Name,
						DisplayName:                    template.DisplayName,
						Description:                    template.Description,
						Icon:                           template.Icon,
						AllowUserCancelWorkspaceJobs:   template.AllowUserCancelWorkspaceJobs,
						TimeTilDormantMillis:           c.TimeTilDormantMS,
						FailureTTLMillis:               c.FailureTTLMS,
						TimeTilDormantAutoDeleteMillis: c.DormantAutoDeleteMS,
					})
					require.Error(t, err)
					cerr, ok := wirtualsdk.AsError(err)
					require.True(t, ok)
					require.Len(t, cerr.Validations, 3)
					require.Equal(t, "Value must be at least one minute.", cerr.Validations[0].Detail)
				})
			}
		})
	})

	t.Run("UpdateTimeTilDormantAutoDelete", func(t *testing.T) {
		t.Parallel()

		ctx := testutil.Context(t, testutil.WaitMedium)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
				},
			},
		})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		activeWS := wirtualdtest.CreateWorkspace(t, anotherClient, template.ID)
		dormantWS := wirtualdtest.CreateWorkspace(t, anotherClient, template.ID)
		require.Nil(t, activeWS.DeletingAt)
		require.Nil(t, dormantWS.DeletingAt)

		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, activeWS.LatestBuild.ID)
		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, dormantWS.LatestBuild.ID)

		err := anotherClient.UpdateWorkspaceDormancy(ctx, dormantWS.ID, wirtualsdk.UpdateWorkspaceDormancy{
			Dormant: true,
		})
		require.NoError(t, err)

		dormantWS = wirtualdtest.MustWorkspace(t, client, dormantWS.ID)
		require.NotNil(t, dormantWS.DormantAt)
		// The deleting_at field should be nil since there is no template time_til_dormant_autodelete set.
		require.Nil(t, dormantWS.DeletingAt)

		dormantTTL := time.Minute
		updated, err := anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			TimeTilDormantAutoDeleteMillis: dormantTTL.Milliseconds(),
		})
		require.NoError(t, err)
		require.Equal(t, dormantTTL.Milliseconds(), updated.TimeTilDormantAutoDeleteMillis)

		activeWS = wirtualdtest.MustWorkspace(t, client, activeWS.ID)
		require.Nil(t, activeWS.DormantAt)
		require.Nil(t, activeWS.DeletingAt)

		updatedDormantWorkspace := wirtualdtest.MustWorkspace(t, client, dormantWS.ID)
		require.NotNil(t, updatedDormantWorkspace.DormantAt)
		require.NotNil(t, updatedDormantWorkspace.DeletingAt)
		require.Equal(t, updatedDormantWorkspace.DormantAt.Add(dormantTTL), *updatedDormantWorkspace.DeletingAt)
		require.Equal(t, updatedDormantWorkspace.DormantAt, dormantWS.DormantAt)

		// Disable the time_til_dormant_auto_delete on the template, then we can assert that the workspaces
		// no longer have a deleting_at field.
		updated, err = anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			TimeTilDormantAutoDeleteMillis: 0,
		})
		require.NoError(t, err)
		require.EqualValues(t, 0, updated.TimeTilDormantAutoDeleteMillis)

		// The active workspace should remain unchanged.
		activeWS = wirtualdtest.MustWorkspace(t, client, activeWS.ID)
		require.Nil(t, activeWS.DormantAt)
		require.Nil(t, activeWS.DeletingAt)

		// Fetch the dormant workspace. It should still be dormant, but it should no
		// longer be scheduled for deletion.
		dormantWS = wirtualdtest.MustWorkspace(t, client, dormantWS.ID)
		require.NotNil(t, dormantWS.DormantAt)
		require.Nil(t, dormantWS.DeletingAt)
	})

	t.Run("UpdateDormantAt", func(t *testing.T) {
		t.Parallel()

		ctx := testutil.Context(t, testutil.WaitMedium)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
				},
			},
		})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		activeWS := wirtualdtest.CreateWorkspace(t, anotherClient, template.ID)
		dormantWS := wirtualdtest.CreateWorkspace(t, anotherClient, template.ID)
		require.Nil(t, activeWS.DeletingAt)
		require.Nil(t, dormantWS.DeletingAt)

		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, activeWS.LatestBuild.ID)
		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, dormantWS.LatestBuild.ID)

		err := anotherClient.UpdateWorkspaceDormancy(ctx, dormantWS.ID, wirtualsdk.UpdateWorkspaceDormancy{
			Dormant: true,
		})
		require.NoError(t, err)

		dormantWS = wirtualdtest.MustWorkspace(t, client, dormantWS.ID)
		require.NotNil(t, dormantWS.DormantAt)
		// The deleting_at field should be nil since there is no template time_til_dormant_autodelete set.
		require.Nil(t, dormantWS.DeletingAt)

		dormantTTL := time.Minute
		//nolint:gocritic // non-template-admin cannot update template meta
		updated, err := client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			TimeTilDormantAutoDeleteMillis: dormantTTL.Milliseconds(),
			UpdateWorkspaceDormantAt:       true,
		})
		require.NoError(t, err)
		require.Equal(t, dormantTTL.Milliseconds(), updated.TimeTilDormantAutoDeleteMillis)

		activeWS = wirtualdtest.MustWorkspace(t, client, activeWS.ID)
		require.Nil(t, activeWS.DormantAt)
		require.Nil(t, activeWS.DeletingAt)

		updatedDormantWorkspace := wirtualdtest.MustWorkspace(t, client, dormantWS.ID)
		require.NotNil(t, updatedDormantWorkspace.DormantAt)
		require.NotNil(t, updatedDormantWorkspace.DeletingAt)
		// Validate that the workspace dormant_at value is updated.
		require.True(t, updatedDormantWorkspace.DormantAt.After(*dormantWS.DormantAt))
		require.Equal(t, updatedDormantWorkspace.DormantAt.Add(dormantTTL), *updatedDormantWorkspace.DeletingAt)
	})

	t.Run("UpdateLastUsedAt", func(t *testing.T) {
		t.Parallel()

		ctx := testutil.Context(t, testutil.WaitMedium)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
				},
			},
		})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		activeWorkspace := wirtualdtest.CreateWorkspace(t, anotherClient, template.ID)
		dormantWorkspace := wirtualdtest.CreateWorkspace(t, anotherClient, template.ID)
		require.Nil(t, activeWorkspace.DeletingAt)
		require.Nil(t, dormantWorkspace.DeletingAt)

		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, activeWorkspace.LatestBuild.ID)
		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, dormantWorkspace.LatestBuild.ID)

		err := anotherClient.UpdateWorkspaceDormancy(ctx, dormantWorkspace.ID, wirtualsdk.UpdateWorkspaceDormancy{
			Dormant: true,
		})
		require.NoError(t, err)

		dormantWorkspace = wirtualdtest.MustWorkspace(t, client, dormantWorkspace.ID)
		require.NotNil(t, dormantWorkspace.DormantAt)
		// The deleting_at field should be nil since there is no template time_til_dormant_autodelete set.
		require.Nil(t, dormantWorkspace.DeletingAt)

		inactivityTTL := time.Minute
		updated, err := anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			TimeTilDormantMillis:      inactivityTTL.Milliseconds(),
			UpdateWorkspaceLastUsedAt: true,
		})
		require.NoError(t, err)
		require.Equal(t, inactivityTTL.Milliseconds(), updated.TimeTilDormantMillis)

		updatedActiveWS := wirtualdtest.MustWorkspace(t, client, activeWorkspace.ID)
		require.Nil(t, updatedActiveWS.DormantAt)
		require.Nil(t, updatedActiveWS.DeletingAt)
		require.True(t, updatedActiveWS.LastUsedAt.After(activeWorkspace.LastUsedAt))

		updatedDormantWS := wirtualdtest.MustWorkspace(t, client, dormantWorkspace.ID)
		require.NotNil(t, updatedDormantWS.DormantAt)
		require.Nil(t, updatedDormantWS.DeletingAt)
		// Validate that the workspace dormant_at value is updated.
		require.Equal(t, updatedDormantWS.DormantAt, dormantWorkspace.DormantAt)
		require.True(t, updatedDormantWS.LastUsedAt.After(dormantWorkspace.LastUsedAt))
	})

	t.Run("RequireActiveVersion", func(t *testing.T) {
		t.Parallel()
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAccessControl: 1,
				},
			},
		})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.RequireActiveVersion = true
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		require.True(t, template.RequireActiveVersion)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		// Update the field and assert it persists.
		updatedTemplate, err := anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			RequireActiveVersion: false,
		})
		require.NoError(t, err)
		require.False(t, updatedTemplate.RequireActiveVersion)

		// Flip it back to ensure we aren't hardcoding to a default value.
		updatedTemplate, err = anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			RequireActiveVersion: true,
		})
		require.NoError(t, err)
		require.True(t, updatedTemplate.RequireActiveVersion)

		// Assert that fetching a template is no different from the response
		// when updating.
		template, err = anotherClient.Template(ctx, template.ID)
		require.NoError(t, err)
		require.Equal(t, updatedTemplate, template)
		require.Empty(t, template.DeprecationMessage)
		require.False(t, template.Deprecated)
	})

	// Create a template, remove the group, see if an owner can
	// still fetch the template.
	t.Run("GetOnEveryoneRemove", func(t *testing.T) {
		t.Parallel()
		owner, first := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAccessControl: 1,
					wirtualsdk.FeatureTemplateRBAC:  1,
				},
			},
		})

		client, _ := wirtualdtest.CreateAnotherUser(t, owner, first.OrganizationID, rbac.RoleTemplateAdmin())
		version := wirtualdtest.CreateTemplateVersion(t, client, first.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, first.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitMedium)
		err := client.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: nil,
			GroupPerms: map[string]wirtualsdk.TemplateRole{
				// OrgID is the everyone ID
				first.OrganizationID.String(): wirtualsdk.TemplateRoleDeleted,
			},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, err = owner.Template(ctx, template.ID)
		require.NoError(t, err)
	})

	// Create a template in a second organization via custom role
	t.Run("SecondOrganization", func(t *testing.T) {
		t.Parallel()

		dv := wirtualdtest.DeploymentValues(t)
		ownerClient, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				DeploymentValues:         dv,
				IncludeProvisionerDaemon: false,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAccessControl:              1,
					wirtualsdk.FeatureCustomRoles:                1,
					wirtualsdk.FeatureExternalProvisionerDaemons: 1,
					wirtualsdk.FeatureMultipleOrganizations:      1,
				},
			},
		})

		ctx := testutil.Context(t, testutil.WaitMedium)
		secondOrg := wirtualdenttest.CreateOrganization(t, ownerClient, wirtualdenttest.CreateOrganizationOptions{
			IncludeProvisionerDaemon: true,
		})

		//nolint:gocritic // owner required to make custom roles
		orgTemplateAdminRole, err := ownerClient.CreateOrganizationRole(ctx, wirtualsdk.Role{
			Name:           "org-template-admin",
			OrganizationID: secondOrg.ID.String(),
			OrganizationPermissions: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceTemplate: wirtualsdk.RBACResourceActions[wirtualsdk.ResourceTemplate],
			}),
		})
		require.NoError(t, err, "create admin role")

		orgTemplateAdmin, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, secondOrg.ID, rbac.RoleIdentifier{
			Name:           orgTemplateAdminRole.Name,
			OrganizationID: secondOrg.ID,
		})

		version := wirtualdtest.CreateTemplateVersion(t, orgTemplateAdmin, secondOrg.ID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionApply: echo.ApplyComplete,
			ProvisionPlan:  echo.PlanComplete,
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, orgTemplateAdmin, version.ID)

		template := wirtualdtest.CreateTemplate(t, orgTemplateAdmin, secondOrg.ID, version.ID)
		require.Equal(t, template.OrganizationID, secondOrg.ID)
	})

	t.Run("MultipleOrganizations", func(t *testing.T) {
		t.Parallel()
		dv := wirtualdtest.DeploymentValues(t)
		ownerClient, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				DeploymentValues: dv,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations: 1,
				},
			},
		})

		client, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleTemplateAdmin())
		org2 := wirtualdenttest.CreateOrganization(t, ownerClient, wirtualdenttest.CreateOrganizationOptions{})
		user, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, org2.ID)

		// 2 templates in first organization
		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		version2 := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
		wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version2.ID)

		// 2 in the second organization
		version3 := wirtualdtest.CreateTemplateVersion(t, client, org2.ID, nil)
		version4 := wirtualdtest.CreateTemplateVersion(t, client, org2.ID, nil)
		wirtualdtest.CreateTemplate(t, client, org2.ID, version3.ID)
		wirtualdtest.CreateTemplate(t, client, org2.ID, version4.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		// All 4 are viewable by the owner
		templates, err := client.Templates(ctx, wirtualsdk.TemplateFilter{})
		require.NoError(t, err)
		require.Len(t, templates, 4)

		// View a single organization from the owner
		templates, err = client.Templates(ctx, wirtualsdk.TemplateFilter{
			OrganizationID: owner.OrganizationID,
		})
		require.NoError(t, err)
		require.Len(t, templates, 2)

		// Only 2 are viewable by the org user
		templates, err = user.Templates(ctx, wirtualsdk.TemplateFilter{})
		require.NoError(t, err)
		require.Len(t, templates, 2)
		for _, tmpl := range templates {
			require.Equal(t, tmpl.OrganizationName, org2.Name, "organization name on template")
		}
	})
}

func TestTemplateACL(t *testing.T) {
	t.Parallel()

	t.Run("UserRoles", func(t *testing.T) {
		t.Parallel()
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		_, user2 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		_, user3 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		err := anotherClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user2.ID.String(): wirtualsdk.TemplateRoleUse,
				user3.ID.String(): wirtualsdk.TemplateRoleAdmin,
			},
		})
		require.NoError(t, err)

		acl, err := anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		templateUser2 := wirtualsdk.TemplateUser{
			User: user2,
			Role: wirtualsdk.TemplateRoleUse,
		}

		templateUser3 := wirtualsdk.TemplateUser{
			User: user3,
			Role: wirtualsdk.TemplateRoleAdmin,
		}

		require.Len(t, acl.Users, 2)
		require.Contains(t, acl.Users, templateUser2)
		require.Contains(t, acl.Users, templateUser3)
	})

	t.Run("everyoneGroup", func(t *testing.T) {
		t.Parallel()
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})

		// Create a user to assert they aren't returned in the response.
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		acl, err := anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		require.Len(t, acl.Groups, 1)
		require.Len(t, acl.Groups[0].Members, 2)
		require.Len(t, acl.Users, 0)
	})

	t.Run("NoGroups", func(t *testing.T) {
		t.Parallel()
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})

		client1, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		//nolint:gocritic // non-template-admin cannot update template acl
		acl, err := client.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		require.Len(t, acl.Groups, 1)
		require.Len(t, acl.Users, 0)

		// User should be able to read template due to allUsers group.
		_, err = client1.Template(ctx, template.ID)
		require.NoError(t, err)

		allUsers := acl.Groups[0]

		err = client.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			GroupPerms: map[string]wirtualsdk.TemplateRole{
				allUsers.ID.String(): wirtualsdk.TemplateRoleDeleted,
			},
		})
		require.NoError(t, err)

		//nolint:gocritic // non-template-admin cannot update template acl
		acl, err = client.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		require.Len(t, acl.Groups, 0)
		require.Len(t, acl.Users, 0)

		// User should not be able to read template due to allUsers group being deleted.
		_, err = client1.Template(ctx, template.ID)
		require.Error(t, err)
		cerr, ok := wirtualsdk.AsError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusNotFound, cerr.StatusCode())
	})

	t.Run("DisableEveryoneGroupAccess", func(t *testing.T) {
		t.Parallel()

		client, admin := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		version := wirtualdtest.CreateTemplateVersion(t, client, admin.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, admin.OrganizationID, version.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		//nolint:gocritic // non-template-admin cannot get template acl
		acl, err := client.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		require.Equal(t, 1, len(acl.Groups))
		_, err = client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			Name:                         template.Name,
			DisplayName:                  template.DisplayName,
			Description:                  template.Description,
			Icon:                         template.Icon,
			AllowUserCancelWorkspaceJobs: template.AllowUserCancelWorkspaceJobs,
			DisableEveryoneGroupAccess:   true,
		})
		require.NoError(t, err)

		acl, err = client.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		require.Equal(t, 0, len(acl.Groups), acl.Groups)
	})

	// Test that we do not return deleted users.
	t.Run("FilterDeletedUsers", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin(), rbac.RoleUserAdmin())

		_, user1 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		err := anotherClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user1.ID.String(): wirtualsdk.TemplateRoleUse,
			},
		})
		require.NoError(t, err)

		acl, err := anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		require.Contains(t, acl.Users, wirtualsdk.TemplateUser{
			User: user1,
			Role: wirtualsdk.TemplateRoleUse,
		})

		err = anotherClient.DeleteUser(ctx, user1.ID)
		require.NoError(t, err)

		acl, err = anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		require.Len(t, acl.Users, 0, "deleted users should be filtered")
	})

	// Test that we do not filter dormant users.
	t.Run("IncludeDormantUsers", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin(), rbac.RoleUserAdmin())

		ctx := testutil.Context(t, testutil.WaitLong)

		// nolint:gocritic // Must use owner to create user.
		user1, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           "coder@coder.com",
			Username:        "coder",
			Password:        "SomeStrongPassword!",
			OrganizationIDs: []uuid.UUID{user.OrganizationID},
		})
		require.NoError(t, err)
		require.Equal(t, wirtualsdk.UserStatusDormant, user1.Status)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		err = anotherClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user1.ID.String(): wirtualsdk.TemplateRoleUse,
			},
		})
		require.NoError(t, err)

		acl, err := anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		require.Contains(t, acl.Users, wirtualsdk.TemplateUser{
			User: user1,
			Role: wirtualsdk.TemplateRoleUse,
		})
	})

	// Test that we do not return suspended users.
	t.Run("FilterSuspendedUsers", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin(), rbac.RoleUserAdmin())

		_, user1 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		err := anotherClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user1.ID.String(): wirtualsdk.TemplateRoleUse,
			},
		})
		require.NoError(t, err)

		acl, err := anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		require.Contains(t, acl.Users, wirtualsdk.TemplateUser{
			User: user1,
			Role: wirtualsdk.TemplateRoleUse,
		})

		_, err = anotherClient.UpdateUserStatus(ctx, user1.ID.String(), wirtualsdk.UserStatusSuspended)
		require.NoError(t, err)

		acl, err = anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		require.Len(t, acl.Users, 0, "suspended users should be filtered")
	})

	// Test that we do not return deleted groups.
	t.Run("FilterDeletedGroups", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin(), rbac.RoleUserAdmin())

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		group, err := anotherClient.CreateGroup(ctx, user.OrganizationID, wirtualsdk.CreateGroupRequest{
			Name: "test",
		})
		require.NoError(t, err)

		err = anotherClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			GroupPerms: map[string]wirtualsdk.TemplateRole{
				group.ID.String(): wirtualsdk.TemplateRoleUse,
			},
		})
		require.NoError(t, err)

		acl, err := anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		// Length should be 2 for test group and the implicit allUsers group.
		require.Len(t, acl.Groups, 2)

		require.Contains(t, acl.Groups, wirtualsdk.TemplateGroup{
			Group: group,
			Role:  wirtualsdk.TemplateRoleUse,
		})

		err = anotherClient.DeleteGroup(ctx, group.ID)
		require.NoError(t, err)

		acl, err = anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		// Length should be 1 for the allUsers group.
		require.Len(t, acl.Groups, 1)
		require.NotContains(t, acl.Groups, wirtualsdk.TemplateGroup{
			Group: group,
			Role:  wirtualsdk.TemplateRoleUse,
		})
	})

	t.Run("AdminCanPushVersions", func(t *testing.T) {
		t.Parallel()
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})

		client1, user1 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		//nolint:gocritic // test setup
		err := client.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user1.ID.String(): wirtualsdk.TemplateRoleUse,
			},
		})
		require.NoError(t, err)

		data, err := echo.Tar(nil)
		require.NoError(t, err)
		file, err := client1.Upload(context.Background(), wirtualsdk.ContentTypeTar, bytes.NewReader(data))
		require.NoError(t, err)

		_, err = client1.CreateTemplateVersion(ctx, user.OrganizationID, wirtualsdk.CreateTemplateVersionRequest{
			Name:          "testme",
			TemplateID:    template.ID,
			FileID:        file.ID,
			StorageMethod: wirtualsdk.ProvisionerStorageMethodFile,
			Provisioner:   wirtualsdk.ProvisionerTypeEcho,
		})
		require.Error(t, err)

		//nolint:gocritic // test setup
		err = client.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user1.ID.String(): wirtualsdk.TemplateRoleAdmin,
			},
		})
		require.NoError(t, err)

		_, err = client1.CreateTemplateVersion(ctx, user.OrganizationID, wirtualsdk.CreateTemplateVersionRequest{
			Name:          "testme",
			TemplateID:    template.ID,
			FileID:        file.ID,
			StorageMethod: wirtualsdk.ProvisionerStorageMethodFile,
			Provisioner:   wirtualsdk.ProvisionerTypeEcho,
		})
		require.NoError(t, err)
	})
}

func TestUpdateTemplateACL(t *testing.T) {
	t.Parallel()

	t.Run("UserPerms", func(t *testing.T) {
		t.Parallel()
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		_, user2 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		_, user3 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		err := anotherClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user2.ID.String(): wirtualsdk.TemplateRoleUse,
				user3.ID.String(): wirtualsdk.TemplateRoleAdmin,
			},
		})
		require.NoError(t, err)

		acl, err := anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		templateUser2 := wirtualsdk.TemplateUser{
			User: user2,
			Role: wirtualsdk.TemplateRoleUse,
		}

		templateUser3 := wirtualsdk.TemplateUser{
			User: user3,
			Role: wirtualsdk.TemplateRoleAdmin,
		}

		require.Len(t, acl.Users, 2)
		require.Contains(t, acl.Users, templateUser2)
		require.Contains(t, acl.Users, templateUser3)
	})

	t.Run("Audit", func(t *testing.T) {
		t.Parallel()

		auditor := audit.NewMock()
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			AuditLogging: true,
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
				Auditor:                  auditor,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureTemplateRBAC: 1,
					wirtualsdk.FeatureAuditLog:     1,
				},
			},
		})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		numLogs := len(auditor.AuditLogs())

		req := wirtualsdk.UpdateTemplateACL{
			GroupPerms: map[string]wirtualsdk.TemplateRole{
				user.OrganizationID.String(): wirtualsdk.TemplateRoleDeleted,
			},
		}
		err := anotherClient.UpdateTemplateACL(ctx, template.ID, req)
		require.NoError(t, err)
		numLogs++

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.True(t, auditor.Contains(t, database.AuditLog{
			Action:     database.AuditActionWrite,
			ResourceID: template.ID,
		}))
	})

	t.Run("DeleteUser", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		_, user2 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		_, user3 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		req := wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user2.ID.String(): wirtualsdk.TemplateRoleUse,
				user3.ID.String(): wirtualsdk.TemplateRoleAdmin,
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		err := anotherClient.UpdateTemplateACL(ctx, template.ID, req)
		require.NoError(t, err)

		acl, err := anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		require.Contains(t, acl.Users, wirtualsdk.TemplateUser{
			User: user2,
			Role: wirtualsdk.TemplateRoleUse,
		})
		require.Contains(t, acl.Users, wirtualsdk.TemplateUser{
			User: user3,
			Role: wirtualsdk.TemplateRoleAdmin,
		})

		req = wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user2.ID.String(): wirtualsdk.TemplateRoleAdmin,
				user3.ID.String(): wirtualsdk.TemplateRoleDeleted,
			},
		}

		err = anotherClient.UpdateTemplateACL(ctx, template.ID, req)
		require.NoError(t, err)

		acl, err = anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		require.Contains(t, acl.Users, wirtualsdk.TemplateUser{
			User: user2,
			Role: wirtualsdk.TemplateRoleAdmin,
		})

		require.NotContains(t, acl.Users, wirtualsdk.TemplateUser{
			User: user3,
			Role: wirtualsdk.TemplateRoleAdmin,
		})
	})

	t.Run("InvalidUUID", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		req := wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				"hi": "admin",
			},
		}

		ctx := testutil.Context(t, testutil.WaitLong)

		//nolint:gocritic // we're testing invalid UUID so testing RBAC is not relevant here.
		err := client.UpdateTemplateACL(ctx, template.ID, req)
		require.Error(t, err)
		cerr, _ := wirtualsdk.AsError(err)
		require.Equal(t, http.StatusBadRequest, cerr.StatusCode())
	})

	t.Run("InvalidUser", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		req := wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				uuid.NewString(): "admin",
			},
		}

		ctx := testutil.Context(t, testutil.WaitLong)

		//nolint:gocritic // we're testing invalid user so testing RBAC is not relevant here.
		err := client.UpdateTemplateACL(ctx, template.ID, req)
		require.Error(t, err)
		cerr, _ := wirtualsdk.AsError(err)
		require.Equal(t, http.StatusBadRequest, cerr.StatusCode())
	})

	t.Run("InvalidRole", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})

		_, user2 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		req := wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user2.ID.String(): "updater",
			},
		}

		ctx := testutil.Context(t, testutil.WaitLong)

		//nolint:gocritic // we're testing invalid role so testing RBAC is not relevant here.
		err := client.UpdateTemplateACL(ctx, template.ID, req)
		require.Error(t, err)
		cerr, _ := wirtualsdk.AsError(err)
		require.Equal(t, http.StatusBadRequest, cerr.StatusCode())
	})

	t.Run("RegularUserCannotUpdatePerms", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})

		client1, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		client2, user2 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		req := wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user2.ID.String(): wirtualsdk.TemplateRoleUse,
			},
		}

		ctx := testutil.Context(t, testutil.WaitLong)

		err := client1.UpdateTemplateACL(ctx, template.ID, req)
		require.NoError(t, err)

		req = wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user2.ID.String(): wirtualsdk.TemplateRoleAdmin,
			},
		}

		err = client2.UpdateTemplateACL(ctx, template.ID, req)
		require.Error(t, err)
		cerr, _ := wirtualsdk.AsError(err)
		require.Equal(t, http.StatusInternalServerError, cerr.StatusCode())
	})

	t.Run("RegularUserWithAdminCanUpdate", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		client1, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		client2, user2 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		_, user3 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		req := wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user2.ID.String(): wirtualsdk.TemplateRoleAdmin,
			},
		}

		// Group adds complexity to the /available endpoint
		// Intentionally omit user2
		wirtualdtest.CreateGroup(t, client, user.OrganizationID, "some-group", user3)

		ctx := testutil.Context(t, testutil.WaitLong)

		err := client1.UpdateTemplateACL(ctx, template.ID, req)
		require.NoError(t, err)

		// Should be able to see user 3
		available, err := client2.TemplateACLAvailable(ctx, template.ID)
		require.NoError(t, err)
		userFound := false
		for _, avail := range available.Users {
			if avail.ID == user3.ID {
				userFound = true
			}
		}
		require.True(t, userFound, "user not found in acl available")

		req = wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				user3.ID.String(): wirtualsdk.TemplateRoleUse,
			},
		}

		err = client2.UpdateTemplateACL(ctx, template.ID, req)
		require.NoError(t, err)

		acl, err := client2.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		found := false
		for _, u := range acl.Users {
			if u.ID == user3.ID {
				found = true
			}
		}
		require.True(t, found, "user not found in acl")
	})

	t.Run("allUsersGroup", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		acl, err := anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		require.Len(t, acl.Groups, 1)
		require.Len(t, acl.Users, 0)
	})

	t.Run("CustomGroupHasAccess", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin(), rbac.RoleUserAdmin())

		client1, user1 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		// Create a group to add to the template.
		group, err := anotherClient.CreateGroup(ctx, user.OrganizationID, wirtualsdk.CreateGroupRequest{
			Name: "test",
		})
		require.NoError(t, err)

		// Check that the only current group is the allUsers group.
		acl, err := anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		require.Len(t, acl.Groups, 1)

		// Update the template to only allow access to the 'test' group.
		err = anotherClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			GroupPerms: map[string]wirtualsdk.TemplateRole{
				// The allUsers group shares the same ID as the organization.
				user.OrganizationID.String(): wirtualsdk.TemplateRoleDeleted,
				group.ID.String():            wirtualsdk.TemplateRoleUse,
			},
		})
		require.NoError(t, err)

		// Get the ACL list for the template and assert the test group is
		// present.
		acl, err = anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		require.Len(t, acl.Groups, 1)
		require.Len(t, acl.Users, 0)
		require.Equal(t, group.ID, acl.Groups[0].ID)

		// Try to get the template as the regular user. This should
		// fail since we haven't been added to the template yet.
		_, err = client1.Template(ctx, template.ID)
		require.Error(t, err)
		cerr, ok := wirtualsdk.AsError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusNotFound, cerr.StatusCode())

		// Patch the group to add the regular user.
		group, err = anotherClient.PatchGroup(ctx, group.ID, wirtualsdk.PatchGroupRequest{
			AddUsers: []string{user1.ID.String()},
		})
		require.NoError(t, err)
		require.Len(t, group.Members, 1)
		require.Equal(t, user1.ID, group.Members[0].ID)

		// Fetching the template should succeed since our group has view access.
		_, err = client1.Template(ctx, template.ID)
		require.NoError(t, err)
	})

	t.Run("NoAccess", func(t *testing.T) {
		t.Parallel()
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		client1, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		acl, err := anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		require.Len(t, acl.Groups, 1)
		require.Len(t, acl.Users, 0)

		// User should be able to read template due to allUsers group.
		_, err = client1.Template(ctx, template.ID)
		require.NoError(t, err)

		allUsers := acl.Groups[0]

		err = anotherClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			GroupPerms: map[string]wirtualsdk.TemplateRole{
				allUsers.ID.String(): wirtualsdk.TemplateRoleDeleted,
			},
		})
		require.NoError(t, err)

		acl, err = anotherClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		require.Len(t, acl.Groups, 0)
		require.Len(t, acl.Users, 0)

		// User should not be able to read template due to allUsers group being deleted.
		_, err = client1.Template(ctx, template.ID)
		require.Error(t, err)
		cerr, ok := wirtualsdk.AsError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusNotFound, cerr.StatusCode())
	})
}

func TestReadFileWithTemplateUpdate(t *testing.T) {
	t.Parallel()
	t.Run("HasTemplateUpdate", func(t *testing.T) {
		t.Parallel()

		// Upload a file
		client, first := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})

		ctx := testutil.Context(t, testutil.WaitLong)

		//nolint:gocritic // regular user cannot create file
		resp, err := client.Upload(ctx, wirtualsdk.ContentTypeTar, bytes.NewReader(make([]byte, 1024)))
		require.NoError(t, err)

		// Make a new user
		member, memberData := wirtualdtest.CreateAnotherUser(t, client, first.OrganizationID)

		// Try to download file, this should fail
		_, _, err = member.Download(ctx, resp.ID)
		require.Error(t, err, "no template yet")

		// Make a new template version with the file
		version := wirtualdtest.CreateTemplateVersion(t, client, first.OrganizationID, nil, func(request *wirtualsdk.CreateTemplateVersionRequest) {
			request.FileID = resp.ID
		})
		template := wirtualdtest.CreateTemplate(t, client, first.OrganizationID, version.ID)

		// Not in acl yet
		_, _, err = member.Download(ctx, resp.ID)
		require.Error(t, err, "not in acl yet")

		//nolint:gocritic // regular user cannot update template acl
		err = client.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				memberData.ID.String(): wirtualsdk.TemplateRoleAdmin,
			},
		})
		require.NoError(t, err)

		_, _, err = member.Download(ctx, resp.ID)
		require.NoError(t, err)
	})
}

// TestTemplateAccess tests the rego -> sql conversion. We need to implement
// this test on at least 1 table type to ensure that the conversion is correct.
// The rbac tests only assert against static SQL queries.
// This is a full rbac test of many of the common role combinations.
//
//nolint:tparallel
func TestTemplateAccess(t *testing.T) {
	t.Parallel()
	// TODO: This context is for all the subtests. Each subtest should have its
	// own context.
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong*3)
	t.Cleanup(cancel)

	dv := wirtualdtest.DeploymentValues(t)
	ownerClient, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
		Options: &wirtualdtest.Options{
			DeploymentValues: dv,
		},
		LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC:          1,
				wirtualsdk.FeatureMultipleOrganizations: 1,
			},
		},
	})

	type coderUser struct {
		*wirtualsdk.Client
		User wirtualsdk.User
	}

	type orgSetup struct {
		Admin         coderUser
		MemberInGroup coderUser
		MemberNoGroup coderUser

		DefaultTemplate wirtualsdk.Template
		AllRead         wirtualsdk.Template
		UserACL         wirtualsdk.Template
		GroupACL        wirtualsdk.Template

		Group wirtualsdk.Group
		Org   wirtualsdk.Organization
	}

	// Create the following users
	// - owner: Site wide owner
	// - template-admin
	// - org-admin (org 1)
	// - org-admin (org 2)
	// - org-member (org 1)
	// - org-member (org 2)

	// Create the following templates in each org
	// - template 1, default acls
	// - template 2, all_user read
	// - template 3, user_acl read for member
	// - template 4, group_acl read for groupMember

	templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleTemplateAdmin())

	makeTemplate := func(t *testing.T, client *wirtualsdk.Client, orgID uuid.UUID, acl wirtualsdk.UpdateTemplateACL) wirtualsdk.Template {
		version := wirtualdtest.CreateTemplateVersion(t, client, orgID, nil)
		template := wirtualdtest.CreateTemplate(t, client, orgID, version.ID)

		err := client.UpdateTemplateACL(ctx, template.ID, acl)
		require.NoError(t, err, "failed to update template acl")

		return template
	}

	makeOrg := func(t *testing.T) orgSetup {
		// Make org
		orgName, err := cryptorand.String(5)
		require.NoError(t, err, "org name")

		// Make users
		newOrg, err := ownerClient.CreateOrganization(ctx, wirtualsdk.CreateOrganizationRequest{Name: orgName})
		require.NoError(t, err, "failed to create org")

		adminCli, adminUsr := wirtualdtest.CreateAnotherUser(t, ownerClient, newOrg.ID, rbac.ScopedRoleOrgAdmin(newOrg.ID))
		groupMemCli, groupMemUsr := wirtualdtest.CreateAnotherUser(t, ownerClient, newOrg.ID, rbac.ScopedRoleOrgMember(newOrg.ID))
		memberCli, memberUsr := wirtualdtest.CreateAnotherUser(t, ownerClient, newOrg.ID, rbac.ScopedRoleOrgMember(newOrg.ID))

		// Make group
		group, err := adminCli.CreateGroup(ctx, newOrg.ID, wirtualsdk.CreateGroupRequest{
			Name: "SingleUser",
		})
		require.NoError(t, err, "failed to create group")

		group, err = adminCli.PatchGroup(ctx, group.ID, wirtualsdk.PatchGroupRequest{
			AddUsers: []string{groupMemUsr.ID.String()},
		})
		require.NoError(t, err, "failed to add user to group")

		// Make templates

		return orgSetup{
			Admin:         coderUser{Client: adminCli, User: adminUsr},
			MemberInGroup: coderUser{Client: groupMemCli, User: groupMemUsr},
			MemberNoGroup: coderUser{Client: memberCli, User: memberUsr},
			Org:           newOrg,
			Group:         group,

			DefaultTemplate: makeTemplate(t, adminCli, newOrg.ID, wirtualsdk.UpdateTemplateACL{
				GroupPerms: map[string]wirtualsdk.TemplateRole{
					newOrg.ID.String(): wirtualsdk.TemplateRoleDeleted,
				},
			}),
			AllRead: makeTemplate(t, adminCli, newOrg.ID, wirtualsdk.UpdateTemplateACL{
				GroupPerms: map[string]wirtualsdk.TemplateRole{
					newOrg.ID.String(): wirtualsdk.TemplateRoleUse,
				},
			}),
			UserACL: makeTemplate(t, adminCli, newOrg.ID, wirtualsdk.UpdateTemplateACL{
				GroupPerms: map[string]wirtualsdk.TemplateRole{
					newOrg.ID.String(): wirtualsdk.TemplateRoleDeleted,
				},
				UserPerms: map[string]wirtualsdk.TemplateRole{
					memberUsr.ID.String(): wirtualsdk.TemplateRoleUse,
				},
			}),
			GroupACL: makeTemplate(t, adminCli, newOrg.ID, wirtualsdk.UpdateTemplateACL{
				GroupPerms: map[string]wirtualsdk.TemplateRole{
					group.ID.String():  wirtualsdk.TemplateRoleUse,
					newOrg.ID.String(): wirtualsdk.TemplateRoleDeleted,
				},
			}),
		}
	}

	// Make 2 organizations
	orgs := []orgSetup{
		makeOrg(t),
		makeOrg(t),
	}

	testTemplateRead := func(t *testing.T, org orgSetup, usr *wirtualsdk.Client, read []wirtualsdk.Template) {
		found, err := usr.TemplatesByOrganization(ctx, org.Org.ID)
		if len(read) == 0 && err != nil {
			require.ErrorContains(t, err, "Resource not found")
			return
		}
		require.NoError(t, err, "failed to get templates")

		exp := make(map[uuid.UUID]wirtualsdk.Template)
		for _, tmpl := range read {
			exp[tmpl.ID] = tmpl
		}

		for _, f := range found {
			if _, ok := exp[f.ID]; !ok {
				t.Errorf("found unexpected template %q", f.Name)
			}
			delete(exp, f.ID)
		}
		require.Len(t, exp, 0, "expected templates not found")
	}

	// nolint:paralleltest
	t.Run("OwnerReadAll", func(t *testing.T) {
		for _, o := range orgs {
			// Owners can read all templates in all orgs
			exp := []wirtualsdk.Template{o.DefaultTemplate, o.AllRead, o.UserACL, o.GroupACL}
			testTemplateRead(t, o, ownerClient, exp)
		}
	})

	// nolint:paralleltest
	t.Run("TemplateAdminReadAll", func(t *testing.T) {
		for _, o := range orgs {
			// Template Admins can read all templates in all orgs
			exp := []wirtualsdk.Template{o.DefaultTemplate, o.AllRead, o.UserACL, o.GroupACL}
			testTemplateRead(t, o, templateAdmin, exp)
		}
	})

	// nolint:paralleltest
	t.Run("OrgAdminReadAllTheirs", func(t *testing.T) {
		for i, o := range orgs {
			cli := o.Admin.Client
			// Only read their own org
			exp := []wirtualsdk.Template{o.DefaultTemplate, o.AllRead, o.UserACL, o.GroupACL}
			testTemplateRead(t, o, cli, exp)

			other := orgs[(i+1)%len(orgs)]
			require.NotEqual(t, other.Org.ID, o.Org.ID, "this test needs at least 2 orgs")
			testTemplateRead(t, other, cli, []wirtualsdk.Template{})
		}
	})

	// nolint:paralleltest
	t.Run("TestMemberNoGroup", func(t *testing.T) {
		for i, o := range orgs {
			cli := o.MemberNoGroup.Client
			// Only read their own org
			exp := []wirtualsdk.Template{o.AllRead, o.UserACL}
			testTemplateRead(t, o, cli, exp)

			other := orgs[(i+1)%len(orgs)]
			require.NotEqual(t, other.Org.ID, o.Org.ID, "this test needs at least 2 orgs")
			testTemplateRead(t, other, cli, []wirtualsdk.Template{})
		}
	})

	// nolint:paralleltest
	t.Run("TestMemberInGroup", func(t *testing.T) {
		for i, o := range orgs {
			cli := o.MemberInGroup.Client
			// Only read their own org
			exp := []wirtualsdk.Template{o.AllRead, o.GroupACL}
			testTemplateRead(t, o, cli, exp)

			other := orgs[(i+1)%len(orgs)]
			require.NotEqual(t, other.Org.ID, o.Org.ID, "this test needs at least 2 orgs")
			testTemplateRead(t, other, cli, []wirtualsdk.Template{})
		}
	})
}

func TestMultipleOrganizationTemplates(t *testing.T) {
	t.Parallel()

	dv := wirtualdtest.DeploymentValues(t)
	ownerClient, first := wirtualdenttest.New(t, &wirtualdenttest.Options{
		Options: &wirtualdtest.Options{
			// This only affects the first org.
			IncludeProvisionerDaemon: true,
			DeploymentValues:         dv,
		},
		LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureExternalProvisionerDaemons: 1,
				wirtualsdk.FeatureMultipleOrganizations:      1,
			},
		},
	})

	templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, first.OrganizationID, rbac.RoleTemplateAdmin())

	second := wirtualdenttest.CreateOrganization(t, ownerClient, wirtualdenttest.CreateOrganizationOptions{
		IncludeProvisionerDaemon: true,
	})

	third := wirtualdenttest.CreateOrganization(t, ownerClient, wirtualdenttest.CreateOrganizationOptions{
		IncludeProvisionerDaemon: true,
	})

	t.Logf("First organization: %s", first.OrganizationID.String())
	t.Logf("Second organization: %s", second.ID.String())
	t.Logf("Third organization: %s", third.ID.String())

	t.Logf("Creating template version in second organization")

	start := time.Now()
	version := wirtualdtest.CreateTemplateVersion(t, templateAdmin, second.ID, nil)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, ownerClient, version.ID)
	wirtualdtest.CreateTemplate(t, templateAdmin, second.ID, version.ID, func(request *wirtualsdk.CreateTemplateRequest) {
		request.Name = "random"
	})

	if time.Since(start) > time.Second*10 {
		// The test can sometimes pass because 'AwaitTemplateVersionJobCompleted'
		// allows 25s, and the provisioner will check every 30s if not awakened
		// from the pubsub. So there is a chance it will pass. If it takes longer
		// than 10s, then it's a problem. The provisioner is not getting clearance.
		t.Error("Creating template version in second organization took too long")
		t.FailNow()
	}
}
