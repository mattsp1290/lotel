package storage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenAIMixedCaseIngestedCorrelation(t *testing.T) {
	path := t.TempDir()
	inputs := map[string]string{
		"traces": `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"mixed-case"}}]},"scopeSpans":[{"spans":[{"traceId":"ABCDEF0123456789ABCDEF0123456789","spanId":"ABCDEF0123456789","name":"chat","startTimeUnixNano":"1700000000000000000","endTimeUnixNano":"1700000001000000000","attributes":[{"key":"gen_ai.operation.name","value":{"stringValue":"chat"}},{"key":"gen_ai.usage.input_tokens","value":{"intValue":"5"}}]}]}]}]}`,
		"logs": `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"mixed-case"}}]},"scopeLogs":[{"logRecords":[
			{"timeUnixNano":"1700000000000000000","eventName":"gen_ai.client.inference.operation.details","traceId":"abcdef0123456789abcdef0123456789","spanId":"abcdef0123456789","attributes":[{"key":"gen_ai.input.messages","value":{"stringValue":"first"}}]},
			{"timeUnixNano":"1700000000000001000","eventName":"gen_ai.client.inference.operation.details","traceId":"ABCDEF0123456789ABCDEF0123456789","spanId":"AbCdEf0123456789","attributes":[{"key":"gen_ai.output.messages","value":{"stringValue":"second"}}]},
			{"timeUnixNano":"1700000000000002000","eventName":"gen_ai.client.inference.operation.details","traceId":"abcdef0123456789abcdef0123456789","attributes":[{"key":"gen_ai.tool.definitions","value":{"stringValue":"unscoped"}}]},
			{"timeUnixNano":"1700000000000003000","eventName":"gen_ai.client.inference.operation.details","traceId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","spanId":"abcdef0123456789","attributes":[{"key":"gen_ai.tool.call.arguments","value":{"stringValue":"wrong trace"}}]},
			{"timeUnixNano":"1700000000000004000","eventName":"gen_ai.client.inference.operation.details","traceId":"abcdef0123456789abcdef0123456789","spanId":"bbbbbbbbbbbbbbbb","attributes":[{"key":"gen_ai.tool.call.result","value":{"stringValue":"wrong span"}}]}
		]}]}]}`,
	}
	for signal, data := range inputs {
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(data)); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(path, signal)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, signal+".jsonl"), append(compact.Bytes(), '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := IngestAll(db, path); err != nil {
		t.Fatal(err)
	}
	const id = "abcdef0123456789abcdef0123456789"
	t.Run("filter both cases and usage", func(t *testing.T) {
		for _, filter := range []string{id, strings.ToUpper(id)} {
			calls, err := QueryGenAICalls(db, GenAIOptions{QueryOptions: QueryOptions{TraceID: filter}})
			if err != nil || len(calls) != 1 {
				t.Fatalf("filter %q: calls=%+v err=%v", filter, calls, err)
			}
			if calls[0].TraceID != strings.ToUpper(id) || calls[0].EventCount != 2 {
				t.Fatalf("wrong identity or event count: %+v", calls[0])
			}
			if calls[0].InputMessages != nil || calls[0].OutputMessages != nil {
				t.Fatalf("content returned without opt-in: %+v", calls[0])
			}
			usage, err := QueryGenAIUsage(db, GenAIOptions{QueryOptions: QueryOptions{TraceID: filter}})
			if err != nil || len(usage) != 1 || usage[0].Calls != 1 || usage[0].InputTokens != 5 {
				t.Fatalf("usage=%+v err=%v", usage, err)
			}
		}
	})
	t.Run("content correlates by both IDs and skips unscoped logs", func(t *testing.T) {
		calls, err := QueryGenAICalls(db, GenAIOptions{Content: true})
		if err != nil || len(calls) != 1 {
			t.Fatalf("calls=%+v err=%v", calls, err)
		}
		if calls[0].EventCount != 2 || calls[0].InputMessages != "first" || calls[0].OutputMessages != "second" || calls[0].ContentSource != "event" {
			t.Fatalf("content correlation: %+v", calls[0])
		}
		if calls[0].ToolDefinitions != nil || calls[0].ToolCallArguments != nil || calls[0].ToolCallResult != nil {
			t.Fatalf("content leaked from unrelated events: %+v", calls[0])
		}
	})
	t.Run("duplicate span records remain separate", func(t *testing.T) {
		if _, err := db.Exec("INSERT INTO traces SELECT * FROM traces"); err != nil {
			t.Fatal(err)
		}
		calls, err := QueryGenAICalls(db, GenAIOptions{Content: true, QueryOptions: QueryOptions{TraceID: id}})
		if err != nil || len(calls) != 2 {
			t.Fatalf("calls=%+v err=%v", calls, err)
		}
		for _, call := range calls {
			if call.EventCount != 2 || call.InputMessages != "first" || call.OutputMessages != "second" {
				t.Fatalf("duplicate span lost correlation: %+v", call)
			}
			if call.ToolDefinitions != nil || call.ToolCallArguments != nil || call.ToolCallResult != nil {
				t.Fatalf("content leaked from unrelated events: %+v", call)
			}
		}
	})
}
