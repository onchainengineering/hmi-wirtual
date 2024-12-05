// Package db2sdk provides common conversion routines from database types to wirtualsdk types
package db2sdk

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/exp/slices"
	"golang.org/x/xerrors"
	"tailscale.com/tailcfg"

	"github.com/onchainengineering/hmi-wirtual/provisionersdk/proto"
	"github.com/onchainengineering/hmi-wirtual/tailnet"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/render"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/workspaceapps/appurl"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

// List is a helper function to reduce boilerplate when converting slices of
// database types to slices of wirtualsdk types.
// Only works if the function takes a single argument.
func List[F any, T any](list []F, convert func(F) T) []T {
	return ListLazy(convert)(list)
}

// ListLazy returns the converter function for a list, but does not eval
// the input. Helpful for combining the Map and the List functions.
func ListLazy[F any, T any](convert func(F) T) func(list []F) []T {
	return func(list []F) []T {
		into := make([]T, 0, len(list))
		for _, item := range list {
			into = append(into, convert(item))
		}
		return into
	}
}

func Map[K comparable, F any, T any](params map[K]F, convert func(F) T) map[K]T {
	into := make(map[K]T)
	for k, item := range params {
		into[k] = convert(item)
	}
	return into
}

type ExternalAuthMeta struct {
	Authenticated bool
	ValidateError string
}

func ExternalAuths(auths []database.ExternalAuthLink, meta map[string]ExternalAuthMeta) []wirtualsdk.ExternalAuthLink {
	out := make([]wirtualsdk.ExternalAuthLink, 0, len(auths))
	for _, auth := range auths {
		out = append(out, ExternalAuth(auth, meta[auth.ProviderID]))
	}
	return out
}

func ExternalAuth(auth database.ExternalAuthLink, meta ExternalAuthMeta) wirtualsdk.ExternalAuthLink {
	return wirtualsdk.ExternalAuthLink{
		ProviderID:      auth.ProviderID,
		CreatedAt:       auth.CreatedAt,
		UpdatedAt:       auth.UpdatedAt,
		HasRefreshToken: auth.OAuthRefreshToken != "",
		Expires:         auth.OAuthExpiry,
		Authenticated:   meta.Authenticated,
		ValidateError:   meta.ValidateError,
	}
}

func WorkspaceBuildParameter(p database.WorkspaceBuildParameter) wirtualsdk.WorkspaceBuildParameter {
	return wirtualsdk.WorkspaceBuildParameter{
		Name:  p.Name,
		Value: p.Value,
	}
}

func WorkspaceBuildParameters(params []database.WorkspaceBuildParameter) []wirtualsdk.WorkspaceBuildParameter {
	return List(params, WorkspaceBuildParameter)
}

func TemplateVersionParameters(params []database.TemplateVersionParameter) ([]wirtualsdk.TemplateVersionParameter, error) {
	out := make([]wirtualsdk.TemplateVersionParameter, len(params))
	var err error
	for i, p := range params {
		out[i], err = TemplateVersionParameter(p)
		if err != nil {
			return nil, xerrors.Errorf("convert template version parameter %q: %w", p.Name, err)
		}
	}

	return out, nil
}

func TemplateVersionParameter(param database.TemplateVersionParameter) (wirtualsdk.TemplateVersionParameter, error) {
	options, err := templateVersionParameterOptions(param.Options)
	if err != nil {
		return wirtualsdk.TemplateVersionParameter{}, err
	}

	descriptionPlaintext, err := render.PlaintextFromMarkdown(param.Description)
	if err != nil {
		return wirtualsdk.TemplateVersionParameter{}, err
	}

	var validationMin *int32
	if param.ValidationMin.Valid {
		validationMin = &param.ValidationMin.Int32
	}

	var validationMax *int32
	if param.ValidationMax.Valid {
		validationMax = &param.ValidationMax.Int32
	}

	return wirtualsdk.TemplateVersionParameter{
		Name:                 param.Name,
		DisplayName:          param.DisplayName,
		Description:          param.Description,
		DescriptionPlaintext: descriptionPlaintext,
		Type:                 param.Type,
		Mutable:              param.Mutable,
		DefaultValue:         param.DefaultValue,
		Icon:                 param.Icon,
		Options:              options,
		ValidationRegex:      param.ValidationRegex,
		ValidationMin:        validationMin,
		ValidationMax:        validationMax,
		ValidationError:      param.ValidationError,
		ValidationMonotonic:  wirtualsdk.ValidationMonotonicOrder(param.ValidationMonotonic),
		Required:             param.Required,
		Ephemeral:            param.Ephemeral,
	}, nil
}

func ReducedUser(user database.User) wirtualsdk.ReducedUser {
	return wirtualsdk.ReducedUser{
		MinimalUser: wirtualsdk.MinimalUser{
			ID:        user.ID,
			Username:  user.Username,
			AvatarURL: user.AvatarURL,
		},
		Email:           user.Email,
		Name:            user.Name,
		CreatedAt:       user.CreatedAt,
		UpdatedAt:       user.UpdatedAt,
		LastSeenAt:      user.LastSeenAt,
		Status:          wirtualsdk.UserStatus(user.Status),
		LoginType:       wirtualsdk.LoginType(user.LoginType),
		ThemePreference: user.ThemePreference,
	}
}

func UserFromGroupMember(member database.GroupMember) database.User {
	return database.User{
		ID:                 member.UserID,
		Email:              member.UserEmail,
		Username:           member.UserUsername,
		HashedPassword:     member.UserHashedPassword,
		CreatedAt:          member.UserCreatedAt,
		UpdatedAt:          member.UserUpdatedAt,
		Status:             member.UserStatus,
		RBACRoles:          member.UserRbacRoles,
		LoginType:          member.UserLoginType,
		AvatarURL:          member.UserAvatarUrl,
		Deleted:            member.UserDeleted,
		LastSeenAt:         member.UserLastSeenAt,
		QuietHoursSchedule: member.UserQuietHoursSchedule,
		ThemePreference:    member.UserThemePreference,
		Name:               member.UserName,
		GithubComUserID:    member.UserGithubComUserID,
	}
}

func ReducedUserFromGroupMember(member database.GroupMember) wirtualsdk.ReducedUser {
	return ReducedUser(UserFromGroupMember(member))
}

func ReducedUsersFromGroupMembers(members []database.GroupMember) []wirtualsdk.ReducedUser {
	return List(members, ReducedUserFromGroupMember)
}

func ReducedUsers(users []database.User) []wirtualsdk.ReducedUser {
	return List(users, ReducedUser)
}

func User(user database.User, organizationIDs []uuid.UUID) wirtualsdk.User {
	convertedUser := wirtualsdk.User{
		ReducedUser:     ReducedUser(user),
		OrganizationIDs: organizationIDs,
		Roles:           SlimRolesFromNames(user.RBACRoles),
	}

	return convertedUser
}

func Users(users []database.User, organizationIDs map[uuid.UUID][]uuid.UUID) []wirtualsdk.User {
	return List(users, func(user database.User) wirtualsdk.User {
		return User(user, organizationIDs[user.ID])
	})
}

func Group(row database.GetGroupsRow, members []database.GroupMember, totalMemberCount int) wirtualsdk.Group {
	return wirtualsdk.Group{
		ID:                      row.Group.ID,
		Name:                    row.Group.Name,
		DisplayName:             row.Group.DisplayName,
		OrganizationID:          row.Group.OrganizationID,
		AvatarURL:               row.Group.AvatarURL,
		Members:                 ReducedUsersFromGroupMembers(members),
		TotalMemberCount:        totalMemberCount,
		QuotaAllowance:          int(row.Group.QuotaAllowance),
		Source:                  wirtualsdk.GroupSource(row.Group.Source),
		OrganizationName:        row.OrganizationName,
		OrganizationDisplayName: row.OrganizationDisplayName,
	}
}

func TemplateInsightsParameters(parameterRows []database.GetTemplateParameterInsightsRow) ([]wirtualsdk.TemplateParameterUsage, error) {
	// Use a stable sort, similarly to how we would sort in the query, note that
	// we don't sort in the query because order varies depending on the table
	// collation.
	//
	// ORDER BY utp.name, utp.type, utp.display_name, utp.description, utp.options, wbp.value
	slices.SortFunc(parameterRows, func(a, b database.GetTemplateParameterInsightsRow) int {
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		if a.Type != b.Type {
			return strings.Compare(a.Type, b.Type)
		}
		if a.DisplayName != b.DisplayName {
			return strings.Compare(a.DisplayName, b.DisplayName)
		}
		if a.Description != b.Description {
			return strings.Compare(a.Description, b.Description)
		}
		if string(a.Options) != string(b.Options) {
			return strings.Compare(string(a.Options), string(b.Options))
		}
		return strings.Compare(a.Value, b.Value)
	})

	parametersUsage := []wirtualsdk.TemplateParameterUsage{}
	indexByNum := make(map[int64]int)
	for _, param := range parameterRows {
		if _, ok := indexByNum[param.Num]; !ok {
			var opts []wirtualsdk.TemplateVersionParameterOption
			err := json.Unmarshal(param.Options, &opts)
			if err != nil {
				return nil, err
			}

			plaintextDescription, err := render.PlaintextFromMarkdown(param.Description)
			if err != nil {
				return nil, err
			}

			parametersUsage = append(parametersUsage, wirtualsdk.TemplateParameterUsage{
				TemplateIDs: param.TemplateIDs,
				Name:        param.Name,
				Type:        param.Type,
				DisplayName: param.DisplayName,
				Description: plaintextDescription,
				Options:     opts,
			})
			indexByNum[param.Num] = len(parametersUsage) - 1
		}

		i := indexByNum[param.Num]
		parametersUsage[i].Values = append(parametersUsage[i].Values, wirtualsdk.TemplateParameterValue{
			Value: param.Value,
			Count: param.Count,
		})
	}

	return parametersUsage, nil
}

func templateVersionParameterOptions(rawOptions json.RawMessage) ([]wirtualsdk.TemplateVersionParameterOption, error) {
	var protoOptions []*proto.RichParameterOption
	err := json.Unmarshal(rawOptions, &protoOptions)
	if err != nil {
		return nil, err
	}
	var options []wirtualsdk.TemplateVersionParameterOption
	for _, option := range protoOptions {
		options = append(options, wirtualsdk.TemplateVersionParameterOption{
			Name:        option.Name,
			Description: option.Description,
			Value:       option.Value,
			Icon:        option.Icon,
		})
	}
	return options, nil
}

func OAuth2ProviderApp(accessURL *url.URL, dbApp database.OAuth2ProviderApp) wirtualsdk.OAuth2ProviderApp {
	return wirtualsdk.OAuth2ProviderApp{
		ID:          dbApp.ID,
		Name:        dbApp.Name,
		CallbackURL: dbApp.CallbackURL,
		Icon:        dbApp.Icon,
		Endpoints: wirtualsdk.OAuth2AppEndpoints{
			Authorization: accessURL.ResolveReference(&url.URL{
				Path: "/oauth2/authorize",
			}).String(),
			Token: accessURL.ResolveReference(&url.URL{
				Path: "/oauth2/tokens",
			}).String(),
			// We do not currently support DeviceAuth.
			DeviceAuth: "",
		},
	}
}

func OAuth2ProviderApps(accessURL *url.URL, dbApps []database.OAuth2ProviderApp) []wirtualsdk.OAuth2ProviderApp {
	return List(dbApps, func(dbApp database.OAuth2ProviderApp) wirtualsdk.OAuth2ProviderApp {
		return OAuth2ProviderApp(accessURL, dbApp)
	})
}

func convertDisplayApps(apps []database.DisplayApp) []wirtualsdk.DisplayApp {
	dapps := make([]wirtualsdk.DisplayApp, 0, len(apps))
	for _, app := range apps {
		switch wirtualsdk.DisplayApp(app) {
		case wirtualsdk.DisplayAppVSCodeDesktop, wirtualsdk.DisplayAppVSCodeInsiders, wirtualsdk.DisplayAppPortForward, wirtualsdk.DisplayAppWebTerminal, wirtualsdk.DisplayAppSSH:
			dapps = append(dapps, wirtualsdk.DisplayApp(app))
		}
	}

	return dapps
}

func WorkspaceAgentEnvironment(workspaceAgent database.WorkspaceAgent) (map[string]string, error) {
	var envs map[string]string
	if workspaceAgent.EnvironmentVariables.Valid {
		err := json.Unmarshal(workspaceAgent.EnvironmentVariables.RawMessage, &envs)
		if err != nil {
			return nil, xerrors.Errorf("unmarshal environment variables: %w", err)
		}
	}

	return envs, nil
}

func WorkspaceAgent(derpMap *tailcfg.DERPMap, coordinator tailnet.Coordinator,
	dbAgent database.WorkspaceAgent, apps []wirtualsdk.WorkspaceApp, scripts []wirtualsdk.WorkspaceAgentScript, logSources []wirtualsdk.WorkspaceAgentLogSource,
	agentInactiveDisconnectTimeout time.Duration, agentFallbackTroubleshootingURL string,
) (wirtualsdk.WorkspaceAgent, error) {
	envs, err := WorkspaceAgentEnvironment(dbAgent)
	if err != nil {
		return wirtualsdk.WorkspaceAgent{}, err
	}
	troubleshootingURL := agentFallbackTroubleshootingURL
	if dbAgent.TroubleshootingURL != "" {
		troubleshootingURL = dbAgent.TroubleshootingURL
	}
	subsystems := make([]wirtualsdk.AgentSubsystem, len(dbAgent.Subsystems))
	for i, subsystem := range dbAgent.Subsystems {
		subsystems[i] = wirtualsdk.AgentSubsystem(subsystem)
	}

	legacyStartupScriptBehavior := wirtualsdk.WorkspaceAgentStartupScriptBehaviorNonBlocking
	for _, script := range scripts {
		if !script.RunOnStart {
			continue
		}
		if !script.StartBlocksLogin {
			continue
		}
		legacyStartupScriptBehavior = wirtualsdk.WorkspaceAgentStartupScriptBehaviorBlocking
	}

	workspaceAgent := wirtualsdk.WorkspaceAgent{
		ID:                       dbAgent.ID,
		CreatedAt:                dbAgent.CreatedAt,
		UpdatedAt:                dbAgent.UpdatedAt,
		ResourceID:               dbAgent.ResourceID,
		InstanceID:               dbAgent.AuthInstanceID.String,
		Name:                     dbAgent.Name,
		Architecture:             dbAgent.Architecture,
		OperatingSystem:          dbAgent.OperatingSystem,
		Scripts:                  scripts,
		StartupScriptBehavior:    legacyStartupScriptBehavior,
		LogsLength:               dbAgent.LogsLength,
		LogsOverflowed:           dbAgent.LogsOverflowed,
		LogSources:               logSources,
		Version:                  dbAgent.Version,
		APIVersion:               dbAgent.APIVersion,
		EnvironmentVariables:     envs,
		Directory:                dbAgent.Directory,
		ExpandedDirectory:        dbAgent.ExpandedDirectory,
		Apps:                     apps,
		ConnectionTimeoutSeconds: dbAgent.ConnectionTimeoutSeconds,
		TroubleshootingURL:       troubleshootingURL,
		LifecycleState:           wirtualsdk.WorkspaceAgentLifecycle(dbAgent.LifecycleState),
		Subsystems:               subsystems,
		DisplayApps:              convertDisplayApps(dbAgent.DisplayApps),
	}
	node := coordinator.Node(dbAgent.ID)
	if node != nil {
		workspaceAgent.DERPLatency = map[string]wirtualsdk.DERPRegion{}
		for rawRegion, latency := range node.DERPLatency {
			regionParts := strings.SplitN(rawRegion, "-", 2)
			regionID, err := strconv.Atoi(regionParts[0])
			if err != nil {
				return wirtualsdk.WorkspaceAgent{}, xerrors.Errorf("convert derp region id %q: %w", rawRegion, err)
			}
			region, found := derpMap.Regions[regionID]
			if !found {
				// It's possible that a workspace agent is using an old DERPMap
				// and reports regions that do not exist. If that's the case,
				// report the region as unknown!
				region = &tailcfg.DERPRegion{
					RegionID:   regionID,
					RegionName: fmt.Sprintf("Unnamed %d", regionID),
				}
			}
			workspaceAgent.DERPLatency[region.RegionName] = wirtualsdk.DERPRegion{
				Preferred:           node.PreferredDERP == regionID,
				LatencyMilliseconds: latency * 1000,
			}
		}
	}

	status := dbAgent.Status(agentInactiveDisconnectTimeout)
	workspaceAgent.Status = wirtualsdk.WorkspaceAgentStatus(status.Status)
	workspaceAgent.FirstConnectedAt = status.FirstConnectedAt
	workspaceAgent.LastConnectedAt = status.LastConnectedAt
	workspaceAgent.DisconnectedAt = status.DisconnectedAt

	if dbAgent.StartedAt.Valid {
		workspaceAgent.StartedAt = &dbAgent.StartedAt.Time
	}
	if dbAgent.ReadyAt.Valid {
		workspaceAgent.ReadyAt = &dbAgent.ReadyAt.Time
	}

	switch {
	case workspaceAgent.Status != wirtualsdk.WorkspaceAgentConnected && workspaceAgent.LifecycleState == wirtualsdk.WorkspaceAgentLifecycleOff:
		workspaceAgent.Health.Reason = "agent is not running"
	case workspaceAgent.Status == wirtualsdk.WorkspaceAgentTimeout:
		workspaceAgent.Health.Reason = "agent is taking too long to connect"
	case workspaceAgent.Status == wirtualsdk.WorkspaceAgentDisconnected:
		workspaceAgent.Health.Reason = "agent has lost connection"
	// Note: We could also handle wirtualsdk.WorkspaceAgentLifecycleStartTimeout
	// here, but it's more of a soft issue, so we don't want to mark the agent
	// as unhealthy.
	case workspaceAgent.LifecycleState == wirtualsdk.WorkspaceAgentLifecycleStartError:
		workspaceAgent.Health.Reason = "agent startup script exited with an error"
	case workspaceAgent.LifecycleState.ShuttingDown():
		workspaceAgent.Health.Reason = "agent is shutting down"
	default:
		workspaceAgent.Health.Healthy = true
	}

	return workspaceAgent, nil
}

func AppSubdomain(dbApp database.WorkspaceApp, agentName, workspaceName, ownerName string) string {
	if !dbApp.Subdomain || agentName == "" || ownerName == "" || workspaceName == "" {
		return ""
	}

	appSlug := dbApp.Slug
	if appSlug == "" {
		appSlug = dbApp.DisplayName
	}
	return appurl.ApplicationURL{
		// We never generate URLs with a prefix. We only allow prefixes when
		// parsing URLs from the hostname. Users that want this feature can
		// write out their own URLs.
		Prefix:        "",
		AppSlugOrPort: appSlug,
		AgentName:     agentName,
		WorkspaceName: workspaceName,
		Username:      ownerName,
	}.String()
}

func Apps(dbApps []database.WorkspaceApp, agent database.WorkspaceAgent, ownerName string, workspace database.Workspace) []wirtualsdk.WorkspaceApp {
	sort.Slice(dbApps, func(i, j int) bool {
		if dbApps[i].DisplayOrder != dbApps[j].DisplayOrder {
			return dbApps[i].DisplayOrder < dbApps[j].DisplayOrder
		}
		if dbApps[i].DisplayName != dbApps[j].DisplayName {
			return dbApps[i].DisplayName < dbApps[j].DisplayName
		}
		return dbApps[i].Slug < dbApps[j].Slug
	})

	apps := make([]wirtualsdk.WorkspaceApp, 0)
	for _, dbApp := range dbApps {
		apps = append(apps, wirtualsdk.WorkspaceApp{
			ID:            dbApp.ID,
			URL:           dbApp.Url.String,
			External:      dbApp.External,
			Slug:          dbApp.Slug,
			DisplayName:   dbApp.DisplayName,
			Command:       dbApp.Command.String,
			Icon:          dbApp.Icon,
			Subdomain:     dbApp.Subdomain,
			SubdomainName: AppSubdomain(dbApp, agent.Name, workspace.Name, ownerName),
			SharingLevel:  wirtualsdk.WorkspaceAppSharingLevel(dbApp.SharingLevel),
			Healthcheck: wirtualsdk.Healthcheck{
				URL:       dbApp.HealthcheckUrl,
				Interval:  dbApp.HealthcheckInterval,
				Threshold: dbApp.HealthcheckThreshold,
			},
			Health: wirtualsdk.WorkspaceAppHealth(dbApp.Health),
			Hidden: dbApp.Hidden,
		})
	}
	return apps
}

func ProvisionerDaemon(dbDaemon database.ProvisionerDaemon) wirtualsdk.ProvisionerDaemon {
	result := wirtualsdk.ProvisionerDaemon{
		ID:             dbDaemon.ID,
		OrganizationID: dbDaemon.OrganizationID,
		CreatedAt:      dbDaemon.CreatedAt,
		LastSeenAt:     wirtualsdk.NullTime{NullTime: dbDaemon.LastSeenAt},
		Name:           dbDaemon.Name,
		Tags:           dbDaemon.Tags,
		Version:        dbDaemon.Version,
		APIVersion:     dbDaemon.APIVersion,
		KeyID:          dbDaemon.KeyID,
	}
	for _, provisionerType := range dbDaemon.Provisioners {
		result.Provisioners = append(result.Provisioners, wirtualsdk.ProvisionerType(provisionerType))
	}
	return result
}

func RecentProvisionerDaemons(now time.Time, staleInterval time.Duration, daemons []database.ProvisionerDaemon) []wirtualsdk.ProvisionerDaemon {
	results := []wirtualsdk.ProvisionerDaemon{}

	for _, daemon := range daemons {
		// Daemon never connected, skip.
		if !daemon.LastSeenAt.Valid {
			continue
		}
		// Daemon has gone away, skip.
		if now.Sub(daemon.LastSeenAt.Time) > staleInterval {
			continue
		}

		results = append(results, ProvisionerDaemon(daemon))
	}

	// Ensure stable order for display and for tests
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results
}

func SlimRole(role rbac.Role) wirtualsdk.SlimRole {
	orgID := ""
	if role.Identifier.OrganizationID != uuid.Nil {
		orgID = role.Identifier.OrganizationID.String()
	}

	return wirtualsdk.SlimRole{
		DisplayName:    role.DisplayName,
		Name:           role.Identifier.Name,
		OrganizationID: orgID,
	}
}

func SlimRolesFromNames(names []string) []wirtualsdk.SlimRole {
	convertedRoles := make([]wirtualsdk.SlimRole, 0, len(names))

	for _, name := range names {
		convertedRoles = append(convertedRoles, SlimRoleFromName(name))
	}

	return convertedRoles
}

func SlimRoleFromName(name string) wirtualsdk.SlimRole {
	rbacRole, err := rbac.RoleByName(rbac.RoleIdentifier{Name: name})
	var convertedRole wirtualsdk.SlimRole
	if err == nil {
		convertedRole = SlimRole(rbacRole)
	} else {
		convertedRole = wirtualsdk.SlimRole{Name: name}
	}
	return convertedRole
}

func RBACRole(role rbac.Role) wirtualsdk.Role {
	slim := SlimRole(role)

	orgPerms := role.Org[slim.OrganizationID]
	return wirtualsdk.Role{
		Name:                    slim.Name,
		OrganizationID:          slim.OrganizationID,
		DisplayName:             slim.DisplayName,
		SitePermissions:         List(role.Site, RBACPermission),
		OrganizationPermissions: List(orgPerms, RBACPermission),
		UserPermissions:         List(role.User, RBACPermission),
	}
}

func Role(role database.CustomRole) wirtualsdk.Role {
	orgID := ""
	if role.OrganizationID.UUID != uuid.Nil {
		orgID = role.OrganizationID.UUID.String()
	}

	return wirtualsdk.Role{
		Name:                    role.Name,
		OrganizationID:          orgID,
		DisplayName:             role.DisplayName,
		SitePermissions:         List(role.SitePermissions, Permission),
		OrganizationPermissions: List(role.OrgPermissions, Permission),
		UserPermissions:         List(role.UserPermissions, Permission),
	}
}

func Permission(permission database.CustomRolePermission) wirtualsdk.Permission {
	return wirtualsdk.Permission{
		Negate:       permission.Negate,
		ResourceType: wirtualsdk.RBACResource(permission.ResourceType),
		Action:       wirtualsdk.RBACAction(permission.Action),
	}
}

func RBACPermission(permission rbac.Permission) wirtualsdk.Permission {
	return wirtualsdk.Permission{
		Negate:       permission.Negate,
		ResourceType: wirtualsdk.RBACResource(permission.ResourceType),
		Action:       wirtualsdk.RBACAction(permission.Action),
	}
}

func Organization(organization database.Organization) wirtualsdk.Organization {
	return wirtualsdk.Organization{
		MinimalOrganization: wirtualsdk.MinimalOrganization{
			ID:          organization.ID,
			Name:        organization.Name,
			DisplayName: organization.DisplayName,
			Icon:        organization.Icon,
		},
		Description: organization.Description,
		CreatedAt:   organization.CreatedAt,
		UpdatedAt:   organization.UpdatedAt,
		IsDefault:   organization.IsDefault,
	}
}

func CryptoKeys(keys []database.CryptoKey) []wirtualsdk.CryptoKey {
	return List(keys, CryptoKey)
}

func CryptoKey(key database.CryptoKey) wirtualsdk.CryptoKey {
	return wirtualsdk.CryptoKey{
		Feature:   wirtualsdk.CryptoKeyFeature(key.Feature),
		Sequence:  key.Sequence,
		StartsAt:  key.StartsAt,
		DeletesAt: key.DeletesAt.Time,
		Secret:    key.Secret.String,
	}
}