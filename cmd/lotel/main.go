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

var version = "dev"
var distribution = "source"

func main() {
	rootCmd := &cobra.Command{
		Use:     "lotel",
		Version: version,
		Short:   "Local OpenTelemetry — manage a collector and query telemetry",
	}

	rootCmd.SetVersionTemplate("lotel {{.Version}}\n")

	// --- start ---
	var waitHealthy bool
	var collectorConfig string
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
			configPath, dataPath, err := config.ResolvePathsWithConfig(collectorConfig)
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
	startCmd.Flags().StringVar(&collectorConfig, "config", "", "use an explicit Collector config path")
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

	ingestCmd := ingestCommand()

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
			opts, err := parseCommandQueryOpts(cmd, service, since, until, limit)
			if err != nil {
				return err
			}
			traceOpts := storage.TraceQueryOptions{QueryOptions: opts}
			traceOpts.ErrorsOnly, _ = cmd.Flags().GetBool("errors-only")
			if cmd.Flags().Changed("min-duration") {
				duration, _ := cmd.Flags().GetDuration("min-duration")
				if duration < 0 {
					return fmt.Errorf("--min-duration must be nonnegative")
				}
				traceOpts.MinDuration = &duration
			}
			return withDatabaseJSON(func(cmd *cobra.Command, args []string, db *sql.DB) (any, error) {
				results, err := storage.QueryTraces(db, traceOpts)
				if err != nil {
					return nil, err
				}
				if results == nil {
					results = []storage.TraceResult{}
				}
				return results, nil
			}, true)(cmd, args)
		},
	}

	queryMetricsCmd := &cobra.Command{
		Use:   "metrics",
		Short: "Query metrics (JSON output)",
		RunE: withDatabaseJSON(func(cmd *cobra.Command, args []string, db *sql.DB) (any, error) {
			opts, err := parseCommandQueryOpts(cmd, service, since, until, limit)
			if err != nil {
				return nil, err
			}
			results, err := storage.QueryMetrics(db, opts)
			if err != nil {
				return nil, err
			}
			if results == nil {
				results = []storage.MetricResult{}
			}
			return results, nil
		}, true),
	}

	queryLogsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Query logs (JSON output)",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := parseCommandQueryOpts(cmd, service, since, until, limit)
			if err != nil {
				return err
			}
			logOpts := storage.LogQueryOptions{QueryOptions: opts}
			logOpts.MinSeverity, logOpts.BodyContains, err = parseLogFilters(cmd)
			if err != nil {
				return err
			}
			return withDatabaseJSON(func(cmd *cobra.Command, args []string, db *sql.DB) (any, error) {
				results, err := storage.QueryLogs(db, logOpts)
				if err != nil {
					return nil, err
				}
				if results == nil {
					results = []storage.LogResult{}
				}
				return results, nil
			}, true)(cmd, args)
		},
	}

	// --- query metrics aggregate ---
	var metricName string
	queryAggCmd := &cobra.Command{
		Use:   "aggregate",
		Short: "Compute avg/min/max; for cumulative histograms, read the latest query metrics point",
		RunE: withDatabaseJSON(func(cmd *cobra.Command, args []string, db *sql.DB) (any, error) {
			if metricName == "" {
				return nil, fmt.Errorf("--metric is required")
			}
			opts, err := parseQueryOpts(service, since, until, 0)
			if err != nil {
				return nil, err
			}
			result, err := storage.AggregateMetrics(db, opts, metricName)
			if err != nil {
				return nil, err
			}
			return result, nil
		}, true),
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
	queryTracesCmd.Flags().Bool("errors-only", false, "filter spans with OpenTelemetry error status")
	queryTracesCmd.Flags().Duration("min-duration", 0, "minimum span duration (inclusive, e.g. 100ms)")
	for _, cmd := range []*cobra.Command{queryTracesCmd, queryLogsCmd} {
		cmd.Flags().String("trace-id", "", "trace ID (lowercase hex)")
	}
	queryLogsCmd.Flags().String("event-name", "", "exact log event name")
	queryLogsCmd.Flags().String("min-severity", "", "minimum severity: trace, debug, info, warn, error, fatal")
	queryLogsCmd.Flags().String("body-contains", "", "case-insensitive literal substring in projected log body")
	for _, cmd := range []*cobra.Command{queryTracesCmd, queryMetricsCmd, queryLogsCmd} {
		cmd.Flags().StringArray("attr", nil, "attribute key=value; repeat for AND; matches compact JSON string form, null never matches")
	}
	queryCmd.AddCommand(metricDifferenceCommand(), pageCommand(), recentCommand(), discoveryCommand("services"), discoveryCommand("operations"), dependenciesCommand(), spanStatsCommand(), logStatsCommand(), traceCommand(), queryTracesCmd, queryMetricsCmd, queryLogsCmd, queryAggCmd, genAICommand(), otlpQueryCommand(""), otlpQueryCommand("profiles"))

	pruneCmd := pruneCommand()

	var catalog commandCatalog
	rootCmd.AddCommand(runtimeRunCommand(), signalDiagnosisWorkerCommand())
	rootCmd.AddCommand(startCmd, stopCmd, statusCmd, healthCmd, ingestCmd, queryCmd, assertCommand(), pruneCmd, upgradeCommand(), diagnoseCommand(), setupCommand(), commandsCommand(&catalog), outputSchemaCommand())
	rootCmd.InitDefaultVersionFlag()
	catalog = buildCommandCatalog(rootCmd, version)

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

func withDatabase(run func(*cobra.Command, []string, *sql.DB) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		dataPath, err := config.DataPath()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		var rebuilt bool
		err = storage.WithDB(ctx, filepath.Join(dataPath, "lotel.db"), func(db *sql.DB) error {
			if storage.SchemaRebuilt {
				rebuilt = true
				storage.SchemaRebuilt = false
			}
			return run(cmd, args, db)
		})
		if rebuilt {
			fmt.Fprintln(cmd.ErrOrStderr(), "lotel: database index rebuilt; run 'lotel ingest' to reload raw telemetry")
		}
		return err
	}
}

func writeJSON(cmd *cobra.Command, value any, indent bool) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	if indent {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}

func withDatabaseJSON(run func(*cobra.Command, []string, *sql.DB) (any, error), indent bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		var result any
		err := withDatabase(func(cmd *cobra.Command, args []string, db *sql.DB) error {
			var err error
			result, err = run(cmd, args, db)
			return err
		})(cmd, args)
		if err != nil {
			return err
		}
		return writeJSON(cmd, result, indent)
	}
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
