package main

import (
	"encoding/json"

	"github.com/mattsp1290/lotel/internal/collector"
	"github.com/spf13/cobra"
)

func diagnoseCommand() *cobra.Command {
	parent := &cobra.Command{Use: "diagnose", Short: "Inspect lotel without changing it"}
	parent.AddCommand(&cobra.Command{
		Use: "runtime", Short: "Inspect the recorded Collector runtime (JSON)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(collector.DiagnoseRuntime(cmd.Context()))
		},
	})
	parent.AddCommand(signalDiagnosisCommand())
	return parent
}
