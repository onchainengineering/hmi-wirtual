package wirtuald_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/coder/v2/wirtuald"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest/oidctest"
	"github.com/coder/coder/v2/wirtuald/notifications"
	"github.com/coder/coder/v2/wirtuald/notifications/notificationstest"
	"github.com/coder/coder/v2/wirtuald/rbac/policy"
	"github.com/coder/serpent"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/wirtuald/audit"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/database/dbauthz"
	"github.com/coder/coder/v2/wirtuald/database/dbfake"
	"github.com/coder/coder/v2/wirtuald/database/dbgen"
	"github.com/coder/coder/v2/wirtuald/database/dbtime"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtuald/util/ptr"
	"github.com/coder/coder/v2/wirtuald/util/slice"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/testutil"
)

func TestFirstUser(t *testing.T) {
	t.Parallel()
	t.Run("BadRequest", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		has, err := client.HasFirstUser(context.Background())
		require.NoError(t, err)
		require.False(t, has)

		_, err = client.CreateFirstUser(ctx, wirtualsdk.CreateFirstUserRequest{})
		require.Error(t, err)
	})

	t.Run("AlreadyExists", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		_ = wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, err := client.CreateFirstUser(ctx, wirtualsdk.CreateFirstUserRequest{
			Email:    "some@email.com",
			Username: "exampleuser",
			Password: "SomeSecurePassword!",
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusConflict, apiErr.StatusCode())
	})

	t.Run("Create", func(t *testing.T) {
		t.Parallel()
		ctx := testutil.Context(t, testutil.WaitShort)
		client := wirtualdtest.New(t, nil)
		_ = wirtualdtest.CreateFirstUser(t, client)
		u, err := client.User(ctx, wirtualsdk.Me)
		require.NoError(t, err)
		assert.Equal(t, wirtualdtest.FirstUserParams.Name, u.Name)
		assert.Equal(t, wirtualdtest.FirstUserParams.Email, u.Email)
		assert.Equal(t, wirtualdtest.FirstUserParams.Username, u.Username)
	})

	t.Run("Trial", func(t *testing.T) {
		t.Parallel()
		trialGenerated := make(chan struct{})
		entitlementsRefreshed := make(chan struct{})

		client := wirtualdtest.New(t, &wirtualdtest.Options{
			TrialGenerator: func(context.Context, wirtualsdk.LicensorTrialRequest) error {
				close(trialGenerated)
				return nil
			},
			RefreshEntitlements: func(context.Context) error {
				close(entitlementsRefreshed)
				return nil
			},
		})

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		req := wirtualsdk.CreateFirstUserRequest{
			Email:    "testuser@coder.com",
			Username: "testuser",
			Name:     "Test User",
			Password: "SomeSecurePassword!",
			Trial:    true,
		}
		_, err := client.CreateFirstUser(ctx, req)
		require.NoError(t, err)

		_ = testutil.RequireRecvCtx(ctx, t, trialGenerated)
		_ = testutil.RequireRecvCtx(ctx, t, entitlementsRefreshed)
	})
}

func TestPostLogin(t *testing.T) {
	t.Parallel()
	t.Run("InvalidUser", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, err := client.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
			Email:    "my@email.org",
			Password: "password",
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode())
	})

	t.Run("BadPassword", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
		numLogs := len(auditor.AuditLogs())

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		req := wirtualsdk.CreateFirstUserRequest{
			Email:    "testuser@coder.com",
			Username: "testuser",
			Password: "SomeSecurePassword!",
		}
		_, err := client.CreateFirstUser(ctx, req)
		require.NoError(t, err)
		_, err = client.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
			Email:    req.Email,
			Password: "badpass",
		})
		numLogs++ // add an audit log for login
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode())

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionLogin, auditor.AuditLogs()[numLogs-1].Action)
	})

	t.Run("Suspended", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
		numLogs := len(auditor.AuditLogs())
		first := wirtualdtest.CreateFirstUser(t, client)
		numLogs++ // add an audit log for create user
		numLogs++ // add an audit log for login

		member, _ := wirtualdtest.CreateAnotherUser(t, client, first.OrganizationID)
		numLogs++ // add an audit log for create user

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		memberUser, err := member.User(ctx, wirtualsdk.Me)
		require.NoError(t, err, "fetch member user")

		_, err = client.UpdateUserStatus(ctx, memberUser.Username, wirtualsdk.UserStatusSuspended)
		require.NoError(t, err, "suspend member")
		numLogs++ // add an audit log for update user

		// Test an existing session
		_, err = member.User(ctx, wirtualsdk.Me)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode())
		require.Contains(t, apiErr.Message, "Contact an admin")

		// Test a new session
		_, err = client.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
			Email:    memberUser.Email,
			Password: "SomeSecurePassword!",
		})
		numLogs++ // add an audit log for login
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode())
		require.Contains(t, apiErr.Message, "suspended")

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionLogin, auditor.AuditLogs()[numLogs-1].Action)
	})

	t.Run("DisabledPasswordAuth", func(t *testing.T) {
		t.Parallel()

		dc := wirtualdtest.DeploymentValues(t)
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			DeploymentValues: dc,
		})

		first := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		// With a user account.
		const password = "SomeSecurePassword!"
		user, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           "test+user-@coder.com",
			Username:        "user",
			Password:        password,
			OrganizationIDs: []uuid.UUID{first.OrganizationID},
		})
		require.NoError(t, err)

		dc.DisablePasswordAuth = serpent.Bool(true)

		userClient := wirtualsdk.New(client.URL)
		_, err = userClient.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
			Email:    user.Email,
			Password: password,
		})
		require.Error(t, err)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusForbidden, apiErr.StatusCode())
		require.Contains(t, apiErr.Message, "Password authentication is disabled")
	})

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
		numLogs := len(auditor.AuditLogs())

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		req := wirtualsdk.CreateFirstUserRequest{
			Email:    "testuser@coder.com",
			Username: "testuser",
			Password: "SomeSecurePassword!",
		}
		_, err := client.CreateFirstUser(ctx, req)
		require.NoError(t, err)
		numLogs++ // add an audit log for create user
		numLogs++ // add an audit log for login

		_, err = client.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
			Email:    req.Email,
			Password: req.Password,
		})
		require.NoError(t, err)

		// Login should be case insensitive
		_, err = client.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
			Email:    strings.ToUpper(req.Email),
			Password: req.Password,
		})
		require.NoError(t, err)

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionLogin, auditor.AuditLogs()[numLogs-1].Action)
	})

	t.Run("Lifetime&Expire", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, nil)
		owner := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		split := strings.Split(client.SessionToken(), "-")
		key, err := client.APIKeyByID(ctx, owner.UserID.String(), split[0])
		require.NoError(t, err, "fetch login key")
		require.Equal(t, int64(86400), key.LifetimeSeconds, "default should be 86400")

		// tokens have a longer life
		token, err := client.CreateToken(ctx, wirtualsdk.Me, wirtualsdk.CreateTokenRequest{})
		require.NoError(t, err, "make new token api key")
		split = strings.Split(token.Key, "-")
		apiKey, err := client.APIKeyByID(ctx, owner.UserID.String(), split[0])
		require.NoError(t, err, "fetch api key")

		require.True(t, apiKey.ExpiresAt.After(time.Now().Add(time.Hour*24*6)), "default tokens lasts more than 6 days")
		require.True(t, apiKey.ExpiresAt.Before(time.Now().Add(time.Hour*24*8)), "default tokens lasts less than 8 days")
		require.Greater(t, apiKey.LifetimeSeconds, key.LifetimeSeconds, "token should have longer lifetime")
	})
}

func TestDeleteUser(t *testing.T) {
	t.Parallel()
	t.Run("Works", func(t *testing.T) {
		t.Parallel()
		client, _, api := wirtualdtest.NewWithAPI(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		authz := wirtualdtest.AssertRBAC(t, api, client)

		anotherClient, another := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		err := client.DeleteUser(context.Background(), another.ID)
		require.NoError(t, err)
		// Attempt to create a user with the same email and username, and delete them again.
		another, err = client.CreateUserWithOrgs(context.Background(), wirtualsdk.CreateUserRequestWithOrgs{
			Email:           another.Email,
			Username:        another.Username,
			Password:        "SomeSecurePassword!",
			OrganizationIDs: []uuid.UUID{user.OrganizationID},
		})
		require.NoError(t, err)
		err = client.DeleteUser(context.Background(), another.ID)
		require.NoError(t, err)

		// IMPORTANT: assert that the deleted user's session is no longer valid.
		_, err = anotherClient.User(context.Background(), wirtualsdk.Me)
		require.Error(t, err)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode())

		// RBAC checks
		authz.AssertChecked(t, policy.ActionCreate, rbac.ResourceUser)
		authz.AssertChecked(t, policy.ActionDelete, another)
	})
	t.Run("NoPermission", func(t *testing.T) {
		t.Parallel()
		api := wirtualdtest.New(t, nil)
		firstUser := wirtualdtest.CreateFirstUser(t, api)
		client, _ := wirtualdtest.CreateAnotherUser(t, api, firstUser.OrganizationID)
		err := client.DeleteUser(context.Background(), firstUser.UserID)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode())
	})
	t.Run("HasWorkspaces", func(t *testing.T) {
		t.Parallel()
		client, _ := wirtualdtest.NewWithProvisionerCloser(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		anotherClient, another := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		wirtualdtest.CreateWorkspace(t, anotherClient, template.ID)
		err := client.DeleteUser(context.Background(), another.ID)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusExpectationFailed, apiErr.StatusCode())
	})
	t.Run("Self", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		err := client.DeleteUser(context.Background(), user.UserID)
		var apiErr *wirtualsdk.Error
		require.Error(t, err, "should not be able to delete self")
		require.ErrorAs(t, err, &apiErr, "should be a wirtuald error")
		require.Equal(t, http.StatusForbidden, apiErr.StatusCode(), "should be forbidden")
	})
}

func TestNotifyUserStatusChanged(t *testing.T) {
	t.Parallel()

	type expectedNotification struct {
		TemplateID uuid.UUID
		UserID     uuid.UUID
	}

	verifyNotificationDispatched := func(notifyEnq *notificationstest.FakeEnqueuer, expectedNotifications []expectedNotification, member wirtualsdk.User, label string) {
		require.Equal(t, len(expectedNotifications), len(notifyEnq.Sent()))

		// Validate that each expected notification is present in notifyEnq.Sent()
		for _, expected := range expectedNotifications {
			found := false
			for _, sent := range notifyEnq.Sent() {
				if sent.TemplateID == expected.TemplateID &&
					sent.UserID == expected.UserID &&
					slices.Contains(sent.Targets, member.ID) &&
					sent.Labels[label] == member.Username {
					found = true
					break
				}
			}
			require.True(t, found, "Expected notification not found: %+v", expected)
		}
	}

	t.Run("Account suspended", func(t *testing.T) {
		t.Parallel()

		notifyEnq := &notificationstest.FakeEnqueuer{}
		adminClient := wirtualdtest.New(t, &wirtualdtest.Options{
			NotificationsEnqueuer: notifyEnq,
		})
		firstUser := wirtualdtest.CreateFirstUser(t, adminClient)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, userAdmin := wirtualdtest.CreateAnotherUser(t, adminClient, firstUser.OrganizationID, rbac.RoleUserAdmin())

		member, err := adminClient.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{firstUser.OrganizationID},
			Email:           "another@user.org",
			Username:        "someone-else",
			Password:        "SomeSecurePassword!",
		})
		require.NoError(t, err)

		notifyEnq.Clear()

		// when
		_, err = adminClient.UpdateUserStatus(context.Background(), member.Username, wirtualsdk.UserStatusSuspended)
		require.NoError(t, err)

		// then
		verifyNotificationDispatched(notifyEnq, []expectedNotification{
			{TemplateID: notifications.TemplateUserAccountSuspended, UserID: firstUser.UserID},
			{TemplateID: notifications.TemplateUserAccountSuspended, UserID: userAdmin.ID},
			{TemplateID: notifications.TemplateYourAccountSuspended, UserID: member.ID},
		}, member, "suspended_account_name")
	})

	t.Run("Account reactivated", func(t *testing.T) {
		t.Parallel()

		// given
		notifyEnq := &notificationstest.FakeEnqueuer{}
		adminClient := wirtualdtest.New(t, &wirtualdtest.Options{
			NotificationsEnqueuer: notifyEnq,
		})
		firstUser := wirtualdtest.CreateFirstUser(t, adminClient)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, userAdmin := wirtualdtest.CreateAnotherUser(t, adminClient, firstUser.OrganizationID, rbac.RoleUserAdmin())

		member, err := adminClient.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{firstUser.OrganizationID},
			Email:           "another@user.org",
			Username:        "someone-else",
			Password:        "SomeSecurePassword!",
		})
		require.NoError(t, err)

		_, err = adminClient.UpdateUserStatus(context.Background(), member.Username, wirtualsdk.UserStatusSuspended)
		require.NoError(t, err)

		notifyEnq.Clear()

		// when
		_, err = adminClient.UpdateUserStatus(context.Background(), member.Username, wirtualsdk.UserStatusActive)
		require.NoError(t, err)

		// then
		verifyNotificationDispatched(notifyEnq, []expectedNotification{
			{TemplateID: notifications.TemplateUserAccountActivated, UserID: firstUser.UserID},
			{TemplateID: notifications.TemplateUserAccountActivated, UserID: userAdmin.ID},
			{TemplateID: notifications.TemplateYourAccountActivated, UserID: member.ID},
		}, member, "activated_account_name")
	})
}

func TestNotifyDeletedUser(t *testing.T) {
	t.Parallel()

	t.Run("OwnerNotified", func(t *testing.T) {
		t.Parallel()

		// given
		notifyEnq := &notificationstest.FakeEnqueuer{}
		adminClient := wirtualdtest.New(t, &wirtualdtest.Options{
			NotificationsEnqueuer: notifyEnq,
		})
		firstUserResponse := wirtualdtest.CreateFirstUser(t, adminClient)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		firstUser, err := adminClient.User(ctx, firstUserResponse.UserID.String())
		require.NoError(t, err)

		user, err := adminClient.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{firstUserResponse.OrganizationID},
			Email:           "another@user.org",
			Username:        "someone-else",
			Password:        "SomeSecurePassword!",
		})
		require.NoError(t, err)

		// when
		err = adminClient.DeleteUser(context.Background(), user.ID)
		require.NoError(t, err)

		// then
		require.Len(t, notifyEnq.Sent(), 2)
		// notifyEnq.Sent()[0] is create account event
		require.Equal(t, notifications.TemplateUserAccountDeleted, notifyEnq.Sent()[1].TemplateID)
		require.Equal(t, firstUser.ID, notifyEnq.Sent()[1].UserID)
		require.Contains(t, notifyEnq.Sent()[1].Targets, user.ID)
		require.Equal(t, user.Username, notifyEnq.Sent()[1].Labels["deleted_account_name"])
		require.Equal(t, user.Name, notifyEnq.Sent()[1].Labels["deleted_account_user_name"])
		require.Equal(t, firstUser.Name, notifyEnq.Sent()[1].Labels["initiator"])
	})

	t.Run("UserAdminNotified", func(t *testing.T) {
		t.Parallel()

		// given
		notifyEnq := &notificationstest.FakeEnqueuer{}
		adminClient := wirtualdtest.New(t, &wirtualdtest.Options{
			NotificationsEnqueuer: notifyEnq,
		})
		firstUser := wirtualdtest.CreateFirstUser(t, adminClient)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, userAdmin := wirtualdtest.CreateAnotherUser(t, adminClient, firstUser.OrganizationID, rbac.RoleUserAdmin())

		member, err := adminClient.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{firstUser.OrganizationID},
			Email:           "another@user.org",
			Username:        "someone-else",
			Password:        "SomeSecurePassword!",
		})
		require.NoError(t, err)

		// when
		err = adminClient.DeleteUser(context.Background(), member.ID)
		require.NoError(t, err)

		// then
		sent := notifyEnq.Sent()
		require.Len(t, sent, 5)
		// sent[0]: "User admin" account created, "owner" notified
		// sent[1]: "Member" account created, "owner" notified
		// sent[2]: "Member" account created, "user admin" notified

		// "Member" account deleted, "owner" notified
		require.Equal(t, notifications.TemplateUserAccountDeleted, sent[3].TemplateID)
		require.Equal(t, firstUser.UserID, sent[3].UserID)
		require.Contains(t, sent[3].Targets, member.ID)
		require.Equal(t, member.Username, sent[3].Labels["deleted_account_name"])

		// "Member" account deleted, "user admin" notified
		require.Equal(t, notifications.TemplateUserAccountDeleted, sent[4].TemplateID)
		require.Equal(t, userAdmin.ID, sent[4].UserID)
		require.Contains(t, sent[4].Targets, member.ID)
		require.Equal(t, member.Username, sent[4].Labels["deleted_account_name"])
	})
}

func TestPostLogout(t *testing.T) {
	t.Parallel()

	// Checks that the cookie is cleared and the API Key is deleted from the database.
	t.Run("Logout", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
		numLogs := len(auditor.AuditLogs())

		owner := wirtualdtest.CreateFirstUser(t, client)
		numLogs++ // add an audit log for login

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		keyID := strings.Split(client.SessionToken(), "-")[0]
		apiKey, err := client.APIKeyByID(ctx, owner.UserID.String(), keyID)
		require.NoError(t, err)
		require.Equal(t, keyID, apiKey.ID, "API key should exist in the database")

		fullURL, err := client.URL.Parse("/api/v2/users/logout")
		require.NoError(t, err, "Server URL should parse successfully")

		res, err := client.Request(ctx, http.MethodPost, fullURL.String(), nil)
		numLogs++ // add an audit log for logout

		require.NoError(t, err, "/logout request should succeed")
		res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode)

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionLogout, auditor.AuditLogs()[numLogs-1].Action)

		cookies := res.Cookies()

		var found bool
		for _, cookie := range cookies {
			if cookie.Name == wirtualsdk.SessionTokenCookie {
				require.Equal(t, wirtualsdk.SessionTokenCookie, cookie.Name, "Cookie should be the auth cookie")
				require.Equal(t, -1, cookie.MaxAge, "Cookie should be set to delete")
				found = true
			}
		}
		require.True(t, found, "auth cookie should be returned")

		_, err = client.APIKeyByID(ctx, owner.UserID.String(), keyID)
		sdkErr := &wirtualsdk.Error{}
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusUnauthorized, sdkErr.StatusCode(), "Expecting 401")
	})
}

// nolint:bodyclose
func TestPostUsers(t *testing.T) {
	t.Parallel()
	t.Run("NoAuth", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{})
		require.Error(t, err)
	})

	t.Run("Conflicting", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		me, err := client.User(ctx, wirtualsdk.Me)
		require.NoError(t, err)
		_, err = client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           me.Email,
			Username:        me.Username,
			Password:        "MySecurePassword!",
			OrganizationIDs: []uuid.UUID{uuid.New()},
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusConflict, apiErr.StatusCode())
	})

	t.Run("OrganizationNotFound", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{uuid.New()},
			Email:           "another@user.org",
			Username:        "someone-else",
			Password:        "SomeSecurePassword!",
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusNotFound, apiErr.StatusCode())
	})

	t.Run("Create", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
		numLogs := len(auditor.AuditLogs())

		firstUser := wirtualdtest.CreateFirstUser(t, client)
		numLogs++ // add an audit log for user create
		numLogs++ // add an audit log for login

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		user, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{firstUser.OrganizationID},
			Email:           "another@user.org",
			Username:        "someone-else",
			Password:        "SomeSecurePassword!",
		})
		require.NoError(t, err)

		// User should default to dormant.
		require.Equal(t, wirtualsdk.UserStatusDormant, user.Status)

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionCreate, auditor.AuditLogs()[numLogs-1].Action)
		require.Equal(t, database.AuditActionLogin, auditor.AuditLogs()[numLogs-2].Action)

		require.Len(t, user.OrganizationIDs, 1)
		assert.Equal(t, firstUser.OrganizationID, user.OrganizationIDs[0])
	})

	t.Run("CreateWithStatus", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
		numLogs := len(auditor.AuditLogs())

		firstUser := wirtualdtest.CreateFirstUser(t, client)
		numLogs++ // add an audit log for user create
		numLogs++ // add an audit log for login

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		user, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{firstUser.OrganizationID},
			Email:           "another@user.org",
			Username:        "someone-else",
			Password:        "SomeSecurePassword!",
			UserStatus:      ptr.Ref(wirtualsdk.UserStatusActive),
		})
		require.NoError(t, err)

		require.Equal(t, wirtualsdk.UserStatusActive, user.Status)

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionCreate, auditor.AuditLogs()[numLogs-1].Action)
		require.Equal(t, database.AuditActionLogin, auditor.AuditLogs()[numLogs-2].Action)

		require.Len(t, user.OrganizationIDs, 1)
		assert.Equal(t, firstUser.OrganizationID, user.OrganizationIDs[0])
	})

	t.Run("LastSeenAt", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		client := wirtualdtest.New(t, nil)
		firstUserResp := wirtualdtest.CreateFirstUser(t, client)

		firstUser, err := client.User(ctx, firstUserResp.UserID.String())
		require.NoError(t, err)

		_, _ = wirtualdtest.CreateAnotherUser(t, client, firstUserResp.OrganizationID)

		allUsersRes, err := client.Users(ctx, wirtualsdk.UsersRequest{})
		require.NoError(t, err)

		require.Len(t, allUsersRes.Users, 2)

		// We sent the "GET Users" request with the first user, but the second user
		// should be Never since they haven't performed a request.
		for _, user := range allUsersRes.Users {
			if user.ID == firstUser.ID {
				require.WithinDuration(t, firstUser.LastSeenAt, dbtime.Now(), testutil.WaitShort)
			} else {
				require.Zero(t, user.LastSeenAt)
			}
		}
	})

	t.Run("CreateNoneLoginType", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		first := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		user, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{first.OrganizationID},
			Email:           "another@user.org",
			Username:        "someone-else",
			Password:        "",
			UserLoginType:   wirtualsdk.LoginTypeNone,
		})
		require.NoError(t, err)

		found, err := client.User(ctx, user.ID.String())
		require.NoError(t, err)
		require.Equal(t, found.LoginType, wirtualsdk.LoginTypeNone)
	})

	t.Run("CreateOIDCLoginType", func(t *testing.T) {
		t.Parallel()
		email := "another@user.org"
		fake := oidctest.NewFakeIDP(t,
			oidctest.WithServing(),
		)
		cfg := fake.OIDCConfig(t, nil, func(cfg *wirtuald.OIDCConfig) {
			cfg.AllowSignups = true
		})

		client := wirtualdtest.New(t, &wirtualdtest.Options{
			OIDCConfig: cfg,
		})
		first := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{first.OrganizationID},
			Email:           email,
			Username:        "someone-else",
			Password:        "",
			UserLoginType:   wirtualsdk.LoginTypeOIDC,
		})
		require.NoError(t, err)

		// Try to log in with OIDC.
		userClient, _ := fake.Login(t, client, jwt.MapClaims{
			"email": email,
		})

		found, err := userClient.User(ctx, "me")
		require.NoError(t, err)
		require.Equal(t, found.LoginType, wirtualsdk.LoginTypeOIDC)
	})
}

func TestNotifyCreatedUser(t *testing.T) {
	t.Parallel()

	t.Run("OwnerNotified", func(t *testing.T) {
		t.Parallel()

		// given
		notifyEnq := &notificationstest.FakeEnqueuer{}
		adminClient := wirtualdtest.New(t, &wirtualdtest.Options{
			NotificationsEnqueuer: notifyEnq,
		})
		firstUser := wirtualdtest.CreateFirstUser(t, adminClient)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		// when
		user, err := adminClient.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{firstUser.OrganizationID},
			Email:           "another@user.org",
			Username:        "someone-else",
			Password:        "SomeSecurePassword!",
		})
		require.NoError(t, err)

		// then
		require.Len(t, notifyEnq.Sent(), 1)
		require.Equal(t, notifications.TemplateUserAccountCreated, notifyEnq.Sent()[0].TemplateID)
		require.Equal(t, firstUser.UserID, notifyEnq.Sent()[0].UserID)
		require.Contains(t, notifyEnq.Sent()[0].Targets, user.ID)
		require.Equal(t, user.Username, notifyEnq.Sent()[0].Labels["created_account_name"])
	})

	t.Run("UserAdminNotified", func(t *testing.T) {
		t.Parallel()

		// given
		notifyEnq := &notificationstest.FakeEnqueuer{}
		adminClient := wirtualdtest.New(t, &wirtualdtest.Options{
			NotificationsEnqueuer: notifyEnq,
		})
		firstUser := wirtualdtest.CreateFirstUser(t, adminClient)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		userAdmin, err := adminClient.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{firstUser.OrganizationID},
			Email:           "user-admin@user.org",
			Username:        "mr-user-admin",
			Password:        "SomeSecurePassword!",
		})
		require.NoError(t, err)

		_, err = adminClient.UpdateUserRoles(ctx, userAdmin.Username, wirtualsdk.UpdateRoles{
			Roles: []string{
				rbac.RoleUserAdmin().String(),
			},
		})
		require.NoError(t, err)

		// when
		member, err := adminClient.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			OrganizationIDs: []uuid.UUID{firstUser.OrganizationID},
			Email:           "another@user.org",
			Username:        "someone-else",
			Password:        "SomeSecurePassword!",
		})
		require.NoError(t, err)

		// then
		sent := notifyEnq.Sent()
		require.Len(t, sent, 3)

		// "User admin" account created, "owner" notified
		require.Equal(t, notifications.TemplateUserAccountCreated, sent[0].TemplateID)
		require.Equal(t, firstUser.UserID, sent[0].UserID)
		require.Contains(t, sent[0].Targets, userAdmin.ID)
		require.Equal(t, userAdmin.Username, sent[0].Labels["created_account_name"])

		// "Member" account created, "owner" notified
		require.Equal(t, notifications.TemplateUserAccountCreated, sent[1].TemplateID)
		require.Equal(t, firstUser.UserID, sent[1].UserID)
		require.Contains(t, sent[1].Targets, member.ID)
		require.Equal(t, member.Username, sent[1].Labels["created_account_name"])

		// "Member" account created, "user admin" notified
		require.Equal(t, notifications.TemplateUserAccountCreated, sent[1].TemplateID)
		require.Equal(t, userAdmin.ID, sent[2].UserID)
		require.Contains(t, sent[2].Targets, member.ID)
		require.Equal(t, member.Username, sent[2].Labels["created_account_name"])
	})
}

func TestUpdateUserProfile(t *testing.T) {
	t.Parallel()
	t.Run("UserNotFound", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, err := client.UpdateUserProfile(ctx, uuid.New().String(), wirtualsdk.UpdateUserProfileRequest{
			Username: "newusername",
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		// Right now, we are raising a BAD request error because we don't support a
		// user accessing other users info
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode())
	})

	t.Run("ConflictingUsername", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		existentUser, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           "bruno@coder.com",
			Username:        "bruno",
			Password:        "SomeSecurePassword!",
			OrganizationIDs: []uuid.UUID{user.OrganizationID},
		})
		require.NoError(t, err)
		_, err = client.UpdateUserProfile(ctx, wirtualsdk.Me, wirtualsdk.UpdateUserProfileRequest{
			Username: existentUser.Username,
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusConflict, apiErr.StatusCode())
	})

	t.Run("UpdateSelf", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
		numLogs := len(auditor.AuditLogs())

		wirtualdtest.CreateFirstUser(t, client)
		numLogs++ // add an audit log for login

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		me, err := client.User(ctx, wirtualsdk.Me)
		require.NoError(t, err)

		userProfile, err := client.UpdateUserProfile(ctx, wirtualsdk.Me, wirtualsdk.UpdateUserProfileRequest{
			Username: me.Username + "1",
			Name:     me.Name + "1",
		})
		numLogs++ // add an audit log for user update

		require.NoError(t, err)
		require.Equal(t, me.Username+"1", userProfile.Username)
		require.Equal(t, me.Name+"1", userProfile.Name)

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionWrite, auditor.AuditLogs()[numLogs-1].Action)
	})

	t.Run("UpdateSelfAsMember", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
		numLogs := len(auditor.AuditLogs())

		firstUser := wirtualdtest.CreateFirstUser(t, client)
		numLogs++ // add an audit log for login

		memberClient, memberUser := wirtualdtest.CreateAnotherUser(t, client, firstUser.OrganizationID)
		numLogs++ // add an audit log for user creation

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		userProfile, err := memberClient.UpdateUserProfile(ctx, wirtualsdk.Me, wirtualsdk.UpdateUserProfileRequest{
			Username: memberUser.Username + "1",
			Name:     memberUser.Name + "1",
		})
		numLogs++ // add an audit log for user update
		numLogs++ // add an audit log for API key creation

		require.NoError(t, err)
		require.Equal(t, memberUser.Username+"1", userProfile.Username)
		require.Equal(t, memberUser.Name+"1", userProfile.Name)

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionWrite, auditor.AuditLogs()[numLogs-1].Action)
	})

	t.Run("InvalidRealUserName", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           "john@coder.com",
			Username:        "john",
			Password:        "SomeSecurePassword!",
			OrganizationIDs: []uuid.UUID{user.OrganizationID},
		})
		require.NoError(t, err)
		_, err = client.UpdateUserProfile(ctx, wirtualsdk.Me, wirtualsdk.UpdateUserProfileRequest{
			Name: " Mr Bean", // must not have leading space
		})
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode())
	})
}

func TestUpdateUserPassword(t *testing.T) {
	t.Parallel()

	t.Run("MemberCantUpdateAdminPassword", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		owner := wirtualdtest.CreateFirstUser(t, client)
		member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		err := member.UpdateUserPassword(ctx, owner.UserID.String(), wirtualsdk.UpdateUserPasswordRequest{
			Password: "newpassword",
		})
		require.Error(t, err, "member should not be able to update admin password")
	})

	t.Run("AdminCanUpdateMemberPassword", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		owner := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		member, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           "coder@coder.com",
			Username:        "coder",
			Password:        "SomeStrongPassword!",
			OrganizationIDs: []uuid.UUID{owner.OrganizationID},
		})
		require.NoError(t, err, "create member")
		err = client.UpdateUserPassword(ctx, member.ID.String(), wirtualsdk.UpdateUserPasswordRequest{
			Password: "SomeNewStrongPassword!",
		})
		require.NoError(t, err, "admin should be able to update member password")
		// Check if the member can login using the new password
		_, err = client.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
			Email:    "coder@coder.com",
			Password: "SomeNewStrongPassword!",
		})
		require.NoError(t, err, "member should login successfully with the new password")
	})

	t.Run("AuditorCantUpdateOtherUserPassword", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		owner := wirtualdtest.CreateFirstUser(t, client)

		auditor, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.RoleAuditor())

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		member, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           "coder@coder.com",
			Username:        "coder",
			Password:        "SomeStrongPassword!",
			OrganizationIDs: []uuid.UUID{owner.OrganizationID},
		})
		require.NoError(t, err, "create member")

		err = auditor.UpdateUserPassword(ctx, member.ID.String(), wirtualsdk.UpdateUserPasswordRequest{
			Password: "SomeNewStrongPassword!",
		})
		require.Error(t, err, "auditor should not be able to update member password")
		require.ErrorContains(t, err, "unexpected status code 404: Resource not found or you do not have access to this resource")
	})

	t.Run("MemberCanUpdateOwnPassword", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
		numLogs := len(auditor.AuditLogs())

		owner := wirtualdtest.CreateFirstUser(t, client)
		numLogs++ // add an audit log for user create
		numLogs++ // add an audit log for login

		member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
		numLogs++ // add an audit log for user create

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		err := member.UpdateUserPassword(ctx, "me", wirtualsdk.UpdateUserPasswordRequest{
			OldPassword: "SomeSecurePassword!",
			Password:    "MyNewSecurePassword!",
		})
		numLogs++ // add an audit log for user update

		require.NoError(t, err, "member should be able to update own password")

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionWrite, auditor.AuditLogs()[numLogs-1].Action)
	})

	t.Run("MemberCantUpdateOwnPasswordWithoutOldPassword", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		owner := wirtualdtest.CreateFirstUser(t, client)
		member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		err := member.UpdateUserPassword(ctx, "me", wirtualsdk.UpdateUserPasswordRequest{
			Password: "newpassword",
		})
		require.Error(t, err, "member should not be able to update own password without providing old password")
		require.ErrorContains(t, err, "Old password is required.")
	})

	t.Run("AuditorCantTellIfPasswordIncorrect", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		adminClient := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})

		adminUser := wirtualdtest.CreateFirstUser(t, adminClient)

		auditorClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient,
			adminUser.OrganizationID,
			rbac.RoleAuditor(),
		)

		_, memberUser := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID)
		numLogs := len(auditor.AuditLogs())

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		err := auditorClient.UpdateUserPassword(ctx, memberUser.ID.String(), wirtualsdk.UpdateUserPasswordRequest{
			Password: "MySecurePassword!",
		})
		numLogs++ // add an audit log for user update

		require.Error(t, err, "auditors shouldn't be able to update passwords")
		var httpErr *wirtualsdk.Error
		require.True(t, xerrors.As(err, &httpErr))
		// ensure that the error we get is "not found" and not "bad request"
		require.Equal(t, http.StatusNotFound, httpErr.StatusCode())

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionWrite, auditor.AuditLogs()[numLogs-1].Action)
		require.Equal(t, int32(http.StatusNotFound), auditor.AuditLogs()[numLogs-1].StatusCode)
	})

	t.Run("AdminCantUpdateOwnPasswordWithoutOldPassword", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
		numLogs := len(auditor.AuditLogs())

		_ = wirtualdtest.CreateFirstUser(t, client)
		numLogs++ // add an audit log for login

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		err := client.UpdateUserPassword(ctx, "me", wirtualsdk.UpdateUserPasswordRequest{
			Password: "MySecurePassword!",
		})
		numLogs++ // add an audit log for user update

		require.Error(t, err, "admin should not be able to update own password without providing old password")
		require.ErrorContains(t, err, "Old password is required.")

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionWrite, auditor.AuditLogs()[numLogs-1].Action)
	})

	t.Run("ValidateUserPassword", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})

		_ = wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		resp, err := client.ValidateUserPassword(ctx, wirtualsdk.ValidateUserPasswordRequest{
			Password: "MySecurePassword!",
		})

		require.NoError(t, err, "users shoud be able to validate complexity of a potential new password")
		require.True(t, resp.Valid)
	})

	t.Run("ChangingPasswordDeletesKeys", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		ctx := testutil.Context(t, testutil.WaitLong)

		apikey1, err := client.CreateToken(ctx, user.UserID.String(), wirtualsdk.CreateTokenRequest{})
		require.NoError(t, err)

		apikey2, err := client.CreateToken(ctx, user.UserID.String(), wirtualsdk.CreateTokenRequest{})
		require.NoError(t, err)

		err = client.UpdateUserPassword(ctx, "me", wirtualsdk.UpdateUserPasswordRequest{
			OldPassword: "SomeSecurePassword!",
			Password:    "MyNewSecurePassword!",
		})
		require.NoError(t, err)

		// Trying to get an API key should fail since our client's token
		// has been deleted.
		_, err = client.APIKeyByID(ctx, user.UserID.String(), apikey1.Key)
		require.Error(t, err)
		cerr := wirtualdtest.SDKError(t, err)
		require.Equal(t, http.StatusUnauthorized, cerr.StatusCode())

		resp, err := client.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
			Email:    wirtualdtest.FirstUserParams.Email,
			Password: "MyNewSecurePassword!",
		})
		require.NoError(t, err)

		client.SetSessionToken(resp.SessionToken)

		// Trying to get an API key should fail since all keys are deleted
		// on password change.
		_, err = client.APIKeyByID(ctx, user.UserID.String(), apikey1.Key)
		require.Error(t, err)
		cerr = wirtualdtest.SDKError(t, err)
		require.Equal(t, http.StatusNotFound, cerr.StatusCode())

		_, err = client.APIKeyByID(ctx, user.UserID.String(), apikey2.Key)
		require.Error(t, err)
		cerr = wirtualdtest.SDKError(t, err)
		require.Equal(t, http.StatusNotFound, cerr.StatusCode())
	})

	t.Run("PasswordsMustDiffer", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, nil)
		_ = wirtualdtest.CreateFirstUser(t, client)
		ctx := testutil.Context(t, testutil.WaitLong)

		err := client.UpdateUserPassword(ctx, "me", wirtualsdk.UpdateUserPasswordRequest{
			Password: wirtualdtest.FirstUserParams.Password,
		})
		require.Error(t, err)
		cerr := wirtualdtest.SDKError(t, err)
		require.Equal(t, http.StatusBadRequest, cerr.StatusCode())
	})
}

// TestInitialRoles ensures the starting roles for the first user are correct.
func TestInitialRoles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := wirtualdtest.New(t, nil)
	first := wirtualdtest.CreateFirstUser(t, client)

	roles, err := client.UserRoles(ctx, wirtualsdk.Me)
	require.NoError(t, err)
	require.ElementsMatch(t, roles.Roles, []string{
		wirtualsdk.RoleOwner,
	}, "should be a member and admin")

	require.ElementsMatch(t, roles.OrganizationRoles[first.OrganizationID], []string{}, "should be a member")
}

func TestPutUserSuspend(t *testing.T) {
	t.Parallel()

	t.Run("SuspendAnOwner", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		me := wirtualdtest.CreateFirstUser(t, client)
		_, user := wirtualdtest.CreateAnotherUser(t, client, me.OrganizationID, rbac.RoleOwner())

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, err := client.UpdateUserStatus(ctx, user.Username, wirtualsdk.UserStatusSuspended)
		require.Error(t, err, "cannot suspend owners")
	})

	t.Run("SuspendAnotherUser", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
		numLogs := len(auditor.AuditLogs())

		me := wirtualdtest.CreateFirstUser(t, client)
		numLogs++ // add an audit log for user create
		numLogs++ // add an audit log for login

		_, user := wirtualdtest.CreateAnotherUser(t, client, me.OrganizationID)
		numLogs++ // add an audit log for user create

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		user, err := client.UpdateUserStatus(ctx, user.Username, wirtualsdk.UserStatusSuspended)
		require.NoError(t, err)
		require.Equal(t, user.Status, wirtualsdk.UserStatusSuspended)
		numLogs++ // add an audit log for user update

		require.Len(t, auditor.AuditLogs(), numLogs)
		require.Equal(t, database.AuditActionWrite, auditor.AuditLogs()[numLogs-1].Action)
	})

	t.Run("SuspendItSelf", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		client.User(ctx, wirtualsdk.Me)
		_, err := client.UpdateUserStatus(ctx, wirtualsdk.Me, wirtualsdk.UserStatusSuspended)

		require.ErrorContains(t, err, "suspend yourself", "cannot suspend yourself")
	})
}

func TestActivateDormantUser(t *testing.T) {
	t.Parallel()
	client := wirtualdtest.New(t, nil)

	// Create users
	me := wirtualdtest.CreateFirstUser(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()
	anotherUser, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
		Email:           "coder@coder.com",
		Username:        "coder",
		Password:        "SomeStrongPassword!",
		OrganizationIDs: []uuid.UUID{me.OrganizationID},
	})
	require.NoError(t, err)

	// Ensure that new user has dormant account
	require.Equal(t, wirtualsdk.UserStatusDormant, anotherUser.Status)

	// Activate user account
	_, err = client.UpdateUserStatus(ctx, anotherUser.Username, wirtualsdk.UserStatusActive)
	require.NoError(t, err)

	// Verify if the account is active now
	anotherUser, err = client.User(ctx, anotherUser.Username)
	require.NoError(t, err)
	require.Equal(t, wirtualsdk.UserStatusActive, anotherUser.Status)
}

func TestGetUser(t *testing.T) {
	t.Parallel()

	t.Run("ByMe", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, nil)
		firstUser := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		user, err := client.User(ctx, wirtualsdk.Me)
		require.NoError(t, err)
		require.Equal(t, firstUser.UserID, user.ID)
		require.Equal(t, firstUser.OrganizationID, user.OrganizationIDs[0])
	})

	t.Run("ByID", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, nil)
		firstUser := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		user, err := client.User(ctx, firstUser.UserID.String())
		require.NoError(t, err)
		require.Equal(t, firstUser.UserID, user.ID)
		require.Equal(t, firstUser.OrganizationID, user.OrganizationIDs[0])
	})

	t.Run("ByUsername", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, nil)
		firstUser := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		exp, err := client.User(ctx, firstUser.UserID.String())
		require.NoError(t, err)

		user, err := client.User(ctx, exp.Username)
		require.NoError(t, err)
		require.Equal(t, exp, user)
	})
}

// TestUsersFilter creates a set of users to run various filters against for testing.
func TestUsersFilter(t *testing.T) {
	t.Parallel()

	client, _, api := wirtualdtest.NewWithAPI(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
	first := wirtualdtest.CreateFirstUser(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	t.Cleanup(cancel)

	firstUser, err := client.User(ctx, wirtualsdk.Me)
	require.NoError(t, err, "fetch me")

	// Noon on Jan 18 is the "now" for this test for last_seen timestamps.
	// All these values are equal
	// 2023-01-18T12:00:00Z (UTC)
	// 2023-01-18T07:00:00-05:00 (America/New_York)
	// 2023-01-18T13:00:00+01:00 (Europe/Madrid)
	// 2023-01-16T00:00:00+12:00 (Asia/Anadyr)
	lastSeenNow := time.Date(2023, 1, 18, 12, 0, 0, 0, time.UTC)
	users := make([]wirtualsdk.User, 0)
	users = append(users, firstUser)
	for i := 0; i < 15; i++ {
		roles := []rbac.RoleIdentifier{}
		if i%2 == 0 {
			roles = append(roles, rbac.RoleTemplateAdmin(), rbac.RoleUserAdmin())
		}
		if i%3 == 0 {
			roles = append(roles, rbac.RoleAuditor())
		}
		userClient, userData := wirtualdtest.CreateAnotherUser(t, client, first.OrganizationID, roles...)
		// Set the last seen for each user to a unique day
		// nolint:gocritic // Unit test
		_, err := api.Database.UpdateUserLastSeenAt(dbauthz.AsSystemRestricted(ctx), database.UpdateUserLastSeenAtParams{
			ID:         userData.ID,
			LastSeenAt: lastSeenNow.Add(-1 * time.Hour * 24 * time.Duration(i)),
			UpdatedAt:  time.Now(),
		})
		require.NoError(t, err, "set a last seen")

		user, err := userClient.User(ctx, wirtualsdk.Me)
		require.NoError(t, err, "fetch me")

		if i%4 == 0 {
			user, err = client.UpdateUserStatus(ctx, user.ID.String(), wirtualsdk.UserStatusSuspended)
			require.NoError(t, err, "suspend user")
		}

		if i%5 == 0 {
			user, err = client.UpdateUserProfile(ctx, user.ID.String(), wirtualsdk.UpdateUserProfileRequest{
				Username: strings.ToUpper(user.Username),
			})
			require.NoError(t, err, "update username to uppercase")
		}

		users = append(users, user)
	}

	// --- Setup done ---
	testCases := []struct {
		Name   string
		Filter wirtualsdk.UsersRequest
		// If FilterF is true, we include it in the expected results
		FilterF func(f wirtualsdk.UsersRequest, user wirtualsdk.User) bool
	}{
		{
			Name: "All",
			Filter: wirtualsdk.UsersRequest{
				Status: wirtualsdk.UserStatusSuspended + "," + wirtualsdk.UserStatusActive,
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				return true
			},
		},
		{
			Name: "Active",
			Filter: wirtualsdk.UsersRequest{
				Status: wirtualsdk.UserStatusActive,
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				return u.Status == wirtualsdk.UserStatusActive
			},
		},
		{
			Name: "ActiveUppercase",
			Filter: wirtualsdk.UsersRequest{
				Status: "ACTIVE",
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				return u.Status == wirtualsdk.UserStatusActive
			},
		},
		{
			Name: "Suspended",
			Filter: wirtualsdk.UsersRequest{
				Status: wirtualsdk.UserStatusSuspended,
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				return u.Status == wirtualsdk.UserStatusSuspended
			},
		},
		{
			Name: "NameContains",
			Filter: wirtualsdk.UsersRequest{
				Search: "a",
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				return (strings.ContainsAny(u.Username, "aA") || strings.ContainsAny(u.Email, "aA"))
			},
		},
		{
			Name: "Admins",
			Filter: wirtualsdk.UsersRequest{
				Role:   wirtualsdk.RoleOwner,
				Status: wirtualsdk.UserStatusSuspended + "," + wirtualsdk.UserStatusActive,
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				for _, r := range u.Roles {
					if r.Name == wirtualsdk.RoleOwner {
						return true
					}
				}
				return false
			},
		},
		{
			Name: "AdminsUppercase",
			Filter: wirtualsdk.UsersRequest{
				Role:   "OWNER",
				Status: wirtualsdk.UserStatusSuspended + "," + wirtualsdk.UserStatusActive,
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				for _, r := range u.Roles {
					if r.Name == wirtualsdk.RoleOwner {
						return true
					}
				}
				return false
			},
		},
		{
			Name: "Members",
			Filter: wirtualsdk.UsersRequest{
				Role:   wirtualsdk.RoleMember,
				Status: wirtualsdk.UserStatusSuspended + "," + wirtualsdk.UserStatusActive,
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				return true
			},
		},
		{
			Name: "SearchQuery",
			Filter: wirtualsdk.UsersRequest{
				SearchQuery: "i role:owner status:active",
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				for _, r := range u.Roles {
					if r.Name == wirtualsdk.RoleOwner {
						return (strings.ContainsAny(u.Username, "iI") || strings.ContainsAny(u.Email, "iI")) &&
							u.Status == wirtualsdk.UserStatusActive
					}
				}
				return false
			},
		},
		{
			Name: "SearchQueryInsensitive",
			Filter: wirtualsdk.UsersRequest{
				SearchQuery: "i Role:Owner STATUS:Active",
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				for _, r := range u.Roles {
					if r.Name == wirtualsdk.RoleOwner {
						return (strings.ContainsAny(u.Username, "iI") || strings.ContainsAny(u.Email, "iI")) &&
							u.Status == wirtualsdk.UserStatusActive
					}
				}
				return false
			},
		},
		{
			Name: "LastSeenBeforeNow",
			Filter: wirtualsdk.UsersRequest{
				SearchQuery: `last_seen_before:"2023-01-16T00:00:00+12:00"`,
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				return u.LastSeenAt.Before(lastSeenNow)
			},
		},
		{
			Name: "LastSeenLastWeek",
			Filter: wirtualsdk.UsersRequest{
				SearchQuery: `last_seen_before:"2023-01-14T23:59:59Z" last_seen_after:"2023-01-08T00:00:00Z"`,
			},
			FilterF: func(_ wirtualsdk.UsersRequest, u wirtualsdk.User) bool {
				start := time.Date(2023, 1, 8, 0, 0, 0, 0, time.UTC)
				end := time.Date(2023, 1, 14, 23, 59, 59, 0, time.UTC)
				return u.LastSeenAt.Before(end) && u.LastSeenAt.After(start)
			},
		},
	}

	for _, c := range testCases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			matched, err := client.Users(ctx, c.Filter)
			require.NoError(t, err, "fetch workspaces")

			exp := make([]wirtualsdk.User, 0)
			for _, made := range users {
				match := c.FilterF(c.Filter, made)
				if match {
					exp = append(exp, made)
				}
			}
			require.ElementsMatch(t, exp, matched.Users, "expected users returned")
		})
	}
}

func TestGetUsers(t *testing.T) {
	t.Parallel()
	t.Run("AllUsers", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           "alice@email.com",
			Username:        "alice",
			Password:        "MySecurePassword!",
			OrganizationIDs: []uuid.UUID{user.OrganizationID},
		})
		// No params is all users
		res, err := client.Users(ctx, wirtualsdk.UsersRequest{})
		require.NoError(t, err)
		require.Len(t, res.Users, 2)
		require.Len(t, res.Users[0].OrganizationIDs, 1)
	})
	t.Run("ActiveUsers", func(t *testing.T) {
		t.Parallel()
		active := make([]wirtualsdk.User, 0)
		client := wirtualdtest.New(t, nil)
		first := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		firstUser, err := client.User(ctx, first.UserID.String())
		require.NoError(t, err, "")
		active = append(active, firstUser)

		// Alice will be suspended
		alice, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           "alice@email.com",
			Username:        "alice",
			Password:        "MySecurePassword!",
			OrganizationIDs: []uuid.UUID{first.OrganizationID},
		})
		require.NoError(t, err)

		_, err = client.UpdateUserStatus(ctx, alice.Username, wirtualsdk.UserStatusSuspended)
		require.NoError(t, err)

		// Tom will be active
		tom, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           "tom@email.com",
			Username:        "tom",
			Password:        "MySecurePassword!",
			OrganizationIDs: []uuid.UUID{first.OrganizationID},
		})
		require.NoError(t, err)

		tom, err = client.UpdateUserStatus(ctx, tom.Username, wirtualsdk.UserStatusActive)
		require.NoError(t, err)
		active = append(active, tom)

		res, err := client.Users(ctx, wirtualsdk.UsersRequest{
			Status: wirtualsdk.UserStatusActive,
		})
		require.NoError(t, err)
		require.ElementsMatch(t, active, res.Users)
	})
}

func TestGetUsersPagination(t *testing.T) {
	t.Parallel()
	client := wirtualdtest.New(t, nil)
	first := wirtualdtest.CreateFirstUser(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	_, err := client.User(ctx, first.UserID.String())
	require.NoError(t, err, "")

	_, err = client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
		Email:           "alice@email.com",
		Username:        "alice",
		Password:        "MySecurePassword!",
		OrganizationIDs: []uuid.UUID{first.OrganizationID},
	})
	require.NoError(t, err)

	res, err := client.Users(ctx, wirtualsdk.UsersRequest{})
	require.NoError(t, err)
	require.Len(t, res.Users, 2)
	require.Equal(t, res.Count, 2)

	res, err = client.Users(ctx, wirtualsdk.UsersRequest{
		Pagination: wirtualsdk.Pagination{
			Limit: 1,
		},
	})
	require.NoError(t, err)
	require.Len(t, res.Users, 1)
	require.Equal(t, res.Count, 2)

	res, err = client.Users(ctx, wirtualsdk.UsersRequest{
		Pagination: wirtualsdk.Pagination{
			Offset: 1,
		},
	})
	require.NoError(t, err)
	require.Len(t, res.Users, 1)
	require.Equal(t, res.Count, 2)

	// if offset is higher than the count postgres returns an empty array
	// and not an ErrNoRows error.
	res, err = client.Users(ctx, wirtualsdk.UsersRequest{
		Pagination: wirtualsdk.Pagination{
			Offset: 3,
		},
	})
	require.NoError(t, err)
	require.Len(t, res.Users, 0)
	require.Equal(t, res.Count, 0)
}

func TestPostTokens(t *testing.T) {
	t.Parallel()
	client := wirtualdtest.New(t, nil)
	_ = wirtualdtest.CreateFirstUser(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	apiKey, err := client.CreateToken(ctx, wirtualsdk.Me, wirtualsdk.CreateTokenRequest{})
	require.NotNil(t, apiKey)
	require.GreaterOrEqual(t, len(apiKey.Key), 2)
	require.NoError(t, err)
}

func TestWorkspacesByUser(t *testing.T) {
	t.Parallel()
	t.Run("Empty", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		_ = wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		res, err := client.Workspaces(ctx, wirtualsdk.WorkspaceFilter{
			Owner: wirtualsdk.Me,
		})
		require.NoError(t, err)
		require.Len(t, res.Workspaces, 0)
	})
	t.Run("Access", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		user := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		newUser, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           "test@coder.com",
			Username:        "someone",
			Password:        "MySecurePassword!",
			OrganizationIDs: []uuid.UUID{user.OrganizationID},
		})
		require.NoError(t, err)
		auth, err := client.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
			Email:    newUser.Email,
			Password: "MySecurePassword!",
		})
		require.NoError(t, err)

		newUserClient := wirtualsdk.New(client.URL)
		newUserClient.SetSessionToken(auth.SessionToken)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		wirtualdtest.CreateWorkspace(t, client, template.ID)

		res, err := newUserClient.Workspaces(ctx, wirtualsdk.WorkspaceFilter{Owner: wirtualsdk.Me})
		require.NoError(t, err)
		require.Len(t, res.Workspaces, 0)

		res, err = client.Workspaces(ctx, wirtualsdk.WorkspaceFilter{Owner: wirtualsdk.Me})
		require.NoError(t, err)
		require.Len(t, res.Workspaces, 1)
	})
}

func TestDormantUser(t *testing.T) {
	t.Parallel()

	client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
	user := wirtualdtest.CreateFirstUser(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	// Create a new user
	newUser, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
		Email:           "test@coder.com",
		Username:        "someone",
		Password:        "MySecurePassword!",
		OrganizationIDs: []uuid.UUID{user.OrganizationID},
	})
	require.NoError(t, err)

	// User should be dormant as they haven't logged in yet
	users, err := client.Users(ctx, wirtualsdk.UsersRequest{Search: newUser.Username})
	require.NoError(t, err)
	require.Len(t, users.Users, 1)
	require.Equal(t, wirtualsdk.UserStatusDormant, users.Users[0].Status)

	// User logs in now
	_, err = client.LoginWithPassword(ctx, wirtualsdk.LoginWithPasswordRequest{
		Email:    newUser.Email,
		Password: "MySecurePassword!",
	})
	require.NoError(t, err)

	// User status should be active now
	users, err = client.Users(ctx, wirtualsdk.UsersRequest{Search: newUser.Username})
	require.NoError(t, err)
	require.Len(t, users.Users, 1)
	require.Equal(t, wirtualsdk.UserStatusActive, users.Users[0].Status)
}

// TestSuspendedPagination is when the after_id is a suspended record.
// The database query should still return the correct page, as the after_id
// is in a subquery that finds the record regardless of its status.
// This is mainly to confirm the db fake has the same behavior.
func TestSuspendedPagination(t *testing.T) {
	t.Parallel()
	t.Skip("This fails when two users are created at the exact same time. The reason is unknown... See: https://github.com/coder/coder/actions/runs/3057047622/jobs/4931863163")
	client := wirtualdtest.New(t, nil)
	wirtualdtest.CreateFirstUser(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	t.Cleanup(cancel)

	me, err := client.User(ctx, wirtualsdk.Me)
	require.NoError(t, err)
	orgID := me.OrganizationIDs[0]

	total := 10
	users := make([]wirtualsdk.User, 0, total)
	// Create users
	for i := 0; i < total; i++ {
		email := fmt.Sprintf("%d@coder.com", i)
		username := fmt.Sprintf("user%d", i)
		user, err := client.CreateUserWithOrgs(ctx, wirtualsdk.CreateUserRequestWithOrgs{
			Email:           email,
			Username:        username,
			Password:        "MySecurePassword!",
			OrganizationIDs: []uuid.UUID{orgID},
		})
		require.NoError(t, err)
		users = append(users, user)
	}
	sortUsers(users)
	deletedUser := users[2]
	expected := users[3:8]
	_, err = client.UpdateUserStatus(ctx, deletedUser.ID.String(), wirtualsdk.UserStatusSuspended)
	require.NoError(t, err, "suspend user")

	page, err := client.Users(ctx, wirtualsdk.UsersRequest{
		Pagination: wirtualsdk.Pagination{
			Limit:   len(expected),
			AfterID: deletedUser.ID,
		},
	})
	require.NoError(t, err)
	require.Equal(t, expected, page.Users, "expected page")
}

func TestUserAutofillParameters(t *testing.T) {
	t.Parallel()
	t.Run("NotSelf", func(t *testing.T) {
		t.Parallel()
		client1, _, api := wirtualdtest.NewWithAPI(t, &wirtualdtest.Options{})

		u1 := wirtualdtest.CreateFirstUser(t, client1)

		client2, u2 := wirtualdtest.CreateAnotherUser(t, client1, u1.OrganizationID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		db := api.Database

		version := dbfake.TemplateVersion(t, db).Seed(database.TemplateVersion{
			CreatedBy:      u1.UserID,
			OrganizationID: u1.OrganizationID,
		}).Params(database.TemplateVersionParameter{
			Name:     "param",
			Required: true,
		}).Do()

		_, err := client2.UserAutofillParameters(
			ctx,
			u1.UserID.String(),
			version.Template.ID,
		)

		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode())

		// u1 should be able to read u2's parameters as u1 is site admin.
		_, err = client1.UserAutofillParameters(
			ctx,
			u2.ID.String(),
			version.Template.ID,
		)
		require.NoError(t, err)
	})

	t.Run("FindsParameters", func(t *testing.T) {
		t.Parallel()
		client1, _, api := wirtualdtest.NewWithAPI(t, &wirtualdtest.Options{})

		u1 := wirtualdtest.CreateFirstUser(t, client1)

		client2, u2 := wirtualdtest.CreateAnotherUser(t, client1, u1.OrganizationID)

		db := api.Database

		version := dbfake.TemplateVersion(t, db).Seed(database.TemplateVersion{
			CreatedBy:      u1.UserID,
			OrganizationID: u1.OrganizationID,
		}).Params(database.TemplateVersionParameter{
			Name:     "param",
			Required: true,
		},
			database.TemplateVersionParameter{
				Name:      "param2",
				Ephemeral: true,
			},
		).Do()

		dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OwnerID:        u2.ID,
			TemplateID:     version.Template.ID,
			OrganizationID: u1.OrganizationID,
		}).Params(
			database.WorkspaceBuildParameter{
				Name:  "param",
				Value: "foo",
			},
			database.WorkspaceBuildParameter{
				Name:  "param2",
				Value: "bar",
			},
		).Do()

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		// Use client2 since client1 is site admin, so
		// we don't get good coverage on RBAC working.
		params, err := client2.UserAutofillParameters(
			ctx,
			u2.ID.String(),
			version.Template.ID,
		)
		require.NoError(t, err)

		require.Equal(t, 1, len(params))

		require.Equal(t, "param", params[0].Name)
		require.Equal(t, "foo", params[0].Value)

		// Verify that latest parameter value is returned.
		dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OrganizationID: u1.OrganizationID,
			OwnerID:        u2.ID,
			TemplateID:     version.Template.ID,
		}).Params(
			database.WorkspaceBuildParameter{
				Name:  "param",
				Value: "foo_new",
			},
		).Do()

		params, err = client2.UserAutofillParameters(
			ctx,
			u2.ID.String(),
			version.Template.ID,
		)
		require.NoError(t, err)

		require.Equal(t, 1, len(params))

		require.Equal(t, "param", params[0].Name)
		require.Equal(t, "foo_new", params[0].Value)
	})
}

// TestPaginatedUsers creates a list of users, then tries to paginate through
// them using different page sizes.
func TestPaginatedUsers(t *testing.T) {
	t.Parallel()
	client, db := wirtualdtest.NewWithDatabase(t, nil)
	wirtualdtest.CreateFirstUser(t, client)

	// This test takes longer than a long time.
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong*4)
	t.Cleanup(cancel)

	me, err := client.User(ctx, wirtualsdk.Me)
	require.NoError(t, err)

	// When 50 users exist
	total := 50
	allUsers := make([]database.User, total+1)
	allUsers[0] = database.User{
		Email:    me.Email,
		Username: me.Username,
	}
	specialUsers := make([]database.User, total/2)

	eg, _ := errgroup.WithContext(ctx)
	// Create users
	for i := 0; i < total; i++ {
		i := i
		eg.Go(func() error {
			email := fmt.Sprintf("%d@coder.com", i)
			username := fmt.Sprintf("user%d", i)
			if i%2 == 0 {
				email = fmt.Sprintf("%d@gmail.com", i)
				username = fmt.Sprintf("specialuser%d", i)
			}
			if i%3 == 0 {
				username = strings.ToUpper(username)
			}

			// We used to use the API to ceate users, but that is slow.
			// Instead, we create them directly in the database now
			// to prevent timeout flakes.
			newUser := dbgen.User(t, db, database.User{
				Email:    email,
				Username: username,
			})
			allUsers[i+1] = newUser
			if i%2 == 0 {
				specialUsers[i/2] = newUser
			}

			return nil
		})
	}
	err = eg.Wait()
	require.NoError(t, err, "create users failed")

	// Sorting the users will sort by username.
	sortDatabaseUsers(allUsers)
	sortDatabaseUsers(specialUsers)

	gmailSearch := func(request wirtualsdk.UsersRequest) wirtualsdk.UsersRequest {
		request.Search = "gmail"
		return request
	}
	usernameSearch := func(request wirtualsdk.UsersRequest) wirtualsdk.UsersRequest {
		request.Search = "specialuser"
		return request
	}

	tests := []struct {
		name     string
		limit    int
		allUsers []database.User
		opt      func(request wirtualsdk.UsersRequest) wirtualsdk.UsersRequest
	}{
		{name: "all users", limit: 10, allUsers: allUsers},
		{name: "all users", limit: 5, allUsers: allUsers},
		{name: "all users", limit: 3, allUsers: allUsers},
		{name: "gmail search", limit: 3, allUsers: specialUsers, opt: gmailSearch},
		{name: "gmail search", limit: 7, allUsers: specialUsers, opt: gmailSearch},
		{name: "username search", limit: 3, allUsers: specialUsers, opt: usernameSearch},
		{name: "username search", limit: 3, allUsers: specialUsers, opt: usernameSearch},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(fmt.Sprintf("%s %d", tt.name, tt.limit), func(t *testing.T) {
			t.Parallel()

			// This test takes longer than a long time.
			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong*2)
			defer cancel()

			assertPagination(ctx, t, client, tt.limit, tt.allUsers, tt.opt)
		})
	}
}

// Assert pagination will page through the list of all users using the given
// limit for each page. The 'allUsers' is the expected full list to compare
// against.
func assertPagination(ctx context.Context, t *testing.T, client *wirtualsdk.Client, limit int, allUsers []database.User,
	opt func(request wirtualsdk.UsersRequest) wirtualsdk.UsersRequest,
) {
	var count int
	if opt == nil {
		opt = func(request wirtualsdk.UsersRequest) wirtualsdk.UsersRequest {
			return request
		}
	}

	// Check the first page
	page, err := client.Users(ctx, opt(wirtualsdk.UsersRequest{
		Pagination: wirtualsdk.Pagination{
			Limit: limit,
		},
	}))
	require.NoError(t, err, "first page")
	require.Equalf(t, onlyUsernames(page.Users), onlyUsernames(allUsers[:limit]), "first page, limit=%d", limit)
	count += len(page.Users)

	for {
		if len(page.Users) == 0 {
			break
		}

		afterCursor := page.Users[len(page.Users)-1].ID
		// Assert each page is the next expected page
		// This is using a cursor, and only works if all users created_at
		// is unique.
		page, err = client.Users(ctx, opt(wirtualsdk.UsersRequest{
			Pagination: wirtualsdk.Pagination{
				Limit:   limit,
				AfterID: afterCursor,
			},
		}))
		require.NoError(t, err, "next cursor page")

		// Also check page by offset
		offsetPage, err := client.Users(ctx, opt(wirtualsdk.UsersRequest{
			Pagination: wirtualsdk.Pagination{
				Limit:  limit,
				Offset: count,
			},
		}))
		require.NoError(t, err, "next offset page")

		var expected []database.User
		if count+limit > len(allUsers) {
			expected = allUsers[count:]
		} else {
			expected = allUsers[count : count+limit]
		}
		require.Equalf(t, onlyUsernames(page.Users), onlyUsernames(expected), "next users, after=%s, limit=%d", afterCursor, limit)
		require.Equalf(t, onlyUsernames(offsetPage.Users), onlyUsernames(expected), "offset users, offset=%d, limit=%d", count, limit)

		// Also check the before
		prevPage, err := client.Users(ctx, opt(wirtualsdk.UsersRequest{
			Pagination: wirtualsdk.Pagination{
				Offset: count - limit,
				Limit:  limit,
			},
		}))
		require.NoError(t, err, "prev page")
		require.Equal(t, onlyUsernames(allUsers[count-limit:count]), onlyUsernames(prevPage.Users), "prev users")
		count += len(page.Users)
	}
}

// sortUsers sorts by (created_at, id)
func sortUsers(users []wirtualsdk.User) {
	slices.SortFunc(users, func(a, b wirtualsdk.User) int {
		return slice.Ascending(strings.ToLower(a.Username), strings.ToLower(b.Username))
	})
}

func sortDatabaseUsers(users []database.User) {
	slices.SortFunc(users, func(a, b database.User) int {
		return slice.Ascending(strings.ToLower(a.Username), strings.ToLower(b.Username))
	})
}

func onlyUsernames[U wirtualsdk.User | database.User](users []U) []string {
	var out []string
	for _, u := range users {
		switch u := (any(u)).(type) {
		case wirtualsdk.User:
			out = append(out, u.Username)
		case database.User:
			out = append(out, u.Username)
		}
	}
	return out
}

func BenchmarkUsersMe(b *testing.B) {
	client := wirtualdtest.New(b, nil)
	_ = wirtualdtest.CreateFirstUser(b, client)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.User(ctx, wirtualsdk.Me)
		require.NoError(b, err)
	}
}
