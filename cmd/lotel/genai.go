package main

import (
	"database/sql"
	"strings"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/spf13/cobra"
)

func genAICommand() *cobra.Command {
	parent := &cobra.Command{Use: "genai", Short: "Query GenAI calls and token usage"}
	for _, mode := range []string{"calls", "usage"} {
		mode := mode
		var service, since, until, model, provider, operation, conversation, traceID, groupBy string
		var limit int
		var content, errorsOnly bool
		cmd := &cobra.Command{Use: mode, Short: "Query GenAI " + mode + " (JSON)", RunE: withDatabaseJSON(func(cmd *cobra.Command, args []string, db *sql.DB) (any, error) {
			common, err := parseQueryOpts(service, since, until, limit)
			if err != nil {
				return nil, err
			}
			common.TraceID = traceID
			opts := storage.GenAIOptions{QueryOptions: common, Model: model, Provider: provider, Operation: operation, ConversationID: conversation, ErrorsOnly: errorsOnly, Content: content, GroupBy: strings.Split(groupBy, ",")}
			if mode == "calls" {
				rows, err := storage.QueryGenAICalls(db, opts)
				if err != nil {
					return nil, err
				}
				if rows == nil {
					rows = []storage.GenAICall{}
				}
				return rows, nil
			} else {
				rows, err := storage.QueryGenAIUsage(db, opts)
				if err != nil {
					return nil, err
				}
				if rows == nil {
					rows = []storage.GenAIUsageRow{}
				}
				return rows, nil
			}
		}, true)}
		f := cmd.Flags()
		f.StringVar(&service, "service", "", "filter by service.name")
		f.StringVar(&since, "since", "", "start time (RFC3339 or relative duration)")
		f.StringVar(&until, "until", "", "end time (RFC3339)")
		f.StringVar(&traceID, "trace-id", "", "trace ID")
		f.StringVar(&model, "model", "", "request or response model")
		f.StringVar(&provider, "provider", "", "provider name (including legacy gen_ai.system)")
		f.StringVar(&operation, "operation", "", "operation name")
		f.StringVar(&conversation, "conversation-id", "", "conversation ID")
		f.BoolVar(&errorsOnly, "errors-only", false, "only calls with errors")
		if mode == "calls" {
			f.IntVar(&limit, "limit", 0, "max calls, newest first (0 = unlimited)")
			f.BoolVar(&content, "content", false, "include structured span and event content")
		} else {
			f.StringVar(&groupBy, "group-by", "provider,model,operation", "comma-separated provider,model,operation,service,conversation")
		}
		parent.AddCommand(cmd)
	}
	return parent
}
