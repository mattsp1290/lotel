package main

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/mattsp1290/lotel/internal/config"
	"github.com/mattsp1290/lotel/internal/docker"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "lotel",
		Short: "Manage the local OpenTelemetry Collector",
	}

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the OTel Collector container",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, dataPath, err := config.ResolvePaths()
			if err != nil {
				return err
			}

			dc, err := docker.NewClient()
			if err != nil {
				return err
			}
			defer dc.Close()

			return dc.StartCollector(cmd.Context(), configPath, dataPath)
		},
	}

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop and remove the OTel Collector container",
		RunE: func(cmd *cobra.Command, args []string) error {
			dc, err := docker.NewClient()
			if err != nil {
				return err
			}
			defer dc.Close()

			return dc.StopCollector(cmd.Context())
		},
	}

	rootCmd.AddCommand(startCmd, stopCmd)

	ctx := context.Background()
	if err := fang.Execute(ctx, rootCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
