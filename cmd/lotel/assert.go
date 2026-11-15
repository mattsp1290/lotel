package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
			start, end, err := resolveBoundedWindow(since, until, cmd.Flags().Changed("until"), now)
			if err != nil {
				return err
			}
			if !validBoundedWindow(start, end, now) {
				return fmt.Errorf("assertion window must end no later than now and not precede --since")
			}
			return withDatabase(func(cmd *cobra.Command, args []string, db *sql.DB) error {
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
			})(cmd, args)
		},
	}
	cmd.Flags().StringVar(&signal, "signal", "", "signal: traces, metrics, or logs (required)")
	cmd.Flags().StringVar(&service, "service", "", "service.name (required)")
	cmd.Flags().StringVar(&since, "since", "", "inclusive start (RFC3339 or positive duration, required)")
	cmd.Flags().StringVar(&until, "until", "", "inclusive end (RFC3339, default now)")
	parent.AddCommand(cmd)
	return parent
}
