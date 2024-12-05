package trialer_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/enterprise/trialer"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbmem"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestTrialer(t *testing.T) {
	t.Parallel()
	license := wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
		Trial: true,
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(license))
	}))
	defer srv.Close()
	db := dbmem.New()

	gen := trialer.New(db, srv.URL, wirtualdenttest.Keys)
	err := gen(context.Background(), wirtualsdk.LicensorTrialRequest{Email: "kyle+colin@coder.com"})
	require.NoError(t, err)
	licenses, err := db.GetLicenses(context.Background())
	require.NoError(t, err)
	require.Len(t, licenses, 1)
}
