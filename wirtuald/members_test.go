package wirtuald_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/db2sdk"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestAddMember(t *testing.T) {
	t.Parallel()

	t.Run("AlreadyMember", func(t *testing.T) {
		t.Parallel()
		owner := wirtualdtest.New(t, nil)
		first := wirtualdtest.CreateFirstUser(t, owner)
		_, user := wirtualdtest.CreateAnotherUser(t, owner, first.OrganizationID)

		ctx := testutil.Context(t, testutil.WaitMedium)
		// Add user to org, even though they already exist
		// nolint:gocritic // must be an owner to see the user
		_, err := owner.PostOrganizationMember(ctx, first.OrganizationID, user.Username)
		require.ErrorContains(t, err, "already exists")
	})
}

func TestDeleteMember(t *testing.T) {
	t.Parallel()

	t.Run("Allowed", func(t *testing.T) {
		t.Parallel()
		owner := wirtualdtest.New(t, nil)
		first := wirtualdtest.CreateFirstUser(t, owner)
		_, user := wirtualdtest.CreateAnotherUser(t, owner, first.OrganizationID)

		ctx := testutil.Context(t, testutil.WaitMedium)
		// Deleting members from the default org is not allowed.
		// If this behavior changes, and we allow deleting members from the default org,
		// this test should be updated to check there is no error.
		// nolint:gocritic // must be an owner to see the user
		err := owner.DeleteOrganizationMember(ctx, first.OrganizationID, user.Username)
		require.NoError(t, err)
	})
}

func TestListMembers(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()
		owner := wirtualdtest.New(t, nil)
		first := wirtualdtest.CreateFirstUser(t, owner)

		client, user := wirtualdtest.CreateAnotherUser(t, owner, first.OrganizationID, rbac.ScopedRoleOrgAdmin(first.OrganizationID))

		ctx := testutil.Context(t, testutil.WaitShort)
		members, err := client.OrganizationMembers(ctx, first.OrganizationID)
		require.NoError(t, err)
		require.Len(t, members, 2)
		require.ElementsMatch(t,
			[]uuid.UUID{first.UserID, user.ID},
			db2sdk.List(members, onlyIDs))
	})
}

func onlyIDs(u wirtualsdk.OrganizationMemberWithUserData) uuid.UUID {
	return u.UserID
}
