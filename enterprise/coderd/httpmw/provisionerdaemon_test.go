package httpmw_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/enterprise/wirtuald/license"
	"github.com/coder/coder/v2/enterpris
	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/database/dbmock"
	"github.com/coder/coder/v2/wirtuald/httpmw"
	"github.com/coder/coder/v2/wirtualsdk"
)

func TestExtractProvisionerDaemonAuthenticated(t *testing.T) {
	const (
		//nolint:gosec // test key generated by test
		functionalKey = "5Hl2Qw9kX3nM7vB4jR8pY6tA1cF0eD5uI2oL9gN3mZ4"
	)
	t.Parallel()

	tests := []struct {
		name                    string
		opts                    httpmw.ExtractProvisionerAuthConfig
		expectedStatusCode      int
		expectedResponseMessage string
		provisionerKey          string
		provisionerPSK          string
	}{
		{
			name: "NoKeyProvided_Optional",
			opts: httpmw.ExtractProvisionerAuthConfig{
				DB:       nil,
				Optional: true,
			},
			expectedStatusCode: http.StatusOK,
		},
		{
			name: "NoKeyProvided_NotOptional",
			opts: httpmw.ExtractProvisionerAuthConfig{
				DB:       nil,
				Optional: false,
			},
			expectedStatusCode:      http.StatusUnauthorized,
			expectedResponseMessage: "provisioner daemon key required",
		},
		{
			name: "ProvisionerKeyAndPSKProvided_NotOptional",
			opts: httpmw.ExtractProvisionerAuthConfig{
				DB:       nil,
				Optional: false,
			},
			provisionerKey:          "key",
			provisionerPSK:          "psk",
			expectedStatusCode:      http.StatusBadRequest,
			expectedResponseMessage: "provisioner daemon key and psk provided, but only one is allowed",
		},
		{
			name: "ProvisionerKeyAndPSKProvided_Optional",
			opts: httpmw.ExtractProvisionerAuthConfig{
				DB:       nil,
				Optional: true,
			},
			provisionerKey:     "key",
			expectedStatusCode: http.StatusOK,
		},
		{
			name: "InvalidProvisionerKey_NotOptional",
			opts: httpmw.ExtractProvisionerAuthConfig{
				DB:       nil,
				Optional: false,
			},
			provisionerKey:          "invalid",
			expectedStatusCode:      http.StatusBadRequest,
			expectedResponseMessage: "provisioner daemon key invalid",
		},
		{
			name: "InvalidProvisionerKey_Optional",
			opts: httpmw.ExtractProvisionerAuthConfig{
				DB:       nil,
				Optional: true,
			},
			provisionerKey:     "invalid",
			expectedStatusCode: http.StatusOK,
		},
		{
			name: "InvalidProvisionerPSK_NotOptional",
			opts: httpmw.ExtractProvisionerAuthConfig{
				DB:       nil,
				Optional: false,
				PSK:      "psk",
			},
			provisionerPSK:          "invalid",
			expectedStatusCode:      http.StatusUnauthorized,
			expectedResponseMessage: "provisioner daemon psk invalid",
		},
		{
			name: "InvalidProvisionerPSK_Optional",
			opts: httpmw.ExtractProvisionerAuthConfig{
				DB:       nil,
				Optional: true,
				PSK:      "psk",
			},
			provisionerPSK:     "invalid",
			expectedStatusCode: http.StatusOK,
		},
		{
			name: "ValidProvisionerPSK_NotOptional",
			opts: httpmw.ExtractProvisionerAuthConfig{
				DB:       nil,
				Optional: false,
				PSK:      "ThisIsAValidPSK",
			},
			provisionerPSK:     "ThisIsAValidPSK",
			expectedStatusCode: http.StatusOK,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			routeCtx := chi.NewRouteContext()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
			res := httptest.NewRecorder()

			if test.provisionerKey != "" {
				r.Header.Set(wirtualsdk.ProvisionerDaemonKey, test.provisionerKey)
			}
			if test.provisionerPSK != "" {
				r.Header.Set(wirtualsdk.ProvisionerDaemonPSK, test.provisionerPSK)
			}

			httpmw.ExtractProvisionerDaemonAuthenticated(test.opts)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})).ServeHTTP(res, r)

			//nolint:bodyclose
			require.Equal(t, test.expectedStatusCode, res.Result().StatusCode)
			if test.expectedResponseMessage != "" {
				require.Contains(t, res.Body.String(), test.expectedResponseMessage)
			}
		})
	}

	t.Run("ProvisionerKey", func(t *testing.T) {
		t.Parallel()

		ctx := testutil.Context(t, testutil.WaitShort)
		client, db, user := wirtualdenttest.NewWithDatabase(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureExternalProvisionerDaemons: 1,
				},
			},
		})
		// nolint:gocritic // test
		key, err := client.CreateProvisionerKey(ctx, user.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
			Name: "dont-TEST-me",
		})
		require.NoError(t, err)

		routeCtx := chi.NewRouteContext()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
		res := httptest.NewRecorder()

		r.Header.Set(wirtualsdk.ProvisionerDaemonKey, key.Key)

		httpmw.ExtractProvisionerDaemonAuthenticated(httpmw.ExtractProvisionerAuthConfig{
			DB:       db,
			Optional: false,
		})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(res, r)

		//nolint:bodyclose
		require.Equal(t, http.StatusOK, res.Result().StatusCode)
	})

	t.Run("ProvisionerKey_NotFound", func(t *testing.T) {
		t.Parallel()

		ctx := testutil.Context(t, testutil.WaitShort)
		client, db, user := wirtualdenttest.NewWithDatabase(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureExternalProvisionerDaemons: 1,
				},
			},
		})
		// nolint:gocritic // test
		_, err := client.CreateProvisionerKey(ctx, user.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
			Name: "dont-TEST-me",
		})
		require.NoError(t, err)

		routeCtx := chi.NewRouteContext()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
		res := httptest.NewRecorder()

		//nolint:gosec // test key generated by test
		pkey := "5Hl2Qw9kX3nM7vB4jR8pY6tA1cF0eD5uI2oL9gN3mZ4"
		r.Header.Set(wirtualsdk.ProvisionerDaemonKey, pkey)

		httpmw.ExtractProvisionerDaemonAuthenticated(httpmw.ExtractProvisionerAuthConfig{
			DB:       db,
			Optional: false,
		})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(res, r)

		//nolint:bodyclose
		require.Equal(t, http.StatusUnauthorized, res.Result().StatusCode)
		require.Contains(t, res.Body.String(), "provisioner daemon key invalid")
	})

	t.Run("ProvisionerKey_CompareFail", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		mockDB := dbmock.NewMockStore(ctrl)

		gomock.InOrder(
			mockDB.EXPECT().GetProvisionerKeyByHashedSecret(gomock.Any(), gomock.Any()).Times(1).Return(database.ProvisionerKey{
				ID:           uuid.New(),
				HashedSecret: []byte("hashedSecret"),
			}, nil),
		)

		routeCtx := chi.NewRouteContext()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
		res := httptest.NewRecorder()

		r.Header.Set(wirtualsdk.ProvisionerDaemonKey, functionalKey)

		httpmw.ExtractProvisionerDaemonAuthenticated(httpmw.ExtractProvisionerAuthConfig{
			DB:       mockDB,
			Optional: false,
		})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(res, r)

		//nolint:bodyclose
		require.Equal(t, http.StatusUnauthorized, res.Result().StatusCode)
		require.Contains(t, res.Body.String(), "provisioner daemon key invalid")
	})

	t.Run("ProvisionerKey_DBError", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		mockDB := dbmock.NewMockStore(ctrl)

		gomock.InOrder(
			mockDB.EXPECT().GetProvisionerKeyByHashedSecret(gomock.Any(), gomock.Any()).Times(1).Return(database.ProvisionerKey{}, xerrors.New("error")),
		)

		routeCtx := chi.NewRouteContext()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
		res := httptest.NewRecorder()

		//nolint:gosec // test key generated by test
		r.Header.Set(wirtualsdk.ProvisionerDaemonKey, functionalKey)

		httpmw.ExtractProvisionerDaemonAuthenticated(httpmw.ExtractProvisionerAuthConfig{
			DB:       mockDB,
			Optional: false,
		})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(res, r)

		//nolint:bodyclose
		require.Equal(t, http.StatusInternalServerError, res.Result().StatusCode)
		require.Contains(t, res.Body.String(), "get provisioner daemon key")
	})
}
