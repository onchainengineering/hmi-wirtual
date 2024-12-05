package enidpsync_test

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"cdr.dev/slog/sloggers/slogtest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/enidpsync"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/db2sdk"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbauthz"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbgen"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtestutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/entitlements"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/idpsync"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/runtimeconfig"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

type ExpectedUser struct {
	SyncError     bool
	Organizations []uuid.UUID
}

type Expectations struct {
	Name   string
	Claims jwt.MapClaims
	// Parse
	ParseError      func(t *testing.T, httpErr *idpsync.HTTPError)
	ExpectedParams  idpsync.OrganizationParams
	ExpectedEnabled bool
	// Mutate allows mutating the user before syncing
	Mutate func(t *testing.T, db database.Store, user database.User)
	Sync   ExpectedUser
}

type OrganizationSyncTestCase struct {
	Settings        idpsync.DeploymentSyncSettings
	RuntimeSettings *idpsync.OrganizationSyncSettings
	Entitlements    *entitlements.Set
	Exps            []Expectations
}

func TestOrganizationSync(t *testing.T) {
	t.Parallel()

	if dbtestutil.WillUsePostgres() {
		t.Skip("Skipping test because it populates a lot of db entries, which is slow on postgres")
	}

	requireUserOrgs := func(t *testing.T, db database.Store, user database.User, expected []uuid.UUID) {
		t.Helper()

		// nolint:gocritic // in testing
		members, err := db.OrganizationMembers(dbauthz.AsSystemRestricted(context.Background()), database.OrganizationMembersParams{
			UserID: user.ID,
		})
		require.NoError(t, err)

		foundIDs := db2sdk.List(members, func(m database.OrganizationMembersRow) uuid.UUID {
			return m.OrganizationMember.OrganizationID
		})
		require.ElementsMatch(t, expected, foundIDs, "match user organizations")
	}

	entitled := entitlements.New()
	entitled.Modify(func(entitlements *wirtualsdk.Entitlements) {
		entitlements.Features[wirtualsdk.FeatureMultipleOrganizations] = wirtualsdk.Feature{
			Entitlement: wirtualsdk.EntitlementEntitled,
			Enabled:     true,
			Limit:       nil,
			Actual:      nil,
		}
	})

	testCases := []struct {
		Name string
		Case func(t *testing.T, db database.Store) OrganizationSyncTestCase
	}{
		{
			Name: "SingleOrgDeployment",
			Case: func(t *testing.T, db database.Store) OrganizationSyncTestCase {
				def, _ := db.GetDefaultOrganization(context.Background())
				other := dbgen.Organization(t, db, database.Organization{})
				return OrganizationSyncTestCase{
					Entitlements: entitled,
					Settings: idpsync.DeploymentSyncSettings{
						OrganizationField:         "",
						OrganizationMapping:       nil,
						OrganizationAssignDefault: true,
					},
					Exps: []Expectations{
						{
							Name:   "NoOrganizations",
							Claims: jwt.MapClaims{},
							ExpectedParams: idpsync.OrganizationParams{
								SyncEntitled: true,
							},
							ExpectedEnabled: false,
							Sync: ExpectedUser{
								Organizations: []uuid.UUID{},
							},
						},
						{
							Name:   "AlreadyInOrgs",
							Claims: jwt.MapClaims{},
							ExpectedParams: idpsync.OrganizationParams{
								SyncEntitled: true,
							},
							ExpectedEnabled: false,
							Mutate: func(t *testing.T, db database.Store, user database.User) {
								dbgen.OrganizationMember(t, db, database.OrganizationMember{
									UserID:         user.ID,
									OrganizationID: def.ID,
								})
								dbgen.OrganizationMember(t, db, database.OrganizationMember{
									UserID:         user.ID,
									OrganizationID: other.ID,
								})
							},
							Sync: ExpectedUser{
								Organizations: []uuid.UUID{def.ID, other.ID},
							},
						},
					},
				}
			},
		},
		{
			Name: "MultiOrgWithDefault",
			Case: func(t *testing.T, db database.Store) OrganizationSyncTestCase {
				def, _ := db.GetDefaultOrganization(context.Background())
				one := dbgen.Organization(t, db, database.Organization{})
				two := dbgen.Organization(t, db, database.Organization{})
				three := dbgen.Organization(t, db, database.Organization{})
				return OrganizationSyncTestCase{
					Entitlements: entitled,
					Settings: idpsync.DeploymentSyncSettings{
						OrganizationField: "organizations",
						OrganizationMapping: map[string][]uuid.UUID{
							"first":  {one.ID},
							"second": {two.ID},
							"third":  {three.ID},
						},
						OrganizationAssignDefault: true,
					},
					Exps: []Expectations{
						{
							Name:   "NoOrganizations",
							Claims: jwt.MapClaims{},
							ExpectedParams: idpsync.OrganizationParams{
								SyncEntitled: true,
							},
							ExpectedEnabled: true,
							Sync: ExpectedUser{
								Organizations: []uuid.UUID{def.ID},
							},
						},
						{
							Name: "AlreadyInOrgs",
							Claims: jwt.MapClaims{
								"organizations": []string{"second", "extra"},
							},
							ExpectedParams: idpsync.OrganizationParams{
								SyncEntitled: true,
							},
							ExpectedEnabled: true,
							Mutate: func(t *testing.T, db database.Store, user database.User) {
								dbgen.OrganizationMember(t, db, database.OrganizationMember{
									UserID:         user.ID,
									OrganizationID: def.ID,
								})
								dbgen.OrganizationMember(t, db, database.OrganizationMember{
									UserID:         user.ID,
									OrganizationID: one.ID,
								})
							},
							Sync: ExpectedUser{
								Organizations: []uuid.UUID{def.ID, two.ID},
							},
						},
						{
							Name: "ManyClaims",
							Claims: jwt.MapClaims{
								// Add some repeats
								"organizations": []string{"second", "extra", "first", "third", "second", "second"},
							},
							ExpectedParams: idpsync.OrganizationParams{
								SyncEntitled: true,
							},
							ExpectedEnabled: true,
							Mutate: func(t *testing.T, db database.Store, user database.User) {
								dbgen.OrganizationMember(t, db, database.OrganizationMember{
									UserID:         user.ID,
									OrganizationID: def.ID,
								})
								dbgen.OrganizationMember(t, db, database.OrganizationMember{
									UserID:         user.ID,
									OrganizationID: one.ID,
								})
							},
							Sync: ExpectedUser{
								Organizations: []uuid.UUID{def.ID, one.ID, two.ID, three.ID},
							},
						},
					},
				}
			},
		},
		{
			Name: "DynamicSettings",
			Case: func(t *testing.T, db database.Store) OrganizationSyncTestCase {
				def, _ := db.GetDefaultOrganization(context.Background())
				one := dbgen.Organization(t, db, database.Organization{})
				two := dbgen.Organization(t, db, database.Organization{})
				three := dbgen.Organization(t, db, database.Organization{})
				return OrganizationSyncTestCase{
					Entitlements: entitled,
					Settings: idpsync.DeploymentSyncSettings{
						OrganizationField: "organizations",
						OrganizationMapping: map[string][]uuid.UUID{
							"first":  {one.ID},
							"second": {two.ID},
							"third":  {three.ID},
						},
						OrganizationAssignDefault: true,
					},
					// Override
					RuntimeSettings: &idpsync.OrganizationSyncSettings{
						Field: "dynamic",
						Mapping: map[string][]uuid.UUID{
							"third": {three.ID},
						},
						AssignDefault: false,
					},
					Exps: []Expectations{
						{
							Name:   "NoOrganizations",
							Claims: jwt.MapClaims{},
							ExpectedParams: idpsync.OrganizationParams{
								SyncEntitled: true,
							},
							ExpectedEnabled: true,
							Sync: ExpectedUser{
								Organizations: []uuid.UUID{},
							},
						},
						{
							Name: "AlreadyInOrgs",
							Claims: jwt.MapClaims{
								"organizations": []string{"second", "extra"},
								"dynamic":       []string{"third"},
							},
							ExpectedParams: idpsync.OrganizationParams{
								SyncEntitled: true,
							},
							ExpectedEnabled: true,
							Mutate: func(t *testing.T, db database.Store, user database.User) {
								dbgen.OrganizationMember(t, db, database.OrganizationMember{
									UserID:         user.ID,
									OrganizationID: def.ID,
								})
								dbgen.OrganizationMember(t, db, database.OrganizationMember{
									UserID:         user.ID,
									OrganizationID: one.ID,
								})
							},
							Sync: ExpectedUser{
								Organizations: []uuid.UUID{three.ID},
							},
						},
					},
				}
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			ctx := testutil.Context(t, testutil.WaitMedium)
			logger := slogtest.Make(t, &slogtest.Options{})

			rdb, _ := dbtestutil.NewDB(t)
			db := dbauthz.New(rdb, rbac.NewAuthorizer(prometheus.NewRegistry()), logger, wirtualdtest.AccessControlStorePointer())
			caseData := tc.Case(t, rdb)
			if caseData.Entitlements == nil {
				caseData.Entitlements = entitlements.New()
			}

			// Create a new sync object
			sync := enidpsync.NewSync(logger, runtimeconfig.NewManager(), caseData.Entitlements, caseData.Settings)
			if caseData.RuntimeSettings != nil {
				err := sync.UpdateOrganizationSettings(ctx, rdb, *caseData.RuntimeSettings)
				require.NoError(t, err)
			}

			for _, exp := range caseData.Exps {
				t.Run(exp.Name, func(t *testing.T) {
					params, httpErr := sync.ParseOrganizationClaims(ctx, exp.Claims)
					if exp.ParseError != nil {
						exp.ParseError(t, httpErr)
						return
					}
					require.Nil(t, httpErr, "no parse error")

					require.Equal(t, exp.ExpectedParams.SyncEntitled, params.SyncEntitled, "match enabled")
					require.Equal(t, exp.ExpectedEnabled, sync.OrganizationSyncEnabled(context.Background(), rdb))

					user := dbgen.User(t, db, database.User{})
					if exp.Mutate != nil {
						exp.Mutate(t, rdb, user)
					}

					err := sync.SyncOrganizations(ctx, rdb, user, params)
					if exp.Sync.SyncError {
						require.Error(t, err)
						return
					}
					require.NoError(t, err)
					requireUserOrgs(t, db, user, exp.Sync.Organizations)
				})
			}
		})
	}
}