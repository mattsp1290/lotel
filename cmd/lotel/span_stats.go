package main

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

func spanStatsCommand() *cobra.Command {
	var service, since, until string
	var limit int
	cmd := &cobra.Command{
		Use: "span-stats", Short: "Summarize observed spans (JSON output)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now().UTC()
			start, end, err := resolveBoundedWindow(since, until, cmd.Flags().Changed("until"), now)
			if err != nil {
				return err
			}
			if !validBoundedWindow(start, end, now) || limit < 0 {
				return fmt.Errorf("invalid span statistics window or limit")
			}
			return withDatabaseJSON(func(cmd *cobra.Command, args []string, db *sql.DB) (any, error) {
				results, err := storage.QuerySpanStats(db, storage.SpanStatsOptions{Service: service, Since: start, Until: end, Limit: limit})
				if err != nil {
					return nil, err
				}
				return results, nil
			}, false)(cmd, args)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "filter by service.name")
	cmd.Flags().StringVar(&since, "since", "", "inclusive start (RFC3339 or positive duration, required)")
	cmd.Flags().StringVar(&until, "until", "", "inclusive end (RFC3339, default now)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max groups (0 = unlimited)")
	return cmd
}
