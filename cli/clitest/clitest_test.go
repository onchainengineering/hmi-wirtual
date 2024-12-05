package clitest_test

import (
	"testing"

	"go.uber.org/goleak"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestCli(t *testing.T) {
	t.Parallel()
	clitest.CreateTemplateVersionSource(t, nil)
	client := wirtualdtest.New(t, nil)
	i, config := clitest.New(t)
	clitest.SetupConfig(t, client, config)
	pty := ptytest.New(t).Attach(i)
	clitest.Start(t, i)
	pty.ExpectMatch("coder")
}
