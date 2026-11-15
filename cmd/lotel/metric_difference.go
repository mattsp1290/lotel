package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

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
			if err := storage.ValidateMetricSelector(input); err != nil {
				return fmt.Errorf("invalid metric selector: %w", err)
			}
			var result storage.MetricDifference
			if err := withDatabase(func(_ *cobra.Command, _ []string, db *sql.DB) error {
				var queryErr error
				result, queryErr = storage.QueryMetricDifference(db, input, from, to)
				return queryErr
			})(cmd, args); err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
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
