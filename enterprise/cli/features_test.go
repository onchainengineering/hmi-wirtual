package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestFeaturesList(t *testing.T) {
	t.Parallel()
	t.Run("Table", func(t *testing.T) {
		t.Parallel()
		client, admin := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID)
		inv, conf := newCLI(t, "features", "list")
		clitest.SetupConfig(t, anotherClient, conf)
		pty := ptytest.New(t).Attach(inv)
		clitest.Start(t, inv)
		pty.ExpectMatch("user_limit")
		pty.ExpectMatch("not_entitled")
	})
	t.Run("JSON", func(t *testing.T) {
		t.Parallel()

		client, admin := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID)
		inv, conf := newCLI(t, "features", "list", "-o", "json")
		clitest.SetupConfig(t, anotherClient, conf)
		doneChan := make(chan struct{})

		buf := bytes.NewBuffer(nil)
		inv.Stdout = buf
		go func() {
			defer close(doneChan)
			err := inv.Run()
			assert.NoError(t, err)
		}()

		<-doneChan

		var entitlements wirtualsdk.Entitlements
		err := json.Unmarshal(buf.Bytes(), &entitlements)
		require.NoError(t, err, "unmarshal JSON output")
		assert.Empty(t, entitlements.Warnings)
		for _, featureName := range wirtualsdk.FeatureNames {
			assert.Equal(t, wirtualsdk.EntitlementNotEntitled, entitlements.Features[featureName].Entitlement)
		}
		assert.False(t, entitlements.HasLicense)
	})
}
