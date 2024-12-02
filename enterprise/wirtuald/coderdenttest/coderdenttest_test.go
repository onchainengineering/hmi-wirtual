package wirtualdenttest_test

import (
	"testing"

	"github.com/coder/coder/v2/enterprise/wirtuald/wirtualdenttest"
)

func TestNew(t *testing.T) {
	t.Parallel()
	_, _ = wirtualdenttest.New(t, nil)
}
