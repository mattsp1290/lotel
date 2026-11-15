package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/mattsp1290/lotel/internal/collector"
	"github.com/spf13/cobra"
)

func nativeRunCommand() *cobra.Command {
	var configPath, dataPath, socket, token string
	command := &cobra.Command{
		Use: "run-collector", Hidden: true, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return collector.RunNative(ctx, configPath, dataPath, socket, token)
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "collector configuration path")
	command.Flags().StringVar(&dataPath, "data", "", "telemetry data directory")
	command.Flags().StringVar(&socket, "control", "", "private control socket")
	command.Flags().StringVar(&token, "token", "", "private control token")
	for _, name := range []string{"config", "data", "control", "token"} {
		_ = command.MarkFlagRequired(name)
	}
	return command
}
