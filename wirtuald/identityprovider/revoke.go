package identityprovider

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/httpapi"
	"github.com/coder/coder/v2/wirtuald/httpmw"
)

func RevokeApp(db database.Store) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		apiKey := httpmw.APIKey(r)
		app := httpmw.OAuth2ProviderApp(r)

		err := db.InTx(func(tx database.Store) error {
			err := tx.DeleteOAuth2ProviderAppCodesByAppAndUserID(ctx, database.DeleteOAuth2ProviderAppCodesByAppAndUserIDParams{
				AppID:  app.ID,
				UserID: apiKey.UserID,
			})
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			err = tx.DeleteOAuth2ProviderAppTokensByAppAndUserID(ctx, database.DeleteOAuth2ProviderAppTokensByAppAndUserIDParams{
				AppID:  app.ID,
				UserID: apiKey.UserID,
			})
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			return nil
		}, nil)
		if err != nil {
			httpapi.InternalServerError(rw, err)
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	}
}
