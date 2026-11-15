package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestOutputSchemasValidatePageCLI(t *testing.T) {
	home := t.TempDir()
	run := func(args ...string) ([]byte, error) {
		t.Helper()
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestCLIHelper$", "--"}, args...)...)
		cmd.Env = append(os.Environ(), "LOTEL_CLI_HELPER=1", "HOME="+home, "USERPROFILE="+home)
		return cmd.Output()
	}
	decode := func(data []byte) map[string]any {
		t.Helper()
		var v map[string]any
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatalf("decode %s: %v", data, err)
		}
		return v
	}
	schemas := map[string]*jsonschema.Schema{}
	for _, signal := range []string{"traces", "logs"} {
		data, err := run("output-schema", "--schema-version", "1", "query", "page", signal)
		if err != nil {
			t.Fatal(err)
		}
		compiler := jsonschema.NewCompiler()
		compiler.AssertFormat()
		if err := compiler.AddResource("urn:test:"+signal, decode(data)); err != nil {
			t.Fatal(err)
		}
		schemas[signal], err = compiler.Compile("urn:test:" + signal)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
		t.Fatalf("schema discovery touched telemetry state: %v", err)
	}
	for _, args := range [][]string{{"output-schema", "query", "traces"}, {"output-schema", "--schema-version", "2", "query", "page", "logs"}, {"output-schema", "query", "page", "logs", "extra"}} {
		if _, err := run(args...); err == nil {
			t.Fatalf("expected error for %q", args)
		}
	}
	path := filepath.Join(home, ".lotel", "data", "lotel.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b"} {
		_, err = db.Exec(`INSERT INTO traces(trace_id,span_id,name,kind,start_time,end_time,duration_ns,status_code,service_name,events,links,attributes,date) VALUES (?,?,?,1,?,?,42,0,'app',?, ?, ?, '2025-01-02')`, id, id, id, at, at, `[{"time":"2025-01-02T12:00:00Z","name":"event","attributes":null}]`, `[{"trace_id":"trace","span_id":"span","trace_state":"","attributes":null}]`, `{"array":[null,1,{"nested":true}]}`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`INSERT INTO logs(timestamp,severity_number,body,body_json,service_name,attributes,date) VALUES (?,9,?,?,'app',?,'2025-01-02')`, at, id, `{"nested":[true,null]}`, `{"array":[1,"a"]}`)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, signal := range []string{"traces", "logs"} {
		t.Run(signal, func(t *testing.T) {
			page := func(args ...string) map[string]any {
				t.Helper()
				out, err := run(append([]string{"query", "page", signal}, args...)...)
				if err != nil {
					t.Fatalf("page command: %v", err)
				}
				v := decode(out)
				if err := schemas[signal].Validate(v); err != nil {
					t.Fatalf("invalid page %s: %v", out, err)
				}
				return v
			}
			firstArgs := []string{"--since", "2025-01-02T11:00:00Z", "--until", "2025-01-02T13:00:00Z", "--limit", "1"}
			empty := page(append(firstArgs, "--service", "missing")...)
			if len(empty["records"].([]any)) != 0 {
				t.Fatal("expected empty page")
			}
			first := page(firstArgs...)
			cursor, ok := first["next_cursor"].(string)
			if !ok || cursor == "" {
				t.Fatalf("missing cursor: %v", first)
			}
			last := page("--cursor", cursor)
			if _, ok := last["next_cursor"]; ok {
				t.Fatal("unexpected cursor on last page")
			}
			for _, mutate := range []func(map[string]any){
				func(v map[string]any) { delete(v, "records") },
				func(v map[string]any) { v["records"] = nil },
				func(v map[string]any) { v["signal"] = "wrong" },
				func(v map[string]any) { v["next_cursor"] = 1 },
				func(v map[string]any) { v["since"] = "not-a-date" },
				func(v map[string]any) { v["until"] = "2025-02-30T00:00:00Z" },
				func(v map[string]any) {
					records := v["records"].([]any)
					for len(records) <= storage.MaxRecentLimit {
						records = append(records, records[0])
					}
					v["records"] = records
				},
				func(v map[string]any) { v["records"].([]any)[0].(map[string]any)["service_name"] = 1 },
				func(v map[string]any) { delete(v["records"].([]any)[0].(map[string]any), "service_name") },
			} {
				copyData, _ := json.Marshal(first)
				invalid := decode(copyData)
				mutate(invalid)
				if err := schemas[signal].Validate(invalid); err == nil {
					t.Fatalf("accepted invalid page: %v", invalid)
				}
			}
			copyData, _ := json.Marshal(first)
			boundary := decode(copyData)
			records := boundary["records"].([]any)
			for len(records) < storage.MaxRecentLimit {
				records = append(records, records[0])
			}
			boundary["records"] = records
			boundary["since"] = "0000-01-01T00:00:00Z"
			if err := schemas[signal].Validate(boundary); err != nil {
				t.Fatalf("valid boundary rejected: %v", err)
			}
			for _, field := range map[string][]string{"traces": {"start_time", "end_time"}, "logs": {"timestamp", "observed_timestamp"}}[signal] {
				copyData, _ := json.Marshal(first)
				invalid := decode(copyData)
				invalid["records"].([]any)[0].(map[string]any)[field] = "not-a-date"
				if err := schemas[signal].Validate(invalid); err == nil {
					t.Fatalf("accepted invalid %s", field)
				}
			}
			if signal == "traces" {
				copyData, _ := json.Marshal(first)
				invalid := decode(copyData)
				record := invalid["records"].([]any)[0].(map[string]any)
				record["events"].([]any)[0].(map[string]any)["attributes"] = []any{1}
				if err := schemas[signal].Validate(invalid); err == nil {
					t.Fatal("accepted invalid event attributes")
				}
				copyData, _ = json.Marshal(first)
				invalid = decode(copyData)
				record = invalid["records"].([]any)[0].(map[string]any)
				record["events"].([]any)[0].(map[string]any)["time"] = "not-a-date"
				if err := schemas[signal].Validate(invalid); err == nil {
					t.Fatal("accepted invalid event time")
				}
			}
		})
	}
}
