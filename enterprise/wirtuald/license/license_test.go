package license_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbmem"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtime"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
)

func TestEntitlements(t *testing.T) {
	t.Parallel()
	all := make(map[wirtualsdk.FeatureName]bool)
	for _, n := range wirtualsdk.FeatureNames {
		all[n] = true
	}

	empty := map[wirtualsdk.FeatureName]bool{}

	t.Run("Defaults", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)
		require.NoError(t, err)
		require.False(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)
		for _, featureName := range wirtualsdk.FeatureNames {
			require.False(t, entitlements.Features[featureName].Enabled)
			require.Equal(t, wirtualsdk.EntitlementNotEntitled, entitlements.Features[featureName].Entitlement)
		}
	})
	t.Run("Always return the current user count", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)
		require.NoError(t, err)
		require.False(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)
		require.Equal(t, *entitlements.Features[wirtualsdk.FeatureUserLimit].Actual, int64(0))
	})
	t.Run("SingleLicenseNothing", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{}),
			Exp: dbtime.Now().Add(time.Hour),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, empty)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)
		for _, featureName := range wirtualsdk.FeatureNames {
			require.False(t, entitlements.Features[featureName].Enabled)
			require.Equal(t, wirtualsdk.EntitlementNotEntitled, entitlements.Features[featureName].Entitlement)
		}
	})
	t.Run("SingleLicenseAll", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: func() license.Features {
					f := make(license.Features)
					for _, name := range wirtualsdk.FeatureNames {
						f[name] = 1
					}
					return f
				}(),
			}),
			Exp: dbtime.Now().Add(time.Hour),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, empty)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)
		for _, featureName := range wirtualsdk.FeatureNames {
			require.Equal(t, wirtualsdk.EntitlementEntitled, entitlements.Features[featureName].Entitlement, featureName)
		}
	})
	t.Run("SingleLicenseGrace", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureUserLimit: 100,
					wirtualsdk.FeatureAuditLog:  1,
				},

				GraceAt:   dbtime.Now().Add(-time.Hour),
				ExpiresAt: dbtime.Now().Add(time.Hour),
			}),
			Exp: dbtime.Now().Add(time.Hour),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)

		require.Equal(t, wirtualsdk.EntitlementGracePeriod, entitlements.Features[wirtualsdk.FeatureAuditLog].Entitlement)
		require.Contains(
			t, entitlements.Warnings,
			fmt.Sprintf("%s is enabled but your license for this feature is expired.", wirtualsdk.FeatureAuditLog.Humanize()),
		)
	})
	t.Run("Expiration warning", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureUserLimit: 100,
					wirtualsdk.FeatureAuditLog:  1,
				},

				GraceAt:   dbtime.Now().AddDate(0, 0, 2),
				ExpiresAt: dbtime.Now().AddDate(0, 0, 5),
			}),
			Exp: dbtime.Now().AddDate(0, 0, 5),
		})

		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)

		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)

		require.Equal(t, wirtualsdk.EntitlementEntitled, entitlements.Features[wirtualsdk.FeatureAuditLog].Entitlement)
		require.Contains(
			t, entitlements.Warnings,
			"Your license expires in 2 days.",
		)
	})

	t.Run("Expiration warning for license expiring in 1 day", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureUserLimit: 100,
					wirtualsdk.FeatureAuditLog:  1,
				},

				GraceAt:   dbtime.Now().AddDate(0, 0, 1),
				ExpiresAt: dbtime.Now().AddDate(0, 0, 5),
			}),
			Exp: time.Now().AddDate(0, 0, 5),
		})

		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)

		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)

		require.Equal(t, wirtualsdk.EntitlementEntitled, entitlements.Features[wirtualsdk.FeatureAuditLog].Entitlement)
		require.Contains(
			t, entitlements.Warnings,
			"Your license expires in 1 day.",
		)
	})

	t.Run("Expiration warning for trials", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureUserLimit: 100,
					wirtualsdk.FeatureAuditLog:  1,
				},

				Trial:     true,
				GraceAt:   dbtime.Now().AddDate(0, 0, 8),
				ExpiresAt: dbtime.Now().AddDate(0, 0, 5),
			}),
			Exp: dbtime.Now().AddDate(0, 0, 5),
		})

		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)

		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.True(t, entitlements.Trial)

		require.Equal(t, wirtualsdk.EntitlementEntitled, entitlements.Features[wirtualsdk.FeatureAuditLog].Entitlement)
		require.NotContains( // it should not contain a warning since it is a trial license
			t, entitlements.Warnings,
			"Your license expires in 8 days.",
		)
	})

	t.Run("Expiration warning for non trials", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureUserLimit: 100,
					wirtualsdk.FeatureAuditLog:  1,
				},

				GraceAt:   dbtime.Now().AddDate(0, 0, 30),
				ExpiresAt: dbtime.Now().AddDate(0, 0, 5),
			}),
			Exp: dbtime.Now().AddDate(0, 0, 5),
		})

		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)

		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)

		require.Equal(t, wirtualsdk.EntitlementEntitled, entitlements.Features[wirtualsdk.FeatureAuditLog].Entitlement)
		require.NotContains( // it should not contain a warning since it is a trial license
			t, entitlements.Warnings,
			"Your license expires in 30 days.",
		)
	})

	t.Run("SingleLicenseNotEntitled", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{}),
			Exp: time.Now().Add(time.Hour),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)
		for _, featureName := range wirtualsdk.FeatureNames {
			if featureName == wirtualsdk.FeatureUserLimit {
				continue
			}
			if featureName == wirtualsdk.FeatureHighAvailability {
				continue
			}
			if featureName == wirtualsdk.FeatureMultipleExternalAuth {
				continue
			}
			niceName := featureName.Humanize()
			// Ensures features that are not entitled are properly disabled.
			require.False(t, entitlements.Features[featureName].Enabled)
			require.Equal(t, wirtualsdk.EntitlementNotEntitled, entitlements.Features[featureName].Entitlement)
			require.Contains(t, entitlements.Warnings, fmt.Sprintf("%s is enabled but your license is not entitled to this feature.", niceName))
		}
	})
	t.Run("TooManyUsers", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		activeUser1, err := db.InsertUser(context.Background(), database.InsertUserParams{
			ID:        uuid.New(),
			Username:  "test1",
			LoginType: database.LoginTypePassword,
		})
		require.NoError(t, err)
		_, err = db.UpdateUserStatus(context.Background(), database.UpdateUserStatusParams{
			ID:        activeUser1.ID,
			Status:    database.UserStatusActive,
			UpdatedAt: dbtime.Now(),
		})
		require.NoError(t, err)
		activeUser2, err := db.InsertUser(context.Background(), database.InsertUserParams{
			ID:        uuid.New(),
			Username:  "test2",
			LoginType: database.LoginTypePassword,
		})
		require.NoError(t, err)
		_, err = db.UpdateUserStatus(context.Background(), database.UpdateUserStatusParams{
			ID:        activeUser2.ID,
			Status:    database.UserStatusActive,
			UpdatedAt: dbtime.Now(),
		})
		require.NoError(t, err)
		_, err = db.InsertUser(context.Background(), database.InsertUserParams{
			ID:        uuid.New(),
			Username:  "dormant-user",
			LoginType: database.LoginTypePassword,
		})
		require.NoError(t, err)
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureUserLimit: 1,
				},
			}),
			Exp: time.Now().Add(time.Hour),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, empty)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.Contains(t, entitlements.Warnings, "Your deployment has 2 active users but is only licensed for 1.")
	})
	t.Run("MaximizeUserLimit", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertUser(context.Background(), database.InsertUserParams{})
		db.InsertUser(context.Background(), database.InsertUserParams{})
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureUserLimit: 10,
				},
				GraceAt: time.Now().Add(59 * 24 * time.Hour),
			}),
			Exp: time.Now().Add(60 * 24 * time.Hour),
		})
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureUserLimit: 1,
				},
				GraceAt: time.Now().Add(59 * 24 * time.Hour),
			}),
			Exp: time.Now().Add(60 * 24 * time.Hour),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, empty)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.Empty(t, entitlements.Warnings)
	})
	t.Run("MultipleLicenseEnabled", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		// One trial
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			Exp: time.Now().Add(time.Hour),
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Trial: true,
			}),
		})
		// One not
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			Exp: time.Now().Add(time.Hour),
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Trial: false,
			}),
		})

		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, empty)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)
	})

	t.Run("Enterprise", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		_, err := db.InsertLicense(context.Background(), database.InsertLicenseParams{
			Exp: time.Now().Add(time.Hour),
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				FeatureSet: wirtualsdk.FeatureSetEnterprise,
			}),
		})
		require.NoError(t, err)
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)

		// All enterprise features should be entitled
		enterpriseFeatures := wirtualsdk.FeatureSetEnterprise.Features()
		for _, featureName := range wirtualsdk.FeatureNames {
			if featureName == wirtualsdk.FeatureUserLimit {
				continue
			}
			if slices.Contains(enterpriseFeatures, featureName) {
				require.True(t, entitlements.Features[featureName].Enabled, featureName)
				require.Equal(t, wirtualsdk.EntitlementEntitled, entitlements.Features[featureName].Entitlement)
			} else {
				require.False(t, entitlements.Features[featureName].Enabled, featureName)
				require.Equal(t, wirtualsdk.EntitlementNotEntitled, entitlements.Features[featureName].Entitlement)
			}
		}
	})

	t.Run("Premium", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		_, err := db.InsertLicense(context.Background(), database.InsertLicenseParams{
			Exp: time.Now().Add(time.Hour),
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				FeatureSet: wirtualsdk.FeatureSetPremium,
			}),
		})
		require.NoError(t, err)
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)

		// All premium features should be entitled
		enterpriseFeatures := wirtualsdk.FeatureSetPremium.Features()
		for _, featureName := range wirtualsdk.FeatureNames {
			if featureName == wirtualsdk.FeatureUserLimit {
				continue
			}
			if slices.Contains(enterpriseFeatures, featureName) {
				require.True(t, entitlements.Features[featureName].Enabled, featureName)
				require.Equal(t, wirtualsdk.EntitlementEntitled, entitlements.Features[featureName].Entitlement)
			} else {
				require.False(t, entitlements.Features[featureName].Enabled, featureName)
				require.Equal(t, wirtualsdk.EntitlementNotEntitled, entitlements.Features[featureName].Entitlement)
			}
		}
	})

	t.Run("SetNone", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		_, err := db.InsertLicense(context.Background(), database.InsertLicenseParams{
			Exp: time.Now().Add(time.Hour),
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				FeatureSet: "",
			}),
		})
		require.NoError(t, err)
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)

		for _, featureName := range wirtualsdk.FeatureNames {
			require.False(t, entitlements.Features[featureName].Enabled, featureName)
			require.Equal(t, wirtualsdk.EntitlementNotEntitled, entitlements.Features[featureName].Entitlement)
		}
	})

	// AllFeatures uses the deprecated 'AllFeatures' boolean.
	t.Run("AllFeatures", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			Exp: time.Now().Add(time.Hour),
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				AllFeatures: true,
			}),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)

		// All enterprise features should be entitled
		enterpriseFeatures := wirtualsdk.FeatureSetEnterprise.Features()
		for _, featureName := range wirtualsdk.FeatureNames {
			if featureName == wirtualsdk.FeatureUserLimit {
				continue
			}
			if slices.Contains(enterpriseFeatures, featureName) {
				require.True(t, entitlements.Features[featureName].Enabled, featureName)
				require.Equal(t, wirtualsdk.EntitlementEntitled, entitlements.Features[featureName].Entitlement)
			} else {
				require.False(t, entitlements.Features[featureName].Enabled, featureName)
				require.Equal(t, wirtualsdk.EntitlementNotEntitled, entitlements.Features[featureName].Entitlement)
			}
		}
	})

	t.Run("AllFeaturesAlwaysEnable", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			Exp: dbtime.Now().Add(time.Hour),
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				AllFeatures: true,
			}),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, empty)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)
		// All enterprise features should be entitled
		enterpriseFeatures := wirtualsdk.FeatureSetEnterprise.Features()
		for _, featureName := range wirtualsdk.FeatureNames {
			if featureName == wirtualsdk.FeatureUserLimit {
				continue
			}

			feature := entitlements.Features[featureName]
			if slices.Contains(enterpriseFeatures, featureName) {
				require.Equal(t, featureName.AlwaysEnable(), feature.Enabled)
				require.Equal(t, wirtualsdk.EntitlementEntitled, feature.Entitlement)
			} else {
				require.False(t, entitlements.Features[featureName].Enabled, featureName)
				require.Equal(t, wirtualsdk.EntitlementNotEntitled, entitlements.Features[featureName].Entitlement)
			}
		}
	})

	t.Run("AllFeaturesGrace", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			Exp: dbtime.Now().Add(time.Hour),
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				AllFeatures: true,
				GraceAt:     dbtime.Now().Add(-time.Hour),
				ExpiresAt:   dbtime.Now().Add(time.Hour),
			}),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 1, 1, wirtualdenttest.Keys, all)
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.False(t, entitlements.Trial)
		// All enterprise features should be entitled
		enterpriseFeatures := wirtualsdk.FeatureSetEnterprise.Features()
		for _, featureName := range wirtualsdk.FeatureNames {
			if featureName == wirtualsdk.FeatureUserLimit {
				continue
			}
			if slices.Contains(enterpriseFeatures, featureName) {
				require.True(t, entitlements.Features[featureName].Enabled, featureName)
				require.Equal(t, wirtualsdk.EntitlementGracePeriod, entitlements.Features[featureName].Entitlement)
			} else {
				require.False(t, entitlements.Features[featureName].Enabled, featureName)
				require.Equal(t, wirtualsdk.EntitlementNotEntitled, entitlements.Features[featureName].Entitlement)
			}
		}
	})

	t.Run("MultipleReplicasNoLicense", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		entitlements, err := license.Entitlements(context.Background(), db, 2, 1, wirtualdenttest.Keys, all)
		require.NoError(t, err)
		require.False(t, entitlements.HasLicense)
		require.Len(t, entitlements.Errors, 1)
		require.Equal(t, "You have multiple replicas but high availability is an Enterprise feature. You will be unable to connect to workspaces.", entitlements.Errors[0])
	})

	t.Run("MultipleReplicasNotEntitled", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			Exp: time.Now().Add(time.Hour),
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAuditLog: 1,
				},
			}),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 2, 1, wirtualdenttest.Keys, map[wirtualsdk.FeatureName]bool{
			wirtualsdk.FeatureHighAvailability: true,
		})
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.Len(t, entitlements.Errors, 1)
		require.Equal(t, "You have multiple replicas but your license is not entitled to high availability. You will be unable to connect to workspaces.", entitlements.Errors[0])
	})

	t.Run("MultipleReplicasGrace", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureHighAvailability: 1,
				},
				GraceAt:   time.Now().Add(-time.Hour),
				ExpiresAt: time.Now().Add(time.Hour),
			}),
			Exp: time.Now().Add(time.Hour),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 2, 1, wirtualdenttest.Keys, map[wirtualsdk.FeatureName]bool{
			wirtualsdk.FeatureHighAvailability: true,
		})
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.Len(t, entitlements.Warnings, 1)
		require.Equal(t, "You have multiple replicas but your license for high availability is expired. Reduce to one replica or workspace connections will stop working.", entitlements.Warnings[0])
	})

	t.Run("MultipleGitAuthNoLicense", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		entitlements, err := license.Entitlements(context.Background(), db, 1, 2, wirtualdenttest.Keys, all)
		require.NoError(t, err)
		require.False(t, entitlements.HasLicense)
		require.Len(t, entitlements.Errors, 1)
		require.Equal(t, "You have multiple External Auth Providers configured but this is an Enterprise feature. Reduce to one.", entitlements.Errors[0])
	})

	t.Run("MultipleGitAuthNotEntitled", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			Exp: time.Now().Add(time.Hour),
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAuditLog: 1,
				},
			}),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 1, 2, wirtualdenttest.Keys, map[wirtualsdk.FeatureName]bool{
			wirtualsdk.FeatureMultipleExternalAuth: true,
		})
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.Len(t, entitlements.Errors, 1)
		require.Equal(t, "You have multiple External Auth Providers configured but your license is limited at one.", entitlements.Errors[0])
	})

	t.Run("MultipleGitAuthGrace", func(t *testing.T) {
		t.Parallel()
		db := dbmem.New()
		db.InsertLicense(context.Background(), database.InsertLicenseParams{
			JWT: wirtualdenttest.GenerateLicense(t, wirtualdenttest.LicenseOptions{
				GraceAt:   time.Now().Add(-time.Hour),
				ExpiresAt: time.Now().Add(time.Hour),
				Features: license.Features{
					wirtualsdk.FeatureMultipleExternalAuth: 1,
				},
			}),
			Exp: time.Now().Add(time.Hour),
		})
		entitlements, err := license.Entitlements(context.Background(), db, 1, 2, wirtualdenttest.Keys, map[wirtualsdk.FeatureName]bool{
			wirtualsdk.FeatureMultipleExternalAuth: true,
		})
		require.NoError(t, err)
		require.True(t, entitlements.HasLicense)
		require.Len(t, entitlements.Warnings, 1)
		require.Equal(t, "You have multiple External Auth Providers configured but your license is expired. Reduce to one.", entitlements.Warnings[0])
	})
}

func TestLicenseEntitlements(t *testing.T) {
	t.Parallel()

	// We must use actual 'time.Now()' in tests because the jwt library does
	// not accept a custom time function. The only way to change it is as a
	// package global, which does not work in t.Parallel().

	// This list comes from wirtuald.go on launch. This list is a bit arbitrary,
	// maybe some should be moved to "AlwaysEnabled" instead.
	defaultEnablements := map[wirtualsdk.FeatureName]bool{
		wirtualsdk.FeatureAuditLog:                   true,
		wirtualsdk.FeatureBrowserOnly:                true,
		wirtualsdk.FeatureSCIM:                       true,
		wirtualsdk.FeatureMultipleExternalAuth:       true,
		wirtualsdk.FeatureTemplateRBAC:               true,
		wirtualsdk.FeatureExternalTokenEncryption:    true,
		wirtualsdk.FeatureExternalProvisionerDaemons: true,
		wirtualsdk.FeatureAdvancedTemplateScheduling: true,
		wirtualsdk.FeatureWorkspaceProxy:             true,
		wirtualsdk.FeatureUserRoleManagement:         true,
		wirtualsdk.FeatureAccessControl:              true,
		wirtualsdk.FeatureControlSharedPorts:         true,
	}

	legacyLicense := func() *wirtualdenttest.LicenseOptions {
		return (&wirtualdenttest.LicenseOptions{
			AccountType: "salesforce",
			AccountID:   "Alice",
			Trial:       false,
			// Use the legacy boolean
			AllFeatures: true,
		}).Valid(time.Now())
	}

	enterpriseLicense := func() *wirtualdenttest.LicenseOptions {
		return (&wirtualdenttest.LicenseOptions{
			AccountType:   "salesforce",
			AccountID:     "Bob",
			DeploymentIDs: nil,
			Trial:         false,
			FeatureSet:    wirtualsdk.FeatureSetEnterprise,
			AllFeatures:   true,
		}).Valid(time.Now())
	}

	premiumLicense := func() *wirtualdenttest.LicenseOptions {
		return (&wirtualdenttest.LicenseOptions{
			AccountType:   "salesforce",
			AccountID:     "Charlie",
			DeploymentIDs: nil,
			Trial:         false,
			FeatureSet:    wirtualsdk.FeatureSetPremium,
			AllFeatures:   true,
		}).Valid(time.Now())
	}

	testCases := []struct {
		Name        string
		Licenses    []*wirtualdenttest.LicenseOptions
		Enablements map[wirtualsdk.FeatureName]bool
		Arguments   license.FeatureArguments

		ExpectedErrorContains string
		AssertEntitlements    func(t *testing.T, entitlements wirtualsdk.Entitlements)
	}{
		{
			Name: "NoLicenses",
			AssertEntitlements: func(t *testing.T, entitlements wirtualsdk.Entitlements) {
				assertNoErrors(t, entitlements)
				assertNoWarnings(t, entitlements)
				assert.False(t, entitlements.HasLicense)
				assert.False(t, entitlements.Trial)
			},
		},
		{
			Name: "MixedUsedCounts",
			Licenses: []*wirtualdenttest.LicenseOptions{
				legacyLicense().UserLimit(100),
				enterpriseLicense().UserLimit(500),
			},
			Enablements: defaultEnablements,
			Arguments: license.FeatureArguments{
				ActiveUserCount:   50,
				ReplicaCount:      0,
				ExternalAuthCount: 0,
			},
			AssertEntitlements: func(t *testing.T, entitlements wirtualsdk.Entitlements) {
				assertEnterpriseFeatures(t, entitlements)
				assertNoErrors(t, entitlements)
				assertNoWarnings(t, entitlements)
				userFeature := entitlements.Features[wirtualsdk.FeatureUserLimit]
				assert.Equalf(t, int64(500), *userFeature.Limit, "user limit")
				assert.Equalf(t, int64(50), *userFeature.Actual, "user count")
			},
		},
		{
			Name: "MixedUsedCountsWithExpired",
			Licenses: []*wirtualdenttest.LicenseOptions{
				// This license is ignored
				enterpriseLicense().UserLimit(500).Expired(time.Now()),
				enterpriseLicense().UserLimit(100),
			},
			Enablements: defaultEnablements,
			Arguments: license.FeatureArguments{
				ActiveUserCount:   200,
				ReplicaCount:      0,
				ExternalAuthCount: 0,
			},
			AssertEntitlements: func(t *testing.T, entitlements wirtualsdk.Entitlements) {
				assertEnterpriseFeatures(t, entitlements)
				userFeature := entitlements.Features[wirtualsdk.FeatureUserLimit]
				assert.Equalf(t, int64(100), *userFeature.Limit, "user limit")
				assert.Equalf(t, int64(200), *userFeature.Actual, "user count")

				require.Len(t, entitlements.Errors, 1, "invalid license error")
				require.Len(t, entitlements.Warnings, 1, "user count exceeds warning")
				require.Contains(t, entitlements.Errors[0], "Invalid license")
				require.Contains(t, entitlements.Warnings[0], "active users but is only licensed for")
			},
		},
		{
			// The new license does not have enough seats to cover the active user count.
			// The old license is in it's grace period.
			Name: "MixedUsedCountsWithGrace",
			Licenses: []*wirtualdenttest.LicenseOptions{
				enterpriseLicense().UserLimit(500).GracePeriod(time.Now()),
				enterpriseLicense().UserLimit(100),
			},
			Enablements: defaultEnablements,
			Arguments: license.FeatureArguments{
				ActiveUserCount:   200,
				ReplicaCount:      0,
				ExternalAuthCount: 0,
			},
			AssertEntitlements: func(t *testing.T, entitlements wirtualsdk.Entitlements) {
				userFeature := entitlements.Features[wirtualsdk.FeatureUserLimit]
				assert.Equalf(t, int64(500), *userFeature.Limit, "user limit")
				assert.Equalf(t, int64(200), *userFeature.Actual, "user count")
				assert.Equal(t, userFeature.Entitlement, wirtualsdk.EntitlementGracePeriod)
			},
		},
		{
			// Legacy license uses the "AllFeatures" boolean
			Name: "LegacyLicense",
			Licenses: []*wirtualdenttest.LicenseOptions{
				legacyLicense().UserLimit(100),
			},
			Enablements: defaultEnablements,
			Arguments: license.FeatureArguments{
				ActiveUserCount:   50,
				ReplicaCount:      0,
				ExternalAuthCount: 0,
			},
			AssertEntitlements: func(t *testing.T, entitlements wirtualsdk.Entitlements) {
				assertEnterpriseFeatures(t, entitlements)
				assertNoErrors(t, entitlements)
				assertNoWarnings(t, entitlements)
				userFeature := entitlements.Features[wirtualsdk.FeatureUserLimit]
				assert.Equalf(t, int64(100), *userFeature.Limit, "user limit")
				assert.Equalf(t, int64(50), *userFeature.Actual, "user count")
			},
		},
		{
			Name: "EnterpriseDisabledMultiOrg",
			Licenses: []*wirtualdenttest.LicenseOptions{
				enterpriseLicense().UserLimit(100),
			},
			Enablements:           defaultEnablements,
			Arguments:             license.FeatureArguments{},
			ExpectedErrorContains: "",
			AssertEntitlements: func(t *testing.T, entitlements wirtualsdk.Entitlements) {
				assert.False(t, entitlements.Features[wirtualsdk.FeatureMultipleOrganizations].Enabled, "multi-org only enabled for premium")
				assert.False(t, entitlements.Features[wirtualsdk.FeatureCustomRoles].Enabled, "custom-roles only enabled for premium")
			},
		},
		{
			Name: "PremiumEnabledMultiOrg",
			Licenses: []*wirtualdenttest.LicenseOptions{
				premiumLicense().UserLimit(100),
			},
			Enablements:           defaultEnablements,
			Arguments:             license.FeatureArguments{},
			ExpectedErrorContains: "",
			AssertEntitlements: func(t *testing.T, entitlements wirtualsdk.Entitlements) {
				assert.True(t, entitlements.Features[wirtualsdk.FeatureMultipleOrganizations].Enabled, "multi-org enabled for premium")
				assert.True(t, entitlements.Features[wirtualsdk.FeatureCustomRoles].Enabled, "custom-roles enabled for premium")
			},
		},
		{
			Name: "CurrentAndFuture",
			Licenses: []*wirtualdenttest.LicenseOptions{
				enterpriseLicense().UserLimit(100),
				premiumLicense().UserLimit(200).FutureTerm(time.Now()),
			},
			Enablements: defaultEnablements,
			AssertEntitlements: func(t *testing.T, entitlements wirtualsdk.Entitlements) {
				assertEnterpriseFeatures(t, entitlements)
				assertNoErrors(t, entitlements)
				assertNoWarnings(t, entitlements)
				userFeature := entitlements.Features[wirtualsdk.FeatureUserLimit]
				assert.Equalf(t, int64(100), *userFeature.Limit, "user limit")
				assert.Equal(t, wirtualsdk.EntitlementNotEntitled,
					entitlements.Features[wirtualsdk.FeatureMultipleOrganizations].Entitlement)
				assert.Equal(t, wirtualsdk.EntitlementNotEntitled,
					entitlements.Features[wirtualsdk.FeatureCustomRoles].Entitlement)
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			generatedLicenses := make([]database.License, 0, len(tc.Licenses))
			for i, lo := range tc.Licenses {
				generatedLicenses = append(generatedLicenses, database.License{
					ID:         int32(i),
					UploadedAt: time.Now().Add(time.Hour * -1),
					JWT:        lo.Generate(t),
					Exp:        lo.GraceAt,
					UUID:       uuid.New(),
				})
			}

			entitlements, err := license.LicensesEntitlements(time.Now(), generatedLicenses, tc.Enablements, wirtualdenttest.Keys, tc.Arguments)
			if tc.ExpectedErrorContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.ExpectedErrorContains)
			} else {
				require.NoError(t, err)
				tc.AssertEntitlements(t, entitlements)
			}
		})
	}
}

func assertNoErrors(t *testing.T, entitlements wirtualsdk.Entitlements) {
	assert.Empty(t, entitlements.Errors, "no errors")
}

func assertNoWarnings(t *testing.T, entitlements wirtualsdk.Entitlements) {
	assert.Empty(t, entitlements.Warnings, "no warnings")
}

func assertEnterpriseFeatures(t *testing.T, entitlements wirtualsdk.Entitlements) {
	for _, expected := range wirtualsdk.FeatureSetEnterprise.Features() {
		f := entitlements.Features[expected]
		assert.Equalf(t, wirtualsdk.EntitlementEntitled, f.Entitlement, "%s entitled", expected)
		assert.Equalf(t, true, f.Enabled, "%s enabled", expected)
	}
}
