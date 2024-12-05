package wirtuald_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/coder/serpent"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/audit"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtestutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtime"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestTokenCRUD(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()
	auditor := audit.NewMock()
	numLogs := len(auditor.AuditLogs())
	client := wirtualdtest.New(t, &wirtualdtest.Options{Auditor: auditor})
	_ = wirtualdtest.CreateFirstUser(t, client)
	numLogs++ // add an audit log for user creation

	keys, err := client.Tokens(ctx, wirtualsdk.Me, wirtualsdk.TokensFilter{})
	require.NoError(t, err)
	require.Empty(t, keys)

	res, err := client.CreateToken(ctx, wirtualsdk.Me, wirtualsdk.CreateTokenRequest{})
	require.NoError(t, err)
	require.Greater(t, len(res.Key), 2)
	numLogs++ // add an audit log for token creation

	keys, err = client.Tokens(ctx, wirtualsdk.Me, wirtualsdk.TokensFilter{})
	require.NoError(t, err)
	require.EqualValues(t, len(keys), 1)
	require.Contains(t, res.Key, keys[0].ID)
	// expires_at should default to 30 days
	require.Greater(t, keys[0].ExpiresAt, time.Now().Add(time.Hour*24*6))
	require.Less(t, keys[0].ExpiresAt, time.Now().Add(time.Hour*24*8))
	require.Equal(t, wirtualsdk.APIKeyScopeAll, keys[0].Scope)

	// no update

	err = client.DeleteAPIKey(ctx, wirtualsdk.Me, keys[0].ID)
	require.NoError(t, err)
	numLogs++ // add an audit log for token deletion
	keys, err = client.Tokens(ctx, wirtualsdk.Me, wirtualsdk.TokensFilter{})
	require.NoError(t, err)
	require.Empty(t, keys)

	// ensure audit log count is correct
	require.Len(t, auditor.AuditLogs(), numLogs)
	require.Equal(t, database.AuditActionCreate, auditor.AuditLogs()[numLogs-2].Action)
	require.Equal(t, database.AuditActionDelete, auditor.AuditLogs()[numLogs-1].Action)
}

func TestTokenScoped(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()
	client := wirtualdtest.New(t, nil)
	_ = wirtualdtest.CreateFirstUser(t, client)

	res, err := client.CreateToken(ctx, wirtualsdk.Me, wirtualsdk.CreateTokenRequest{
		Scope: wirtualsdk.APIKeyScopeApplicationConnect,
	})
	require.NoError(t, err)
	require.Greater(t, len(res.Key), 2)

	keys, err := client.Tokens(ctx, wirtualsdk.Me, wirtualsdk.TokensFilter{})
	require.NoError(t, err)
	require.EqualValues(t, len(keys), 1)
	require.Contains(t, res.Key, keys[0].ID)
	require.Equal(t, keys[0].Scope, wirtualsdk.APIKeyScopeApplicationConnect)
}

func TestUserSetTokenDuration(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()
	client := wirtualdtest.New(t, nil)
	_ = wirtualdtest.CreateFirstUser(t, client)

	_, err := client.CreateToken(ctx, wirtualsdk.Me, wirtualsdk.CreateTokenRequest{
		Lifetime: time.Hour * 24 * 7,
	})
	require.NoError(t, err)
	keys, err := client.Tokens(ctx, wirtualsdk.Me, wirtualsdk.TokensFilter{})
	require.NoError(t, err)
	require.Greater(t, keys[0].ExpiresAt, time.Now().Add(time.Hour*6*24))
	require.Less(t, keys[0].ExpiresAt, time.Now().Add(time.Hour*8*24))
}

func TestDefaultTokenDuration(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()
	client := wirtualdtest.New(t, nil)
	_ = wirtualdtest.CreateFirstUser(t, client)

	_, err := client.CreateToken(ctx, wirtualsdk.Me, wirtualsdk.CreateTokenRequest{})
	require.NoError(t, err)
	keys, err := client.Tokens(ctx, wirtualsdk.Me, wirtualsdk.TokensFilter{})
	require.NoError(t, err)
	require.Greater(t, keys[0].ExpiresAt, time.Now().Add(time.Hour*24*6))
	require.Less(t, keys[0].ExpiresAt, time.Now().Add(time.Hour*24*8))
}

func TestTokenUserSetMaxLifetime(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()
	dc := wirtualdtest.DeploymentValues(t)
	dc.Sessions.MaximumTokenDuration = serpent.Duration(time.Hour * 24 * 7)
	client := wirtualdtest.New(t, &wirtualdtest.Options{
		DeploymentValues: dc,
	})
	_ = wirtualdtest.CreateFirstUser(t, client)

	// success
	_, err := client.CreateToken(ctx, wirtualsdk.Me, wirtualsdk.CreateTokenRequest{
		Lifetime: time.Hour * 24 * 6,
	})
	require.NoError(t, err)

	// fail
	_, err = client.CreateToken(ctx, wirtualsdk.Me, wirtualsdk.CreateTokenRequest{
		Lifetime: time.Hour * 24 * 8,
	})
	require.ErrorContains(t, err, "lifetime must be less")
}

func TestTokenCustomDefaultLifetime(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()
	dc := wirtualdtest.DeploymentValues(t)
	dc.Sessions.DefaultTokenDuration = serpent.Duration(time.Hour * 12)
	client := wirtualdtest.New(t, &wirtualdtest.Options{
		DeploymentValues: dc,
	})
	_ = wirtualdtest.CreateFirstUser(t, client)

	_, err := client.CreateToken(ctx, wirtualsdk.Me, wirtualsdk.CreateTokenRequest{})
	require.NoError(t, err)

	tokens, err := client.Tokens(ctx, wirtualsdk.Me, wirtualsdk.TokensFilter{})
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.EqualValues(t, dc.Sessions.DefaultTokenDuration.Value().Seconds(), tokens[0].LifetimeSeconds)
}

func TestSessionExpiry(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()
	dc := wirtualdtest.DeploymentValues(t)

	db, pubsub := dbtestutil.NewDB(t)
	adminClient := wirtualdtest.New(t, &wirtualdtest.Options{
		DeploymentValues: dc,
		Database:         db,
		Pubsub:           pubsub,
	})
	adminUser := wirtualdtest.CreateFirstUser(t, adminClient)

	// This is a hack, but we need the admin account to have a long expiry
	// otherwise the test will flake, so we only update the expiry config after
	// the admin account has been created.
	//
	// We don't support updating the deployment config after startup, but for
	// this test it works because we don't copy the value (and we use pointers).
	dc.Sessions.DefaultDuration = serpent.Duration(time.Second)

	userClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID)

	// Find the session cookie, and ensure it has the correct expiry.
	token := userClient.SessionToken()
	apiKey, err := db.GetAPIKeyByID(ctx, strings.Split(token, "-")[0])
	require.NoError(t, err)

	require.EqualValues(t, dc.Sessions.DefaultDuration.Value().Seconds(), apiKey.LifetimeSeconds)
	require.WithinDuration(t, apiKey.CreatedAt.Add(dc.Sessions.DefaultDuration.Value()), apiKey.ExpiresAt, 2*time.Second)

	// Update the session token to be expired so we can test that it is
	// rejected for extra points.
	err = db.UpdateAPIKeyByID(ctx, database.UpdateAPIKeyByIDParams{
		ID:        apiKey.ID,
		LastUsed:  apiKey.LastUsed,
		ExpiresAt: dbtime.Now().Add(-time.Hour),
		IPAddress: apiKey.IPAddress,
	})
	require.NoError(t, err)

	_, err = userClient.User(ctx, wirtualsdk.Me)
	require.Error(t, err)
	var sdkErr *wirtualsdk.Error
	if assert.ErrorAs(t, err, &sdkErr) {
		require.Equal(t, http.StatusUnauthorized, sdkErr.StatusCode())
		require.Contains(t, sdkErr.Message, "session has expired")
	}
}

func TestAPIKey_OK(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()
	client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
	_ = wirtualdtest.CreateFirstUser(t, client)

	res, err := client.CreateAPIKey(ctx, wirtualsdk.Me)
	require.NoError(t, err)
	require.Greater(t, len(res.Key), 2)
}

func TestAPIKey_Deleted(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()
	client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
	user := wirtualdtest.CreateFirstUser(t, client)
	_, anotherUser := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)
	require.NoError(t, client.DeleteUser(context.Background(), anotherUser.ID))

	// Attempt to create an API key for the deleted user. This should fail.
	_, err := client.CreateAPIKey(ctx, anotherUser.Username)
	require.Error(t, err)
	var apiErr *wirtualsdk.Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode())
}

func TestAPIKey_SetDefault(t *testing.T) {
	t.Parallel()

	db, pubsub := dbtestutil.NewDB(t)
	dc := wirtualdtest.DeploymentValues(t)
	dc.Sessions.DefaultTokenDuration = serpent.Duration(time.Hour * 12)
	client := wirtualdtest.New(t, &wirtualdtest.Options{
		Database:         db,
		Pubsub:           pubsub,
		DeploymentValues: dc,
	})
	owner := wirtualdtest.CreateFirstUser(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	token, err := client.CreateAPIKey(ctx, owner.UserID.String())
	require.NoError(t, err)
	split := strings.Split(token.Key, "-")
	apiKey1, err := db.GetAPIKeyByID(ctx, split[0])
	require.NoError(t, err)
	require.EqualValues(t, dc.Sessions.DefaultTokenDuration.Value().Seconds(), apiKey1.LifetimeSeconds)
}