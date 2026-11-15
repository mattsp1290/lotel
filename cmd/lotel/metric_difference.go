package main

import (
	"encoding/json"
	"fmt"
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
			input, err := os.ReadFile(selector)
			if err != nil {
				return fmt.Errorf("reading metric selector: %w", err)
			}
			if err := storage.ValidateMetricSelector(input); err != nil {
				return fmt.Errorf("invalid metric selector: %w", err)
			}
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			result, err := storage.QueryMetricDifference(db, input, from, to)
			if err != nil {
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
