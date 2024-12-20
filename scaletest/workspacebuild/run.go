package workspacebuild

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"golang.org/x/xerrors"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/sloghuman"

	"github.com/onchainengineering/hmi-wirtual/cryptorand"
	"github.com/onchainengineering/hmi-wirtual/scaletest/harness"
	"github.com/onchainengineering/hmi-wirtual/scaletest/loadtestutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/tracing"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

type Runner struct {
	client *wirtualsdk.Client
	cfg    Config

	workspaceID uuid.UUID
}

var (
	_ harness.Runnable  = &Runner{}
	_ harness.Cleanable = &Runner{}
)

func NewRunner(client *wirtualsdk.Client, cfg Config) *Runner {
	return &Runner{
		client: client,
		cfg:    cfg,
	}
}

// Run implements Runnable.
func (r *Runner) Run(ctx context.Context, _ string, logs io.Writer) error {
	ctx, span := tracing.StartSpan(ctx)
	defer span.End()

	logs = loadtestutil.NewSyncWriter(logs)
	logger := slog.Make(sloghuman.Sink(logs)).Leveled(slog.LevelDebug)
	r.client.SetLogger(logger)
	r.client.SetLogBodies(true)

	req := r.cfg.Request
	if req.Name == "" {
		randName, err := cryptorand.HexString(8)
		if err != nil {
			return xerrors.Errorf("generate random name for workspace: %w", err)
		}
		req.Name = "test-" + randName
	}

	workspace, err := r.client.CreateWorkspace(ctx, r.cfg.OrganizationID, r.cfg.UserID, req)
	if err != nil {
		return xerrors.Errorf("create workspace: %w", err)
	}
	r.workspaceID = workspace.ID

	err = waitForBuild(ctx, logs, r.client, workspace.LatestBuild.ID)
	if err != nil {
		for i := 0; i < r.cfg.Retry; i++ {
			_, _ = fmt.Fprintf(logs, "Retrying build %d/%d...\n", i+1, r.cfg.Retry)

			workspace.LatestBuild, err = r.client.CreateWorkspaceBuild(ctx, workspace.ID, wirtualsdk.CreateWorkspaceBuildRequest{
				Transition:          wirtualsdk.WorkspaceTransitionStart,
				RichParameterValues: req.RichParameterValues,
				TemplateVersionID:   req.TemplateVersionID,
			})
			if err != nil {
				return xerrors.Errorf("create workspace build: %w", err)
			}
			err = waitForBuild(ctx, logs, r.client, workspace.LatestBuild.ID)
			if err == nil {
				break
			}
		}
		if err != nil {
			return xerrors.Errorf("wait for build: %w", err)
		}
	}

	if r.cfg.NoWaitForAgents {
		_, _ = fmt.Fprintln(logs, "Skipping agent connectivity check.")
	} else {
		_, _ = fmt.Fprintln(logs, "")
		err = waitForAgents(ctx, logs, r.client, workspace.ID)
		if err != nil {
			return xerrors.Errorf("wait for agent: %w", err)
		}
	}

	return nil
}

func (r *Runner) WorkspaceID() (uuid.UUID, error) {
	if r.workspaceID == uuid.Nil {
		return uuid.Nil, xerrors.New("workspace ID not set")
	}

	return r.workspaceID, nil
}

// CleanupRunner is a runner that deletes a workspace in the Run phase.
type CleanupRunner struct {
	client      *wirtualsdk.Client
	workspaceID uuid.UUID
}

var _ harness.Runnable = &CleanupRunner{}

func NewCleanupRunner(client *wirtualsdk.Client, workspaceID uuid.UUID) *CleanupRunner {
	return &CleanupRunner{
		client:      client,
		workspaceID: workspaceID,
	}
}

// Run implements Runnable.
func (r *CleanupRunner) Run(ctx context.Context, _ string, logs io.Writer) error {
	ctx, span := tracing.StartSpan(ctx)
	defer span.End()
	logs = loadtestutil.NewSyncWriter(logs)
	logger := slog.Make(sloghuman.Sink(logs)).Leveled(slog.LevelDebug)
	if r.workspaceID == uuid.Nil {
		return nil
	}
	logger.Info(ctx, "deleting workspace", slog.F("workspace_id", r.workspaceID))
	r.client.SetLogger(logger)
	r.client.SetLogBodies(true)

	ws, err := r.client.Workspace(ctx, r.workspaceID)
	if err != nil {
		var sdkErr *wirtualsdk.Error
		if xerrors.As(err, &sdkErr) && sdkErr.StatusCode() == http.StatusNotFound {
			logger.Info(ctx, "workspace not found, skipping delete", slog.F("workspace_id", r.workspaceID))
			return nil
		}
		return err
	}

	build, err := r.client.WorkspaceBuild(ctx, ws.LatestBuild.ID)
	if err == nil && build.Job.Status.Active() {
		// mark the build as canceled
		logger.Info(ctx, "canceling workspace build", slog.F("build_id", build.ID), slog.F("workspace_id", r.workspaceID))
		if err = r.client.CancelWorkspaceBuild(ctx, build.ID); err == nil {
			// Wait for the job to cancel before we delete it
			_ = waitForBuild(ctx, logs, r.client, build.ID) // it will return a "build canceled" error
		} else {
			logger.Warn(ctx, "failed to cancel workspace build, attempting to delete anyway", slog.Error(err))
		}
	} else {
		logger.Warn(ctx, "unable to lookup latest workspace build, attempting to delete anyway", slog.Error(err))
	}

	build, err = r.client.CreateWorkspaceBuild(ctx, r.workspaceID, wirtualsdk.CreateWorkspaceBuildRequest{
		Transition: wirtualsdk.WorkspaceTransitionDelete,
	})
	if err != nil {
		return xerrors.Errorf("delete workspace: %w", err)
	}

	err = waitForBuild(ctx, logs, r.client, build.ID)
	if err != nil {
		return xerrors.Errorf("wait for build: %w", err)
	}

	return nil
}

// Cleanup implements Cleanable by wrapping CleanupRunner.
func (r *Runner) Cleanup(ctx context.Context, id string, w io.Writer) error {
	return (&CleanupRunner{
		client:      r.client,
		workspaceID: r.workspaceID,
	}).Run(ctx, id, w)
}

func waitForBuild(ctx context.Context, w io.Writer, client *wirtualsdk.Client, buildID uuid.UUID) error {
	ctx, span := tracing.StartSpan(ctx)
	defer span.End()
	_, _ = fmt.Fprint(w, "Build is currently queued...")

	// Wait for build to start.
	for {
		build, err := client.WorkspaceBuild(ctx, buildID)
		if err != nil {
			return xerrors.Errorf("fetch build: %w", err)
		}

		if build.Job.Status != wirtualsdk.ProvisionerJobPending {
			break
		}

		_, _ = fmt.Fprint(w, ".")
		time.Sleep(500 * time.Millisecond)
	}

	_, _ = fmt.Fprintln(w, "\nBuild started! Streaming logs below:")

	logs, closer, err := client.WorkspaceBuildLogsAfter(ctx, buildID, 0)
	if err != nil {
		return xerrors.Errorf("start streaming build logs: %w", err)
	}
	defer closer.Close()

	currentStage := ""
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case log, ok := <-logs:
			if !ok {
				build, err := client.WorkspaceBuild(ctx, buildID)
				if err != nil {
					return xerrors.Errorf("fetch build: %w", err)
				}

				_, _ = fmt.Fprintln(w, "")
				switch build.Job.Status {
				case wirtualsdk.ProvisionerJobSucceeded:
					_, _ = fmt.Fprintln(w, "\nBuild succeeded!")
					return nil
				case wirtualsdk.ProvisionerJobFailed:
					_, _ = fmt.Fprintf(w, "\nBuild failed with error %q.\nSee logs above for more details.\n", build.Job.Error)
					return xerrors.Errorf("build failed with status %q: %s", build.Job.Status, build.Job.Error)
				case wirtualsdk.ProvisionerJobCanceled:
					_, _ = fmt.Fprintln(w, "\nBuild canceled.")
					return xerrors.New("build canceled")
				default:
					_, _ = fmt.Fprintf(w, "\nLogs disconnected with unexpected job status %q and error %q.\n", build.Job.Status, build.Job.Error)
					return xerrors.Errorf("logs disconnected with unexpected job status %q and error %q", build.Job.Status, build.Job.Error)
				}
			}

			if log.Stage != currentStage {
				currentStage = log.Stage
				_, _ = fmt.Fprintf(w, "\n%s\n", currentStage)
			}

			level := "unknown"
			if log.Level != "" {
				level = string(log.Level)
			}
			_, _ = fmt.Fprintf(w, "\t%s:\t%s\n", level, log.Output)
		}
	}
}

func waitForAgents(ctx context.Context, w io.Writer, client *wirtualsdk.Client, workspaceID uuid.UUID) error {
	ctx, span := tracing.StartSpan(ctx)
	defer span.End()
	_, _ = fmt.Fprint(w, "Waiting for agents to connect...\n\n")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		workspace, err := client.Workspace(ctx, workspaceID)
		if err != nil {
			return xerrors.Errorf("fetch workspace: %w", err)
		}

		ok := true
		for _, res := range workspace.LatestBuild.Resources {
			for _, agent := range res.Agents {
				if agent.Status != wirtualsdk.WorkspaceAgentConnected {
					ok = false
				}

				_, _ = fmt.Fprintf(w, "\tAgent %q is %s\n", agent.Name, agent.Status)
			}
		}
		if ok {
			break
		}

		_, _ = fmt.Fprintln(w, "")
		time.Sleep(1 * time.Second)
	}

	_, _ = fmt.Fprint(w, "\nAgents connected!\n\n")
	return nil
}
