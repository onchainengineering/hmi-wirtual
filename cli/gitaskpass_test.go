package cli_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/cli/cliui"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpapi"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk/agentsdk"
)

func TestGitAskpass(t *testing.T) {
	t.Parallel()
	t.Run("UsernameAndPassword", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpapi.Write(context.Background(), w, http.StatusOK, agentsdk.ExternalAuthResponse{
				Username: "something",
				Password: "bananas",
			})
		}))
		t.Cleanup(srv.Close)
		url := srv.URL
		inv, _ := clitest.New(t, "--agent-url", url, "Username for 'https://github.com':")
		inv.Environ.Set("GIT_PREFIX", "/")
		pty := ptytest.New(t)
		inv.Stdout = pty.Output()
		clitest.Start(t, inv)
		pty.ExpectMatch("something")

		inv, _ = clitest.New(t, "--agent-url", url, "Password for 'https://potato@github.com':")
		inv.Environ.Set("GIT_PREFIX", "/")
		pty = ptytest.New(t)
		inv.Stdout = pty.Output()
		clitest.Start(t, inv)
		pty.ExpectMatch("bananas")
	})

	t.Run("NoHost", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpapi.Write(context.Background(), w, http.StatusNotFound, wirtualsdk.Response{
				Message: "Nope!",
			})
		}))
		t.Cleanup(srv.Close)
		url := srv.URL
		inv, _ := clitest.New(t, "--agent-url", url, "--no-open", "Username for 'https://github.com':")
		inv.Environ.Set("GIT_PREFIX", "/")
		pty := ptytest.New(t)
		inv.Stderr = pty.Output()
		err := inv.Run()
		require.ErrorIs(t, err, cliui.Canceled)
		pty.ExpectMatch("Nope!")
	})

	t.Run("Poll", func(t *testing.T) {
		t.Parallel()
		resp := atomic.Pointer[agentsdk.ExternalAuthResponse]{}
		resp.Store(&agentsdk.ExternalAuthResponse{
			URL: "https://something.org",
		})
		poll := make(chan struct{}, 10)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			val := resp.Load()
			if r.URL.Query().Has("listen") {
				poll <- struct{}{}
				if val.URL != "" {
					httpapi.Write(context.Background(), w, http.StatusInternalServerError, val)
					return
				}
			}
			httpapi.Write(context.Background(), w, http.StatusOK, val)
		}))
		t.Cleanup(srv.Close)
		url := srv.URL

		inv, _ := clitest.New(t, "--agent-url", url, "--no-open", "Username for 'https://github.com':")
		inv.Environ.Set("GIT_PREFIX", "/")
		stdout := ptytest.New(t)
		inv.Stdout = stdout.Output()
		stderr := ptytest.New(t)
		inv.Stderr = stderr.Output()
		go func() {
			err := inv.Run()
			assert.NoError(t, err)
		}()
		<-poll
		stderr.ExpectMatch("Open the following URL to authenticate")
		resp.Store(&agentsdk.ExternalAuthResponse{
			Username: "username",
			Password: "password",
		})
		stdout.ExpectMatch("username")
	})
}
