package storage

import (
	"path/filepath"
	"testing"
)

func TestQueryGenAICalls(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, path := ingestGenAIFixtures(t)
	if _, err := IngestAll(db, path); err != nil {
		t.Fatal(err)
	}
	calls, err := QueryGenAICalls(db, GenAIOptions{Content: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Fatalf("calls: %d", len(calls))
	}
	first := calls[0]
	if first.Provider != "openai" || first.InputTokens == nil || *first.InputTokens != 5 || first.EventCount != 1 || len(first.FinishReasons) != 1 {
		t.Fatalf("native fields: %+v", first)
	}
	if _, ok := first.InputMessages.([]any); !ok || first.ContentSource != "span" {
		t.Fatalf("content: %+v", first)
	}
	legacy := calls[3]
	if legacy.Provider != "legacy" || legacy.InputTokens == nil || *legacy.InputTokens != 11 {
		t.Fatalf("legacy aliases: %+v", legacy)
	}
	filtered, err := QueryGenAICalls(db, GenAIOptions{Model: "legacy-model"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 {
		t.Fatal("model filter failed")
	}
	usage, err := QueryGenAIUsage(db, GenAIOptions{GroupBy: []string{"model"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 3 || usage[0].TotalTokens != 18 {
		t.Fatalf("usage: %+v", usage)
	}
}

func TestQueryFilters(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, path := ingestGenAIFixtures(t)
	if _, err := IngestAll(db, path); err != nil {
		t.Fatal(err)
	}
	rows, err := QueryTraces(db, QueryOptions{Attrs: []AttrFilter{{Key: "gen_ai.usage.input_tokens", Value: "5"}, {Key: "gen_ai.provider.name", Value: "openai"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("attribute filters: %d", len(rows))
	}
	logs, err := QueryLogs(db, QueryOptions{EventName: "missing"})
	if err != nil || len(logs) != 0 {
		t.Fatalf("event filter: %v", err)
	}
	metrics, err := QueryMetrics(db, QueryOptions{Name: "summary"})
	if err != nil || len(metrics) != 1 {
		t.Fatalf("name filter: %v", err)
	}
	_, err = QueryTraces(db, QueryOptions{Attrs: []AttrFilter{{Key: "bad\n", Value: "x"}}})
	if err == nil {
		t.Fatal("control character accepted")
	}
}

func TestQueryGenAICalls_ContentFromEventAndOrder(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, path := ingestGenAIFixtures(t)
	if _, err := IngestAll(db, path); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE traces SET attributes=json_merge_patch(attributes,'{"gen_ai.input.messages":null}'),start_time=start_time+INTERVAL 1 SECOND WHERE span_id='1111111111111111'`); err != nil {
		t.Fatal(err)
	}
	calls, err := QueryGenAICalls(db, GenAIOptions{Content: true, QueryOptions: QueryOptions{Limit: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].SpanID != "1111111111111111" || calls[0].ContentSource != "event" {
		t.Fatalf("fallback/order: %+v", calls)
	}
	if _, ok := calls[0].InputMessages.([]any); !ok {
		t.Fatal("event content lost")
	}
}

func TestQueryFiltersEscaping(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, path := ingestGenAIFixtures(t)
	if _, err := IngestAll(db, path); err != nil {
		t.Fatal(err)
	}
	attrs := jsonText(map[string]any{`dot."quote\slash`: "exact", "integral": 1.0, "large": 1e21, "null": nil})
	if _, err := db.Exec("UPDATE traces SET attributes=? WHERE span_id='1111111111111111'", attrs); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key, value string
		want       int
	}{
		{`dot."quote\slash`, "exact", 1}, {"integral", "1", 1}, {"integral", "1.0", 0}, {"large", "1e21", 1}, {"null", "null", 0},
	} {
		rows, err := QueryTraces(db, QueryOptions{Attrs: []AttrFilter{{Key: tc.key, Value: tc.value}}})
		if err != nil || len(rows) != tc.want {
			t.Fatalf("%q=%s: %d rows, %v", tc.key, tc.value, len(rows), err)
		}
	}
}

func TestQueryGenAIEventIdentity(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, path := ingestGenAIFixtures(t)
	if _, err := IngestAll(db, path); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO logs(timestamp,event_name,service_name,trace_id,span_id,date) VALUES(CURRENT_TIMESTAMP,'gen_ai.other','other','other-trace','1111111111111111',CURRENT_DATE)`)
	if err != nil {
		t.Fatal(err)
	}
	calls, err := QueryGenAICalls(db, GenAIOptions{QueryOptions: QueryOptions{TraceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}})
	if err != nil {
		t.Fatal(err)
	}
	if calls[0].EventCount != 1 {
		t.Fatalf("event count crossed traces: %d", calls[0].EventCount)
	}
}
