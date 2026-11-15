package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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
	var backend string
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the OTel Collector",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if backend == "native" {
				var cancel context.CancelFunc
				ctx, cancel = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
				defer cancel()
			}
			configPath, dataPath, err := config.ResolvePaths()
			if err != nil {
				return err
			}
			if err := collector.Start(ctx, configPath, dataPath, backend); err != nil {
				return err
			}
			if waitHealthy {
				fmt.Print("Waiting for collector to become healthy...")
				if err := collector.WaitHealthy(ctx, 30*time.Second); err != nil {
					fmt.Println(" FAILED")
					return fmt.Errorf("collector did not become healthy: %w", err)
				}
				fmt.Println(" OK")
			}
			return nil
		},
	}
	startCmd.Flags().StringVar(&backend, "backend", "docker", "collector backend: docker or native")
	startCmd.Flags().BoolVar(&waitHealthy, "wait", false, "wait for collector to become healthy before returning")

	// --- stop ---
	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the OTel Collector",
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
	var fullIngest bool
	ingestCmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest JSONL telemetry files into the query database",
		RunE: func(cmd *cobra.Command, args []string) error {
			dataPath, err := config.DataPath()
			if err != nil {
				return err
			}
			db, err := openDB()
			if err != nil {
				return err
			}
			var report *storage.IngestReport
			if fullIngest {
				report, err = storage.ReingestAll(db, dataPath)
			} else {
				report, err = storage.IngestAll(db, dataPath)
			}
			if err != nil {
				return err
			}
			printJSON(report)
			return nil
		},
	}

	ingestCmd.Flags().BoolVar(&fullIngest, "full", false, "rebuild the query index from retained JSONL; previously pruned records can return")

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
			db, err := openDB()
			if err != nil {
				return err
			}
			opts, err := parseCommandQueryOpts(cmd, service, since, until, limit)
			if err != nil {
				return err
			}
			results, err := storage.QueryTraces(db, opts)
			if err != nil {
				return err
			}
			if results == nil {
				results = []storage.TraceResult{}
			}
			printJSON(results)
			return nil
		},
	}

	queryMetricsCmd := &cobra.Command{
		Use:   "metrics",
		Short: "Query metrics (JSON output)",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			opts, err := parseCommandQueryOpts(cmd, service, since, until, limit)
			if err != nil {
				return err
			}
			results, err := storage.QueryMetrics(db, opts)
			if err != nil {
				return err
			}
			if results == nil {
				results = []storage.MetricResult{}
			}
			printJSON(results)
			return nil
		},
	}

	queryLogsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Query logs (JSON output)",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			opts, err := parseCommandQueryOpts(cmd, service, since, until, limit)
			if err != nil {
				return err
			}
			results, err := storage.QueryLogs(db, opts)
			if err != nil {
				return err
			}
			if results == nil {
				results = []storage.LogResult{}
			}
			printJSON(results)
			return nil
		},
	}

	// --- query metrics aggregate ---
	var metricName string
	queryAggCmd := &cobra.Command{
		Use:   "aggregate",
		Short: "Compute avg/min/max; for cumulative histograms, read the latest query metrics point",
		RunE: func(cmd *cobra.Command, args []string) error {
			if metricName == "" {
				return fmt.Errorf("--metric is required")
			}
			db, err := openDB()
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

	for _, cmd := range []*cobra.Command{queryTracesCmd, queryMetricsCmd} {
		cmd.Flags().String("name", "", "exact span or metric name")
	}
	for _, cmd := range []*cobra.Command{queryTracesCmd, queryLogsCmd} {
		cmd.Flags().String("trace-id", "", "trace ID (lowercase hex)")
	}
	queryLogsCmd.Flags().String("event-name", "", "exact log event name")
	for _, cmd := range []*cobra.Command{queryTracesCmd, queryMetricsCmd, queryLogsCmd} {
		cmd.Flags().StringArray("attr", nil, "attribute key=value; repeat for AND; matches compact JSON string form, null never matches")
	}
	queryCmd.AddCommand(queryTracesCmd, queryMetricsCmd, queryLogsCmd, queryAggCmd, genAICommand())

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

			db, err := openDB()
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

	rootCmd.AddCommand(nativeRunCommand())
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

func openDB() (*sql.DB, error) {
	dataPath, err := config.DataPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		return nil, err
	}
	var db *sql.DB
	deadline := time.Now().Add(2 * time.Second)
	for {
		db, err = storage.OpenDB(filepath.Join(dataPath, "lotel.db"))
		if err == nil || !storage.IsDatabaseLocked(err) || time.Now().After(deadline) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err == nil && storage.SchemaRebuilt {
		fmt.Fprintln(os.Stderr, "lotel: database rebuilt for schema v2; run 'lotel ingest' to reload raw telemetry")
		storage.SchemaRebuilt = false
	}
	return db, err
}

func parseAttrFlags(flags []string) ([]storage.AttrFilter, error) {
	var out []storage.AttrFilter
	for _, flag := range flags {
		key, value, ok := strings.Cut(flag, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid --attr %q: expected nonempty key=value", flag)
		}
		out = append(out, storage.AttrFilter{Key: key, Value: value})
	}
	return out, nil
}

func parseCommandQueryOpts(cmd *cobra.Command, service, since, until string, limit int) (storage.QueryOptions, error) {
	opts, err := parseQueryOpts(service, since, until, limit)
	if err != nil {
		return opts, err
	}
	opts.Name, _ = cmd.Flags().GetString("name")
	opts.TraceID, _ = cmd.Flags().GetString("trace-id")
	opts.EventName, _ = cmd.Flags().GetString("event-name")
	flags, _ := cmd.Flags().GetStringArray("attr")
	opts.Attrs, err = parseAttrFlags(flags)
	return opts, err
}
