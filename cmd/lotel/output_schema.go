package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed schemas/*.json
var outputSchemaFiles embed.FS

type outputContract struct {
	Command         string `json:"command"`
	SchemaSupported bool   `json:"schema_supported"`
	Format          string `json:"format"`
	SchemaVersion   int    `json:"schema_version,omitempty"`
	SchemaID        string `json:"schema_id,omitempty"`
	Condition       string `json:"condition,omitempty"`
	Note            string `json:"note,omitempty"`
	file            string
}

type errorContract struct {
	Format          string `json:"format"`
	SchemaSupported bool   `json:"schema_supported"`
	Stream          string `json:"stream"`
	Note            string `json:"note"`
}

type outputSchemaCatalog struct {
	SchemaVersion int              `json:"schema_version"`
	Outputs       []outputContract `json:"outputs"`
	Errors        errorContract    `json:"errors"`
}

var outputContracts = map[string]outputContract{
	"commands":                          {file: "commands-v1.json"},
	"status":                            {file: "status-v1.json", Note: "Also emits this JSON with exit status 1 when the Collector is stopped."},
	"ingest":                            {file: "ingest-v1.json"},
	"prune":                             {file: "prune-v1.json"},
	"setup":                             {file: "setup-v1.json"},
	"upgrade":                           {file: "upgrade-v1.json", Condition: "--json", Note: "Without --json, stdout is text. A JSON report can also accompany an error."},
	"diagnose runtime":                  {file: "diagnose-runtime-v1.json"},
	"diagnose signals":                  {file: "diagnose-signals-v1.json"},
	"assert signal":                     {file: "assert-signal-v1.json", Note: "A failed assertion also emits this JSON and exits with status 1. Cobra error and usage text are suppressed; diagnostics can still appear on stderr."},
	"query traces":                      {file: "query-traces-v1.json"},
	"query logs":                        {file: "query-logs-v1.json"},
	"query metrics":                     {file: "query-metrics-v1.json"},
	"query aggregate":                   {file: "query-aggregate-v1.json"},
	"query trace":                       {file: "query-trace-v1.json"},
	"query services":                    {file: "query-services-v1.json"},
	"query operations":                  {file: "query-operations-v1.json"},
	"query dependencies":                {file: "query-dependencies-v1.json"},
	"query span-stats":                  {file: "query-span-stats-v1.json"},
	"query log-stats":                   {file: "query-log-stats-v1.json"},
	"query genai calls":                 {file: "query-genai-calls-v1.json"},
	"query genai usage":                 {file: "query-genai-usage-v1.json"},
	"query otlp":                        {file: "query-otlp-v1.json"},
	"query profiles":                    {file: "query-otlp-v1.json"},
	"query recent traces":               {file: "query-recent-traces-v1.json"},
	"query recent logs":                 {file: "query-recent-logs-v1.json"},
	"query page traces":                 {file: "page-traces-v1.json"},
	"query page logs":                   {file: "page-logs-v1.json"},
	"query metric-difference":           {file: "query-metric-difference-v1.json"},
	"query metric-delta-total":          {file: "query-metric-delta-total-v1.json"},
	"query metric-delta-histogram":      {file: "query-metric-delta-histogram-v1.json"},
	"query metric-cumulative-histogram": {file: "query-metric-cumulative-histogram-v1.json"},
	"output-schema":                     {file: "output-schema-v1.json", Note: "The default output is a JSON Schema document. With --list, stdout follows the output-schema --list contract."},
	"output-schema --list":              {file: "output-schema-list-v1.json"},
	"lotel":                             {Format: "text", Note: "Without a command, stdout is help text. --version also emits text."},
	"assert":                            {Format: "text", Note: "Without a subcommand, stdout is help text."},
	"diagnose":                          {Format: "text", Note: "Without a subcommand, stdout is help text."},
	"query":                             {Format: "text", Note: "Without a subcommand, stdout is help text."},
	"query genai":                       {Format: "text", Note: "Without a subcommand, stdout is help text."},
	"query recent":                      {Format: "text", Note: "Without a subcommand, stdout is help text."},
	"query page":                        {Format: "text", Note: "Without a subcommand, stdout is help text."},
	"start":                             {Format: "text", Note: "No JSON output schema."},
	"stop":                              {Format: "text", Note: "No JSON output schema."},
	"health":                            {Format: "text", Note: "Health messages are text and unhealthy checks exit with status 1."},
}

func outputContractFor(command string) (outputContract, bool) {
	contract, ok := outputContracts[command]
	if !ok {
		return outputContract{}, false
	}
	contract.Command = command
	if contract.file != "" {
		contract.Format = "json"
		contract.SchemaSupported = true
		contract.SchemaVersion = 1
		data, err := outputSchemaFiles.ReadFile("schemas/" + contract.file)
		if err == nil {
			var schema struct {
				ID string `json:"$id"`
			}
			if json.Unmarshal(data, &schema) == nil {
				contract.SchemaID = schema.ID
			}
		}
	}
	return contract, true
}

func buildOutputSchemaCatalog() outputSchemaCatalog {
	catalog := outputSchemaCatalog{
		SchemaVersion: 1,
		Outputs:       []outputContract{},
		Errors: errorContract{
			Format: "text", Stream: "stderr",
			Note: "No JSON error protocol or error schema. Cobra errors and usage are text on stderr. Nonzero exits may also have structured stdout as noted per command; health writes text to stdout. Commands may write diagnostic text to stderr at any exit status. Inspect the exit status separately from stdout.",
		},
	}
	for command := range outputContracts {
		contract, _ := outputContractFor(command)
		catalog.Outputs = append(catalog.Outputs, contract)
	}
	sort.Slice(catalog.Outputs, func(i, j int) bool { return catalog.Outputs[i].Command < catalog.Outputs[j].Command })
	return catalog
}

func outputSchemaCommand() *cobra.Command {
	var version int
	var list bool
	cmd := &cobra.Command{
		Use:   "output-schema [COMMAND...]",
		Short: "Print a versioned output JSON Schema or list output contracts",
		Long:  "Print a versioned JSON Schema for structured command stdout. Use --list to discover JSON commands, conditional formats, unsupported text commands, and the stderr text error contract. This command does not access telemetry state.",
		Args: func(cmd *cobra.Command, args []string) error {
			if list {
				return cobra.NoArgs(cmd, args)
			}
			return cobra.MinimumNArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if version != 1 {
				return fmt.Errorf("unsupported output schema version %d", version)
			}
			if list {
				return writeJSON(cmd, buildOutputSchemaCatalog(), true)
			}
			command := strings.Join(args, " ")
			contract, ok := outputContractFor(command)
			if !ok {
				return fmt.Errorf("no output schema version %d for %q; use output-schema --list", version, command)
			}
			if contract.Format != "json" {
				return fmt.Errorf("%q emits %s; no JSON output schema", command, contract.Format)
			}
			data, err := outputSchemaFiles.ReadFile("schemas/" + contract.file)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	cmd.Flags().IntVar(&version, "schema-version", 1, "output schema version")
	cmd.Flags().BoolVar(&list, "list", false, "list output contracts and the error format as JSON")
	return cmd
}
