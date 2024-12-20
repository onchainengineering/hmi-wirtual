package wirtuald

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"golang.org/x/xerrors"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/audit"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/db2sdk"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbauthz"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpapi"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpmw"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac/policy"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

// @Summary Get template available acl users/groups
// @ID get-template-available-acl-usersgroups
// @Security CoderSessionToken
// @Produce json
// @Tags Enterprise
// @Param template path string true "Template ID" format(uuid)
// @Success 200 {array} wirtualsdk.ACLAvailable
// @Router /templates/{template}/acl/available [get]
func (api *API) templateAvailablePermissions(rw http.ResponseWriter, r *http.Request) {
	var (
		ctx      = r.Context()
		template = httpmw.TemplateParam(r)
	)

	// Requires update permission on the template to list all avail users/groups
	// for assignment.
	if !api.Authorize(r, policy.ActionUpdate, template) {
		httpapi.ResourceNotFound(rw)
		return
	}

	// We have to use the system restricted context here because the caller
	// might not have permission to read all users.
	// nolint:gocritic
	users, _, ok := api.AGPL.GetUsers(rw, r.WithContext(dbauthz.AsSystemRestricted(ctx)))
	if !ok {
		return
	}

	// Perm check is the template update check.
	// nolint:gocritic
	groups, err := api.Database.GetGroups(dbauthz.AsSystemRestricted(ctx), database.GetGroupsParams{
		OrganizationID: template.OrganizationID,
	})
	if err != nil {
		httpapi.InternalServerError(rw, err)
		return
	}

	sdkGroups := make([]wirtualsdk.Group, 0, len(groups))
	for _, group := range groups {
		// nolint:gocritic
		members, err := api.Database.GetGroupMembersByGroupID(dbauthz.AsSystemRestricted(ctx), group.Group.ID)
		if err != nil {
			httpapi.InternalServerError(rw, err)
			return
		}

		// nolint:gocritic
		memberCount, err := api.Database.GetGroupMembersCountByGroupID(dbauthz.AsSystemRestricted(ctx), group.Group.ID)
		if err != nil {
			httpapi.InternalServerError(rw, err)
			return
		}

		sdkGroups = append(sdkGroups, db2sdk.Group(group, members, int(memberCount)))
	}

	httpapi.Write(ctx, rw, http.StatusOK, wirtualsdk.ACLAvailable{
		// TODO: @emyrk we should return a MinimalUser here instead of a full user.
		// The FE requires the `email` field, so this cannot be done without
		// a UI change.
		Users:  db2sdk.ReducedUsers(users),
		Groups: sdkGroups,
	})
}

// @Summary Get template ACLs
// @ID get-template-acls
// @Security CoderSessionToken
// @Produce json
// @Tags Enterprise
// @Param template path string true "Template ID" format(uuid)
// @Success 200 {array} wirtualsdk.TemplateUser
// @Router /templates/{template}/acl [get]
func (api *API) templateACL(rw http.ResponseWriter, r *http.Request) {
	var (
		ctx      = r.Context()
		template = httpmw.TemplateParam(r)
	)

	users, err := api.Database.GetTemplateUserRoles(ctx, template.ID)
	if err != nil {
		httpapi.InternalServerError(rw, err)
		return
	}

	dbGroups, err := api.Database.GetTemplateGroupRoles(ctx, template.ID)
	if err != nil {
		httpapi.InternalServerError(rw, err)
		return
	}

	userIDs := make([]uuid.UUID, 0, len(users))
	for _, user := range users {
		userIDs = append(userIDs, user.ID)
	}

	orgIDsByMemberIDsRows, err := api.Database.GetOrganizationIDsByMemberIDs(r.Context(), userIDs)
	if err != nil && !xerrors.Is(err, sql.ErrNoRows) {
		httpapi.InternalServerError(rw, err)
		return
	}

	organizationIDsByUserID := map[uuid.UUID][]uuid.UUID{}
	for _, organizationIDsByMemberIDsRow := range orgIDsByMemberIDsRows {
		organizationIDsByUserID[organizationIDsByMemberIDsRow.UserID] = organizationIDsByMemberIDsRow.OrganizationIDs
	}

	groups := make([]wirtualsdk.TemplateGroup, 0, len(dbGroups))
	for _, group := range dbGroups {
		var members []database.GroupMember

		// This is a bit of a hack. The caller might not have permission to do this,
		// but they can read the acl list if the function got this far. So we let
		// them read the group members.
		// We should probably at least return more truncated user data here.
		// nolint:gocritic
		members, err = api.Database.GetGroupMembersByGroupID(dbauthz.AsSystemRestricted(ctx), group.ID)
		if err != nil {
			httpapi.InternalServerError(rw, err)
			return
		}
		// nolint:gocritic
		memberCount, err := api.Database.GetGroupMembersCountByGroupID(dbauthz.AsSystemRestricted(ctx), group.ID)
		if err != nil {
			httpapi.InternalServerError(rw, err)
			return
		}
		groups = append(groups, wirtualsdk.TemplateGroup{
			Group: db2sdk.Group(database.GetGroupsRow{
				Group:                   group.Group,
				OrganizationName:        template.OrganizationName,
				OrganizationDisplayName: template.OrganizationDisplayName,
			}, members, int(memberCount)),
			Role: convertToTemplateRole(group.Actions),
		})
	}

	httpapi.Write(ctx, rw, http.StatusOK, wirtualsdk.TemplateACL{
		Users:  convertTemplateUsers(users, organizationIDsByUserID),
		Groups: groups,
	})
}

// @Summary Update template ACL
// @ID update-template-acl
// @Security CoderSessionToken
// @Accept json
// @Produce json
// @Tags Enterprise
// @Param template path string true "Template ID" format(uuid)
// @Param request body wirtualsdk.UpdateTemplateACL true "Update template request"
// @Success 200 {object} wirtualsdk.Response
// @Router /templates/{template}/acl [patch]
func (api *API) patchTemplateACL(rw http.ResponseWriter, r *http.Request) {
	var (
		ctx               = r.Context()
		template          = httpmw.TemplateParam(r)
		auditor           = api.AGPL.Auditor.Load()
		aReq, commitAudit = audit.InitRequest[database.Template](rw, &audit.RequestParams{
			Audit:          *auditor,
			Log:            api.Logger,
			Request:        r,
			Action:         database.AuditActionWrite,
			OrganizationID: template.OrganizationID,
		})
	)
	defer commitAudit()
	aReq.Old = template

	var req wirtualsdk.UpdateTemplateACL
	if !httpapi.Read(ctx, rw, r, &req) {
		return
	}

	validErrs := validateTemplateACLPerms(ctx, api.Database, req.UserPerms, "user_perms", true)
	validErrs = append(validErrs,
		validateTemplateACLPerms(ctx, api.Database, req.GroupPerms, "group_perms", false)...)

	if len(validErrs) > 0 {
		httpapi.Write(ctx, rw, http.StatusBadRequest, wirtualsdk.Response{
			Message:     "Invalid request to update template metadata!",
			Validations: validErrs,
		})
		return
	}

	err := api.Database.InTx(func(tx database.Store) error {
		var err error
		template, err = tx.GetTemplateByID(ctx, template.ID)
		if err != nil {
			return xerrors.Errorf("get template by ID: %w", err)
		}

		if len(req.UserPerms) > 0 {
			for id, role := range req.UserPerms {
				// A user with an empty string implies
				// deletion.
				if role == "" {
					delete(template.UserACL, id)
					continue
				}
				template.UserACL[id] = convertSDKTemplateRole(role)
			}
		}

		if len(req.GroupPerms) > 0 {
			for id, role := range req.GroupPerms {
				// An id with an empty string implies
				// deletion.
				if role == "" {
					delete(template.GroupACL, id)
					continue
				}
				template.GroupACL[id] = convertSDKTemplateRole(role)
			}
		}

		err = tx.UpdateTemplateACLByID(ctx, database.UpdateTemplateACLByIDParams{
			ID:       template.ID,
			UserACL:  template.UserACL,
			GroupACL: template.GroupACL,
		})
		if err != nil {
			return xerrors.Errorf("update template ACL by ID: %w", err)
		}
		template, err = tx.GetTemplateByID(ctx, template.ID)
		if err != nil {
			return xerrors.Errorf("get updated template by ID: %w", err)
		}
		return nil
	}, nil)
	if err != nil {
		httpapi.InternalServerError(rw, err)
		return
	}

	aReq.New = template

	httpapi.Write(ctx, rw, http.StatusOK, wirtualsdk.Response{
		Message: "Successfully updated template ACL list.",
	})
}

// nolint TODO fix stupid flag.
func validateTemplateACLPerms(ctx context.Context, db database.Store, perms map[string]wirtualsdk.TemplateRole, field string, isUser bool) []wirtualsdk.ValidationError {
	// Validate requires full read access to users and groups
	// nolint:gocritic
	ctx = dbauthz.AsSystemRestricted(ctx)
	var validErrs []wirtualsdk.ValidationError
	for k, v := range perms {
		if err := validateTemplateRole(v); err != nil {
			validErrs = append(validErrs, wirtualsdk.ValidationError{Field: field, Detail: err.Error()})
			continue
		}

		id, err := uuid.Parse(k)
		if err != nil {
			validErrs = append(validErrs, wirtualsdk.ValidationError{Field: field, Detail: "ID " + k + "must be a valid UUID."})
			continue
		}

		if isUser {
			// This could get slow if we get a ton of user perm updates.
			_, err = db.GetUserByID(ctx, id)
			if err != nil {
				validErrs = append(validErrs, wirtualsdk.ValidationError{Field: field, Detail: fmt.Sprintf("Failed to find resource with ID %q: %v", k, err.Error())})
				continue
			}
		} else {
			// This could get slow if we get a ton of group perm updates.
			_, err = db.GetGroupByID(ctx, id)
			if err != nil {
				validErrs = append(validErrs, wirtualsdk.ValidationError{Field: field, Detail: fmt.Sprintf("Failed to find resource with ID %q: %v", k, err.Error())})
				continue
			}
		}
	}

	return validErrs
}

func convertTemplateUsers(tus []database.TemplateUser, orgIDsByUserIDs map[uuid.UUID][]uuid.UUID) []wirtualsdk.TemplateUser {
	users := make([]wirtualsdk.TemplateUser, 0, len(tus))

	for _, tu := range tus {
		users = append(users, wirtualsdk.TemplateUser{
			User: db2sdk.User(tu.User, orgIDsByUserIDs[tu.User.ID]),
			Role: convertToTemplateRole(tu.Actions),
		})
	}

	return users
}

func validateTemplateRole(role wirtualsdk.TemplateRole) error {
	actions := convertSDKTemplateRole(role)
	if actions == nil && role != wirtualsdk.TemplateRoleDeleted {
		return xerrors.Errorf("role %q is not a valid Template role", role)
	}

	return nil
}

func convertToTemplateRole(actions []policy.Action) wirtualsdk.TemplateRole {
	switch {
	case len(actions) == 1 && actions[0] == policy.ActionRead:
		return wirtualsdk.TemplateRoleUse
	case len(actions) == 1 && actions[0] == policy.WildcardSymbol:
		return wirtualsdk.TemplateRoleAdmin
	}

	return ""
}

func convertSDKTemplateRole(role wirtualsdk.TemplateRole) []policy.Action {
	switch role {
	case wirtualsdk.TemplateRoleAdmin:
		return []policy.Action{policy.WildcardSymbol}
	case wirtualsdk.TemplateRoleUse:
		return []policy.Action{policy.ActionRead}
	}

	return nil
}

// TODO move to api.RequireFeatureMW when we are OK with changing the behavior.
func (api *API) templateRBACEnabledMW(next http.Handler) http.Handler {
	return api.RequireFeatureMW(wirtualsdk.FeatureTemplateRBAC)(next)
}

func (api *API) RequireFeatureMW(feat wirtualsdk.FeatureName) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			// Entitlement must be enabled.
			if !api.Entitlements.Enabled(feat) {
				// All feature warnings should be "Premium", not "Enterprise".
				httpapi.Write(r.Context(), rw, http.StatusForbidden, wirtualsdk.Response{
					Message: fmt.Sprintf("%s is a Premium feature. Contact sales!", feat.Humanize()),
				})
				return
			}

			next.ServeHTTP(rw, r)
		})
	}
}
