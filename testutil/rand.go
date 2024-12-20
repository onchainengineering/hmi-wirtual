package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cryptorand"
)

// MustRandString returns a random string of length n.
func MustRandString(t *testing.T, n int) string {
	t.Helper()
	s, err := cryptorand.String(n)
	require.NoError(t, err)
	return s
}
