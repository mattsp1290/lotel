package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeOTLPSignal(t *testing.T, dir, signal, payload string) string {
	t.Helper()
	signalDir := filepath.Join(dir, signal)
	if err := os.MkdirAll(signalDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(signalDir, signal+".jsonl")
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertSameJSON(t *testing.T, want, got []byte) {
	t.Helper()
	var a, b any
	if err := decodeJSON(want, &a); err != nil {
		t.Fatal(err)
	}
	if err := decodeJSON(got, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("OTLP payload changed\nwant %s\ngot  %s", want, got)
	}
}

func TestOTLPCompleteSignalFidelity(t *testing.T) {
	records := map[string]string{
		"traces":   `[{"name":"span","traceId":"12345678901234567890123456789012","spanId":"1234567890123456","startTimeUnixNano":"1794700000000000001","endTimeUnixNano":"1794700000000000009","flags":4294967295,"droppedAttributesCount":2,"droppedEventsCount":3,"droppedLinksCount":4,"events":[{"name":"event","timeUnixNano":"1794700000000000003","droppedAttributesCount":5}],"links":[{"flags":4294967295,"droppedAttributesCount":6}],"attributes":[{"key":"binary","value":{"bytesValue":"AQID"}}],"futureSpan":{"precise":9007199254740993}}]`,
		"metrics":  `[{"name":"int","gauge":{"dataPoints":[{"asInt":"9223372036854775807","timeUnixNano":"1794700000000000001","flags":1,"exemplars":[{"timeUnixNano":"1794700000000000002","asDouble":"NaN","spanId":"1234567890123456"}]}]},"metadata":[{"key":"x","value":{"stringValue":"y"}}]},{"name":"sum","sum":{"isMonotonic":true,"aggregationTemporality":2,"dataPoints":[{"timeUnixNano":"1794700000000000003","asDouble":"Infinity"}]}},{"name":"hist","histogram":{"dataPoints":[{"timeUnixNano":"1794700000000000004","count":"18446744073709551615","bucketCounts":["18446744073709551615"],"sum":"-Infinity"}]}},{"name":"exp","exponentialHistogram":{"dataPoints":[{"timeUnixNano":"1794700000000000005","zeroCount":"18446744073709551615","zeroThreshold":0.001,"positive":{"offset":-1,"bucketCounts":["18446744073709551615"]}}]}},{"name":"summary","summary":{"dataPoints":[{"timeUnixNano":"1794700000000000006","count":"18446744073709551615","quantileValues":[{"quantile":0.9,"value":7}]}]}},{"name":"future","newMetricKind":{"dataPoints":[{"timeUnixNano":"1794700000000000007","value":9007199254740993}]}}]`,
		"logs":     `[{"timeUnixNano":"1794700000000000001","observedTimeUnixNano":"1794700000000000009","eventName":"event","flags":4294967295,"droppedAttributesCount":3,"body":{"kvlistValue":{"values":[{"key":"list","value":{"arrayValue":{"values":[{"intValue":"9223372036854775807"},{"bytesValue":"AQID"},{"doubleValue":"NaN"}]}}}]}}}]`,
		"profiles": `[{"profileId":"12345678901234567890123456789012","timeUnixNano":"1794700000000000001","durationNano":"8","attributeIndices":[1],"sampleType":{"typeStrindex":1,"unitStrindex":2},"samples":[{"stackIndex":1,"values":["9223372036854775807"],"timestampsUnixNano":["1794700000000000003"]}],"originalPayloadFormat":"pprof","originalPayload":"AQID","futureProfile":{"value":9007199254740993}}]`,
	}
	for _, signal := range otlpSignals {
		t.Run(signal.name, func(t *testing.T) {
			db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			root := t.TempDir()
			payload := fmt.Sprintf(`{"%s":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"fidelity"}}],"droppedAttributesCount":7},"schemaUrl":"resource-schema","futureResource":9007199254740993,"%s":[{"scope":{"name":"scope","version":"1","attributes":[{"key":"x","value":{"boolValue":true}}],"droppedAttributesCount":8},"schemaUrl":"scope-schema","%s":%s}]}],"dictionary":{"stringTable":["","cpu","ns"],"futureDict":18446744073709551615},"futureRoot":{"typed":{"bytesValue":"AQID"}}}`, signal.resourceKey, signal.scopeKey, signal.recordKey, records[signal.name])
			writeOTLPSignal(t, root, signal.name, payload+"\n")
			report, err := IngestAll(db, root)
			if err != nil {
				t.Fatal(err)
			}
			if report.Signals[signal.name].OTLPEnvelopes != 1 {
				t.Fatalf("report %+v", report.Signals[signal.name])
			}
			rows, err := QueryOTLP(db, signal.name, QueryOptions{Service: "fidelity"})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("got %d envelopes", len(rows))
			}
			assertSameJSON(t, []byte(payload), rows[0].Payload)
			if rows[0].FirstTimeUnixNano != "1794700000000000001" {
				t.Fatalf("timestamp lost: %+v", rows[0])
			}
			encoded, err := json.Marshal(rows)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(encoded, []byte("9007199254740993")) {
				t.Fatal("query serialization lost integer precision")
			}
			report, err = IngestAll(db, root)
			if err != nil {
				t.Fatal(err)
			}
			if report.Signals[signal.name].Records != 0 || report.Signals[signal.name].OTLPEnvelopes != 0 {
				t.Fatal("repeat ingest duplicated data")
			}
		})
	}
}

func TestOTLPPreserveUndatedAndEmptyEnvelopes(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	for _, signal := range otlpSignals {
		payload := fmt.Sprintf(`{"%s":[],"futureRoot":{"number":18446744073709551615}}`, signal.resourceKey)
		writeOTLPSignal(t, root, signal.name, payload+"\n")
		if _, err := IngestAll(db, root); err != nil {
			t.Fatal(err)
		}
		rows, err := QueryOTLP(db, signal.name, QueryOptions{})
		if err != nil || len(rows) != 1 {
			t.Fatalf("empty envelope lost %v %+v", err, rows)
		}
		assertSameJSON(t, []byte(payload), rows[0].Payload)
		dated, err := QueryOTLP(db, signal.name, QueryOptions{Since: time.Now().Add(-time.Hour)})
		if err != nil || len(dated) != 0 {
			t.Fatalf("undated indexed as new data %v %+v", err, dated)
		}
	}
	undated := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"undated"}}]}]}]}`
	writeOTLPSignal(t, root, "logs", undated+"\n")
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	rows, err := QueryOTLP(db, "logs", QueryOptions{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("undated log lost %v %+v", err, rows)
	}
	if _, err := Prune(db, time.Now(), "", false); err != nil {
		t.Fatal(err)
	}
	rows, _ = QueryOTLP(db, "logs", QueryOptions{})
	if len(rows) != 2 {
		t.Fatal("age prune removed undated envelopes")
	}
	if _, err := PruneAll(db, time.Now(), "", false); err != nil {
		t.Fatal(err)
	}
	for _, signal := range otlpSignals {
		rows, _ := QueryOTLP(db, signal.name, QueryOptions{})
		if len(rows) != 0 {
			t.Fatal("all prune left undated data")
		}
	}
}

func TestOTLPResourceFiltersAndRetention(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	resource := func(service, start, end string) string {
		return fmt.Sprintf(`{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"%s"}}]},"scopeSpans":[{"spans":[{"startTimeUnixNano":"%s","endTimeUnixNano":"%s"}]}]}`, service, start, end)
	}
	a, b := resource("a", "1000000001", "3000000009"), resource("b", "1000000001", "2000000009")
	payload := `{"resourceSpans":[` + a + `,` + b + `],"futureRoot":9007199254740993}`
	writeOTLPSignal(t, root, "traces", payload+"\n")
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	rows, err := QueryOTLP(db, "traces", QueryOptions{Service: "a", Since: time.Unix(2, 0), Until: time.Unix(2, 1), Limit: 1})
	if err != nil || len(rows) != 1 {
		t.Fatalf("interval filter failed %v %+v", err, rows)
	}
	assertSameJSON(t, []byte(`{"resourceSpans":[`+a+`],"futureRoot":9007199254740993}`), rows[0].Payload)
	reports, err := Prune(db, time.Unix(4, 0), "a", true)
	if err != nil {
		t.Fatal(err)
	}
	if reports[0].OTLPEnvelopesDeleted != 1 {
		t.Fatalf("dry run raw count %+v", reports)
	}
	rows, _ = QueryOTLP(db, "traces", QueryOptions{})
	if len(rows) != 2 {
		t.Fatal("dry run deleted data")
	}
	if _, err := Prune(db, time.Unix(4, 0), "a", false); err != nil {
		t.Fatal(err)
	}
	rows, _ = QueryOTLP(db, "traces", QueryOptions{})
	if len(rows) != 1 || rows[0].ServiceName != "b" {
		t.Fatal("service prune crossed resource boundary")
	}
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	rows, _ = QueryOTLP(db, "traces", QueryOptions{})
	if len(rows) != 1 {
		t.Fatal("ingest resurrected pruned envelope")
	}
}

func TestOTLPPartialAndProjectionUnsupported(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	payload := `{"resourceSpans":[{"scopeSpans":[{"spans":[{"startTimeUnixNano":"18446744073709551615","name":"future time"}]}]}]}`
	path := writeOTLPSignal(t, root, "traces", payload)
	report, err := IngestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Signals["traces"].OTLPEnvelopes != 0 {
		t.Fatal("partial envelope ingested")
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	report, err = IngestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Signals["traces"].OTLPEnvelopes != 1 || report.Signals["traces"].ProjectionSkippedLines != 1 {
		t.Fatalf("unsupported projection report %+v", report.Signals["traces"])
	}
	rows, err := QueryOTLP(db, "traces", QueryOptions{Since: time.Unix(1, 0)})
	if err != nil || len(rows) != 1 {
		t.Fatalf("uint64 timestamp not indexed %v %+v", err, rows)
	}
	assertSameJSON(t, []byte(payload), rows[0].Payload)
}

func TestOTLPV2UpgradePreservesDataAndPruneHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	payload := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"timeUnixNano":"1794700000000000001","body":{"stringValue":"old"}}]}]}]}` + "\n"
	source := writeOTLPSignal(t, root, "logs", payload)
	if _, err := db.Exec(`INSERT INTO metrics(metric_name,metric_type,timestamp,service_name,date) VALUES ('retained','gauge',CURRENT_TIMESTAMP,'old-service',CURRENT_DATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err = IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DELETE FROM logs; DROP TABLE otlp_envelopes; ALTER TABLE logs ALTER COLUMN flags TYPE INTEGER; UPDATE schema_meta SET value='2'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if SchemaRebuilt {
		t.Fatal("v2 upgrade rebuilt existing tables")
	}
	retained, err := QueryMetrics(db, QueryOptions{Service: "old-service"})
	if err != nil || len(retained) != 1 || retained[0].MetricName != "retained" {
		t.Fatalf("v2 data changed %v %+v", err, retained)
	}
	var offset int64
	if err = db.QueryRow(`SELECT byte_offset FROM ingest_state WHERE path=?`, source).Scan(&offset); err != nil || offset != int64(len(payload)) {
		t.Fatalf("cursor changed %d %v", offset, err)
	}
	report, err := IngestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Signals["logs"].OTLPEnvelopes != 0 || report.Signals["logs"].Records != 0 {
		t.Fatal("upgrade replayed old history")
	}
	file, err := os.OpenFile(source, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString(strings.ReplaceAll(payload, "old", "new"))
	file.Close()
	if _, err = IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	rows, err := QueryOTLP(db, "logs", QueryOptions{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("new raw data absent %v %+v", err, rows)
	}
	logs, err := QueryLogs(db, QueryOptions{})
	if err != nil || len(logs) != 1 || logs[0].Body != "new" {
		t.Fatalf("projection prune history lost %v %+v", err, logs)
	}
}

func TestOTLPV3UnusedCursorColumnDoesNotReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE ingest_state ADD COLUMN raw_byte_offset BIGINT DEFAULT 0`); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	payload := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"timeUnixNano":"1794700000000000001","body":{"stringValue":"old"}}]}]}]}` + "\n"
	source := writeOTLPSignal(t, root, "logs", payload)
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	report, err := IngestAll(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Signals["logs"].Records != 0 || report.Signals["logs"].OTLPEnvelopes != 0 {
		t.Fatal("unused legacy cursor replayed ingested history")
	}
	file, err := os.OpenFile(source, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(strings.ReplaceAll(payload, "old", "new")); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	rows, err := QueryOTLP(db, "logs", QueryOptions{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("legacy schema changed ingestion %v %+v", err, rows)
	}
	logs, err := QueryLogs(db, QueryOptions{})
	if err != nil || len(logs) != 2 {
		t.Fatalf("legacy schema changed projections %v %+v", err, logs)
	}
	bodies := map[string]bool{}
	for _, log := range logs {
		bodies[log.Body] = true
	}
	if !bodies["old"] || !bodies["new"] {
		t.Fatalf("legacy schema changed projected bodies %+v", logs)
	}
}

func TestOTLPQueryRejectsUnsupportedOptions(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, opts := range []QueryOptions{{Name: "name"}, {TraceID: "trace"}, {EventName: "event"}, {Attrs: []AttrFilter{{Key: "key", Value: "value"}}}, {Limit: -1}} {
		if _, err := QueryOTLP(db, "traces", opts); err == nil {
			t.Fatalf("accepted unsupported options %+v", opts)
		}
	}
	if _, err := QueryOTLP(db, "unknown", QueryOptions{}); err == nil {
		t.Fatal("accepted unknown signal")
	}
}

func TestOTLPQueryRejectsReversedTimeWindow(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	writeOTLPSignal(t, root, "traces", `{"resourceSpans":[{"scopeSpans":[{"spans":[{"startTimeUnixNano":"1000000000","endTimeUnixNano":"4000000000"}]}]}]}`+"\n")
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	rows, err := QueryOTLP(db, "traces", QueryOptions{Since: time.Unix(3, 0), Until: time.Unix(3, 0)})
	if err != nil || len(rows) != 1 {
		t.Fatalf("equal bounds did not select the spanning envelope: %v %+v", err, rows)
	}
	if _, err := QueryOTLP(db, "traces", QueryOptions{Since: time.Unix(3, 0), Until: time.Unix(2, 0)}); err == nil {
		t.Fatal("accepted reversed time window for a spanning envelope")
	}
}

func TestOTLPTransactionRollback(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	payload := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"timeUnixNano":"1794700000000000001","severityNumber":4294967295}]}]}]}`
	writeOTLPSignal(t, root, "logs", payload+"\n")
	if _, err := IngestAll(db, root); err == nil {
		t.Fatal("SQL projection failure did not abort")
	}
	rows, err := QueryOTLP(db, "logs", QueryOptions{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("failed transaction leaked raw rows %v %+v", err, rows)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM ingest_state WHERE path LIKE '%logs.jsonl'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed transaction advanced cursor %d %v", count, err)
	}
}

func TestOTLPProfileDictionaryServiceName(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	payload := `{"resourceProfiles":[{"resource":{"attributes":[{"keyStrindex":3,"value":{"stringValueStrindex":4}}]},"scopeProfiles":[{"profiles":[{"timeUnixNano":"1794700000000000001","samples":[{"stackIndex":0,"values":["1"]}]}]}]}],"dictionary":{"stringTable":["","cpu","nanoseconds","service.name","profile-service"]}}`
	writeOTLPSignal(t, root, "profiles", payload+"\n")
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	rows, err := QueryOTLP(db, "profiles", QueryOptions{Service: "profile-service"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("dictionary service unresolved %v %+v", err, rows)
	}
	assertSameJSON(t, []byte(payload), rows[0].Payload)
	for _, attr := range []string{`{"keyStrindex":-1,"value":{"stringValueStrindex":4}}`, `{"keyStrindex":99,"value":{"stringValueStrindex":4}}`, `{"keyStrindex":3,"value":{"stringValueStrindex":99}}`, `{"keyStrindex":3,"value":{"stringValueStrindex":-1}}`} {
		resource := json.RawMessage(`{"attributes":[` + attr + `]}`)
		if service := rawServiceName(resource, json.RawMessage(`{"stringTable":["","cpu","ns","service.name","service"]}`)); service != "unknown" {
			t.Fatalf("invalid dictionary index resolved %s", service)
		}
	}
}

func TestOTLPPruneAllIncludesFutureTimestamps(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	writeOTLPSignal(t, root, "profiles", `{"resourceProfiles":[{"scopeProfiles":[{"profiles":[{"timeUnixNano":"18446744073709551615"}]}]}]}`+"\n")
	if _, err := IngestAll(db, root); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(db, time.Now(), "", false); err != nil {
		t.Fatal(err)
	}
	rows, err := QueryOTLP(db, "profiles", QueryOptions{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("age prune removed future data %v %+v", err, rows)
	}
	if _, err := PruneAll(db, time.Now(), "", false); err != nil {
		t.Fatal(err)
	}
	rows, err = QueryOTLP(db, "profiles", QueryOptions{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("all prune retained future data %v %+v", err, rows)
	}
}
