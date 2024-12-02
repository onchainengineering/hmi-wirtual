package provisionerdserver

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbgen"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbmem"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtime"
	"github.com/onchainengineering/hmi-wirtual/testutil"
)

func TestObtainOIDCAccessToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("NoToken", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		_, err := obtainOIDCAccessToken(ctx, db, nil, uuid.Nil)
		require.NoError(t, err)
	})
	t.Run("InvalidConfig", func(t *testing.T) {
		// We still want OIDC to succeed even if exchanging the token fails.
		t.Parallel()
		db := dbmem.New()
		user := dbgen.User(t, db, database.User{})
		dbgen.UserLink(t, db, database.UserLink{
			UserID:      user.ID,
			LoginType:   database.LoginTypeOIDC,
			OAuthExpiry: dbtime.Now().Add(-time.Hour),
		})
		_, err := obtainOIDCAccessToken(ctx, db, &oauth2.Config{}, user.ID)
		require.NoError(t, err)
	})
	t.Run("MissingLink", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		user := dbgen.User(t, db, database.User{
			LoginType: database.LoginTypeOIDC,
		})
		tok, err := obtainOIDCAccessToken(ctx, db, &oauth2.Config{}, user.ID)
		require.Empty(t, tok)
		require.NoError(t, err)
	})
	t.Run("Exchange", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		user := dbgen.User(t, db, database.User{})
		dbgen.UserLink(t, db, database.UserLink{
			UserID:      user.ID,
			LoginType:   database.LoginTypeOIDC,
			OAuthExpiry: dbtime.Now().Add(-time.Hour),
		})
		_, err := obtainOIDCAccessToken(ctx, db, &testutil.OAuth2Config{
			Token: &oauth2.Token{
				AccessToken: "token",
			},
		}, user.ID)
		require.NoError(t, err)
		link, err := db.GetUserLinkByUserIDLoginType(ctx, database.GetUserLinkByUserIDLoginTypeParams{
			UserID:    user.ID,
			LoginType: database.LoginTypeOIDC,
		})
		require.NoError(t, err)
		require.Equal(t, "token", link.OAuthAccessToken)
	})
}
