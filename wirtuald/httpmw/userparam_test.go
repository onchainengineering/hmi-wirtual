package httpmw_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbgen"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbmem"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpmw"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestUserParam(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (database.Store, *httptest.ResponseRecorder, *http.Request) {
		var (
			db = dbmem.New()
			r  = httptest.NewRequest("GET", "/", nil)
			rw = httptest.NewRecorder()
		)
		user := dbgen.User(t, db, database.User{})
		_, token := dbgen.APIKey(t, db, database.APIKey{
			UserID: user.ID,
		})
		r.Header.Set(wirtualsdk.SessionTokenHeader, token)

		return db, rw, r
	}

	t.Run("None", func(t *testing.T) {
		t.Parallel()
		db, rw, r := setup(t)

		httpmw.ExtractAPIKeyMW(httpmw.ExtractAPIKeyConfig{
			DB:              db,
			RedirectToLogin: false,
		})(http.HandlerFunc(func(rw http.ResponseWriter, returnedRequest *http.Request) {
			r = returnedRequest
		})).ServeHTTP(rw, r)

		httpmw.ExtractUserParam(db)(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusOK)
		})).ServeHTTP(rw, r)
		res := rw.Result()
		defer res.Body.Close()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("NotMe", func(t *testing.T) {
		t.Parallel()
		db, rw, r := setup(t)

		httpmw.ExtractAPIKeyMW(httpmw.ExtractAPIKeyConfig{
			DB:              db,
			RedirectToLogin: false,
		})(http.HandlerFunc(func(rw http.ResponseWriter, returnedRequest *http.Request) {
			r = returnedRequest
		})).ServeHTTP(rw, r)

		routeContext := chi.NewRouteContext()
		routeContext.URLParams.Add("user", "ben")
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeContext))
		httpmw.ExtractUserParam(db)(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusOK)
		})).ServeHTTP(rw, r)
		res := rw.Result()
		defer res.Body.Close()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("me", func(t *testing.T) {
		t.Parallel()
		db, rw, r := setup(t)

		httpmw.ExtractAPIKeyMW(httpmw.ExtractAPIKeyConfig{
			DB:              db,
			RedirectToLogin: false,
		})(http.HandlerFunc(func(rw http.ResponseWriter, returnedRequest *http.Request) {
			r = returnedRequest
		})).ServeHTTP(rw, r)

		routeContext := chi.NewRouteContext()
		routeContext.URLParams.Add("user", "me")
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeContext))
		httpmw.ExtractUserParam(db)(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			_ = httpmw.UserParam(r)
			rw.WriteHeader(http.StatusOK)
		})).ServeHTTP(rw, r)
		res := rw.Result()
		defer res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode)
	})
}
