package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

func parseLogFilters(cmd *cobra.Command) (int, *string, error) {
	severity := 0
	if cmd.Flags().Changed("min-severity") {
		value, _ := cmd.Flags().GetString("min-severity")
		var ok bool
		severity, ok = storage.ParseLogSeverity(value)
		if !ok {
			return 0, nil, fmt.Errorf("invalid --min-severity %q: use trace, debug, info, warn, error, or fatal", value)
		}
	}
	var body *string
	if cmd.Flags().Changed("body-contains") {
		value, _ := cmd.Flags().GetString("body-contains")
		if value == "" {
			return 0, nil, fmt.Errorf("--body-contains cannot be empty")
		}
		body = &value
	}
	return severity, body, nil
}

func logStatsCommand() *cobra.Command {
	var service, since, until string
	var limit int
	cmd := &cobra.Command{
		Use: "log-stats", Short: "Summarize observed logs (JSON output)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now().UTC()
			start, end, err := resolveBoundedWindow(since, until, cmd.Flags().Changed("until"), now)
			if err != nil {
				return err
			}
			if !validBoundedWindow(start, end, now) || limit < 0 {
				return fmt.Errorf("invalid log statistics window or limit")
			}
			severity, body, err := parseLogFilters(cmd)
			if err != nil {
				return err
			}
			return withDatabase(func(cmd *cobra.Command, args []string, db *sql.DB) error {
				results, err := storage.QueryLogStats(db, storage.LogStatsOptions{Service: service, Since: start, Until: end, Limit: limit, MinSeverity: severity, BodyContains: body})
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(results)
			})(cmd, args)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "filter by service.name")
	cmd.Flags().StringVar(&since, "since", "", "inclusive start (RFC3339 or positive duration, required)")
	cmd.Flags().StringVar(&until, "until", "", "inclusive end (RFC3339, default now)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max groups (0 = unlimited)")
	cmd.Flags().String("min-severity", "", "minimum severity: trace, debug, info, warn, error, or fatal")
	cmd.Flags().String("body-contains", "", "case-insensitive literal substring in projected log body")
	return cmd
}
