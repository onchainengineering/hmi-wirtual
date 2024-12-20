package cli_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
)

func TestPublicKey(t *testing.T) {
	t.Parallel()
	t.Run("OK", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		_ = wirtualdtest.CreateFirstUser(t, client)
		inv, root := clitest.New(t, "publickey")
		clitest.SetupConfig(t, client, root)
		buf := new(bytes.Buffer)
		inv.Stdout = buf
		err := inv.Run()
		require.NoError(t, err)
		publicKey := buf.String()
		require.NotEmpty(t, publicKey)
	})
}
