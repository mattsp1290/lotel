package main

import (
	_ "embed"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed schemas/page-traces-v1.json
var pageTracesSchemaV1 string

//go:embed schemas/page-logs-v1.json
var pageLogsSchemaV1 string

func outputSchemaCommand() *cobra.Command {
	var version int
	cmd := &cobra.Command{
		Use:   "output-schema COMMAND...",
		Short: "Print a supported command's successful output JSON Schema",
		Long:  "Print a JSON Schema for a supported command's successful output. Version 1 supports query page traces and query page logs. Errors are not described.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := fmt.Sprintf("%d:%s", version, strings.Join(args, " "))
			schemas := map[string]string{
				"1:query page traces": pageTracesSchemaV1,
				"1:query page logs":   pageLogsSchemaV1,
			}
			schema, ok := schemas[key]
			if !ok {
				return fmt.Errorf("no output schema version %d for %q", version, strings.Join(args, " "))
			}
			_, err := io.WriteString(cmd.OutOrStdout(), schema)
			return err
		},
	}
	cmd.Flags().IntVar(&version, "schema-version", 1, "output schema version")
	return cmd
}
