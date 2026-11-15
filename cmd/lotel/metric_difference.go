package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

func metricDifferenceCommand() *cobra.Command {
	var selector, fromText, toText string
	cmd := &cobra.Command{
		Use:   "metric-difference",
		Short: "Compare two observed cumulative integer sum points (JSON)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			cmd.SetContext(ctx)
			from, err := strconv.ParseUint(fromText, 10, 64)
			if err != nil || from == 0 {
				return fmt.Errorf("--from-ns must be nonzero Unix nanoseconds")
			}
			to, err := strconv.ParseUint(toText, 10, 64)
			if err != nil || to <= from {
				return fmt.Errorf("--to-ns must be later Unix nanoseconds")
			}
			file, err := os.Open(selector)
			if err != nil {
				return fmt.Errorf("reading metric selector: %w", err)
			}
			input, readErr := io.ReadAll(io.LimitReader(file, storage.MetricSelectorByteLimit+1))
			closeErr := file.Close()
			if readErr != nil {
				return fmt.Errorf("reading metric selector: %w", readErr)
			}
			if closeErr != nil {
				return fmt.Errorf("closing metric selector: %w", closeErr)
			}
			if len(input) > storage.MetricSelectorByteLimit {
				return fmt.Errorf("metric selector exceeds size limit")
			}
			prepared, err := storage.PrepareMetricSelector(ctx, input)
			if err != nil {
				return fmt.Errorf("invalid metric selector: %w", err)
			}
			return withDatabaseJSON(func(_ *cobra.Command, _ []string, db *sql.DB) (any, error) {
				return storage.QueryMetricDifference(ctx, db, prepared, from, to)
			}, true)(cmd, args)
		},
	}
	cmd.Flags().StringVar(&selector, "selector", "", "path to single-stream OTLP JSON selector (required)")
	cmd.Flags().StringVar(&fromText, "from-ns", "", "first point timestamp as Unix nanoseconds (required)")
	cmd.Flags().StringVar(&toText, "to-ns", "", "second point timestamp as Unix nanoseconds (required)")
	_ = cmd.MarkFlagRequired("selector")
	_ = cmd.MarkFlagRequired("from-ns")
	_ = cmd.MarkFlagRequired("to-ns")
	return cmd
}
