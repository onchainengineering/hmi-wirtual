package wirtuald_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestProvisionerKeys(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong*10)
	t.Cleanup(cancel)
	dv := wirtualdtest.DeploymentValues(t)
	client, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
		Options: &wirtualdtest.Options{
			DeploymentValues: dv,
		},
		LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureExternalProvisionerDaemons: 1,
				wirtualsdk.FeatureMultipleOrganizations:      1,
			},
		},
	})
	orgAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.ScopedRoleOrgAdmin(owner.OrganizationID))
	member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
	otherOrg := wirtualdenttest.CreateOrganization(t, client, wirtualdenttest.CreateOrganizationOptions{})
	outsideOrgAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, otherOrg.ID, rbac.ScopedRoleOrgAdmin(otherOrg.ID))

	// member cannot create a provisioner key
	_, err := member.CreateProvisionerKey(ctx, otherOrg.ID, wirtualsdk.CreateProvisionerKeyRequest{
		Name: "key",
	})
	require.ErrorContains(t, err, "Resource not found")

	// member cannot list provisioner keys
	_, err = member.ListProvisionerKeys(ctx, otherOrg.ID)
	require.ErrorContains(t, err, "Resource not found")

	// member cannot delete a provisioner key
	err = member.DeleteProvisionerKey(ctx, otherOrg.ID, "key")
	require.ErrorContains(t, err, "Resource not found")

	// outside org admin cannot create a provisioner key
	_, err = outsideOrgAdmin.CreateProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
		Name: "key",
	})
	require.ErrorContains(t, err, "Resource not found")

	// outside org admin cannot list provisioner keys
	_, err = outsideOrgAdmin.ListProvisionerKeys(ctx, owner.OrganizationID)
	require.ErrorContains(t, err, "Resource not found")

	// outside org admin cannot delete a provisioner key
	err = outsideOrgAdmin.DeleteProvisionerKey(ctx, owner.OrganizationID, "key")
	require.ErrorContains(t, err, "Resource not found")

	// org admin cannot create reserved provisioner keys
	_, err = orgAdmin.CreateProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
		Name: wirtualsdk.ProvisionerKeyNameBuiltIn,
	})
	require.ErrorContains(t, err, "reserved")
	_, err = orgAdmin.CreateProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
		Name: wirtualsdk.ProvisionerKeyNameUserAuth,
	})
	require.ErrorContains(t, err, "reserved")
	_, err = orgAdmin.CreateProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
		Name: wirtualsdk.ProvisionerKeyNamePSK,
	})
	require.ErrorContains(t, err, "reserved")

	// org admin can list provisioner keys and get an empty list
	keys, err := orgAdmin.ListProvisionerKeys(ctx, owner.OrganizationID)
	require.NoError(t, err, "org admin list provisioner keys")
	require.Len(t, keys, 0, "org admin list provisioner keys")

	tags := map[string]string{
		"my": "way",
	}
	// org admin can create a provisioner key
	_, err = orgAdmin.CreateProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
		Name: "Key", // case insensitive
		Tags: tags,
	})
	require.NoError(t, err, "org admin create provisioner key")

	// org admin can conflict on name creating a provisioner key
	_, err = orgAdmin.CreateProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
		Name: "KEY", // still conflicts
	})
	require.ErrorContains(t, err, "already exists in organization")

	// key name cannot be too long
	_, err = orgAdmin.CreateProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
		Name: "Everyone please pass your watermelons to the front of the pool, the storm is approaching.",
	})
	require.ErrorContains(t, err, "must be at most 64 characters")

	// key name cannot be empty
	_, err = orgAdmin.CreateProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
		Name: "",
	})
	require.ErrorContains(t, err, "is required")

	// org admin can list provisioner keys
	keys, err = orgAdmin.ListProvisionerKeys(ctx, owner.OrganizationID)
	require.NoError(t, err, "org admin list provisioner keys")
	require.Len(t, keys, 1, "org admin list provisioner keys")
	require.Equal(t, "key", keys[0].Name, "org admin list provisioner keys name matches")
	require.EqualValues(t, tags, keys[0].Tags, "org admin list provisioner keys tags match")

	// org admin can delete a provisioner key
	err = orgAdmin.DeleteProvisionerKey(ctx, owner.OrganizationID, "key") // using lowercase here works
	require.NoError(t, err, "org admin delete provisioner key")

	// org admin cannot delete a provisioner key that doesn't exist
	err = orgAdmin.DeleteProvisionerKey(ctx, owner.OrganizationID, "key")
	require.ErrorContains(t, err, "Resource not found")

	// org admin cannot delete reserved provisioner keys
	err = orgAdmin.DeleteProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.ProvisionerKeyNameBuiltIn)
	require.ErrorContains(t, err, "reserved")
	err = orgAdmin.DeleteProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.ProvisionerKeyNameUserAuth)
	require.ErrorContains(t, err, "reserved")
	err = orgAdmin.DeleteProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.ProvisionerKeyNamePSK)
	require.ErrorContains(t, err, "reserved")
}

func TestGetProvisionerKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		useFakeKey  bool
		fakeKey     string
		success     bool
		expectedErr string
	}{
		{
			name:        "ok",
			success:     true,
			expectedErr: "",
		},
		{
			name:        "using unknown key",
			useFakeKey:  true,
			fakeKey:     "unknownKey",
			success:     false,
			expectedErr: "provisioner daemon key invalid",
		},
		{
			name:        "no key provided",
			useFakeKey:  true,
			fakeKey:     "",
			success:     false,
			expectedErr: "provisioner daemon key required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := testutil.Context(t, testutil.WaitShort)
			dv := wirtualdtest.DeploymentValues(t)
			client, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
				Options: &wirtualdtest.Options{
					DeploymentValues: dv,
				},
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					Features: license.Features{
						wirtualsdk.FeatureMultipleOrganizations:      1,
						wirtualsdk.FeatureExternalProvisionerDaemons: 1,
					},
				},
			})

			//nolint:gocritic // ignore This client is operating as the owner user, which has unrestricted permissions
			key, err := client.CreateProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
				Name: "my-test-key",
				Tags: map[string]string{"key1": "value1", "key2": "value2"},
			})
			require.NoError(t, err)

			pk := key.Key
			if tt.useFakeKey {
				pk = tt.fakeKey
			}

			fetchedKey, err := client.GetProvisionerKey(ctx, pk)
			if !tt.success {
				require.ErrorContains(t, err, tt.expectedErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, fetchedKey.Name, "my-test-key")
				require.Equal(t, fetchedKey.Tags, wirtualsdk.ProvisionerKeyTags{"key1": "value1", "key2": "value2"})
			}
		})
	}

	t.Run("TestPSK", func(t *testing.T) {
		t.Parallel()
		const testPSK = "psk-testing-purpose"
		ctx := testutil.Context(t, testutil.WaitShort)
		dv := wirtualdtest.DeploymentValues(t)
		client, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			ProvisionerDaemonPSK: testPSK,
			Options: &wirtualdtest.Options{
				DeploymentValues: dv,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations:      1,
					wirtualsdk.FeatureExternalProvisionerDaemons: 1,
				},
			},
		})

		//nolint:gocritic // ignore This client is operating as the owner user, which has unrestricted permissions
		_, err := client.CreateProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
			Name: "my-test-key",
			Tags: map[string]string{"key1": "value1", "key2": "value2"},
		})
		require.NoError(t, err)

		fetchedKey, err := client.GetProvisionerKey(ctx, testPSK)
		require.ErrorContains(t, err, "provisioner daemon key invalid")
		require.Empty(t, fetchedKey)
	})

	t.Run("TestSessionToken", func(t *testing.T) {
		t.Parallel()

		ctx := testutil.Context(t, testutil.WaitShort)
		dv := wirtualdtest.DeploymentValues(t)
		client, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				DeploymentValues: dv,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations:      1,
					wirtualsdk.FeatureExternalProvisionerDaemons: 1,
				},
			},
		})

		//nolint:gocritic // ignore This client is operating as the owner user, which has unrestricted permissions
		_, err := client.CreateProvisionerKey(ctx, owner.OrganizationID, wirtualsdk.CreateProvisionerKeyRequest{
			Name: "my-test-key",
			Tags: map[string]string{"key1": "value1", "key2": "value2"},
		})
		require.NoError(t, err)

		fetchedKey, err := client.GetProvisionerKey(ctx, client.SessionToken())
		require.ErrorContains(t, err, "provisioner daemon key invalid")
		require.Empty(t, fetchedKey)
	})
}
