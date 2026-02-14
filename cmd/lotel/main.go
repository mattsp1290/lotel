package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/lotel/internal/collector"
	"github.com/mattsp1290/lotel/internal/config"
	"github.com/mattsp1290/lotel/internal/storage"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "lotel",
		Short: "Local OpenTelemetry — manage a collector and query telemetry",
	}

	// --- start ---
	var waitHealthy bool
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the OTel Collector container",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, dataPath, err := config.ResolvePaths()
			if err != nil {
				return err
			}
			if err := collector.Start(cmd.Context(), configPath, dataPath); err != nil {
				return err
			}
			if waitHealthy {
				fmt.Print("Waiting for collector to become healthy...")
				if err := collector.WaitHealthy(cmd.Context(), 30*time.Second); err != nil {
					fmt.Println(" FAILED")
					return fmt.Errorf("collector did not become healthy: %w", err)
				}
				fmt.Println(" OK")
			}
			return nil
		},
	}
	startCmd.Flags().BoolVar(&waitHealthy, "wait", false, "wait for collector to become healthy before returning")

	// --- stop ---
	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the OTel Collector container",
		RunE: func(cmd *cobra.Command, args []string) error {
			return collector.Stop(cmd.Context())
		},
	}

	// --- status ---
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show collector status (JSON)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := collector.GetStatus(cmd.Context())
			if err != nil {
				return err
			}
			printJSON(s)
			if !s.Running {
				os.Exit(1)
			}
			return nil
		},
	}

	// --- health ---
	healthCmd := &cobra.Command{
		Use:   "health",
		Short: "Check collector health (exit 0 if healthy, 1 if not)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := collector.GetStatus(cmd.Context())
			if err != nil {
				return err
			}
			if !s.Running {
				fmt.Println("Collector is not running.")
				os.Exit(1)
			}
			if !s.Healthy {
				fmt.Println("Collector is running but not healthy.")
				os.Exit(1)
			}
			fmt.Println("Collector is healthy.")
			return nil
		},
	}

	// --- ingest ---
	ingestCmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest JSONL telemetry files into the query database",
		RunE: func(cmd *cobra.Command, args []string) error {
			dataPath, err := config.DataPath()
			if err != nil {
				return err
			}
			db, err := storage.DB()
			if err != nil {
				return err
			}
			if err := storage.IngestAll(db, dataPath); err != nil {
				return err
			}
			fmt.Println("Ingestion complete.")
			return nil
		},
	}

	// --- query traces ---
	queryCmd := &cobra.Command{
		Use:   "query",
		Short: "Query telemetry data",
	}

	var service, since, until string
	var limit int

	queryTracesCmd := &cobra.Command{
		Use:   "traces",
		Short: "Query traces (JSON output)",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := storage.DB()
			if err != nil {
				return err
			}
			opts, err := parseQueryOpts(service, since, until, limit)
			if err != nil {
				return err
			}
			results, err := storage.QueryTraces(db, opts)
			if err != nil {
				return err
			}
			printJSON(results)
			return nil
		},
	}

	queryMetricsCmd := &cobra.Command{
		Use:   "metrics",
		Short: "Query metrics (JSON output)",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := storage.DB()
			if err != nil {
				return err
			}
			opts, err := parseQueryOpts(service, since, until, limit)
			if err != nil {
				return err
			}
			results, err := storage.QueryMetrics(db, opts)
			if err != nil {
				return err
			}
			printJSON(results)
			return nil
		},
	}

	queryLogsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Query logs (JSON output)",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := storage.DB()
			if err != nil {
				return err
			}
			opts, err := parseQueryOpts(service, since, until, limit)
			if err != nil {
				return err
			}
			results, err := storage.QueryLogs(db, opts)
			if err != nil {
				return err
			}
			printJSON(results)
			return nil
		},
	}

	// --- query metrics aggregate ---
	var metricName string
	queryAggCmd := &cobra.Command{
		Use:   "aggregate",
		Short: "Compute avg/min/max for a metric over a time window",
		RunE: func(cmd *cobra.Command, args []string) error {
			if metricName == "" {
				return fmt.Errorf("--metric is required")
			}
			db, err := storage.DB()
			if err != nil {
				return err
			}
			opts, err := parseQueryOpts(service, since, until, 0)
			if err != nil {
				return err
			}
			result, err := storage.AggregateMetrics(db, opts, metricName)
			if err != nil {
				return err
			}
			printJSON(result)
			return nil
		},
	}
	queryAggCmd.Flags().StringVar(&metricName, "metric", "", "metric name to aggregate (required)")

	// Shared query flags.
	for _, cmd := range []*cobra.Command{queryTracesCmd, queryMetricsCmd, queryLogsCmd, queryAggCmd} {
		cmd.Flags().StringVar(&service, "service", "", "filter by service.name")
		cmd.Flags().StringVar(&since, "since", "", "start time (RFC3339 or relative like '1h', '24h')")
		cmd.Flags().StringVar(&until, "until", "", "end time (RFC3339)")
		cmd.Flags().IntVar(&limit, "limit", 0, "max results (0 = unlimited)")
	}

	queryCmd.AddCommand(queryTracesCmd, queryMetricsCmd, queryLogsCmd, queryAggCmd)

	// --- prune ---
	var olderThan string
	var pruneService string
	var dryRun bool
	var pruneAll bool
	pruneCmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete telemetry data older than a threshold",
		RunE: func(cmd *cobra.Command, args []string) error {
			if pruneAll && olderThan != "" {
				return fmt.Errorf("--all and --older-than are mutually exclusive")
			}
			if !pruneAll && olderThan == "" {
				return fmt.Errorf("--older-than or --all is required (e.g., '7d', '24h')")
			}

			var cutoff time.Time
			if pruneAll {
				cutoff = time.Now().Add(time.Hour) // future cutoff catches everything
			} else {
				dur, err := parseDuration(olderThan)
				if err != nil {
					return fmt.Errorf("invalid --older-than: %w", err)
				}
				cutoff = time.Now().Add(-dur)
			}

			db, err := storage.DB()
			if err != nil {
				return err
			}
			reports, err := storage.Prune(db, cutoff, pruneService, dryRun)
			if err != nil {
				return err
			}
			if dryRun {
				fmt.Fprintln(os.Stderr, "Dry run — no data was deleted.")
			}
			printJSON(reports)
			return nil
		},
	}
	pruneCmd.Flags().StringVar(&olderThan, "older-than", "", "age threshold (e.g., '7d', '24h', '1h')")
	pruneCmd.Flags().StringVar(&pruneService, "service", "", "limit pruning to a specific service")
	pruneCmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be pruned without deleting")
	pruneCmd.Flags().BoolVar(&pruneAll, "all", false, "delete all telemetry data")

	rootCmd.AddCommand(startCmd, stopCmd, statusCmd, healthCmd, ingestCmd, queryCmd, pruneCmd)

	ctx := context.Background()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

func printJSON(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}

func parseQueryOpts(service, since, until string, limit int) (storage.QueryOptions, error) {
	opts := storage.QueryOptions{
		Service: service,
		Limit:   limit,
	}
	if since != "" {
		t, err := parseTime(since)
		if err != nil {
			return opts, fmt.Errorf("invalid --since: %w", err)
		}
		opts.Since = t
	}
	if until != "" {
		t, err := parseTime(until)
		if err != nil {
			return opts, fmt.Errorf("invalid --until: %w", err)
		}
		opts.Until = t
	}
	return opts, nil
}

func parseTime(s string) (time.Time, error) {
	// Try RFC3339 first.
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}
	// Try relative duration (e.g., "1h", "24h", "7d").
	dur, err := parseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot parse %q as RFC3339 or relative duration", s)
	}
	return time.Now().Add(-dur), nil
}

func parseDuration(s string) (time.Duration, error) {
	// Support "Nd" for days.
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}
