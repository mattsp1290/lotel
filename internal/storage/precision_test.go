package storage

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueryLargeIntegerPrecision(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, path := ingestGenAIFixtures(t)
	if _, err := IngestAll(db, path); err != nil {
		t.Fatal(err)
	}
	const exact = int64(9007199254740993)
	attrs := jsonText(map[string]any{"large": exact, "gen_ai.operation.name": "chat", "gen_ai.input.messages": jsonText([]any{map[string]any{"large": exact}})})
	resource := jsonText(map[string]any{"large": exact})
	events := jsonText([]any{map[string]any{"name": "large", "attributes": map[string]any{"large": exact}}})
	links := jsonText([]any{map[string]any{"trace_id": "x", "span_id": "y", "attributes": map[string]any{"large": exact}}})
	if _, err := db.Exec("UPDATE traces SET attributes=?,resource_attributes=?,events=?,links=? WHERE span_id='1111111111111111'", attrs, resource, events, links); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE logs SET attributes=?,body_json=?,resource_attributes=?", resource, resource, resource); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE metrics SET attributes=?,resource_attributes=?", resource, resource); err != nil {
		t.Fatal(err)
	}
	traces, err := QueryTraces(db, QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	logs, err := QueryLogs(db, QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := QueryMetrics(db, QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	calls, err := QueryGenAICalls(db, GenAIOptions{Content: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{traces[0].Attributes, traces[0].ResourceAttributes, traces[0].Events, traces[0].Links, logs[0].Attributes, logs[0].BodyValue, logs[0].ResourceAttributes, metrics[0].Attributes, metrics[0].ResourceAttributes, calls[0].InputMessages} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "9007199254740993") || strings.Contains(string(data), "9007199254740992") {
			t.Fatalf("integer changed: %s", data)
		}
	}
}
