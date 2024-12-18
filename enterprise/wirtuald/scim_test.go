package wirtuald_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v4"
	"github.com/imulab/go-scim/pkg/v2/handlerutil"
	"github.com/imulab/go-scim/pkg/v2/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/onchainengineering/hmi-wirtual/cryptorand"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/scim"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/audit"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/notifications/notificationstest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest/oidctest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

//nolint:revive
func makeScimUser(t testing.TB) wirtuald.SCIMUser {
	rstr, err := cryptorand.String(10)
	require.NoError(t, err)

	return wirtuald.SCIMUser{
		UserName: rstr,
		Name: struct {
			GivenName  string "json:\"givenName\""
			FamilyName string "json:\"familyName\""
		}{
			GivenName:  rstr,
			FamilyName: rstr,
		},
		Emails: []struct {
			Primary bool   "json:\"primary\""
			Value   string "json:\"value\" format:\"email\""
			Type    string "json:\"type\""
			Display string "json:\"display\""
		}{
			{Primary: true, Value: fmt.Sprintf("%s@coder.com", rstr)},
		},
		Active: true,
	}
}

func setScimAuth(key []byte) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set("Authorization", string(key))
	}
}

func setScimAuthBearer(key []byte) func(*http.Request) {
	return func(r *http.Request) {
		// Do strange casing to ensure it's case-insensitive
		r.Header.Set("Authorization", "beAreR "+string(key))
	}
}

//nolint:gocritic // SCIM authenticates via a special header and bypasses internal RBAC.
func TestScim(t *testing.T) {
	t.Parallel()

	t.Run("postUser", func(t *testing.T) {
		t.Parallel()

		t.Run("disabled", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				SCIMAPIKey: []byte("hi"),
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM: 0,
					},
				},
			})

			res, err := client.Request(ctx, "POST", "/scim/v2/Users", struct{}{})
			require.NoError(t, err)
			defer res.Body.Close()
			assert.Equal(t, http.StatusForbidden, res.StatusCode)
		})

		t.Run("noAuth", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				SCIMAPIKey: []byte("hi"),
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM: 1,
					},
				},
			})

			res, err := client.Request(ctx, "POST", "/scim/v2/Users", struct{}{})
			require.NoError(t, err)
			defer res.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
		})

		t.Run("OK", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			// given
			scimAPIKey := []byte("hi")
			mockAudit := audit.NewMock()
			notifyEnq := &notificationstest.FakeEnqueuer{}
			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				Options: &wirtualdtest.Options{
					Auditor:               mockAudit,
					NotificationsEnqueuer: notifyEnq,
				},
				SCIMAPIKey:   scimAPIKey,
				AuditLogging: true,
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM:     1,
						wirtualsdk.FeatureAuditLog: 1,
					},
				},
			})
			mockAudit.ResetLogs()

			// verify scim is enabled
			res, err := client.Request(ctx, http.MethodGet, "/scim/v2/ServiceProviderConfig", nil)
			require.NoError(t, err)
			defer res.Body.Close()
			require.Equal(t, http.StatusOK, res.StatusCode)

			// when
			sUser := makeScimUser(t)
			res, err = client.Request(ctx, http.MethodPost, "/scim/v2/Users", sUser, setScimAuth(scimAPIKey))
			require.NoError(t, err)
			defer res.Body.Close()
			require.Equal(t, http.StatusOK, res.StatusCode)

			// then
			// Expect audit logs
			aLogs := mockAudit.AuditLogs()
			require.Len(t, aLogs, 1)
			af := map[string]string{}
			err = json.Unmarshal([]byte(aLogs[0].AdditionalFields), &af)
			require.NoError(t, err)
			assert.Equal(t, wirtuald.SCIMAuditAdditionalFields, af)
			assert.Equal(t, database.AuditActionCreate, aLogs[0].Action)

			// Expect users exposed over API
			userRes, err := client.Users(ctx, wirtualsdk.UsersRequest{Search: sUser.Emails[0].Value})
			require.NoError(t, err)
			require.Len(t, userRes.Users, 1)
			assert.Equal(t, sUser.Emails[0].Value, userRes.Users[0].Email)
			assert.Equal(t, sUser.UserName, userRes.Users[0].Username)
			assert.Len(t, userRes.Users[0].OrganizationIDs, 1)

			// Expect zero notifications (SkipNotifications = true)
			require.Empty(t, notifyEnq.Sent())
		})

		t.Run("OK_Bearer", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			// given
			scimAPIKey := []byte("hi")
			mockAudit := audit.NewMock()
			notifyEnq := &notificationstest.FakeEnqueuer{}
			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				Options: &wirtualdtest.Options{
					Auditor:               mockAudit,
					NotificationsEnqueuer: notifyEnq,
				},
				SCIMAPIKey:   scimAPIKey,
				AuditLogging: true,
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM:     1,
						wirtualsdk.FeatureAuditLog: 1,
					},
				},
			})
			mockAudit.ResetLogs()

			// when
			sUser := makeScimUser(t)
			res, err := client.Request(ctx, "POST", "/scim/v2/Users", sUser, setScimAuthBearer(scimAPIKey))
			require.NoError(t, err)
			defer res.Body.Close()
			require.Equal(t, http.StatusOK, res.StatusCode)

			// then
			// Expect audit logs
			aLogs := mockAudit.AuditLogs()
			require.Len(t, aLogs, 1)
			af := map[string]string{}
			err = json.Unmarshal([]byte(aLogs[0].AdditionalFields), &af)
			require.NoError(t, err)
			assert.Equal(t, wirtuald.SCIMAuditAdditionalFields, af)
			assert.Equal(t, database.AuditActionCreate, aLogs[0].Action)

			// Expect users exposed over API
			userRes, err := client.Users(ctx, wirtualsdk.UsersRequest{Search: sUser.Emails[0].Value})
			require.NoError(t, err)
			require.Len(t, userRes.Users, 1)
			assert.Equal(t, sUser.Emails[0].Value, userRes.Users[0].Email)
			assert.Equal(t, sUser.UserName, userRes.Users[0].Username)
			assert.Len(t, userRes.Users[0].OrganizationIDs, 1)

			// Expect zero notifications (SkipNotifications = true)
			require.Empty(t, notifyEnq.Sent())
		})

		t.Run("OKNoDefault", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			// given
			scimAPIKey := []byte("hi")
			mockAudit := audit.NewMock()
			notifyEnq := &notificationstest.FakeEnqueuer{}
			dv := wirtualdtest.DeploymentValues(t)
			dv.OIDC.OrganizationAssignDefault = false
			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				Options: &wirtualdtest.Options{
					Auditor:               mockAudit,
					NotificationsEnqueuer: notifyEnq,
					DeploymentValues:      dv,
				},
				SCIMAPIKey:   scimAPIKey,
				AuditLogging: true,
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM:     1,
						wirtualsdk.FeatureAuditLog: 1,
					},
				},
			})
			mockAudit.ResetLogs()

			// when
			sUser := makeScimUser(t)
			res, err := client.Request(ctx, "POST", "/scim/v2/Users", sUser, setScimAuth(scimAPIKey))
			require.NoError(t, err)
			defer res.Body.Close()
			require.Equal(t, http.StatusOK, res.StatusCode)

			// then
			// Expect audit logs
			aLogs := mockAudit.AuditLogs()
			require.Len(t, aLogs, 1)
			af := map[string]string{}
			err = json.Unmarshal([]byte(aLogs[0].AdditionalFields), &af)
			require.NoError(t, err)
			assert.Equal(t, wirtuald.SCIMAuditAdditionalFields, af)
			assert.Equal(t, database.AuditActionCreate, aLogs[0].Action)

			// Expect users exposed over API
			userRes, err := client.Users(ctx, wirtualsdk.UsersRequest{Search: sUser.Emails[0].Value})
			require.NoError(t, err)
			require.Len(t, userRes.Users, 1)
			assert.Equal(t, sUser.Emails[0].Value, userRes.Users[0].Email)
			assert.Equal(t, sUser.UserName, userRes.Users[0].Username)
			assert.Len(t, userRes.Users[0].OrganizationIDs, 0)

			// Expect zero notifications (SkipNotifications = true)
			require.Empty(t, notifyEnq.Sent())
		})

		t.Run("Duplicate", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			scimAPIKey := []byte("hi")
			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				SCIMAPIKey: scimAPIKey,
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM: 1,
					},
				},
			})

			sUser := makeScimUser(t)
			for i := 0; i < 3; i++ {
				res, err := client.Request(ctx, "POST", "/scim/v2/Users", sUser, setScimAuth(scimAPIKey))
				require.NoError(t, err)
				_ = res.Body.Close()
				assert.Equal(t, http.StatusOK, res.StatusCode)
			}

			userRes, err := client.Users(ctx, wirtualsdk.UsersRequest{Search: sUser.Emails[0].Value})
			require.NoError(t, err)
			require.Len(t, userRes.Users, 1)

			assert.Equal(t, sUser.Emails[0].Value, userRes.Users[0].Email)
			assert.Equal(t, sUser.UserName, userRes.Users[0].Username)
		})

		t.Run("Unsuspend", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			scimAPIKey := []byte("hi")
			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				SCIMAPIKey: scimAPIKey,
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM: 1,
					},
				},
			})

			sUser := makeScimUser(t)
			res, err := client.Request(ctx, "POST", "/scim/v2/Users", sUser, setScimAuth(scimAPIKey))
			require.NoError(t, err)
			defer res.Body.Close()
			assert.Equal(t, http.StatusOK, res.StatusCode)
			err = json.NewDecoder(res.Body).Decode(&sUser)
			require.NoError(t, err)

			sUser.Active = false
			res, err = client.Request(ctx, "PATCH", "/scim/v2/Users/"+sUser.ID, sUser, setScimAuth(scimAPIKey))
			require.NoError(t, err)
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			assert.Equal(t, http.StatusOK, res.StatusCode)

			sUser.Active = true
			res, err = client.Request(ctx, "POST", "/scim/v2/Users", sUser, setScimAuth(scimAPIKey))
			require.NoError(t, err)
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			assert.Equal(t, http.StatusOK, res.StatusCode)

			userRes, err := client.Users(ctx, wirtualsdk.UsersRequest{Search: sUser.Emails[0].Value})
			require.NoError(t, err)
			require.Len(t, userRes.Users, 1)

			assert.Equal(t, sUser.Emails[0].Value, userRes.Users[0].Email)
			assert.Equal(t, sUser.UserName, userRes.Users[0].Username)
			assert.Equal(t, wirtualsdk.UserStatusDormant, userRes.Users[0].Status)
		})

		t.Run("DomainStrips", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			scimAPIKey := []byte("hi")
			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				SCIMAPIKey: scimAPIKey,
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM: 1,
					},
				},
			})

			sUser := makeScimUser(t)
			sUser.UserName = sUser.UserName + "@coder.com"
			res, err := client.Request(ctx, "POST", "/scim/v2/Users", sUser, setScimAuth(scimAPIKey))
			require.NoError(t, err)
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			assert.Equal(t, http.StatusOK, res.StatusCode)

			userRes, err := client.Users(ctx, wirtualsdk.UsersRequest{Search: sUser.Emails[0].Value})
			require.NoError(t, err)
			require.Len(t, userRes.Users, 1)

			assert.Equal(t, sUser.Emails[0].Value, userRes.Users[0].Email)
			// Username should be the same as the given name. They all use the
			// same string before we modified it above.
			assert.Equal(t, sUser.Name.GivenName, userRes.Users[0].Username)
		})
	})

	t.Run("patchUser", func(t *testing.T) {
		t.Parallel()

		t.Run("disabled", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				SCIMAPIKey: []byte("hi"),
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM: 0,
					},
				},
			})

			res, err := client.Request(ctx, "PATCH", "/scim/v2/Users/bob", struct{}{})
			require.NoError(t, err)
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			assert.Equal(t, http.StatusForbidden, res.StatusCode)
		})

		t.Run("noAuth", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				SCIMAPIKey: []byte("hi"),
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM: 1,
					},
				},
			})

			res, err := client.Request(ctx, "PATCH", "/scim/v2/Users/bob", struct{}{})
			require.NoError(t, err)
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
		})

		t.Run("OK", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			scimAPIKey := []byte("hi")
			mockAudit := audit.NewMock()
			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				Options:      &wirtualdtest.Options{Auditor: mockAudit},
				SCIMAPIKey:   scimAPIKey,
				AuditLogging: true,
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM:     1,
						wirtualsdk.FeatureAuditLog: 1,
					},
				},
			})
			mockAudit.ResetLogs()

			sUser := makeScimUser(t)
			res, err := client.Request(ctx, "POST", "/scim/v2/Users", sUser, setScimAuth(scimAPIKey))
			require.NoError(t, err)
			defer res.Body.Close()
			assert.Equal(t, http.StatusOK, res.StatusCode)
			mockAudit.ResetLogs()

			err = json.NewDecoder(res.Body).Decode(&sUser)
			require.NoError(t, err)

			sUser.Active = false

			res, err = client.Request(ctx, "PATCH", "/scim/v2/Users/"+sUser.ID, sUser, setScimAuth(scimAPIKey))
			require.NoError(t, err)
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			assert.Equal(t, http.StatusOK, res.StatusCode)

			aLogs := mockAudit.AuditLogs()
			require.Len(t, aLogs, 1)
			assert.Equal(t, database.AuditActionWrite, aLogs[0].Action)

			userRes, err := client.Users(ctx, wirtualsdk.UsersRequest{Search: sUser.Emails[0].Value})
			require.NoError(t, err)
			require.Len(t, userRes.Users, 1)
			assert.Equal(t, wirtualsdk.UserStatusSuspended, userRes.Users[0].Status)
		})

		// Create a user via SCIM, which starts as dormant.
		// Log in as the user, making them active.
		// Then patch the user again and the user should still be active.
		t.Run("ActiveIsActive", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			scimAPIKey := []byte("hi")

			mockAudit := audit.NewMock()
			fake := oidctest.NewFakeIDP(t, oidctest.WithServing())
			client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
				Options: &wirtualdtest.Options{
					Auditor:    mockAudit,
					OIDCConfig: fake.OIDCConfig(t, []string{}),
				},
				SCIMAPIKey:   scimAPIKey,
				AuditLogging: true,
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					AccountID: "coolin",
					Features: license.Features{
						wirtualsdk.FeatureSCIM:     1,
						wirtualsdk.FeatureAuditLog: 1,
					},
				},
			})
			mockAudit.ResetLogs()

			// User is dormant on create
			sUser := makeScimUser(t)
			res, err := client.Request(ctx, "POST", "/scim/v2/Users", sUser, setScimAuth(scimAPIKey))
			require.NoError(t, err)
			defer res.Body.Close()
			assert.Equal(t, http.StatusOK, res.StatusCode)

			err = json.NewDecoder(res.Body).Decode(&sUser)
			require.NoError(t, err)

			// Check the audit log
			aLogs := mockAudit.AuditLogs()
			require.Len(t, aLogs, 1)
			assert.Equal(t, database.AuditActionCreate, aLogs[0].Action)

			// Verify the user is dormant
			scimUser, err := client.User(ctx, sUser.UserName)
			require.NoError(t, err)
			require.Equal(t, wirtualsdk.UserStatusDormant, scimUser.Status, "user starts as dormant")

			// Log in as the user, making them active
			//nolint:bodyclose
			scimUserClient, _ := fake.Login(t, client, jwt.MapClaims{
				"email": sUser.Emails[0].Value,
			})
			scimUser, err = scimUserClient.User(ctx, wirtualsdk.Me)
			require.NoError(t, err)
			require.Equal(t, wirtualsdk.UserStatusActive, scimUser.Status, "user should now be active")

			// Patch the user
			mockAudit.ResetLogs()
			res, err = client.Request(ctx, "PATCH", "/scim/v2/Users/"+sUser.ID, sUser, setScimAuth(scimAPIKey))
			require.NoError(t, err)
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			assert.Equal(t, http.StatusOK, res.StatusCode)

			// Should be no audit logs since there is no diff
			aLogs = mockAudit.AuditLogs()
			require.Len(t, aLogs, 0)

			// Verify the user is still active.
			scimUser, err = client.User(ctx, sUser.UserName)
			require.NoError(t, err)
			require.Equal(t, wirtualsdk.UserStatusActive, scimUser.Status, "user is still active")
		})
	})
}

func TestScimError(t *testing.T) {
	t.Parallel()

	// Demonstrates that we cannot use the standard errors
	rw := httptest.NewRecorder()
	_ = handlerutil.WriteError(rw, spec.ErrNotFound)
	resp := rw.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	// Our error wrapper works
	rw = httptest.NewRecorder()
	_ = handlerutil.WriteError(rw, scim.NewHTTPError(http.StatusNotFound, spec.ErrNotFound.Type, xerrors.New("not found")))
	resp = rw.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
