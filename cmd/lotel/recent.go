package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	"unicode"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

type recentResult struct {
	Signal    string    `json:"signal"`
	Since     time.Time `json:"since"`
	Until     time.Time `json:"until"`
	Records   any       `json:"records"`
	Truncated bool      `json:"truncated"`
}

func recentCommand() *cobra.Command {
	parent := &cobra.Command{Use: "recent", Short: "Query a bounded newest-first snapshot"}
	for _, signal := range []string{"traces", "logs"} {
		var service, since, until string
		var limit int
		cmd := &cobra.Command{
			Use: signal, Short: "Query recent " + signal, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				now := time.Now().UTC()
				start, end, err := resolveBoundedWindow(since, until, cmd.Flags().Changed("until"), now)
				if err != nil {
					return err
				}
				if !validBoundedWindow(start, end, now) || limit < 1 || limit > storage.MaxRecentLimit {
					return fmt.Errorf("invalid recent query window or limit (limit must be 1 to %d)", storage.MaxRecentLimit)
				}
				exactFlags := []string{"service", "trace-id"}
				if signal == "traces" {
					exactFlags = append(exactFlags, "name")
				} else {
					exactFlags = append(exactFlags, "event-name")
				}
				for _, name := range exactFlags {
					value, _ := cmd.Flags().GetString(name)
					if cmd.Flags().Changed(name) && value == "" {
						return fmt.Errorf("--%s must not be empty", name)
					}
				}
				opts, err := parseCommandQueryOpts(cmd, service, "", "", limit)
				if err != nil {
					return err
				}
				for _, attr := range opts.Attrs {
					for _, r := range attr.Key {
						if unicode.IsControl(r) {
							return fmt.Errorf("attribute key contains a control character")
						}
					}
				}
				opts.Since, opts.Until = start, end
				var severity int
				var body *string
				if signal == "logs" {
					severity, body, err = parseLogFilters(cmd)
					if err != nil {
						return err
					}
				}
				var duration *time.Duration
				if signal == "traces" && cmd.Flags().Changed("min-duration") {
					d, _ := cmd.Flags().GetDuration("min-duration")
					if d < 0 {
						return fmt.Errorf("--min-duration must be nonnegative")
					}
					duration = &d
				}
				var records any
				var truncated bool
				err = withDatabase(func(cmd *cobra.Command, args []string, db *sql.DB) error {
					if signal == "traces" {
						errorsOnly, _ := cmd.Flags().GetBool("errors-only")
						records, truncated, err = storage.RecentTraces(db, storage.TraceQueryOptions{QueryOptions: opts, ErrorsOnly: errorsOnly, MinDuration: duration})
					} else {
						records, truncated, err = storage.RecentLogs(db, storage.LogQueryOptions{QueryOptions: opts, MinSeverity: severity, BodyContains: body})
					}
					return err
				})(cmd, args)
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(recentResult{Signal: signal, Since: start, Until: end, Records: records, Truncated: truncated})
			},
		}
		cmd.Flags().StringVar(&service, "service", "", "filter by service.name")
		cmd.Flags().StringVar(&since, "since", "", "inclusive start (RFC3339 or positive duration, required)")
		cmd.Flags().StringVar(&until, "until", "", "inclusive end (RFC3339, default now)")
		cmd.Flags().IntVar(&limit, "limit", 0, "max records (required, 1 to 1000)")
		cmd.Flags().String("trace-id", "", "trace ID")
		cmd.Flags().StringArray("attr", nil, "attribute key=value; repeat for AND")
		if signal == "traces" {
			cmd.Flags().String("name", "", "exact span name")
			cmd.Flags().Bool("errors-only", false, "filter spans with OpenTelemetry error status")
			cmd.Flags().Duration("min-duration", 0, "minimum span duration")
		} else {
			cmd.Flags().String("event-name", "", "exact log event name")
			cmd.Flags().String("min-severity", "", "minimum severity: trace, debug, info, warn, error, fatal")
			cmd.Flags().String("body-contains", "", "case-insensitive literal substring in projected log body")
		}
		parent.AddCommand(cmd)
	}
	return parent
}
