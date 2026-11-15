package main

import (
	"encoding/json"
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
			start, err := assertionSince(since, now)
			if err != nil {
				return fmt.Errorf("invalid --since: %w", err)
			}
			end := now
			if cmd.Flags().Changed("until") {
				end, err = parseAssertionTimestamp(until)
				if err != nil {
					return fmt.Errorf("invalid --until: %w", err)
				}
				end = end.UTC()
			}
			if start.IsZero() || end.IsZero() || end.After(now) || start.After(end) || limit < 0 {
				return fmt.Errorf("invalid span statistics window or limit")
			}
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			results, err := storage.QuerySpanStats(db, storage.SpanStatsOptions{Service: service, Since: start, Until: end, Limit: limit})
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(results)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "filter by service.name")
	cmd.Flags().StringVar(&since, "since", "", "inclusive start (RFC3339 or positive duration, required)")
	cmd.Flags().StringVar(&until, "until", "", "inclusive end (RFC3339, default now)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max groups (0 = unlimited)")
	return cmd
}
