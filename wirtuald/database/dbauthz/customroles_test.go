package dbauthz_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"cdr.dev/slog"
	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/database/db2sdk"
	"github.com/coder/coder/v2/wirtuald/database/dbauthz"
	"github.com/coder/coder/v2/wirtuald/database/dbmem"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtuald/rbac/policy"
	"github.com/coder/coder/v2/wirtuald/wi
	"github.com/coder/coder/v2/wirtualsdk"
)

// TestInsertCustomRoles verifies creating custom roles cannot escalate permissions.
func TestInsertCustomRoles(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	subjectFromRoles := func(roles rbac.ExpandableRoles) rbac.Subject {
		return rbac.Subject{
			FriendlyName: "Test user",
			ID:           userID.String(),
			Roles:        roles,
			Groups:       nil,
			Scope:        rbac.ScopeAll,
		}
	}

	canAssignRole := rbac.Role{
		Identifier:  rbac.RoleIdentifier{Name: "can-assign"},
		DisplayName: "",
		Site: rbac.Permissions(map[string][]policy.Action{
			rbac.ResourceAssignRole.Type: {policy.ActionRead, policy.ActionCreate},
		}),
	}

	merge := func(u ...interface{}) rbac.Roles {
		all := make([]rbac.Role, 0)
		for _, v := range u {
			v := v
			switch t := v.(type) {
			case rbac.Role:
				all = append(all, t)
			case rbac.ExpandableRoles:
				all = append(all, must(t.Expand())...)
			case rbac.RoleIdentifier:
				all = append(all, must(rbac.RoleByName(t)))
			default:
				panic("unknown type")
			}
		}

		return all
	}

	orgID := uuid.NullUUID{
		UUID:  uuid.New(),
		Valid: true,
	}
	testCases := []struct {
		name string

		subject rbac.ExpandableRoles

		// Perms to create on new custom role
		organizationID uuid.NullUUID
		site           []wirtualsdk.Permission
		org            []wirtualsdk.Permission
		user           []wirtualsdk.Permission
		errorContains  string
	}{
		{
			// No roles, so no assign role
			name:          "no-roles",
			subject:       rbac.RoleIdentifiers{},
			errorContains: "forbidden",
		},
		{
			// This works because the new role has 0 perms
			name:    "empty",
			subject: merge(canAssignRole),
		},
		{
			name:           "mixed-scopes",
			subject:        merge(canAssignRole, rbac.RoleOwner()),
			organizationID: orgID,
			site: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
			org: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
			errorContains: "organization roles specify site or user permissions",
		},
		{
			name:    "invalid-action",
			subject: merge(canAssignRole, rbac.RoleOwner()),
			site: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				// Action does not go with resource
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionViewInsights},
			}),
			errorContains: "invalid action",
		},
		{
			name:    "invalid-resource",
			subject: merge(canAssignRole, rbac.RoleOwner()),
			site: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				"foobar": {wirtualsdk.ActionViewInsights},
			}),
			errorContains: "invalid resource",
		},
		{
			// Not allowing these at this time.
			name:    "negative-permission",
			subject: merge(canAssignRole, rbac.RoleOwner()),
			site: []wirtualsdk.Permission{
				{
					Negate:       true,
					ResourceType: wirtualsdk.ResourceWorkspace,
					Action:       wirtualsdk.ActionRead,
				},
			},
			errorContains: "no negative permissions",
		},
		{
			name:    "wildcard", // not allowed
			subject: merge(canAssignRole, rbac.RoleOwner()),
			site: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {"*"},
			}),
			errorContains: "no wildcard symbols",
		},
		// escalation checks
		{
			name:    "read-workspace-escalation",
			subject: merge(canAssignRole),
			site: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
			errorContains: "not allowed to grant this permission",
		},
		{
			name: "read-workspace-outside-org",
			organizationID: uuid.NullUUID{
				UUID:  uuid.New(),
				Valid: true,
			},
			subject: merge(canAssignRole, rbac.ScopedRoleOrgAdmin(orgID.UUID)),
			org: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
			errorContains: "forbidden",
		},
		{
			name: "user-escalation",
			// These roles do not grant user perms
			subject: merge(canAssignRole, rbac.ScopedRoleOrgAdmin(orgID.UUID)),
			user: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
			errorContains: "not allowed to grant this permission",
		},
		{
			name:    "template-admin-escalation",
			subject: merge(canAssignRole, rbac.RoleTemplateAdmin()),
			site: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace:        {wirtualsdk.ActionRead},   // ok!
				wirtualsdk.ResourceDeploymentConfig: {wirtualsdk.ActionUpdate}, // not ok!
			}),
			user: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead}, // ok!
			}),
			errorContains: "deployment_config",
		},
		// ok!
		{
			name:    "read-workspace-template-admin",
			subject: merge(canAssignRole, rbac.RoleTemplateAdmin()),
			site: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
		},
		{
			name:           "read-workspace-in-org",
			subject:        merge(canAssignRole, rbac.ScopedRoleOrgAdmin(orgID.UUID)),
			organizationID: orgID,
			org: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
		},
		{
			name: "user-perms",
			// This is weird, but is ok
			subject: merge(canAssignRole, rbac.RoleMember()),
			user: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
		},
		{
			name:    "site+user-perms",
			subject: merge(canAssignRole, rbac.RoleMember(), rbac.RoleTemplateAdmin()),
			site: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
			user: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := dbmem.New()
			rec := &wirtualdtest.RecordingAuthorizer{
				Wrapped: rbac.NewAuthorizer(prometheus.NewRegistry()),
			}
			az := dbauthz.New(db, rec, slog.Make(), wirtualdtest.AccessControlStorePointer())

			subject := subjectFromRoles(tc.subject)
			ctx := testutil.Context(t, testutil.WaitMedium)
			ctx = dbauthz.As(ctx, subject)

			_, err := az.InsertCustomRole(ctx, database.InsertCustomRoleParams{
				Name:            "test-role",
				DisplayName:     "",
				OrganizationID:  tc.organizationID,
				SitePermissions: db2sdk.List(tc.site, convertSDKPerm),
				OrgPermissions:  db2sdk.List(tc.org, convertSDKPerm),
				UserPermissions: db2sdk.List(tc.user, convertSDKPerm),
			})
			if tc.errorContains != "" {
				require.ErrorContains(t, err, tc.errorContains)
			} else {
				require.NoError(t, err)

				// Verify the role is fetched with the lookup filter.
				roles, err := az.CustomRoles(ctx, database.CustomRolesParams{
					LookupRoles: []database.NameOrganizationPair{
						{
							Name:           "test-role",
							OrganizationID: tc.organizationID.UUID,
						},
					},
					ExcludeOrgRoles: false,
					OrganizationID:  uuid.UUID{},
				})
				require.NoError(t, err)
				require.Len(t, roles, 1)
			}
		})
	}
}

func convertSDKPerm(perm wirtualsdk.Permission) database.CustomRolePermission {
	return database.CustomRolePermission{
		Negate:       perm.Negate,
		ResourceType: string(perm.ResourceType),
		Action:       policy.Action(perm.Action),
	}
}
