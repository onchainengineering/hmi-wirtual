package cliui_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/onchainengineering/hmi-wirtual/cli/cliui"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtime"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
)

func TestWorkspaceResources(t *testing.T) {
	t.Parallel()
	t.Run("SingleAgentSSH", func(t *testing.T) {
		t.Parallel()
		ptty := ptytest.New(t)
		done := make(chan struct{})
		go func() {
			err := cliui.WorkspaceResources(ptty.Output(), []wirtualsdk.WorkspaceResource{{
				Type:       "google_compute_instance",
				Name:       "dev",
				Transition: wirtualsdk.WorkspaceTransitionStart,
				Agents: []wirtualsdk.WorkspaceAgent{{
					Name:            "dev",
					Status:          wirtualsdk.WorkspaceAgentConnected,
					LifecycleState:  wirtualsdk.WorkspaceAgentLifecycleCreated,
					Architecture:    "amd64",
					OperatingSystem: "linux",
					Health:          wirtualsdk.WorkspaceAgentHealth{Healthy: true},
				}},
			}}, cliui.WorkspaceResourcesOptions{
				WorkspaceName: "example",
			})
			assert.NoError(t, err)
			close(done)
		}()
		ptty.ExpectMatch("coder ssh example")
		<-done
	})

	t.Run("MultipleStates", func(t *testing.T) {
		t.Parallel()
		ptty := ptytest.New(t)
		disconnected := dbtime.Now().Add(-4 * time.Second)
		done := make(chan struct{})
		go func() {
			err := cliui.WorkspaceResources(ptty.Output(), []wirtualsdk.WorkspaceResource{{
				Transition: wirtualsdk.WorkspaceTransitionStart,
				Type:       "google_compute_disk",
				Name:       "root",
			}, {
				Transition: wirtualsdk.WorkspaceTransitionStop,
				Type:       "google_compute_disk",
				Name:       "root",
			}, {
				Transition: wirtualsdk.WorkspaceTransitionStart,
				Type:       "google_compute_instance",
				Name:       "dev",
				Agents: []wirtualsdk.WorkspaceAgent{{
					CreatedAt:       dbtime.Now().Add(-10 * time.Second),
					Status:          wirtualsdk.WorkspaceAgentConnecting,
					LifecycleState:  wirtualsdk.WorkspaceAgentLifecycleCreated,
					Name:            "dev",
					OperatingSystem: "linux",
					Architecture:    "amd64",
					Health:          wirtualsdk.WorkspaceAgentHealth{Healthy: true},
				}},
			}, {
				Transition: wirtualsdk.WorkspaceTransitionStart,
				Type:       "kubernetes_pod",
				Name:       "dev",
				Agents: []wirtualsdk.WorkspaceAgent{{
					Status:          wirtualsdk.WorkspaceAgentConnected,
					LifecycleState:  wirtualsdk.WorkspaceAgentLifecycleReady,
					Name:            "go",
					Architecture:    "amd64",
					OperatingSystem: "linux",
					Health:          wirtualsdk.WorkspaceAgentHealth{Healthy: true},
				}, {
					DisconnectedAt:  &disconnected,
					Status:          wirtualsdk.WorkspaceAgentDisconnected,
					LifecycleState:  wirtualsdk.WorkspaceAgentLifecycleReady,
					Name:            "postgres",
					Architecture:    "amd64",
					OperatingSystem: "linux",
					Health: wirtualsdk.WorkspaceAgentHealth{
						Healthy: false,
						Reason:  "agent has lost connection",
					},
				}},
			}}, cliui.WorkspaceResourcesOptions{
				WorkspaceName:  "dev",
				HideAgentState: false,
				HideAccess:     false,
			})
			assert.NoError(t, err)
			close(done)
		}()
		ptty.ExpectMatch("google_compute_disk.root")
		ptty.ExpectMatch("google_compute_instance.dev")
		ptty.ExpectMatch("healthy")
		ptty.ExpectMatch("coder ssh dev.dev")
		ptty.ExpectMatch("kubernetes_pod.dev")
		ptty.ExpectMatch("healthy")
		ptty.ExpectMatch("coder ssh dev.go")
		ptty.ExpectMatch("agent has lost connection")
		ptty.ExpectMatch("coder ssh dev.postgres")
		<-done
	})
}
