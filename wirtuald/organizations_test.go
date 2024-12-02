package wirtuald_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/testutil"
)

func TestOrganizationByUserAndName(t *testing.T) {
	t.Parallel()
	t.Run("NoExist", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		wirtualdtest.CreateFirstUser(t, client)
		ctx := testutil.Context(t, testutil.WaitLong)

		_, err := client.OrganizationByUserAndName(ctx, wirtualsdk.Me, "nothing")
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusNotFound, apiErr.StatusCode())
	})

	t.Run("Valid", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		ctx := testutil.Context(t, testutil.WaitLong)

		org, err := client.Organization(ctx, user.OrganizationID)
		require.NoError(t, err)
		_, err = client.OrganizationByUserAndName(ctx, wirtualsdk.Me, org.Name)
		require.NoError(t, err)
	})
}
