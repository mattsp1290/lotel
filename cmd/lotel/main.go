package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mattsp1290/lotel/internal/collector"
	"github.com/mattsp1290/lotel/internal/config"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "lotel",
		Short: "Local OpenTelemetry — manage a collector and query telemetry",
	}

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the OTel Collector subprocess",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, dataPath, err := config.ResolvePaths()
			if err != nil {
				return err
			}
			return collector.Start(cmd.Context(), configPath, dataPath)
		},
	}

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the OTel Collector subprocess",
		RunE: func(cmd *cobra.Command, args []string) error {
			return collector.Stop(cmd.Context())
		},
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show collector status",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := collector.GetStatus(cmd.Context())
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(s, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}

	rootCmd.AddCommand(startCmd, stopCmd, statusCmd)

	ctx := context.Background()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
