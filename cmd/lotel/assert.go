package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

var errAssertionFailed = errors.New("no matching signal")

type signalAssertionResult struct {
	Passed          bool       `json:"passed"`
	Signal          string     `json:"signal"`
	Service         string     `json:"service"`
	Since           time.Time  `json:"since"`
	Until           time.Time  `json:"until"`
	LatestTimestamp *time.Time `json:"latest_timestamp,omitempty"`
}

func assertionSince(value string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC(), nil
	}
	var duration time.Duration
	var err error
	if strings.HasSuffix(value, "d") {
		days := strings.TrimSuffix(value, "d")
		if days == "" || strings.Trim(days, "0123456789") != "" {
			return time.Time{}, fmt.Errorf("invalid duration")
		}
		var n int64
		n, err = strconv.ParseInt(days, 10, 64)
		if err == nil && n <= int64((1<<63-1)/int64(24*time.Hour)) {
			duration = time.Duration(n) * 24 * time.Hour
		} else {
			err = fmt.Errorf("invalid duration")
		}
	} else {
		duration, err = time.ParseDuration(value)
	}
	if err != nil || duration <= 0 {
		return time.Time{}, fmt.Errorf("expected an RFC3339 timestamp or positive duration")
	}
	return now.Add(-duration), nil
}

func assertCommand() *cobra.Command {
	parent := &cobra.Command{Use: "assert", Short: "Assert telemetry evidence"}
	var signal, service, since, until string
	cmd := &cobra.Command{
		Use: "signal", Short: "Assert an observed signal for a service", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now().UTC()
			if signal != "traces" && signal != "metrics" && signal != "logs" {
				return fmt.Errorf("--signal must be traces, metrics, or logs")
			}
			if strings.TrimSpace(service) == "" {
				return fmt.Errorf("--service must be nonempty")
			}
			start, err := assertionSince(since, now)
			if err != nil {
				return fmt.Errorf("invalid --since: %w", err)
			}
			end := now
			if until != "" {
				end, err = time.Parse(time.RFC3339Nano, until)
				if err != nil {
					return fmt.Errorf("invalid --until: %w", err)
				}
				end = end.UTC()
			}
			if start.IsZero() || end.IsZero() || end.After(now) || start.After(end) {
				return fmt.Errorf("assertion window must end no later than now and not precede --since")
			}
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			latest, err := storage.LatestSignal(db, storage.SignalAssertion{Signal: signal, Service: service, Since: start, Until: end})
			if err != nil {
				return err
			}
			result := signalAssertionResult{Passed: latest != nil, Signal: signal, Service: service, Since: start, Until: end, LatestTimestamp: latest}
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
				return err
			}
			if latest == nil {
				cmd.Root().SilenceErrors = true
				cmd.Root().SilenceUsage = true
				return errAssertionFailed
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&signal, "signal", "", "signal: traces, metrics, or logs (required)")
	cmd.Flags().StringVar(&service, "service", "", "service.name (required)")
	cmd.Flags().StringVar(&since, "since", "", "inclusive start (RFC3339 or positive duration, required)")
	cmd.Flags().StringVar(&until, "until", "", "inclusive end (RFC3339, default now)")
	parent.AddCommand(cmd)
	return parent
}
