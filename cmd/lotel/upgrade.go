package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattsp1290/lotel/internal/collector"
	"github.com/mattsp1290/lotel/internal/upgrade"
	"github.com/spf13/cobra"
)

func upgradeCommand() *cobra.Command {
	var check, jsonOutput, force bool
	var target string
	command := &cobra.Command{
		Use:   "upgrade",
		Short: "Install a verified release and restart a running Collector",
		Long:  "Install a verified release. A running Collector stops during replacement and restarts through the new executable. Agents must expect collection to be unavailable during this period. --check leaves collection running.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := os.Executable()
			if err != nil {
				return err
			}
			path, err = filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			var restartConfig string
			req := upgrade.Request{Current: version, Executable: path, BaseURL: os.Getenv("LOTEL_RELEASE_BASE_URL"), Version: target, Check: check, Force: force, SourceBuild: distribution != "release"}
			report, err := upgradeWithCollector(cmd.Context(), req, collectorControl{
				running: func(ctx context.Context) (bool, error) {
					s, e := collector.InspectForUpgrade(ctx)
					if e != nil {
						return false, e
					}
					restartConfig = s.ConfigPath
					return s.Running, nil
				},
				stop: func(ctx context.Context) error { return runCollectorCommand(ctx, path, cmd.ErrOrStderr(), "stop") },
				start: func(ctx context.Context) error {
					return runCollectorCommand(ctx, path, cmd.ErrOrStderr(), "start", "--wait", "--config", restartConfig)
				},
				recovery: func() string {
					return fmt.Sprintf("%s start --wait --config %s", shellLiteral(path), shellLiteral(restartConfig))
				},
			}, cmd.ErrOrStderr())
			if jsonOutput {
				if encodeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(report); encodeErr != nil {
					return errors.Join(err, encodeErr)
				}
			} else if err == nil {
				if report.Upgraded {
					fmt.Fprintf(cmd.OutOrStdout(), "Upgraded lotel to %s\n", report.Target)
				} else if check {
					fmt.Fprintf(cmd.OutOrStdout(), "Current %s, target %s, update available: %t\n", report.Current, report.Target, report.UpdateAvailable)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "lotel %s is up to date\n", report.Current)
				}
			}
			return err
		},
	}
	command.Flags().BoolVar(&check, "check", false, "report the target release without changing the executable or Collector")
	command.Flags().BoolVar(&jsonOutput, "json", false, "write a JSON report")
	command.Flags().BoolVar(&force, "force", false, "allow replacement of a source or development build")
	command.Flags().StringVar(&target, "version", "", "install a specific vX.Y.Z release, including a rollback")
	return command
}

type collectorControl struct {
	running  func(context.Context) (bool, error)
	stop     func(context.Context) error
	start    func(context.Context) error
	recovery func() string
}

func upgradeWithCollector(ctx context.Context, req upgrade.Request, control collectorControl, notices io.Writer) (upgrade.Report, error) {
	resume := false
	req.BeforeReplace = func(ctx context.Context) error {
		running, err := control.running(ctx)
		if err != nil {
			return fmt.Errorf("check Collector before upgrade: %w", err)
		}
		if !running {
			return nil
		}
		fmt.Fprintln(notices, "lotel: agents, OpenTelemetry collection will be unavailable during this upgrade. Stopping the Collector; collection resumes after restart and health checks.")
		resume = true
		return control.stop(ctx)
	}
	report, err := upgrade.Run(ctx, req)
	if resume {
		restartCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if restartErr := control.start(restartCtx); restartErr != nil {
			command := fmt.Sprintf("%s start --wait", shellLiteral(report.Path))
			if control.recovery != nil {
				command = control.recovery()
			}
			return report, errors.Join(err, fmt.Errorf("Collector restart failed; collection is unavailable. Run %s to recover: %w", command, restartErr))
		}
		report.CollectorRestarted = true
		fmt.Fprintln(notices, "lotel: Collector is healthy; OpenTelemetry collection has resumed.")
	}
	return report, err
}

func runCollectorCommand(ctx context.Context, path string, output io.Writer, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, args...)
	command.Stdout, command.Stderr = output, output
	return command.Run()
}

func shellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
