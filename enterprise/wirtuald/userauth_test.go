package wirtuald_test

import (
	"context"
	"net/http"
	"regexp"
	"testing"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/coder/serpent"
	wirtualden "github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/db2sdk"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbauthz"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbgen"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtestutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/util/slice"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest/oidctest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

// nolint:bodyclose
func TestUserOIDC(t *testing.T) {
	t.Parallel()

	t.Run("OrganizationSync", func(t *testing.T) {
		t.Parallel()

		t.Run("SingleOrgDeployment", func(t *testing.T) {
			t.Parallel()

			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.UserRoleField = "roles"
				},
			})

			claims := jwt.MapClaims{
				"email": "alice@coder.com",
			}

			// Login a new client that signs up
			client, resp := runner.Login(t, claims)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertOrganizations(t, "alice", true, nil)

			// Force a refresh, and assert nothing has changes
			runner.ForceRefresh(t, client, claims)
			runner.AssertOrganizations(t, "alice", true, nil)
		})

		t.Run("MultiOrgNoSync", func(t *testing.T) {
			t.Parallel()

			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
			})

			ctx := testutil.Context(t, testutil.WaitMedium)
			second, err := runner.AdminClient.CreateOrganization(ctx, wirtualsdk.CreateOrganizationRequest{
				Name:        "second",
				DisplayName: "",
				Description: "",
				Icon:        "",
			})
			require.NoError(t, err)

			claims := jwt.MapClaims{
				"email": "alice@coder.com",
			}

			// Login a new client that signs up
			_, resp := runner.Login(t, claims)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertOrganizations(t, "alice", true, nil)

			// Add alice to new org
			_, err = runner.AdminClient.PostOrganizationMember(ctx, second.ID, "alice")
			require.NoError(t, err)

			// Log in again to refresh the sync. The user should not be removed
			// from the second organization.
			runner.Login(t, claims)
			runner.AssertOrganizations(t, "alice", true, []uuid.UUID{second.ID})
		})

		t.Run("MultiOrgWithDefault", func(t *testing.T) {
			t.Parallel()

			// Given: 4 organizations: default, second, third, and fourth
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					// Will be overwritten by dynamic value
					dv.OIDC.OrganizationAssignDefault = false
					dv.OIDC.OrganizationField = "organization"
					dv.OIDC.OrganizationMapping = serpent.Struct[map[string][]uuid.UUID]{
						Value: map[string][]uuid.UUID{},
					}
				},
			})

			ctx := testutil.Context(t, testutil.WaitMedium)
			orgOne, err := runner.AdminClient.CreateOrganization(ctx, wirtualsdk.CreateOrganizationRequest{
				Name:        "one",
				DisplayName: "One",
				Description: "",
				Icon:        "",
			})
			require.NoError(t, err)

			orgTwo, err := runner.AdminClient.CreateOrganization(ctx, wirtualsdk.CreateOrganizationRequest{
				Name:        "two",
				DisplayName: "two",
				Description: "",
				Icon:        "",
			})
			require.NoError(t, err)

			orgThree, err := runner.AdminClient.CreateOrganization(ctx, wirtualsdk.CreateOrganizationRequest{
				Name:        "three",
				DisplayName: "three",
			})
			require.NoError(t, err)

			expectedSettings := wirtualsdk.OrganizationSyncSettings{
				Field: "organization",
				Mapping: map[string][]uuid.UUID{
					"first":  {orgOne.ID},
					"second": {orgTwo.ID},
				},
				AssignDefault: true,
			}
			settings, err := runner.AdminClient.PatchOrganizationIDPSyncSettings(ctx, expectedSettings)
			require.NoError(t, err)
			require.Equal(t, expectedSettings.Field, settings.Field)

			claims := jwt.MapClaims{
				"email":        "alice@coder.com",
				"organization": []string{"first", "second"},
			}

			// Then: a new user logs in with claims "second" and "third", they
			// should belong to [default, second, third].
			userClient, resp := runner.Login(t, claims)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertOrganizations(t, "alice", true, []uuid.UUID{orgOne.ID, orgTwo.ID})
			user, err := userClient.User(ctx, wirtualsdk.Me)
			require.NoError(t, err)

			// Then: the available sync fields should be "email" and "organization"
			fields, err := runner.AdminClient.GetAvailableIDPSyncFields(ctx)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{
				"aud", "exp", "iss", // Always included from jwt
				"email", "organization",
			}, fields)

			// This should be the same as above
			orgFields, err := runner.AdminClient.GetOrganizationAvailableIDPSyncFields(ctx, orgOne.ID.String())
			require.NoError(t, err)
			require.ElementsMatch(t, fields, orgFields)

			// When: they are manually added to the fourth organization, a new sync
			// should remove them.
			_, err = runner.AdminClient.PostOrganizationMember(ctx, orgThree.ID, "alice")
			require.ErrorContains(t, err, "Organization sync is enabled")

			runner.AssertOrganizations(t, "alice", true, []uuid.UUID{orgOne.ID, orgTwo.ID})
			// Go around the block to add the user to see if they are removed.
			dbgen.OrganizationMember(t, runner.API.Database, database.OrganizationMember{
				UserID:         user.ID,
				OrganizationID: orgThree.ID,
			})
			runner.AssertOrganizations(t, "alice", true, []uuid.UUID{orgOne.ID, orgTwo.ID, orgThree.ID})

			// Then: Log in again will resync the orgs to their updated
			// claims.
			runner.Login(t, jwt.MapClaims{
				"email":        "alice@coder.com",
				"organization": []string{"second"},
			})
			runner.AssertOrganizations(t, "alice", true, []uuid.UUID{orgTwo.ID})
		})

		t.Run("MultiOrgWithoutDefault", func(t *testing.T) {
			t.Parallel()

			second := uuid.New()
			third := uuid.New()

			// Given: 4 organizations: default, second, third, and fourth
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.OrganizationAssignDefault = false
					dv.OIDC.OrganizationField = "organization"
					dv.OIDC.OrganizationMapping = serpent.Struct[map[string][]uuid.UUID]{
						Value: map[string][]uuid.UUID{
							"second": {second},
							"third":  {third},
						},
					}
				},
			})
			dbgen.Organization(t, runner.API.Database, database.Organization{
				ID: second,
			})
			dbgen.Organization(t, runner.API.Database, database.Organization{
				ID: third,
			})
			fourth := dbgen.Organization(t, runner.API.Database, database.Organization{})

			ctx := testutil.Context(t, testutil.WaitMedium)
			claims := jwt.MapClaims{
				"email":        "alice@coder.com",
				"organization": []string{"second", "third"},
			}

			// Then: a new user logs in with claims "second" and "third", they
			// should belong to [ second, third].
			userClient, resp := runner.Login(t, claims)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertOrganizations(t, "alice", false, []uuid.UUID{second, third})
			user, err := userClient.User(ctx, wirtualsdk.Me)
			require.NoError(t, err)

			// When: they are manually added to the fourth organization, a new sync
			// should remove them.
			dbgen.OrganizationMember(t, runner.API.Database, database.OrganizationMember{
				UserID:         user.ID,
				OrganizationID: fourth.ID,
			})
			runner.AssertOrganizations(t, "alice", false, []uuid.UUID{second, third, fourth.ID})

			// Then: Log in again will resync the orgs to their updated
			// claims.
			runner.Login(t, jwt.MapClaims{
				"email":        "alice@coder.com",
				"organization": []string{"third"},
			})
			runner.AssertOrganizations(t, "alice", false, []uuid.UUID{third})
		})
	})

	t.Run("RoleSync", func(t *testing.T) {
		t.Parallel()

		// NoRoles is the "control group". It has claims with 0 roles
		// assigned, and asserts that the user has no roles.
		t.Run("NoRoles", func(t *testing.T) {
			t.Parallel()

			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.UserRoleField = "roles"
				},
			})

			claims := jwt.MapClaims{
				"email": "alice@coder.com",
			}
			// Login a new client that signs up
			client, resp := runner.Login(t, claims)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			// User should be in 0 groups.
			runner.AssertRoles(t, "alice", []string{})
			// Force a refresh, and assert nothing has changes
			runner.ForceRefresh(t, client, claims)
			runner.AssertRoles(t, "alice", []string{})

			runner.AssertOrganizations(t, "alice", true, nil)
		})

		// Some IDPs (ADFS) send the "string" type vs "[]string" if only
		// 1 role exists.
		t.Run("SingleRoleString", func(t *testing.T) {
			t.Parallel()

			const oidcRoleName = "TemplateAuthor"
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.UserRoleField = "roles"
					dv.OIDC.UserRoleMapping = serpent.Struct[map[string][]string]{
						Value: map[string][]string{
							oidcRoleName: {rbac.RoleTemplateAdmin().String()},
						},
					}
				},
			})

			// User starts with the owner role
			_, resp := runner.Login(t, jwt.MapClaims{
				"email": "alice@coder.com",
				// This is sent as a **string** intentionally instead
				// of an array.
				"roles": oidcRoleName,
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertRoles(t, "alice", []string{rbac.RoleTemplateAdmin().String()})
			runner.AssertOrganizations(t, "alice", true, nil)
		})

		// A user has some roles, then on an oauth refresh will lose said
		// roles from an updated claim.
		t.Run("NewUserAndRemoveRolesOnRefresh", func(t *testing.T) {
			// TODO: Implement new feature to update roles/groups on OIDC
			// refresh tokens. https://github.com/coder/coder/issues/9312
			t.Skip("Refreshing tokens does not update roles :(")
			t.Parallel()

			const oidcRoleName = "TemplateAuthor"
			runner := setupOIDCTest(t, oidcTestConfig{
				Userinfo: jwt.MapClaims{oidcRoleName: []string{rbac.RoleTemplateAdmin().String(), rbac.RoleUserAdmin().String()}},
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.UserRoleField = "roles"
					dv.OIDC.UserRoleMapping = serpent.Struct[map[string][]string]{
						Value: map[string][]string{
							oidcRoleName: {rbac.RoleTemplateAdmin().String(), rbac.RoleUserAdmin().String()},
						},
					}
				},
			})

			// User starts with the owner role
			client, resp := runner.Login(t, jwt.MapClaims{
				"email": "alice@coder.com",
				"roles": []string{"random", oidcRoleName, rbac.RoleOwner().String()},
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertRoles(t, "alice", []string{rbac.RoleTemplateAdmin().String(), rbac.RoleUserAdmin().String(), rbac.RoleOwner().String()})

			// Now refresh the oauth, and check the roles are removed.
			// Force a refresh, and assert nothing has changes
			runner.ForceRefresh(t, client, jwt.MapClaims{
				"email": "alice@coder.com",
				"roles": []string{"random"},
			})
			runner.AssertRoles(t, "alice", []string{})
			runner.AssertOrganizations(t, "alice", true, nil)
		})

		// A user has some roles, then on another oauth login will lose said
		// roles from an updated claim.
		t.Run("NewUserAndRemoveRolesOnReAuth", func(t *testing.T) {
			t.Parallel()

			const oidcRoleName = "TemplateAuthor"
			runner := setupOIDCTest(t, oidcTestConfig{
				Userinfo: jwt.MapClaims{oidcRoleName: []string{rbac.RoleTemplateAdmin().String(), rbac.RoleUserAdmin().String()}},
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.UserRoleField = "roles"
					dv.OIDC.UserRoleMapping = serpent.Struct[map[string][]string]{
						Value: map[string][]string{
							oidcRoleName: {rbac.RoleTemplateAdmin().String(), rbac.RoleUserAdmin().String()},
						},
					}
				},
			})

			// User starts with the owner role
			_, resp := runner.Login(t, jwt.MapClaims{
				"email": "alice@coder.com",
				"roles": []string{"random", oidcRoleName, rbac.RoleOwner().String()},
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertRoles(t, "alice", []string{rbac.RoleTemplateAdmin().String(), rbac.RoleUserAdmin().String(), rbac.RoleOwner().String()})

			// Now login with oauth again, and check the roles are removed.
			_, resp = runner.Login(t, jwt.MapClaims{
				"email": "alice@coder.com",
				"roles": []string{"random"},
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)

			runner.AssertRoles(t, "alice", []string{})
			runner.AssertOrganizations(t, "alice", true, nil)
		})

		// All manual role updates should fail when role sync is enabled.
		t.Run("BlockAssignRoles", func(t *testing.T) {
			t.Parallel()

			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.UserRoleField = "roles"
				},
			})

			_, resp := runner.Login(t, jwt.MapClaims{
				"email": "alice@coder.com",
				"roles": []string{},
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			// Try to manually update user roles, even though controlled by oidc
			// role sync.
			ctx := testutil.Context(t, testutil.WaitShort)
			_, err := runner.AdminClient.UpdateUserRoles(ctx, "alice", wirtualsdk.UpdateRoles{
				Roles: []string{
					rbac.RoleTemplateAdmin().String(),
				},
			})
			require.Error(t, err)
			require.ErrorContains(t, err, "Cannot modify roles for OIDC users when role sync is enabled.")
		})
	})

	t.Run("Groups", func(t *testing.T) {
		t.Parallel()

		// Assigns does a simple test of assigning a user to a group based
		// on the oidc claims.
		t.Run("Assigns", func(t *testing.T) {
			t.Parallel()

			const groupClaim = "custom-groups"
			const groupName = "bingbong"
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.GroupField = groupClaim
				},
			})

			ctx := testutil.Context(t, testutil.WaitShort)
			group, err := runner.AdminClient.CreateGroup(ctx, runner.AdminUser.OrganizationIDs[0], wirtualsdk.CreateGroupRequest{
				Name: groupName,
			})
			require.NoError(t, err)
			require.Len(t, group.Members, 0)

			_, resp := runner.Login(t, jwt.MapClaims{
				"email":    "alice@coder.com",
				groupClaim: []string{groupName},
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertGroups(t, "alice", []string{groupName})
			runner.AssertOrganizations(t, "alice", true, nil)
		})

		// Tests the group mapping feature.
		t.Run("AssignsMapped", func(t *testing.T) {
			t.Parallel()

			const groupClaim = "custom-groups"

			const oidcGroupName = "pingpong"
			const coderGroupName = "bingbong"
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.GroupField = groupClaim
					dv.OIDC.GroupMapping = serpent.Struct[map[string]string]{Value: map[string]string{oidcGroupName: coderGroupName}}
				},
			})

			ctx := testutil.Context(t, testutil.WaitShort)
			group, err := runner.AdminClient.CreateGroup(ctx, runner.AdminUser.OrganizationIDs[0], wirtualsdk.CreateGroupRequest{
				Name: coderGroupName,
			})
			require.NoError(t, err)
			require.Len(t, group.Members, 0)

			_, resp := runner.Login(t, jwt.MapClaims{
				"email":    "alice@coder.com",
				groupClaim: []string{oidcGroupName},
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertGroups(t, "alice", []string{coderGroupName})
			runner.AssertOrganizations(t, "alice", true, nil)
		})

		// User is in a group, then on an oauth refresh will lose said
		// group.
		t.Run("AddThenRemoveOnRefresh", func(t *testing.T) {
			t.Parallel()

			// TODO: Implement new feature to update roles/groups on OIDC
			// refresh tokens. https://github.com/coder/coder/issues/9312
			t.Skip("Refreshing tokens does not update groups :(")

			const groupClaim = "custom-groups"
			const groupName = "bingbong"
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.GroupField = groupClaim
				},
			})

			ctx := testutil.Context(t, testutil.WaitShort)
			group, err := runner.AdminClient.CreateGroup(ctx, runner.AdminUser.OrganizationIDs[0], wirtualsdk.CreateGroupRequest{
				Name: groupName,
			})
			require.NoError(t, err)
			require.Len(t, group.Members, 0)

			client, resp := runner.Login(t, jwt.MapClaims{
				"email":    "alice@coder.com",
				groupClaim: []string{groupName},
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertGroups(t, "alice", []string{groupName})

			// Refresh without the group claim
			runner.ForceRefresh(t, client, jwt.MapClaims{
				"email": "alice@coder.com",
			})
			runner.AssertGroups(t, "alice", []string{})
			runner.AssertOrganizations(t, "alice", true, nil)
		})

		t.Run("AddThenRemoveOnReAuth", func(t *testing.T) {
			t.Parallel()

			const groupClaim = "custom-groups"
			const groupName = "bingbong"
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.GroupField = groupClaim
				},
			})

			ctx := testutil.Context(t, testutil.WaitShort)
			group, err := runner.AdminClient.CreateGroup(ctx, runner.AdminUser.OrganizationIDs[0], wirtualsdk.CreateGroupRequest{
				Name: groupName,
			})
			require.NoError(t, err)
			require.Len(t, group.Members, 0)

			_, resp := runner.Login(t, jwt.MapClaims{
				"email":    "alice@coder.com",
				groupClaim: []string{groupName},
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertGroups(t, "alice", []string{groupName})

			// Refresh without the group claim
			_, resp = runner.Login(t, jwt.MapClaims{
				"email": "alice@coder.com",
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertGroups(t, "alice", []string{})
			runner.AssertOrganizations(t, "alice", true, nil)
		})

		// Updating groups where the claimed group does not exist.
		t.Run("NoneMatch", func(t *testing.T) {
			t.Parallel()

			const groupClaim = "custom-groups"
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.GroupField = groupClaim
				},
			})

			_, resp := runner.Login(t, jwt.MapClaims{
				"email":    "alice@coder.com",
				groupClaim: []string{"not-exists"},
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertGroups(t, "alice", []string{})
		})

		// Updating groups where the claimed group does not exist creates
		// the group.
		t.Run("AutoCreate", func(t *testing.T) {
			t.Parallel()

			const groupClaim = "custom-groups"
			const groupName = "make-me"
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.GroupField = groupClaim
					dv.OIDC.GroupAutoCreate = true
				},
			})

			_, resp := runner.Login(t, jwt.MapClaims{
				"email":    "alice@coder.com",
				groupClaim: []string{groupName},
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertGroups(t, "alice", []string{groupName})
		})

		// Some IDPs (ADFS) send the "string" type vs "[]string" if only
		// 1 group exists.
		t.Run("SingleRoleGroup", func(t *testing.T) {
			t.Parallel()

			const groupClaim = "custom-groups"
			const groupName = "bingbong"
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.GroupField = groupClaim
					dv.OIDC.GroupAutoCreate = true
				},
			})

			// User starts with the owner role
			_, resp := runner.Login(t, jwt.MapClaims{
				"email": "alice@coder.com",
				// This is sent as a **string** intentionally instead
				// of an array.
				groupClaim: groupName,
			})
			require.Equal(t, http.StatusOK, resp.StatusCode)
			runner.AssertGroups(t, "alice", []string{groupName})
		})

		t.Run("GroupAllowList", func(t *testing.T) {
			t.Parallel()

			const groupClaim = "custom-groups"
			const allowedGroup = "foo"
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.GroupField = groupClaim
					dv.OIDC.GroupAllowList = []string{allowedGroup}
				},
			})

			// Test forbidden
			_, resp := runner.AttemptLogin(t, jwt.MapClaims{
				"email":    "alice@coder.com",
				groupClaim: []string{"not-allowed"},
			})
			require.Equal(t, http.StatusForbidden, resp.StatusCode)

			// Test allowed
			client, _ := runner.Login(t, jwt.MapClaims{
				"email":    "alice@coder.com",
				groupClaim: []string{allowedGroup},
			})

			ctx := testutil.Context(t, testutil.WaitShort)
			_, err := client.User(ctx, wirtualsdk.Me)
			require.NoError(t, err)
		})
	})

	t.Run("Refresh", func(t *testing.T) {
		t.Run("RefreshTokensMultiple", func(t *testing.T) {
			t.Parallel()

			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.UserRoleField = "roles"
				},
			})

			claims := jwt.MapClaims{
				"email": "alice@coder.com",
			}
			// Login a new client that signs up
			client, resp := runner.Login(t, claims)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			// Refresh multiple times.
			for i := 0; i < 3; i++ {
				runner.ForceRefresh(t, client, claims)
			}
		})

		t.Run("FailedRefresh", func(t *testing.T) {
			t.Parallel()

			runner := setupOIDCTest(t, oidcTestConfig{
				FakeOpts: []oidctest.FakeIDPOpt{
					oidctest.WithRefresh(func(_ string) error {
						// Always "expired" refresh token.
						return xerrors.New("refresh token is expired")
					}),
				},
				Config: func(cfg *wirtuald.OIDCConfig) {
					cfg.AllowSignups = true
				},
			})

			claims := jwt.MapClaims{
				"email": "alice@coder.com",
			}
			// Login a new client that signs up
			client, resp := runner.Login(t, claims)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			// Expire the token, cause a refresh
			runner.ExpireOauthToken(t, client)

			// This should fail because the oauth token refresh should fail.
			_, err := client.User(context.Background(), wirtualsdk.Me)
			require.Error(t, err)
			var apiError *wirtualsdk.Error
			require.ErrorAs(t, err, &apiError)
			require.Equal(t, http.StatusUnauthorized, apiError.StatusCode())
			require.ErrorContains(t, apiError, "refresh")
		})
	})
}

// nolint:bodyclose
func TestGroupSync(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		modCfg func(cfg *wirtuald.OIDCConfig)
		modDV  func(dv *wirtualsdk.DeploymentValues)
		// initialOrgGroups is initial groups in the org
		initialOrgGroups []string
		// initialUserGroups is initial groups for the user
		initialUserGroups []string
		// expectedUserGroups is expected groups for the user
		expectedUserGroups []string
		// expectedOrgGroups is expected all groups on the system
		expectedOrgGroups []string
		claims            jwt.MapClaims
	}{
		{
			name: "NoGroups",
			modCfg: func(cfg *wirtuald.OIDCConfig) {
			},
			initialOrgGroups:   []string{},
			expectedUserGroups: []string{},
			expectedOrgGroups:  []string{},
			claims:             jwt.MapClaims{},
		},
		{
			name: "GroupSyncDisabled",
			modDV: func(dv *wirtualsdk.DeploymentValues) {
				// Disable group sync
				dv.OIDC.GroupField = ""
				dv.OIDC.GroupRegexFilter = serpent.Regexp(*regexp.MustCompile(".*"))
			},
			initialOrgGroups:   []string{"a", "b", "c", "d"},
			initialUserGroups:  []string{"b", "c", "d"},
			expectedUserGroups: []string{"b", "c", "d"},
			expectedOrgGroups:  []string{"a", "b", "c", "d"},
			claims:             jwt.MapClaims{},
		},
		{
			// From a,c,b -> b,c,d
			name: "ChangeUserGroups",
			modDV: func(dv *wirtualsdk.DeploymentValues) {
				dv.OIDC.GroupMapping = serpent.Struct[map[string]string]{Value: map[string]string{"D": "d"}}
			},
			initialOrgGroups:   []string{"a", "b", "c", "d"},
			initialUserGroups:  []string{"a", "b", "c"},
			expectedUserGroups: []string{"b", "c", "d"},
			expectedOrgGroups:  []string{"a", "b", "c", "d"},
			claims: jwt.MapClaims{
				// D -> d mapped
				"groups": []string{"b", "c", "D"},
			},
		},
		{
			// From a,c,b -> []
			name: "RemoveAllGroups",
			modDV: func(dv *wirtualsdk.DeploymentValues) {
				dv.OIDC.GroupRegexFilter = serpent.Regexp(*regexp.MustCompile(".*"))
			},
			initialOrgGroups:   []string{"a", "b", "c", "d"},
			initialUserGroups:  []string{"a", "b", "c"},
			expectedUserGroups: []string{},
			expectedOrgGroups:  []string{"a", "b", "c", "d"},
			claims:             jwt.MapClaims{
				// No claim == no groups
			},
		},
		{
			// From a,c,b -> b,c,d,e,f
			name: "CreateMissingGroups",
			modDV: func(dv *wirtualsdk.DeploymentValues) {
				dv.OIDC.GroupAutoCreate = true
			},
			initialOrgGroups:   []string{"a", "b", "c", "d"},
			initialUserGroups:  []string{"a", "b", "c"},
			expectedUserGroups: []string{"b", "c", "d", "e", "f"},
			expectedOrgGroups:  []string{"a", "b", "c", "d", "e", "f"},
			claims: jwt.MapClaims{
				"groups": []string{"b", "c", "d", "e", "f"},
			},
		},
		{
			// From a,c,b -> b,c,d,e,f
			name: "CreateMissingGroupsFilter",
			modDV: func(dv *wirtualsdk.DeploymentValues) {
				dv.OIDC.GroupAutoCreate = true
				// Only single letter groups
				dv.OIDC.GroupRegexFilter = serpent.Regexp(*regexp.MustCompile("^[a-z]$"))
				dv.OIDC.GroupMapping = serpent.Struct[map[string]string]{Value: map[string]string{"zebra": "z"}}
			},
			initialOrgGroups:   []string{"a", "b", "c", "d"},
			initialUserGroups:  []string{"a", "b", "c"},
			expectedUserGroups: []string{"b", "c", "d", "e", "f", "z"},
			expectedOrgGroups:  []string{"a", "b", "c", "d", "e", "f", "z"},
			claims: jwt.MapClaims{
				"groups": []string{
					"b", "c", "d", "e", "f",
					// These groups are ignored
					"excess", "ignore", "dumb", "foobar", "zebra",
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := setupOIDCTest(t, oidcTestConfig{
				Config: func(cfg *wirtuald.OIDCConfig) {
					if tc.modCfg != nil {
						tc.modCfg(cfg)
					}
				},
				DeploymentValues: func(dv *wirtualsdk.DeploymentValues) {
					dv.OIDC.GroupField = "groups"
					if tc.modDV != nil {
						tc.modDV(dv)
					}
				},
			})

			// Setup
			ctx := testutil.Context(t, testutil.WaitLong)
			org := runner.AdminUser.OrganizationIDs[0]

			initialGroups := make(map[string]wirtualsdk.Group)
			for _, group := range tc.initialOrgGroups {
				newGroup, err := runner.AdminClient.CreateGroup(ctx, org, wirtualsdk.CreateGroupRequest{
					Name: group,
				})
				require.NoError(t, err)
				require.Len(t, newGroup.Members, 0)
				initialGroups[group] = newGroup
			}

			// Create the user and add them to their initial groups
			_, user := wirtualdtest.CreateAnotherUser(t, runner.AdminClient, org)
			for _, group := range tc.initialUserGroups {
				_, err := runner.AdminClient.PatchGroup(ctx, initialGroups[group].ID, wirtualsdk.PatchGroupRequest{
					AddUsers: []string{user.ID.String()},
				})
				require.NoError(t, err)
			}

			// nolint:gocritic
			_, err := runner.API.Database.UpdateUserLoginType(dbauthz.AsSystemRestricted(ctx), database.UpdateUserLoginTypeParams{
				NewLoginType: database.LoginTypeOIDC,
				UserID:       user.ID,
			})
			require.NoError(t, err, "user must be oidc type")

			// Log in the new user
			tc.claims["email"] = user.Email
			_, resp := runner.Login(t, tc.claims)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			// Check group sources
			orgGroups, err := runner.AdminClient.GroupsByOrganization(ctx, org)
			require.NoError(t, err)

			for _, group := range orgGroups {
				if slice.Contains(tc.initialOrgGroups, group.Name) || group.IsEveryone() {
					require.Equal(t, group.Source, wirtualsdk.GroupSourceUser)
				} else {
					require.Equal(t, group.Source, wirtualsdk.GroupSourceOIDC)
				}
			}

			orgGroupsMap := make(map[string]struct{})
			for _, group := range orgGroups {
				orgGroupsMap[group.Name] = struct{}{}
			}

			for _, expected := range tc.expectedOrgGroups {
				if _, ok := orgGroupsMap[expected]; !ok {
					t.Errorf("expected group %s not found", expected)
				}
				delete(orgGroupsMap, expected)
			}
			delete(orgGroupsMap, database.EveryoneGroup)
			require.Empty(t, orgGroupsMap, "unexpected groups found")

			expectedUserGroups := make(map[string]struct{})
			for _, group := range tc.expectedUserGroups {
				expectedUserGroups[group] = struct{}{}
			}

			for _, group := range orgGroups {
				userInGroup := slice.ContainsCompare(group.Members, wirtualsdk.ReducedUser{Email: user.Email}, func(a, b wirtualsdk.ReducedUser) bool {
					return a.Email == b.Email
				})
				if group.IsEveryone() {
					require.True(t, userInGroup, "user cannot be removed from 'Everyone' group")
				} else if _, ok := expectedUserGroups[group.Name]; ok {
					require.Truef(t, userInGroup, "user should be in group %s", group.Name)
				} else {
					require.Falsef(t, userInGroup, "user should not be in group %s", group.Name)
				}
			}
		})
	}
}

func TestEnterpriseUserLogin(t *testing.T) {
	t.Parallel()

	// Login to a user with a custom organization role set.
	t.Run("CustomRole", func(t *testing.T) {
		t.Parallel()
		dv := wirtualdtest.DeploymentValues(t)
		ownerClient, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				DeploymentValues: dv,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureCustomRoles: 1,
				},
			},
		})

		ctx := testutil.Context(t, testutil.WaitShort)
		//nolint:gocritic // owner required
		customRole, err := ownerClient.CreateOrganizationRole(ctx, wirtualsdk.Role{
			Name:                    "custom-role",
			OrganizationID:          owner.OrganizationID.String(),
			OrganizationPermissions: []wirtualsdk.Permission{},
		})
		require.NoError(t, err, "create custom role")

		anotherClient, anotherUser := wirtualdtest.CreateAnotherUserMutators(t, ownerClient, owner.OrganizationID, []rbac.RoleIdentifier{
			{
				Name:           customRole.Name,
				OrganizationID: owner.OrganizationID,
			},
		}, func(r *wirtualsdk.CreateUserRequestWithOrgs) {
			r.Password = "SomeSecurePassword!"
			r.UserLoginType = wirtualsdk.LoginTypePassword
		})

		_, err = anotherClient.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
			Email:    anotherUser.Email,
			Password: "SomeSecurePassword!",
		})
		require.NoError(t, err)
	})

	// Login to a user with a custom organization role that no longer exists
	t.Run("DeletedRole", func(t *testing.T) {
		t.Parallel()

		// The dbauthz layer protects against deleted roles. So use the underlying
		// database directly to corrupt it.
		rawDB, pubsub := dbtestutil.NewDB(t)

		ownerClient, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				Database: rawDB,
				Pubsub:   pubsub,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureCustomRoles: 1,
				},
			},
		})

		anotherClient, anotherUser := wirtualdtest.CreateAnotherUserMutators(t, ownerClient, owner.OrganizationID, nil, func(r *wirtualsdk.CreateUserRequestWithOrgs) {
			r.Password = "SomeSecurePassword!"
			r.UserLoginType = wirtualsdk.LoginTypePassword
		})

		ctx := testutil.Context(t, testutil.WaitShort)
		_, err := rawDB.UpdateMemberRoles(ctx, database.UpdateMemberRolesParams{
			GrantedRoles: []string{"not-exists"},
			UserID:       anotherUser.ID,
			OrgID:        owner.OrganizationID,
		})
		require.NoError(t, err, "assign not-exists role")

		_, err = anotherClient.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
			Email:    anotherUser.Email,
			Password: "SomeSecurePassword!",
		})
		require.NoError(t, err)
	})
}

// oidcTestRunner is just a helper to setup and run oidc tests.
// An actual Wirtuald instance is used to run the tests.
type oidcTestRunner struct {
	AdminClient *wirtualsdk.Client
	AdminUser   wirtualsdk.User
	API         *wirtualden.API

	// Login will call the OIDC flow with an unauthenticated client.
	// The IDP will return the idToken claims.
	Login        func(t *testing.T, idToken jwt.MapClaims) (*wirtualsdk.Client, *http.Response)
	AttemptLogin func(t *testing.T, idToken jwt.MapClaims) (*wirtualsdk.Client, *http.Response)
	// ForceRefresh will use an authenticated wirtualsdk.Client, and force their
	// OIDC token to be expired and require a refresh. The refresh will use the claims provided.
	// It just calls the /users/me endpoint to trigger the refresh.
	ForceRefresh     func(t *testing.T, client *wirtualsdk.Client, idToken jwt.MapClaims)
	ExpireOauthToken func(t *testing.T, client *wirtualsdk.Client)
}

type oidcTestConfig struct {
	Userinfo jwt.MapClaims

	// Config allows modifying the Wirtuald OIDC configuration.
	Config           func(cfg *wirtuald.OIDCConfig)
	DeploymentValues func(dv *wirtualsdk.DeploymentValues)
	FakeOpts         []oidctest.FakeIDPOpt
}

func (r *oidcTestRunner) AssertOrganizations(t *testing.T, userIdent string, includeDefault bool, expected []uuid.UUID) {
	t.Helper()

	ctx := testutil.Context(t, testutil.WaitMedium)
	userOrgs, err := r.AdminClient.OrganizationsByUser(ctx, userIdent)
	require.NoError(t, err)

	cpy := make([]uuid.UUID, 0, len(expected))
	cpy = append(cpy, expected...)
	hasDefault := false
	userOrgIDs := db2sdk.List(userOrgs, func(o wirtualsdk.Organization) uuid.UUID {
		if o.IsDefault {
			hasDefault = true
			cpy = append(cpy, o.ID)
		}
		return o.ID
	})

	require.Equal(t, includeDefault, hasDefault, "expected default org")
	require.ElementsMatch(t, cpy, userOrgIDs, "expected orgs")
}

func (r *oidcTestRunner) AssertRoles(t *testing.T, userIdent string, roles []string) {
	t.Helper()

	ctx := testutil.Context(t, testutil.WaitMedium)
	user, err := r.AdminClient.User(ctx, userIdent)
	require.NoError(t, err)

	roleNames := []string{}
	for _, role := range user.Roles {
		roleNames = append(roleNames, role.Name)
	}
	require.ElementsMatch(t, roles, roleNames, "expected roles")
}

func (r *oidcTestRunner) AssertGroups(t *testing.T, userIdent string, groups []string) {
	t.Helper()

	if !slice.Contains(groups, database.EveryoneGroup) {
		var cpy []string
		cpy = append(cpy, groups...)
		// always include everyone group
		cpy = append(cpy, database.EveryoneGroup)
		groups = cpy
	}
	ctx := testutil.Context(t, testutil.WaitMedium)
	user, err := r.AdminClient.User(ctx, userIdent)
	require.NoError(t, err)

	allGroups, err := r.AdminClient.GroupsByOrganization(ctx, user.OrganizationIDs[0])
	require.NoError(t, err)

	userInGroups := []string{}
	for _, g := range allGroups {
		for _, mem := range g.Members {
			if mem.ID == user.ID {
				userInGroups = append(userInGroups, g.Name)
			}
		}
	}

	require.ElementsMatch(t, groups, userInGroups, "expected groups")
}

func setupOIDCTest(t *testing.T, settings oidcTestConfig) *oidcTestRunner {
	t.Helper()

	fake := oidctest.NewFakeIDP(t,
		append([]oidctest.FakeIDPOpt{
			oidctest.WithStaticUserInfo(settings.Userinfo),
			oidctest.WithLogging(t, nil),
			// Run fake IDP on a real webserver
			oidctest.WithServing(),
		}, settings.FakeOpts...)...,
	)

	ctx := testutil.Context(t, testutil.WaitMedium)
	cfg := fake.OIDCConfig(t, nil, settings.Config)
	dv := wirtualdtest.DeploymentValues(t)
	if settings.DeploymentValues != nil {
		settings.DeploymentValues(dv)
	}
	owner, _, api, _ := wirtualdenttest.NewWithAPI(t, &wirtualdenttest.Options{
		Options: &wirtualdtest.Options{
			OIDCConfig:       cfg,
			DeploymentValues: dv,
		},
		LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureUserRoleManagement:    1,
				wirtualsdk.FeatureTemplateRBAC:          1,
				wirtualsdk.FeatureMultipleOrganizations: 1,
			},
		},
	})
	admin, err := owner.User(ctx, "me")
	require.NoError(t, err)

	helper := oidctest.NewLoginHelper(owner, fake)

	return &oidcTestRunner{
		AdminClient:  owner,
		AdminUser:    admin,
		API:          api,
		Login:        helper.Login,
		AttemptLogin: helper.AttemptLogin,
		ForceRefresh: func(t *testing.T, client *wirtualsdk.Client, idToken jwt.MapClaims) {
			helper.ForceRefresh(t, api.Database, client, idToken)
		},
		ExpireOauthToken: func(t *testing.T, client *wirtualsdk.Client) {
			helper.ExpireOauthToken(t, api.Database, client)
		},
	}
}
