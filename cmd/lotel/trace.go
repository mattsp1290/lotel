package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

type traceBundle struct {
	TraceID string                `json:"trace_id"`
	Spans   []storage.TraceResult `json:"spans"`
	Logs    []storage.LogResult   `json:"logs"`
}

func traceCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "trace <trace-id>",
		Short: "Query a trace and its correlated logs (JSON)",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			id, err := hex.DecodeString(args[0])
			if err != nil || len(id) != 16 || strings.Trim(args[0], "0") == "" {
				return fmt.Errorf("trace ID must be 32 hexadecimal characters and nonzero")
			}
			args[0] = strings.ToLower(args[0])
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			tx, err := db.Begin()
			if err != nil {
				return err
			}
			defer tx.Rollback()
			opts := storage.QueryOptions{TraceID: args[0]}
			spans, err := storage.QueryTraces(tx, storage.TraceQueryOptions{QueryOptions: opts})
			if err != nil {
				return err
			}
			logs, err := storage.QueryLogs(tx, opts)
			if err != nil {
				return err
			}
			if spans == nil {
				spans = []storage.TraceResult{}
			}
			if logs == nil {
				logs = []storage.LogResult{}
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(traceBundle{TraceID: args[0], Spans: spans, Logs: logs})
		},
	}
}
