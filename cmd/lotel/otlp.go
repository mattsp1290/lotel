package main

import (
	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

func otlpQueryCommand(signal string) *cobra.Command {
	name := signal
	if name == "" {
		name = "otlp"
	}
	var service, since, until string
	var limit int
	cmd := &cobra.Command{
		Use:   name,
		Short: "Query complete OTLP resource envelopes (JSON output)",
		Long:  "Query complete OTLP resource envelopes. Time filters select overlapping resource time ranges and return the entire envelope, including profile dictionaries. Limits count resource envelopes.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := parseQueryOpts(service, since, until, limit)
			if err != nil {
				return err
			}
			db, err := openDB()
			if err != nil {
				return err
			}
			results, err := storage.QueryOTLP(db, signal, opts)
			if err != nil {
				return err
			}
			if results == nil {
				results = []storage.OTLPResult{}
			}
			printJSON(results)
			return nil
		},
	}
	if name == "otlp" {
		cmd.Flags().StringVar(&signal, "signal", "", "telemetry signal: traces, metrics, logs, or profiles (required)")
		_ = cmd.MarkFlagRequired("signal")
	}
	cmd.Flags().StringVar(&service, "service", "", "filter by service.name")
	cmd.Flags().StringVar(&since, "since", "", "start time (RFC3339 or relative like '1h', '24h')")
	cmd.Flags().StringVar(&until, "until", "", "end time (RFC3339)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max resource envelopes (0 = unlimited)")
	return cmd
}
