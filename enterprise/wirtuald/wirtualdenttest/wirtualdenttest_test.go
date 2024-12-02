package wirtualdenttest_test

import (
	"testing"

	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
)

func TestNew(t *testing.T) {
	t.Parallel()
	_, _ = wirtualdenttest.New(t, nil)
}
