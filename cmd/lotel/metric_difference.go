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
	return metricComparisonCommand("metric-difference", "Compare cumulative integer sum points and report their observed rate (JSON)", storage.PrepareMetricSelector, storage.QueryMetricDifference)
}

func metricDeltaTotalCommand() *cobra.Command {
	return metricComparisonCommand("metric-delta-total", "Total contiguous delta integer sum points and report their observed rate (JSON)", storage.PrepareDeltaMetricSelector, storage.QueryMetricDeltaTotal)
}

func metricDeltaHistogramCommand() *cobra.Command {
	return metricComparisonCommand("metric-delta-histogram", "Sum observed contiguous delta histogram bucket counts (JSON)", storage.PrepareDeltaHistogramSelector, storage.QueryMetricHistogram)
}

func metricCumulativeHistogramCommand() *cobra.Command {
	return metricComparisonCommand("metric-cumulative-histogram", "Compare observed cumulative histogram bucket counts (JSON)", storage.PrepareCumulativeHistogramSelector, storage.QueryMetricHistogram)
}

func metricComparisonCommand[T any](name, description string, prepare func(context.Context, []byte) (storage.MetricSelector, error), query func(context.Context, *sql.DB, storage.MetricSelector, uint64, uint64) (T, error)) *cobra.Command {
	var selector, fromText, toText string
	cmd := &cobra.Command{
		Use:   name,
		Short: description,
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
			prepared, err := prepare(ctx, input)
			if err != nil {
				return fmt.Errorf("invalid metric selector: %w", err)
			}
			return withDatabaseJSON(func(_ *cobra.Command, _ []string, db *sql.DB) (any, error) {
				return query(ctx, db, prepared, from, to)
			}, true)(cmd, args)
		},
	}
	cmd.Flags().StringVar(&selector, "selector", "", "path to single-stream OTLP JSON selector (required)")
	cmd.Flags().StringVar(&fromText, "from-ns", "", "start timestamp as Unix nanoseconds (required)")
	cmd.Flags().StringVar(&toText, "to-ns", "", "end timestamp as Unix nanoseconds (required)")
	_ = cmd.MarkFlagRequired("selector")
	_ = cmd.MarkFlagRequired("from-ns")
	_ = cmd.MarkFlagRequired("to-ns")
	return cmd
}
