package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattsp1290/lotel/internal/config"
	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

type signalDiagnosticRequest struct {
	Service string    `json:"service"`
	Since   time.Time `json:"since"`
	Until   time.Time `json:"until"`
}

const maxSignalServiceBytes = 4096
const maxSignalProtocolBytes = 64 << 10

type cappedOutput struct{ bytes.Buffer }

func (b *cappedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxSignalProtocolBytes {
		return 0, fmt.Errorf("response too large")
	}
	return b.Buffer.Write(p)
}

func unavailableSignalReport(req signalDiagnosticRequest, reason string) storage.SignalDiagnosis {
	return storage.SignalDiagnosis{SchemaVersion: 1, Observation: "unavailable", Reason: reason, Service: req.Service, Since: req.Since, Until: req.Until, Signals: []storage.SignalEvidence{{Signal: "traces"}, {Signal: "metrics"}, {Signal: "logs"}}}
}

func diagnoseSignals(ctx context.Context, req signalDiagnosticRequest) storage.SignalDiagnosis {
	report := unavailableSignalReport(req, "worker_unavailable")
	executable, err := os.Executable()
	if err != nil {
		return report
	}
	return runSignalWorker(ctx, executable, req)
}

func runSignalWorker(parent context.Context, executable string, req signalDiagnosticRequest) storage.SignalDiagnosis {
	report := unavailableSignalReport(req, "worker_unavailable")
	request, err := json.Marshal(req)
	if err != nil || len(request) > maxSignalProtocolBytes {
		return report
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	worker := exec.CommandContext(ctx, executable, "diagnose-signals-worker")
	worker.WaitDelay = time.Second
	worker.Stdin = bytes.NewReader(request)
	worker.Stderr = io.Discard
	var out cappedOutput
	worker.Stdout = &out
	if err := worker.Run(); err != nil {
		if ctx.Err() != nil {
			report.Reason = "worker_timeout"
		}
		return report
	}
	var result storage.SignalDiagnosis
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.SchemaVersion != 1 || result.Service != req.Service || !result.Since.Equal(req.Since) || !result.Until.Equal(req.Until) || len(result.Signals) != 3 {
		return report
	}
	if result.Observation != "available" && result.Observation != "unavailable" {
		return report
	}
	if result.Observation == "available" && result.Reason != "" {
		return report
	}
	if result.Observation == "unavailable" {
		switch result.Reason {
		case "data_missing", "data_busy", "data_unavailable", "unsafe_path", "schema_unavailable":
		default:
			return report
		}
	}
	if result.Signals[0].Signal != "traces" || result.Signals[1].Signal != "metrics" || result.Signals[2].Signal != "logs" {
		return report
	}
	for _, signal := range result.Signals {
		if result.Observation == "unavailable" {
			if signal.ObservedInWindow != nil || signal.LatestInWindow != nil {
				return report
			}
			continue
		}
		if signal.ObservedInWindow == nil || *signal.ObservedInWindow != (signal.LatestInWindow != nil) {
			return report
		}
		if signal.LatestInWindow != nil && (signal.LatestInWindow.Before(req.Since) || signal.LatestInWindow.After(req.Until)) {
			return report
		}
	}
	return result
}

func signalDiagnosisCommand() *cobra.Command {
	var service, since, until string
	cmd := &cobra.Command{Use: "signals", Short: "Inspect indexed signal evidence without modifying it (JSON)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		now := time.Now().UTC()
		if strings.TrimSpace(service) == "" || len(service) > maxSignalServiceBytes || !utf8.ValidString(service) {
			return fmt.Errorf("--service must be valid UTF-8, nonempty, and at most 4096 bytes")
		}
		start, end, err := resolveBoundedWindow(since, until, cmd.Flags().Changed("until"), now)
		if err != nil {
			return err
		}
		if !validBoundedWindow(start, end, now) {
			return fmt.Errorf("invalid diagnostic window")
		}
		report := diagnoseSignals(cmd.Context(), signalDiagnosticRequest{Service: service, Since: start, Until: end})
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}}
	cmd.Flags().StringVar(&service, "service", "", "service.name (required)")
	cmd.Flags().StringVar(&since, "since", "", "inclusive start (RFC3339 or positive duration, required)")
	cmd.Flags().StringVar(&until, "until", "", "inclusive end (RFC3339, default now)")
	return cmd
}

func signalDiagnosisWorkerCommand() *cobra.Command {
	return &cobra.Command{Use: "diagnose-signals-worker", Hidden: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		var req signalDiagnosticRequest
		input, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxSignalProtocolBytes+1))
		if err != nil || len(input) > maxSignalProtocolBytes || !utf8.Valid(input) {
			return fmt.Errorf("invalid request")
		}
		if err = json.Unmarshal(input, &req); err != nil || strings.TrimSpace(req.Service) == "" || len(req.Service) > maxSignalServiceBytes || !utf8.ValidString(req.Service) || req.Since.IsZero() || req.Until.IsZero() || req.Since.After(req.Until) {
			return fmt.Errorf("invalid request")
		}
		data, err := config.DataPath()
		if err != nil {
			return fmt.Errorf("unavailable")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 4*time.Second)
		defer cancel()
		report := storage.DiagnoseSignals(ctx, filepath.Join(data, "lotel.db"), req.Service, req.Since, req.Until)
		return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
	}}
}
