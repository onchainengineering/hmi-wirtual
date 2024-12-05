package wirtuald_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/agent/agenttest"
	"github.com/onchainengineering/hmi-wirtual/provisioner/echo"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/audit"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbauthz"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtestutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtime"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/notifications"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/notifications/notificationstest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/schedule"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/util/ptr"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk/workspacesdk"
)

func TestTemplate(t *testing.T) {
	t.Parallel()

	t.Run("Get", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		_, err := client.Template(ctx, template.ID)
		require.NoError(t, err)
	})
}

func TestPostTemplateByOrganization(t *testing.T) {
	t.Parallel()
	t.Run("Create", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		ownerClient := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true, Auditor: auditor})
		owner := wirtualdtest.CreateFirstUser(t, ownerClient)

		// Use org scoped template admin
		client, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.ScopedRoleOrgTemplateAdmin(owner.OrganizationID))
		// By default, everyone in the org can read the template.
		user, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)
		auditor.ResetLogs()

		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)

		expected := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.ActivityBumpMillis = ptr.Ref((3 * time.Hour).Milliseconds())
		})
		assert.Equal(t, (3 * time.Hour).Milliseconds(), expected.ActivityBumpMillis)

		ctx := testutil.Context(t, testutil.WaitLong)

		got, err := user.Template(ctx, expected.ID)
		require.NoError(t, err)

		assert.Equal(t, expected.Name, got.Name)
		assert.Equal(t, expected.Description, got.Description)
		assert.Equal(t, expected.ActivityBumpMillis, got.ActivityBumpMillis)

		require.Len(t, auditor.AuditLogs(), 3)
		assert.Equal(t, database.AuditActionCreate, auditor.AuditLogs()[0].Action)
		assert.Equal(t, database.AuditActionWrite, auditor.AuditLogs()[1].Action)
		assert.Equal(t, database.AuditActionCreate, auditor.AuditLogs()[2].Action)
	})

	t.Run("AlreadyExists", func(t *testing.T) {
		t.Parallel()
		ownerClient := wirtualdtest.New(t, nil)
		owner := wirtualdtest.CreateFirstUser(t, ownerClient)
		client, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.ScopedRoleOrgTemplateAdmin(owner.OrganizationID))

		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		_, err := client.CreateTemplate(ctx, owner.OrganizationID, wirtualsdk.CreateTemplateRequest{
			Name:      template.Name,
			VersionID: version.ID,
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusConflict, apiErr.StatusCode())
	})

	t.Run("ReservedName", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)

		ctx := testutil.Context(t, testutil.WaitShort)

		_, err := client.CreateTemplate(ctx, user.OrganizationID, wirtualsdk.CreateTemplateRequest{
			Name:      "new",
			VersionID: version.ID,
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode())
	})

	t.Run("DefaultTTLTooLow", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)

		ctx := testutil.Context(t, testutil.WaitLong)
		_, err := client.CreateTemplate(ctx, user.OrganizationID, wirtualsdk.CreateTemplateRequest{
			Name:             "testing",
			VersionID:        version.ID,
			DefaultTTLMillis: ptr.Ref(int64(-1)),
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode())
		require.Contains(t, err.Error(), "default_ttl_ms: Must be a positive integer")
	})

	t.Run("NoDefaultTTL", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)

		ctx := testutil.Context(t, testutil.WaitLong)
		got, err := client.CreateTemplate(ctx, user.OrganizationID, wirtualsdk.CreateTemplateRequest{
			Name:             "testing",
			VersionID:        version.ID,
			DefaultTTLMillis: ptr.Ref(int64(0)),
		})
		require.NoError(t, err)
		require.Zero(t, got.DefaultTTLMillis)
	})

	t.Run("DisableEveryone", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true, Auditor: auditor})
		owner := wirtualdtest.CreateFirstUser(t, client)
		user, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		expected := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID, func(request *wirtualsdk.CreateTemplateRequest) {
			request.DisableEveryoneGroupAccess = true
		})

		ctx := testutil.Context(t, testutil.WaitLong)
		_, err := user.Template(ctx, expected.ID)

		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusNotFound, apiErr.StatusCode())
	})

	t.Run("Unauthorized", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)

		ctx := testutil.Context(t, testutil.WaitLong)
		_, err := client.CreateTemplate(ctx, uuid.New(), wirtualsdk.CreateTemplateRequest{
			Name:      "test",
			VersionID: uuid.New(),
		})

		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode())
		require.Contains(t, err.Error(), "Try logging in using 'coder login'.")
	})

	t.Run("AllowUserScheduling", func(t *testing.T) {
		t.Parallel()

		t.Run("OK", func(t *testing.T) {
			t.Parallel()

			var setCalled int64
			client := wirtualdtest.New(t, &wirtualdtest.Options{
				TemplateScheduleStore: schedule.MockTemplateScheduleStore{
					SetFn: func(ctx context.Context, db database.Store, template database.Template, options schedule.TemplateScheduleOptions) (database.Template, error) {
						atomic.AddInt64(&setCalled, 1)
						require.False(t, options.UserAutostartEnabled)
						require.False(t, options.UserAutostopEnabled)
						template.AllowUserAutostart = options.UserAutostartEnabled
						template.AllowUserAutostop = options.UserAutostopEnabled
						return template, nil
					},
				},
			})
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			got, err := client.CreateTemplate(ctx, user.OrganizationID, wirtualsdk.CreateTemplateRequest{
				Name:               "testing",
				VersionID:          version.ID,
				AllowUserAutostart: ptr.Ref(false),
				AllowUserAutostop:  ptr.Ref(false),
			})
			require.NoError(t, err)

			require.EqualValues(t, 1, atomic.LoadInt64(&setCalled))
			require.False(t, got.AllowUserAutostart)
			require.False(t, got.AllowUserAutostop)
		})

		t.Run("IgnoredUnlicensed", func(t *testing.T) {
			t.Parallel()

			client := wirtualdtest.New(t, nil)
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			got, err := client.CreateTemplate(ctx, user.OrganizationID, wirtualsdk.CreateTemplateRequest{
				Name:               "testing",
				VersionID:          version.ID,
				AllowUserAutostart: ptr.Ref(false),
				AllowUserAutostop:  ptr.Ref(false),
			})
			require.NoError(t, err)
			// ignored and use AGPL defaults
			require.True(t, got.AllowUserAutostart)
			require.True(t, got.AllowUserAutostop)
		})
	})

	t.Run("NoVersion", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)

		ctx := testutil.Context(t, testutil.WaitLong)

		_, err := client.CreateTemplate(ctx, user.OrganizationID, wirtualsdk.CreateTemplateRequest{
			Name:      "test",
			VersionID: uuid.New(),
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusNotFound, apiErr.StatusCode())
	})

	t.Run("AutostopRequirement", func(t *testing.T) {
		t.Parallel()

		t.Run("None", func(t *testing.T) {
			t.Parallel()

			var setCalled int64
			client := wirtualdtest.New(t, &wirtualdtest.Options{
				TemplateScheduleStore: schedule.MockTemplateScheduleStore{
					SetFn: func(ctx context.Context, db database.Store, template database.Template, options schedule.TemplateScheduleOptions) (database.Template, error) {
						atomic.AddInt64(&setCalled, 1)
						assert.Zero(t, options.AutostopRequirement.DaysOfWeek)
						assert.Zero(t, options.AutostopRequirement.Weeks)

						err := db.UpdateTemplateScheduleByID(ctx, database.UpdateTemplateScheduleByIDParams{
							ID:                            template.ID,
							UpdatedAt:                     dbtime.Now(),
							AllowUserAutostart:            options.UserAutostartEnabled,
							AllowUserAutostop:             options.UserAutostopEnabled,
							DefaultTTL:                    int64(options.DefaultTTL),
							ActivityBump:                  int64(options.ActivityBump),
							AutostopRequirementDaysOfWeek: int16(options.AutostopRequirement.DaysOfWeek),
							AutostopRequirementWeeks:      options.AutostopRequirement.Weeks,
							FailureTTL:                    int64(options.FailureTTL),
							TimeTilDormant:                int64(options.TimeTilDormant),
							TimeTilDormantAutoDelete:      int64(options.TimeTilDormantAutoDelete),
						})
						if !assert.NoError(t, err) {
							return database.Template{}, err
						}

						return db.GetTemplateByID(ctx, template.ID)
					},
				},
			})
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			got, err := client.CreateTemplate(ctx, user.OrganizationID, wirtualsdk.CreateTemplateRequest{
				Name:                "testing",
				VersionID:           version.ID,
				AutostopRequirement: nil,
			})
			require.NoError(t, err)

			require.EqualValues(t, 1, atomic.LoadInt64(&setCalled))
			require.Empty(t, got.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 1, got.AutostopRequirement.Weeks)
		})

		t.Run("OK", func(t *testing.T) {
			t.Parallel()

			var setCalled int64
			client := wirtualdtest.New(t, &wirtualdtest.Options{
				TemplateScheduleStore: schedule.MockTemplateScheduleStore{
					SetFn: func(ctx context.Context, db database.Store, template database.Template, options schedule.TemplateScheduleOptions) (database.Template, error) {
						atomic.AddInt64(&setCalled, 1)
						assert.EqualValues(t, 0b00110000, options.AutostopRequirement.DaysOfWeek)
						assert.EqualValues(t, 2, options.AutostopRequirement.Weeks)

						err := db.UpdateTemplateScheduleByID(ctx, database.UpdateTemplateScheduleByIDParams{
							ID:                            template.ID,
							UpdatedAt:                     dbtime.Now(),
							AllowUserAutostart:            options.UserAutostartEnabled,
							AllowUserAutostop:             options.UserAutostopEnabled,
							DefaultTTL:                    int64(options.DefaultTTL),
							ActivityBump:                  int64(options.ActivityBump),
							AutostopRequirementDaysOfWeek: int16(options.AutostopRequirement.DaysOfWeek),
							AutostopRequirementWeeks:      options.AutostopRequirement.Weeks,
							FailureTTL:                    int64(options.FailureTTL),
							TimeTilDormant:                int64(options.TimeTilDormant),
							TimeTilDormantAutoDelete:      int64(options.TimeTilDormantAutoDelete),
						})
						if !assert.NoError(t, err) {
							return database.Template{}, err
						}

						return db.GetTemplateByID(ctx, template.ID)
					},
				},
			})
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			got, err := client.CreateTemplate(ctx, user.OrganizationID, wirtualsdk.CreateTemplateRequest{
				Name:      "testing",
				VersionID: version.ID,
				AutostopRequirement: &wirtualsdk.TemplateAutostopRequirement{
					// wrong order
					DaysOfWeek: []string{"saturday", "friday"},
					Weeks:      2,
				},
			})
			require.NoError(t, err)

			require.EqualValues(t, 1, atomic.LoadInt64(&setCalled))
			require.Equal(t, []string{"friday", "saturday"}, got.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 2, got.AutostopRequirement.Weeks)

			got, err = client.Template(ctx, got.ID)
			require.NoError(t, err)
			require.Equal(t, []string{"friday", "saturday"}, got.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 2, got.AutostopRequirement.Weeks)
		})

		t.Run("IgnoredUnlicensed", func(t *testing.T) {
			t.Parallel()

			client := wirtualdtest.New(t, nil)
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			got, err := client.CreateTemplate(ctx, user.OrganizationID, wirtualsdk.CreateTemplateRequest{
				Name:      "testing",
				VersionID: version.ID,
				AutostopRequirement: &wirtualsdk.TemplateAutostopRequirement{
					DaysOfWeek: []string{"friday", "saturday"},
					Weeks:      2,
				},
			})
			require.NoError(t, err)
			// ignored and use AGPL defaults
			require.Empty(t, got.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 1, got.AutostopRequirement.Weeks)
		})
	})

	t.Run("MaxPortShareLevel", func(t *testing.T) {
		t.Parallel()

		t.Run("OK", func(t *testing.T) {
			client := wirtualdtest.New(t, nil)
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			got, err := client.CreateTemplate(ctx, user.OrganizationID, wirtualsdk.CreateTemplateRequest{
				Name:      "testing",
				VersionID: version.ID,
			})
			require.NoError(t, err)
			require.Equal(t, wirtualsdk.WorkspaceAgentPortShareLevelPublic, got.MaxPortShareLevel)
		})

		t.Run("EnterpriseLevelError", func(t *testing.T) {
			client := wirtualdtest.New(t, nil)
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			_, err := client.CreateTemplate(ctx, user.OrganizationID, wirtualsdk.CreateTemplateRequest{
				Name:              "testing",
				VersionID:         version.ID,
				MaxPortShareLevel: ptr.Ref(wirtualsdk.WorkspaceAgentPortShareLevelPublic),
			})
			var apiErr *wirtualsdk.Error
			require.ErrorAs(t, err, &apiErr)
			require.Equal(t, http.StatusBadRequest, apiErr.StatusCode())
		})
	})
}

func TestTemplatesByOrganization(t *testing.T) {
	t.Parallel()
	t.Run("ListEmpty", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)

		ctx := testutil.Context(t, testutil.WaitLong)

		templates, err := client.TemplatesByOrganization(ctx, user.OrganizationID)
		require.NoError(t, err)
		require.NotNil(t, templates)
		require.Len(t, templates, 0)
	})

	t.Run("List", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		templates, err := client.Templates(ctx, wirtualsdk.TemplateFilter{
			OrganizationID: user.OrganizationID,
		})
		require.NoError(t, err)
		require.Len(t, templates, 1)
	})
	t.Run("ListMultiple", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		version2 := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		foo := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(request *wirtualsdk.CreateTemplateRequest) {
			request.Name = "foobar"
		})
		bar := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version2.ID, func(request *wirtualsdk.CreateTemplateRequest) {
			request.Name = "barbaz"
		})

		ctx := testutil.Context(t, testutil.WaitLong)

		templates, err := client.TemplatesByOrganization(ctx, user.OrganizationID)
		require.NoError(t, err)
		require.Len(t, templates, 2)

		// Listing all should match
		templates, err = client.Templates(ctx, wirtualsdk.TemplateFilter{})
		require.NoError(t, err)
		require.Len(t, templates, 2)

		org, err := client.Organization(ctx, user.OrganizationID)
		require.NoError(t, err)
		for _, tmpl := range templates {
			require.Equal(t, tmpl.OrganizationID, user.OrganizationID, "organization ID")
			require.Equal(t, tmpl.OrganizationName, org.Name, "organization name")
			require.Equal(t, tmpl.OrganizationDisplayName, org.DisplayName, "organization display name")
			require.Equal(t, tmpl.OrganizationIcon, org.Icon, "organization display name")
		}

		// Check fuzzy name matching
		templates, err = client.Templates(ctx, wirtualsdk.TemplateFilter{
			FuzzyName: "bar",
		})
		require.NoError(t, err)
		require.Len(t, templates, 2)

		templates, err = client.Templates(ctx, wirtualsdk.TemplateFilter{
			FuzzyName: "foo",
		})
		require.NoError(t, err)
		require.Len(t, templates, 1)
		require.Equal(t, foo.ID, templates[0].ID)

		templates, err = client.Templates(ctx, wirtualsdk.TemplateFilter{
			FuzzyName: "baz",
		})
		require.NoError(t, err)
		require.Len(t, templates, 1)
		require.Equal(t, bar.ID, templates[0].ID)
	})
}

func TestTemplateByOrganizationAndName(t *testing.T) {
	t.Parallel()
	t.Run("NotFound", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)

		ctx := testutil.Context(t, testutil.WaitLong)

		_, err := client.TemplateByName(ctx, user.OrganizationID, "something")
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusNotFound, apiErr.StatusCode())
	})

	t.Run("Found", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		_, err := client.TemplateByName(ctx, user.OrganizationID, template.Name)
		require.NoError(t, err)
	})
}

func TestPatchTemplateMeta(t *testing.T) {
	t.Parallel()

	t.Run("Modified", func(t *testing.T) {
		t.Parallel()

		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true, Auditor: auditor})
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		assert.Equal(t, (1 * time.Hour).Milliseconds(), template.ActivityBumpMillis)

		req := wirtualsdk.UpdateTemplateMeta{
			Name:                         "new-template-name",
			DisplayName:                  "Displayed Name 456",
			Description:                  "lorem ipsum dolor sit amet et cetera",
			Icon:                         "/icon/new-icon.png",
			DefaultTTLMillis:             12 * time.Hour.Milliseconds(),
			ActivityBumpMillis:           3 * time.Hour.Milliseconds(),
			AllowUserCancelWorkspaceJobs: false,
		}
		// It is unfortunate we need to sleep, but the test can fail if the
		// updatedAt is too close together.
		time.Sleep(time.Millisecond * 5)

		ctx := testutil.Context(t, testutil.WaitLong)

		updated, err := client.UpdateTemplateMeta(ctx, template.ID, req)
		require.NoError(t, err)
		assert.Greater(t, updated.UpdatedAt, template.UpdatedAt)
		assert.Equal(t, req.Name, updated.Name)
		assert.Equal(t, req.DisplayName, updated.DisplayName)
		assert.Equal(t, req.Description, updated.Description)
		assert.Equal(t, req.Icon, updated.Icon)
		assert.Equal(t, req.DefaultTTLMillis, updated.DefaultTTLMillis)
		assert.Equal(t, req.ActivityBumpMillis, updated.ActivityBumpMillis)
		assert.False(t, req.AllowUserCancelWorkspaceJobs)

		// Extra paranoid: did it _really_ happen?
		updated, err = client.Template(ctx, template.ID)
		require.NoError(t, err)
		assert.Greater(t, updated.UpdatedAt, template.UpdatedAt)
		assert.Equal(t, req.Name, updated.Name)
		assert.Equal(t, req.DisplayName, updated.DisplayName)
		assert.Equal(t, req.Description, updated.Description)
		assert.Equal(t, req.Icon, updated.Icon)
		assert.Equal(t, req.DefaultTTLMillis, updated.DefaultTTLMillis)
		assert.Equal(t, req.ActivityBumpMillis, updated.ActivityBumpMillis)
		assert.False(t, req.AllowUserCancelWorkspaceJobs)

		require.Len(t, auditor.AuditLogs(), 5)
		assert.Equal(t, database.AuditActionWrite, auditor.AuditLogs()[4].Action)
	})

	t.Run("AlreadyExists", func(t *testing.T) {
		t.Parallel()

		if !dbtestutil.WillUsePostgres() {
			t.Skip("This test requires Postgres constraints")
		}

		ownerClient := wirtualdtest.New(t, nil)
		owner := wirtualdtest.CreateFirstUser(t, ownerClient)
		client, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.ScopedRoleOrgTemplateAdmin(owner.OrganizationID))

		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		version2 := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
		template2 := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version2.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		_, err := client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			Name: template2.Name,
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusConflict, apiErr.StatusCode())
	})

	t.Run("AGPL_Deprecated", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: false})
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		// It is unfortunate we need to sleep, but the test can fail if the
		// updatedAt is too close together.
		time.Sleep(time.Millisecond * 5)

		req := wirtualsdk.UpdateTemplateMeta{
			DeprecationMessage: ptr.Ref("APGL cannot deprecate"),
		}

		ctx := testutil.Context(t, testutil.WaitLong)

		updated, err := client.UpdateTemplateMeta(ctx, template.ID, req)
		require.NoError(t, err)
		assert.Greater(t, updated.UpdatedAt, template.UpdatedAt)
		// AGPL cannot deprecate, expect no change
		assert.False(t, updated.Deprecated)
		assert.Empty(t, updated.DeprecationMessage)
	})

	// AGPL cannot deprecate, but it can be unset
	t.Run("AGPL_Unset_Deprecated", func(t *testing.T) {
		t.Parallel()

		owner, db := wirtualdtest.NewWithDatabase(t, &wirtualdtest.Options{IncludeProvisionerDaemon: false})
		user := wirtualdtest.CreateFirstUser(t, owner)
		client, tplAdmin := wirtualdtest.CreateAnotherUser(t, owner, user.OrganizationID, rbac.RoleTemplateAdmin())
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		// It is unfortunate we need to sleep, but the test can fail if the
		// updatedAt is too close together.
		time.Sleep(time.Millisecond * 5)

		ctx := testutil.Context(t, testutil.WaitLong)

		// nolint:gocritic // Setting up unit test data
		err := db.UpdateTemplateAccessControlByID(dbauthz.As(ctx, wirtualdtest.AuthzUserSubject(tplAdmin, user.OrganizationID)), database.UpdateTemplateAccessControlByIDParams{
			ID:                   template.ID,
			RequireActiveVersion: false,
			Deprecated:           "Some deprecated message",
		})
		require.NoError(t, err)

		// Check that it is deprecated
		got, err := client.Template(ctx, template.ID)
		require.NoError(t, err)
		require.NotEmpty(t, got.DeprecationMessage, "template is deprecated to start")
		require.True(t, got.Deprecated, "template is deprecated to start")

		req := wirtualsdk.UpdateTemplateMeta{
			DeprecationMessage: ptr.Ref(""),
		}

		updated, err := client.UpdateTemplateMeta(ctx, template.ID, req)
		require.NoError(t, err)
		assert.Greater(t, updated.UpdatedAt, template.UpdatedAt)
		assert.False(t, updated.Deprecated)
		assert.Empty(t, updated.DeprecationMessage)
	})

	t.Run("AGPL_MaxPortShareLevel", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: false})
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		require.Equal(t, wirtualsdk.WorkspaceAgentPortShareLevelPublic, template.MaxPortShareLevel)

		var level wirtualsdk.WorkspaceAgentPortShareLevel = wirtualsdk.WorkspaceAgentPortShareLevelAuthenticated
		req := wirtualsdk.UpdateTemplateMeta{
			MaxPortShareLevel: &level,
		}

		ctx := testutil.Context(t, testutil.WaitLong)

		_, err := client.UpdateTemplateMeta(ctx, template.ID, req)
		// AGPL cannot change max port sharing level
		require.ErrorContains(t, err, "port sharing level is an enterprise feature")

		// Ensure the same value port share level is a no-op
		level = wirtualsdk.WorkspaceAgentPortShareLevelPublic
		_, err = client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			Name:              wirtualdtest.RandomUsername(t),
			MaxPortShareLevel: &level,
		})
		require.NoError(t, err)
	})

	t.Run("NoDefaultTTL", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.DefaultTTLMillis = ptr.Ref(24 * time.Hour.Milliseconds())
		})
		// It is unfortunate we need to sleep, but the test can fail if the
		// updatedAt is too close together.
		time.Sleep(time.Millisecond * 5)

		req := wirtualsdk.UpdateTemplateMeta{
			DefaultTTLMillis: 0,
		}

		// We're too fast! Sleep so we can be sure that updatedAt is greater
		time.Sleep(time.Millisecond * 5)

		ctx := testutil.Context(t, testutil.WaitLong)

		_, err := client.UpdateTemplateMeta(ctx, template.ID, req)
		require.NoError(t, err)

		// Extra paranoid: did it _really_ happen?
		updated, err := client.Template(ctx, template.ID)
		require.NoError(t, err)
		assert.Greater(t, updated.UpdatedAt, template.UpdatedAt)
		assert.Equal(t, req.DefaultTTLMillis, updated.DefaultTTLMillis)
		assert.Empty(t, updated.DeprecationMessage)
		assert.False(t, updated.Deprecated)
	})

	t.Run("DefaultTTLTooLow", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.DefaultTTLMillis = ptr.Ref(24 * time.Hour.Milliseconds())
		})
		// It is unfortunate we need to sleep, but the test can fail if the
		// updatedAt is too close together.
		time.Sleep(time.Millisecond * 5)

		req := wirtualsdk.UpdateTemplateMeta{
			DefaultTTLMillis: -1,
		}

		ctx := testutil.Context(t, testutil.WaitLong)

		_, err := client.UpdateTemplateMeta(ctx, template.ID, req)
		require.ErrorContains(t, err, "default_ttl_ms: Must be a positive integer")

		// Ensure no update occurred
		updated, err := client.Template(ctx, template.ID)
		require.NoError(t, err)
		assert.Equal(t, updated.UpdatedAt, template.UpdatedAt)
		assert.Equal(t, updated.DefaultTTLMillis, template.DefaultTTLMillis)
		assert.Empty(t, updated.DeprecationMessage)
		assert.False(t, updated.Deprecated)
	})

	t.Run("CleanupTTLs", func(t *testing.T) {
		t.Parallel()

		const (
			failureTTL               = 7 * 24 * time.Hour
			inactivityTTL            = 180 * 24 * time.Hour
			timeTilDormantAutoDelete = 360 * 24 * time.Hour
		)

		t.Run("OK", func(t *testing.T) {
			t.Parallel()

			var setCalled int64
			client := wirtualdtest.New(t, &wirtualdtest.Options{
				TemplateScheduleStore: schedule.MockTemplateScheduleStore{
					SetFn: func(ctx context.Context, db database.Store, template database.Template, options schedule.TemplateScheduleOptions) (database.Template, error) {
						if atomic.AddInt64(&setCalled, 1) == 2 {
							require.Equal(t, failureTTL, options.FailureTTL)
							require.Equal(t, inactivityTTL, options.TimeTilDormant)
							require.Equal(t, timeTilDormantAutoDelete, options.TimeTilDormantAutoDelete)
						}
						template.FailureTTL = int64(options.FailureTTL)
						template.TimeTilDormant = int64(options.TimeTilDormant)
						template.TimeTilDormantAutoDelete = int64(options.TimeTilDormantAutoDelete)
						return template, nil
					},
				},
			})
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
				ctr.FailureTTLMillis = ptr.Ref(0 * time.Hour.Milliseconds())
				ctr.TimeTilDormantMillis = ptr.Ref(0 * time.Hour.Milliseconds())
				ctr.TimeTilDormantAutoDeleteMillis = ptr.Ref(0 * time.Hour.Milliseconds())
			})

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			got, err := client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
				Name:                           template.Name,
				DisplayName:                    template.DisplayName,
				Description:                    template.Description,
				Icon:                           template.Icon,
				DefaultTTLMillis:               0,
				AutostopRequirement:            &template.AutostopRequirement,
				AllowUserCancelWorkspaceJobs:   template.AllowUserCancelWorkspaceJobs,
				FailureTTLMillis:               failureTTL.Milliseconds(),
				TimeTilDormantMillis:           inactivityTTL.Milliseconds(),
				TimeTilDormantAutoDeleteMillis: timeTilDormantAutoDelete.Milliseconds(),
			})
			require.NoError(t, err)

			require.EqualValues(t, 2, atomic.LoadInt64(&setCalled))
			require.Equal(t, failureTTL.Milliseconds(), got.FailureTTLMillis)
			require.Equal(t, inactivityTTL.Milliseconds(), got.TimeTilDormantMillis)
			require.Equal(t, timeTilDormantAutoDelete.Milliseconds(), got.TimeTilDormantAutoDeleteMillis)
		})

		t.Run("IgnoredUnlicensed", func(t *testing.T) {
			t.Parallel()

			client := wirtualdtest.New(t, nil)
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
				ctr.FailureTTLMillis = ptr.Ref(0 * time.Hour.Milliseconds())
				ctr.TimeTilDormantMillis = ptr.Ref(0 * time.Hour.Milliseconds())
				ctr.TimeTilDormantAutoDeleteMillis = ptr.Ref(0 * time.Hour.Milliseconds())
			})

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			got, err := client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
				Name:                           template.Name,
				DisplayName:                    template.DisplayName,
				Description:                    template.Description,
				Icon:                           template.Icon,
				DefaultTTLMillis:               template.DefaultTTLMillis,
				AutostopRequirement:            &template.AutostopRequirement,
				AllowUserCancelWorkspaceJobs:   template.AllowUserCancelWorkspaceJobs,
				FailureTTLMillis:               failureTTL.Milliseconds(),
				TimeTilDormantMillis:           inactivityTTL.Milliseconds(),
				TimeTilDormantAutoDeleteMillis: timeTilDormantAutoDelete.Milliseconds(),
			})
			require.NoError(t, err)
			require.Zero(t, got.FailureTTLMillis)
			require.Zero(t, got.TimeTilDormantMillis)
			require.Zero(t, got.TimeTilDormantAutoDeleteMillis)
			require.Empty(t, got.DeprecationMessage)
			require.False(t, got.Deprecated)
		})
	})

	t.Run("AllowUserScheduling", func(t *testing.T) {
		t.Parallel()

		t.Run("OK", func(t *testing.T) {
			t.Parallel()

			var (
				setCalled      int64
				allowAutostart atomic.Bool
				allowAutostop  atomic.Bool
			)
			allowAutostart.Store(true)
			allowAutostop.Store(true)
			client := wirtualdtest.New(t, &wirtualdtest.Options{
				TemplateScheduleStore: schedule.MockTemplateScheduleStore{
					SetFn: func(ctx context.Context, db database.Store, template database.Template, options schedule.TemplateScheduleOptions) (database.Template, error) {
						atomic.AddInt64(&setCalled, 1)
						assert.Equal(t, allowAutostart.Load(), options.UserAutostartEnabled)
						assert.Equal(t, allowAutostop.Load(), options.UserAutostopEnabled)

						template.DefaultTTL = int64(options.DefaultTTL)
						template.AllowUserAutostart = options.UserAutostartEnabled
						template.AllowUserAutostop = options.UserAutostopEnabled
						return template, nil
					},
				},
			})
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
				ctr.DefaultTTLMillis = ptr.Ref(24 * time.Hour.Milliseconds())
			})
			require.Equal(t, allowAutostart.Load(), template.AllowUserAutostart)
			require.Equal(t, allowAutostop.Load(), template.AllowUserAutostop)

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			allowAutostart.Store(false)
			allowAutostop.Store(false)
			got, err := client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
				Name:                         template.Name,
				DisplayName:                  template.DisplayName,
				Description:                  template.Description,
				Icon:                         template.Icon,
				DefaultTTLMillis:             template.DefaultTTLMillis,
				AutostopRequirement:          &template.AutostopRequirement,
				AllowUserCancelWorkspaceJobs: template.AllowUserCancelWorkspaceJobs,
				AllowUserAutostart:           allowAutostart.Load(),
				AllowUserAutostop:            allowAutostop.Load(),
			})
			require.NoError(t, err)

			require.EqualValues(t, 2, atomic.LoadInt64(&setCalled))
			require.Equal(t, allowAutostart.Load(), got.AllowUserAutostart)
			require.Equal(t, allowAutostop.Load(), got.AllowUserAutostop)
		})

		t.Run("IgnoredUnlicensed", func(t *testing.T) {
			t.Parallel()

			client := wirtualdtest.New(t, nil)
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
				ctr.DefaultTTLMillis = ptr.Ref(24 * time.Hour.Milliseconds())
			})

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			got, err := client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
				Name:        template.Name,
				DisplayName: template.DisplayName,
				Description: template.Description,
				Icon:        template.Icon,
				// Increase the default TTL to avoid error "not modified".
				DefaultTTLMillis:             template.DefaultTTLMillis + 1,
				AutostopRequirement:          &template.AutostopRequirement,
				AllowUserCancelWorkspaceJobs: template.AllowUserCancelWorkspaceJobs,
				AllowUserAutostart:           false,
				AllowUserAutostop:            false,
			})
			require.NoError(t, err)
			require.True(t, got.AllowUserAutostart)
			require.True(t, got.AllowUserAutostop)
		})
	})

	t.Run("NotModified", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.Description = "original description"
			ctr.Icon = "/icon/original-icon.png"
			ctr.DefaultTTLMillis = ptr.Ref(24 * time.Hour.Milliseconds())
		})

		ctx := testutil.Context(t, testutil.WaitLong)

		req := wirtualsdk.UpdateTemplateMeta{
			Name:                template.Name,
			Description:         template.Description,
			Icon:                template.Icon,
			DefaultTTLMillis:    template.DefaultTTLMillis,
			ActivityBumpMillis:  template.ActivityBumpMillis,
			AutostopRequirement: nil,
			AllowUserAutostart:  template.AllowUserAutostart,
			AllowUserAutostop:   template.AllowUserAutostop,
		}
		_, err := client.UpdateTemplateMeta(ctx, template.ID, req)
		require.ErrorContains(t, err, "not modified")
		updated, err := client.Template(ctx, template.ID)
		require.NoError(t, err)
		assert.Equal(t, updated.UpdatedAt, template.UpdatedAt)
		assert.Equal(t, template.Name, updated.Name)
		assert.Equal(t, template.Description, updated.Description)
		assert.Equal(t, template.Icon, updated.Icon)
		assert.Equal(t, template.DefaultTTLMillis, updated.DefaultTTLMillis)
	})

	t.Run("Invalid", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.Description = "original description"
			ctr.DefaultTTLMillis = ptr.Ref(24 * time.Hour.Milliseconds())
		})

		ctx := testutil.Context(t, testutil.WaitLong)

		req := wirtualsdk.UpdateTemplateMeta{
			DefaultTTLMillis: -int64(time.Hour),
		}
		_, err := client.UpdateTemplateMeta(ctx, template.ID, req)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Contains(t, apiErr.Message, "Invalid request")
		require.Len(t, apiErr.Validations, 1)
		assert.Equal(t, apiErr.Validations[0].Field, "default_ttl_ms")

		updated, err := client.Template(ctx, template.ID)
		require.NoError(t, err)
		assert.WithinDuration(t, template.UpdatedAt, updated.UpdatedAt, time.Minute)
		assert.Equal(t, template.Name, updated.Name)
		assert.Equal(t, template.Description, updated.Description)
		assert.Equal(t, template.Icon, updated.Icon)
		assert.Equal(t, template.DefaultTTLMillis, updated.DefaultTTLMillis)
	})

	t.Run("RemoveIcon", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.Icon = "/icon/code.png"
		})
		req := wirtualsdk.UpdateTemplateMeta{
			Icon: "",
		}

		ctx := testutil.Context(t, testutil.WaitLong)

		updated, err := client.UpdateTemplateMeta(ctx, template.ID, req)
		require.NoError(t, err)
		assert.Equal(t, updated.Icon, "")
	})

	t.Run("AutostopRequirement", func(t *testing.T) {
		t.Parallel()

		t.Run("OK", func(t *testing.T) {
			t.Parallel()

			var setCalled int64
			client := wirtualdtest.New(t, &wirtualdtest.Options{
				TemplateScheduleStore: schedule.MockTemplateScheduleStore{
					SetFn: func(ctx context.Context, db database.Store, template database.Template, options schedule.TemplateScheduleOptions) (database.Template, error) {
						if atomic.AddInt64(&setCalled, 1) == 2 {
							assert.EqualValues(t, 0b0110000, options.AutostopRequirement.DaysOfWeek)
							assert.EqualValues(t, 2, options.AutostopRequirement.Weeks)
						}

						err := db.UpdateTemplateScheduleByID(ctx, database.UpdateTemplateScheduleByIDParams{
							ID:                            template.ID,
							UpdatedAt:                     dbtime.Now(),
							AllowUserAutostart:            options.UserAutostartEnabled,
							AllowUserAutostop:             options.UserAutostopEnabled,
							DefaultTTL:                    int64(options.DefaultTTL),
							ActivityBump:                  int64(options.ActivityBump),
							AutostopRequirementDaysOfWeek: int16(options.AutostopRequirement.DaysOfWeek),
							AutostopRequirementWeeks:      options.AutostopRequirement.Weeks,
							FailureTTL:                    int64(options.FailureTTL),
							TimeTilDormant:                int64(options.TimeTilDormant),
							TimeTilDormantAutoDelete:      int64(options.TimeTilDormantAutoDelete),
						})
						if !assert.NoError(t, err) {
							return database.Template{}, err
						}

						return db.GetTemplateByID(ctx, template.ID)
					},
				},
			})
			user := wirtualdtest.CreateFirstUser(t, client)

			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
			require.EqualValues(t, 1, atomic.LoadInt64(&setCalled))
			require.Empty(t, template.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 1, template.AutostopRequirement.Weeks)
			req := wirtualsdk.UpdateTemplateMeta{
				Name:                         template.Name,
				DisplayName:                  template.DisplayName,
				Description:                  template.Description,
				Icon:                         template.Icon,
				AllowUserCancelWorkspaceJobs: template.AllowUserCancelWorkspaceJobs,
				DefaultTTLMillis:             time.Hour.Milliseconds(),
				AutostopRequirement: &wirtualsdk.TemplateAutostopRequirement{
					// wrong order
					DaysOfWeek: []string{"saturday", "friday"},
					Weeks:      2,
				},
			}

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			updated, err := client.UpdateTemplateMeta(ctx, template.ID, req)
			require.NoError(t, err)
			require.EqualValues(t, 2, atomic.LoadInt64(&setCalled))
			require.Equal(t, []string{"friday", "saturday"}, updated.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 2, updated.AutostopRequirement.Weeks)

			template, err = client.Template(ctx, template.ID)
			require.NoError(t, err)
			require.Equal(t, []string{"friday", "saturday"}, template.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 2, template.AutostopRequirement.Weeks)
			require.Empty(t, template.DeprecationMessage)
			require.False(t, template.Deprecated)
		})

		t.Run("Unset", func(t *testing.T) {
			t.Parallel()

			var setCalled int64
			client := wirtualdtest.New(t, &wirtualdtest.Options{
				TemplateScheduleStore: schedule.MockTemplateScheduleStore{
					SetFn: func(ctx context.Context, db database.Store, template database.Template, options schedule.TemplateScheduleOptions) (database.Template, error) {
						if atomic.AddInt64(&setCalled, 1) == 2 {
							assert.EqualValues(t, 0, options.AutostopRequirement.DaysOfWeek)
							assert.EqualValues(t, 1, options.AutostopRequirement.Weeks)
						}

						err := db.UpdateTemplateScheduleByID(ctx, database.UpdateTemplateScheduleByIDParams{
							ID:                            template.ID,
							UpdatedAt:                     dbtime.Now(),
							AllowUserAutostart:            options.UserAutostartEnabled,
							AllowUserAutostop:             options.UserAutostopEnabled,
							DefaultTTL:                    int64(options.DefaultTTL),
							ActivityBump:                  int64(options.ActivityBump),
							AutostopRequirementDaysOfWeek: int16(options.AutostopRequirement.DaysOfWeek),
							AutostopRequirementWeeks:      options.AutostopRequirement.Weeks,
							FailureTTL:                    int64(options.FailureTTL),
							TimeTilDormant:                int64(options.TimeTilDormant),
							TimeTilDormantAutoDelete:      int64(options.TimeTilDormantAutoDelete),
						})
						if !assert.NoError(t, err) {
							return database.Template{}, err
						}

						return db.GetTemplateByID(ctx, template.ID)
					},
				},
			})
			user := wirtualdtest.CreateFirstUser(t, client)

			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
				ctr.AutostopRequirement = &wirtualsdk.TemplateAutostopRequirement{
					// wrong order
					DaysOfWeek: []string{"sunday", "saturday", "friday", "thursday", "wednesday", "tuesday", "monday"},
					Weeks:      2,
				}
			})
			require.EqualValues(t, 1, atomic.LoadInt64(&setCalled))
			require.Equal(t, []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}, template.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 2, template.AutostopRequirement.Weeks)
			req := wirtualsdk.UpdateTemplateMeta{
				Name:                         template.Name,
				DisplayName:                  template.DisplayName,
				Description:                  template.Description,
				Icon:                         template.Icon,
				AllowUserCancelWorkspaceJobs: template.AllowUserCancelWorkspaceJobs,
				DefaultTTLMillis:             time.Hour.Milliseconds(),
				AutostopRequirement: &wirtualsdk.TemplateAutostopRequirement{
					DaysOfWeek: []string{},
					Weeks:      0,
				},
			}

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			updated, err := client.UpdateTemplateMeta(ctx, template.ID, req)
			require.NoError(t, err)
			require.EqualValues(t, 2, atomic.LoadInt64(&setCalled))
			require.Empty(t, updated.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 1, updated.AutostopRequirement.Weeks)

			template, err = client.Template(ctx, template.ID)
			require.NoError(t, err)
			require.Empty(t, template.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 1, template.AutostopRequirement.Weeks)
		})

		t.Run("EnterpriseOnly", func(t *testing.T) {
			t.Parallel()

			client := wirtualdtest.New(t, nil)
			user := wirtualdtest.CreateFirstUser(t, client)
			version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
			require.Empty(t, template.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 1, template.AutostopRequirement.Weeks)
			req := wirtualsdk.UpdateTemplateMeta{
				Name:                         template.Name,
				DisplayName:                  template.DisplayName,
				Description:                  template.Description,
				Icon:                         template.Icon,
				AllowUserCancelWorkspaceJobs: template.AllowUserCancelWorkspaceJobs,
				DefaultTTLMillis:             time.Hour.Milliseconds(),
				AutostopRequirement: &wirtualsdk.TemplateAutostopRequirement{
					DaysOfWeek: []string{"monday"},
					Weeks:      2,
				},
			}

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			updated, err := client.UpdateTemplateMeta(ctx, template.ID, req)
			require.NoError(t, err)
			require.Empty(t, updated.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 1, updated.AutostopRequirement.Weeks)

			template, err = client.Template(ctx, template.ID)
			require.NoError(t, err)
			require.Empty(t, template.AutostopRequirement.DaysOfWeek)
			require.EqualValues(t, 1, template.AutostopRequirement.Weeks)
			require.Empty(t, template.DeprecationMessage)
			require.False(t, template.Deprecated)
		})
	})
}

func TestDeleteTemplate(t *testing.T) {
	t.Parallel()

	t.Run("NoWorkspaces", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true, Auditor: auditor})
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		err := client.DeleteTemplate(ctx, template.ID)
		require.NoError(t, err)

		require.Len(t, auditor.AuditLogs(), 5)
		assert.Equal(t, database.AuditActionDelete, auditor.AuditLogs()[4].Action)
	})

	t.Run("Workspaces", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		wirtualdtest.CreateWorkspace(t, client, template.ID)

		ctx := testutil.Context(t, testutil.WaitLong)

		err := client.DeleteTemplate(ctx, template.ID)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode())
	})
}

func TestTemplateMetrics(t *testing.T) {
	t.Parallel()

	t.Skip("flaky test: https://github.com/coder/coder/issues/6481")

	client := wirtualdtest.New(t, &wirtualdtest.Options{
		IncludeProvisionerDaemon:    true,
		AgentStatsRefreshInterval:   time.Millisecond * 100,
		MetricsCacheRefreshInterval: time.Millisecond * 100,
	})

	user := wirtualdtest.CreateFirstUser(t, client)
	authToken := uuid.NewString()
	version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
		Parse:          echo.ParseComplete,
		ProvisionPlan:  echo.PlanComplete,
		ProvisionApply: echo.ProvisionApplyWithAgent(authToken),
	})
	template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
	require.Equal(t, -1, template.ActiveUserCount)
	require.Empty(t, template.BuildTimeStats[wirtualsdk.WorkspaceTransitionStart])

	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
	workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)
	wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

	_ = agenttest.New(t, client.URL, authToken)
	resources := wirtualdtest.AwaitWorkspaceAgents(t, client, workspace.ID)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	daus, err := client.TemplateDAUs(context.Background(), template.ID, wirtualsdk.TimezoneOffsetHour(time.UTC))
	require.NoError(t, err)

	require.Equal(t, &wirtualsdk.DAUsResponse{
		Entries: []wirtualsdk.DAUEntry{},
	}, daus, "no DAUs when stats are empty")

	res, err := client.Workspaces(ctx, wirtualsdk.WorkspaceFilter{})
	require.NoError(t, err)
	assert.Zero(t, res.Workspaces[0].LastUsedAt)

	conn, err := workspacesdk.New(client).
		DialAgent(ctx, resources[0].Agents[0].ID, &workspacesdk.DialAgentOptions{
			Logger: testutil.Logger(t).Named("tailnet"),
		})
	require.NoError(t, err)
	defer func() {
		_ = conn.Close()
	}()

	sshConn, err := conn.SSHClient(ctx)
	require.NoError(t, err)
	_ = sshConn.Close()

	wantDAUs := &wirtualsdk.DAUsResponse{
		Entries: []wirtualsdk.DAUEntry{
			{
				Date:   time.Now().UTC().Truncate(time.Hour * 24).Format("2006-01-02"),
				Amount: 1,
			},
		},
	}
	require.Eventuallyf(t, func() bool {
		daus, err = client.TemplateDAUs(ctx, template.ID, wirtualsdk.TimezoneOffsetHour(time.UTC))
		require.NoError(t, err)
		return len(daus.Entries) > 0
	},
		testutil.WaitShort, testutil.IntervalFast,
		"template daus never loaded",
	)
	gotDAUs, err := client.TemplateDAUs(ctx, template.ID, wirtualsdk.TimezoneOffsetHour(time.UTC))
	require.NoError(t, err)
	require.Equal(t, gotDAUs, wantDAUs)

	template, err = client.Template(ctx, template.ID)
	require.NoError(t, err)
	require.Equal(t, 1, template.ActiveUserCount)

	require.Eventuallyf(t, func() bool {
		template, err = client.Template(ctx, template.ID)
		require.NoError(t, err)
		startMs := template.BuildTimeStats[wirtualsdk.WorkspaceTransitionStart].P50
		return startMs != nil && *startMs > 1
	},
		testutil.WaitShort, testutil.IntervalFast,
		"BuildTimeStats never loaded",
	)

	res, err = client.Workspaces(ctx, wirtualsdk.WorkspaceFilter{})
	require.NoError(t, err)
	assert.WithinDuration(t,
		dbtime.Now(), res.Workspaces[0].LastUsedAt, time.Minute,
	)
}

func TestTemplateNotifications(t *testing.T) {
	t.Parallel()

	t.Run("Delete", func(t *testing.T) {
		t.Parallel()

		t.Run("InitiatorIsNotNotified", func(t *testing.T) {
			t.Parallel()

			// Given: an initiator
			var (
				notifyEnq = &notificationstest.FakeEnqueuer{}
				client    = wirtualdtest.New(t, &wirtualdtest.Options{
					IncludeProvisionerDaemon: true,
					NotificationsEnqueuer:    notifyEnq,
				})
				initiator = wirtualdtest.CreateFirstUser(t, client)
				version   = wirtualdtest.CreateTemplateVersion(t, client, initiator.OrganizationID, nil)
				_         = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
				template  = wirtualdtest.CreateTemplate(t, client, initiator.OrganizationID, version.ID)
				ctx       = testutil.Context(t, testutil.WaitLong)
			)

			// When: the template is deleted by the initiator
			err := client.DeleteTemplate(ctx, template.ID)
			require.NoError(t, err)

			// Then: the delete notification is not sent to the initiator.
			deleteNotifications := make([]*notificationstest.FakeNotification, 0)
			for _, n := range notifyEnq.Sent() {
				if n.TemplateID == notifications.TemplateTemplateDeleted {
					deleteNotifications = append(deleteNotifications, n)
				}
			}
			require.Len(t, deleteNotifications, 0)
		})

		t.Run("OnlyOwnersAndAdminsAreNotified", func(t *testing.T) {
			t.Parallel()

			// Given: multiple users with different roles
			var (
				notifyEnq = &notificationstest.FakeEnqueuer{}
				client    = wirtualdtest.New(t, &wirtualdtest.Options{
					IncludeProvisionerDaemon: true,
					NotificationsEnqueuer:    notifyEnq,
				})
				initiator = wirtualdtest.CreateFirstUser(t, client)
				ctx       = testutil.Context(t, testutil.WaitLong)

				// Setup template
				version  = wirtualdtest.CreateTemplateVersion(t, client, initiator.OrganizationID, nil)
				_        = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
				template = wirtualdtest.CreateTemplate(t, client, initiator.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
					ctr.DisplayName = "Bobby's Template"
				})
			)

			// Setup users with different roles
			_, owner := wirtualdtest.CreateAnotherUser(t, client, initiator.OrganizationID, rbac.RoleOwner())
			_, tmplAdmin := wirtualdtest.CreateAnotherUser(t, client, initiator.OrganizationID, rbac.RoleTemplateAdmin())
			wirtualdtest.CreateAnotherUser(t, client, initiator.OrganizationID, rbac.RoleMember())
			wirtualdtest.CreateAnotherUser(t, client, initiator.OrganizationID, rbac.RoleUserAdmin())
			wirtualdtest.CreateAnotherUser(t, client, initiator.OrganizationID, rbac.RoleAuditor())

			// When: the template is deleted by the initiator
			err := client.DeleteTemplate(ctx, template.ID)
			require.NoError(t, err)

			// Then: only owners and template admins should receive the
			// notification.
			shouldBeNotified := []uuid.UUID{owner.ID, tmplAdmin.ID}
			var deleteTemplateNotifications []*notificationstest.FakeNotification
			for _, n := range notifyEnq.Sent() {
				if n.TemplateID == notifications.TemplateTemplateDeleted {
					deleteTemplateNotifications = append(deleteTemplateNotifications, n)
				}
			}
			notifiedUsers := make([]uuid.UUID, 0, len(deleteTemplateNotifications))
			for _, n := range deleteTemplateNotifications {
				notifiedUsers = append(notifiedUsers, n.UserID)
			}
			require.ElementsMatch(t, shouldBeNotified, notifiedUsers)

			// Validate the notification content
			for _, n := range deleteTemplateNotifications {
				require.Equal(t, n.TemplateID, notifications.TemplateTemplateDeleted)
				require.Contains(t, notifiedUsers, n.UserID)
				require.Contains(t, n.Targets, template.ID)
				require.Contains(t, n.Targets, template.OrganizationID)
				require.Equal(t, n.Labels["name"], template.DisplayName)
				require.Equal(t, n.Labels["initiator"], wirtualdtest.FirstUserParams.Username)
			}
		})
	})
}
