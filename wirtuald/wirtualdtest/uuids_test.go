package wirtualdtest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
)

func TestDeterministicUUIDGenerator(t *testing.T) {
	t.Parallel()

	ids := wirtualdtest.NewDeterministicUUIDGenerator()
	require.Equal(t, ids.ID("g1"), ids.ID("g1"))
	require.NotEqual(t, ids.ID("g1"), ids.ID("g2"))
}
