package trialer_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/wirtuald/database/dbmem"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/enterprise/wirtuald/wirtualdenttest"
	"github.com/coder/coder/v2/enterprise/trialer"
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
