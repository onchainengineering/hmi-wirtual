package wirtuald_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestPostLicense(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		respLic := wirtualdenttest.AddLicense(t, client, wirtualdenttest.LicenseOptions{
			AccountType: license.AccountTypeSalesforce,
			AccountID:   "testing",
			Features: license.Features{
				wirtualsdk.FeatureAuditLog: 1,
			},
		})
		assert.GreaterOrEqual(t, respLic.ID, int32(0))
		// just a couple spot checks for sanity
		assert.Equal(t, "testing", respLic.Claims["account_id"])
		features, err := respLic.FeaturesClaims()
		require.NoError(t, err)
		assert.EqualValues(t, 1, features[wirtualsdk.FeatureAuditLog])
	})

	t.Run("InvalidDeploymentID", func(t *testing.T) {
		t.Parallel()
		// The generated deployment will start out with a different deployment ID.
		client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		license := wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
			DeploymentIDs: []string{uuid.NewString()},
		})
		_, err := client.AddLicense(context.Background(), wirtualsdk.AddLicenseRequest{
			License: license,
		})
		errResp := &wirtualsdk.Error{}
		require.ErrorAs(t, err, &errResp)
		require.Equal(t, http.StatusBadRequest, errResp.StatusCode())
		require.Contains(t, errResp.Message, "License cannot be used on this deployment!")
	})

	t.Run("Unauthorized", func(t *testing.T) {
		t.Parallel()
		client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		client.SetSessionToken("")
		_, err := client.AddLicense(context.Background(), wirtualsdk.AddLicenseRequest{
			License: "content",
		})
		errResp := &wirtualsdk.Error{}
		if xerrors.As(err, &errResp) {
			assert.Equal(t, 401, errResp.StatusCode())
		} else {
			t.Error("expected to get error status 401")
		}
	})

	t.Run("Corrupted", func(t *testing.T) {
		t.Parallel()
		client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		wirtualdenttest.AddLicense(t, client, wirtualdenttest.LicenseOptions{})
		_, err := client.AddLicense(context.Background(), wirtualsdk.AddLicenseRequest{
			License: "invalid",
		})
		errResp := &wirtualsdk.Error{}
		if xerrors.As(err, &errResp) {
			assert.Equal(t, 400, errResp.StatusCode())
		} else {
			t.Error("expected to get error status 400")
		}
	})

	// Test a license that isn't yet valid, but will be in the future.  We should allow this so that
	// operators can upload a license ahead of time.
	t.Run("NotYet", func(t *testing.T) {
		t.Parallel()
		client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		respLic := wirtualdenttest.AddLicense(t, client, wirtualdenttest.LicenseOptions{
			AccountType: license.AccountTypeSalesforce,
			AccountID:   "testing",
			Features: license.Features{
				wirtualsdk.FeatureAuditLog: 1,
			},
			NotBefore: time.Now().Add(time.Hour),
			GraceAt:   time.Now().Add(2 * time.Hour),
			ExpiresAt: time.Now().Add(3 * time.Hour),
		})
		assert.GreaterOrEqual(t, respLic.ID, int32(0))
		// just a couple spot checks for sanity
		assert.Equal(t, "testing", respLic.Claims["account_id"])
		features, err := respLic.FeaturesClaims()
		require.NoError(t, err)
		assert.EqualValues(t, 1, features[wirtualsdk.FeatureAuditLog])
	})

	// Test we still reject a license that isn't valid yet, but has other issues (e.g. expired
	// before it starts).
	t.Run("NotEver", func(t *testing.T) {
		t.Parallel()
		client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		lic := wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
			AccountType: license.AccountTypeSalesforce,
			AccountID:   "testing",
			Features: license.Features{
				wirtualsdk.FeatureAuditLog: 1,
			},
			NotBefore: time.Now().Add(time.Hour),
			GraceAt:   time.Now().Add(2 * time.Hour),
			ExpiresAt: time.Now().Add(-time.Hour),
		})
		_, err := client.AddLicense(context.Background(), wirtualsdk.AddLicenseRequest{
			License: lic,
		})
		errResp := &wirtualsdk.Error{}
		require.ErrorAs(t, err, &errResp)
		require.Equal(t, http.StatusBadRequest, errResp.StatusCode())
		require.Contains(t, errResp.Detail, license.ErrMultipleIssues.Error())
	})
}

func TestGetLicense(t *testing.T) {
	t.Parallel()
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		wirtualdenttest.AddLicense(t, client, wirtualdenttest.LicenseOptions{
			AccountID: "testing",
			Features: license.Features{
				wirtualsdk.FeatureAuditLog:     1,
				wirtualsdk.FeatureSCIM:         1,
				wirtualsdk.FeatureBrowserOnly:  1,
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		})

		wirtualdenttest.AddLicense(t, client, wirtualdenttest.LicenseOptions{
			AccountID: "testing2",
			Features: license.Features{
				wirtualsdk.FeatureAuditLog:    1,
				wirtualsdk.FeatureSCIM:        1,
				wirtualsdk.FeatureBrowserOnly: 1,
				wirtualsdk.FeatureUserLimit:   200,
			},
			Trial: true,
		})

		licenses, err := client.Licenses(ctx)
		require.NoError(t, err)
		require.Len(t, licenses, 2)
		assert.Equal(t, int32(1), licenses[0].ID)
		assert.Equal(t, "testing", licenses[0].Claims["account_id"])

		features, err := licenses[0].FeaturesClaims()
		require.NoError(t, err)
		assert.Equal(t, map[wirtualsdk.FeatureName]int64{
			wirtualsdk.FeatureAuditLog:     1,
			wirtualsdk.FeatureSCIM:         1,
			wirtualsdk.FeatureBrowserOnly:  1,
			wirtualsdk.FeatureTemplateRBAC: 1,
		}, features)
		assert.Equal(t, int32(2), licenses[1].ID)
		assert.Equal(t, "testing2", licenses[1].Claims["account_id"])
		assert.Equal(t, true, licenses[1].Claims["trial"])

		features, err = licenses[1].FeaturesClaims()
		require.NoError(t, err)
		assert.Equal(t, map[wirtualsdk.FeatureName]int64{
			wirtualsdk.FeatureUserLimit:   200,
			wirtualsdk.FeatureAuditLog:    1,
			wirtualsdk.FeatureSCIM:        1,
			wirtualsdk.FeatureBrowserOnly: 1,
		}, features)
	})
}

func TestDeleteLicense(t *testing.T) {
	t.Parallel()
	t.Run("Empty", func(t *testing.T) {
		t.Parallel()
		client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		err := client.DeleteLicense(ctx, 1)
		errResp := &wirtualsdk.Error{}
		if xerrors.As(err, &errResp) {
			assert.Equal(t, 404, errResp.StatusCode())
		} else {
			t.Error("expected to get error status 404")
		}
	})

	t.Run("BadID", func(t *testing.T) {
		t.Parallel()
		client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		//nolint:gocritic // RBAC is irrelevant here.
		resp, err := client.Request(ctx, http.MethodDelete, "/api/v2/licenses/drivers", nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	})

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		client, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		wirtualdenttest.AddLicense(t, client, wirtualdenttest.LicenseOptions{
			AccountID: "testing",
			Features: license.Features{
				wirtualsdk.FeatureAuditLog: 1,
			},
		})
		wirtualdenttest.AddLicense(t, client, wirtualdenttest.LicenseOptions{
			AccountID: "testing2",
			Features: license.Features{
				wirtualsdk.FeatureAuditLog:  1,
				wirtualsdk.FeatureUserLimit: 200,
			},
		})

		licenses, err := client.Licenses(ctx)
		require.NoError(t, err)
		assert.Len(t, licenses, 2)
		for _, l := range licenses {
			err = client.DeleteLicense(ctx, l.ID)
			require.NoError(t, err)
		}
		licenses, err = client.Licenses(ctx)
		require.NoError(t, err)
		assert.Len(t, licenses, 0)
	})
}
