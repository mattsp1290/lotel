package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/collector"
	"github.com/mattsp1290/lotel/internal/storage"
	"github.com/mattsp1290/lotel/internal/upgrade"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func outputSchemaTypes() map[string]any {
	return map[string]any{
		"commands": commandCatalog{}, "status": collector.Status{}, "ingest": storage.IngestReport{},
		"prune": []storage.PruneReport{}, "setup": setupReport{}, "upgrade": upgrade.Report{},
		"diagnose runtime": collector.RuntimeDiagnosis{}, "diagnose signals": storage.SignalDiagnosis{},
		"assert signal": signalAssertionResult{}, "query traces": []storage.TraceResult{},
		"query logs": []storage.LogResult{}, "query metrics": []storage.MetricResult{},
		"query aggregate": storage.MetricAggregation{}, "query trace": traceBundle{},
		"query services": []storage.ServiceResult{}, "query operations": []storage.OperationResult{},
		"query dependencies": storage.DependencyReport{}, "query span-stats": []storage.SpanStats{},
		"query log-stats": []storage.LogStats{}, "query genai calls": []storage.GenAICall{},
		"query genai usage": []storage.GenAIUsageRow{}, "query otlp": []storage.OTLPResult{},
		"query recent traces": recentResult{}, "query recent logs": recentResult{},
		"query metric-difference": storage.MetricDifference{}, "query metric-delta-total": storage.MetricDeltaTotal{},
		"output-schema --list":              outputSchemaCatalog{},
		"query metric-delta-histogram":      storage.MetricHistogram{},
		"query metric-cumulative-histogram": storage.MetricHistogram{},
	}
}

var normalizedOutputCollections = map[string]bool{
	"main.commandCatalog.Commands":             true,
	"main.catalogCommand.Flags":                true,
	"main.outputSchemaCatalog.Outputs":         true,
	"main.setupReport.Candidates":              true,
	"main.setupReport.Skipped":                 true,
	"main.setupCandidate.Manifests":            true,
	"main.traceBundle.Spans":                   true,
	"main.traceBundle.Logs":                    true,
	"main.traceRelationships.SpanParents":      true,
	"main.traceRelationships.LogSpans":         true,
	"main.traceRelationships.CycleSpanIndices": true,
	"storage.DependencyReport.Edges":           true,
	"storage.ServiceResult.Signals":            true,
	"storage.SignalDiagnosis.Signals":          true,
	"storage.IngestReport.Signals":             true,
	"storage.MetricDifference.Assumptions":     true,
	"storage.MetricDeltaTotal.Assumptions":     true,
	"storage.MetricHistogram.Bounds":           true,
	"storage.MetricHistogram.BucketCounts":     true,
	"storage.MetricHistogram.Assumptions":      true,
}

func schemaType(t reflect.Type, nullable bool) map[string]any {
	if t == reflect.TypeOf(time.Time{}) {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t == reflect.TypeOf(json.RawMessage{}) {
		return map[string]any{}
	}
	if t.Kind() == reflect.Pointer {
		inner := schemaType(t.Elem(), false)
		if nullable {
			return map[string]any{"anyOf": []any{inner, map[string]any{"type": "null"}}}
		}
		return inner
	}
	kind := ""
	schema := map[string]any{}
	switch t.Kind() {
	case reflect.Interface:
		return schema
	case reflect.String:
		kind = "string"
	case reflect.Bool:
		kind = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		kind = "integer"
	case reflect.Float32, reflect.Float64:
		kind = "number"
	case reflect.Slice, reflect.Array:
		kind = "array"
		schema["items"] = schemaType(t.Elem(), true)
	case reflect.Map:
		kind = "object"
		schema["additionalProperties"] = schemaType(t.Elem(), true)
	case reflect.Struct:
		kind = "object"
		properties := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			name := tag[0]
			if name == "" {
				name = field.Name
			}
			optional := false
			for _, option := range tag[1:] {
				if option == "omitempty" {
					optional = true
				}
			}
			properties[name] = schemaType(field.Type, !optional)
			if normalizedOutputCollections[t.String()+"."+field.Name] {
				properties[name] = schemaType(field.Type, false)
			}
			if t == reflect.TypeOf(storage.MetricResult{}) && field.Name == "ExplicitBounds" {
				properties[name].(map[string]any)["items"] = projectedMetricDoubleSchema()
			}
			if t == reflect.TypeOf(storage.Quantile{}) {
				properties[name] = projectedMetricDoubleSchema()
			}
			if t == reflect.TypeOf(storage.IngestReport{}) && field.Name == "Signals" {
				properties[name].(map[string]any)["additionalProperties"] = schemaType(reflect.TypeOf(storage.SignalReport{}), false)
			}
			if !optional {
				required = append(required, name)
			}
		}
		schema["properties"] = properties
		if len(required) > 0 {
			schema["required"] = required
		}
	default:
		panic("unsupported schema type " + t.String())
	}
	if nullable && (t.Kind() == reflect.Slice || t.Kind() == reflect.Map) {
		schema["type"] = []string{kind, "null"}
	} else {
		schema["type"] = kind
	}
	return schema
}

func projectedMetricDoubleSchema() map[string]any {
	return map[string]any{"anyOf": []any{map[string]any{"type": "number"}, map[string]any{"enum": []string{"NaN", "Infinity", "-Infinity"}}}}
}

func generatedOutputSchema(command string, value any) map[string]any {
	schema := schemaType(reflect.TypeOf(value), false)
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["$id"] = "urn:lotel:output:" + strings.ReplaceAll(command, " ", ":") + ":1"
	schema["title"] = "lotel " + command + " stdout, version 1"
	if command == "query otlp" {
		schema["items"].(map[string]any)["properties"].(map[string]any)["payload"] = map[string]any{"type": "object"}
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		if version, ok := properties["schema_version"].(map[string]any); ok {
			version["const"] = 1
		}
	}
	if strings.HasPrefix(command, "query metric-") {
		properties := schema["properties"].(map[string]any)
		for _, name := range []string{"from_unix_nano", "to_unix_nano", "elapsed_nanoseconds"} {
			if field, ok := properties[name].(map[string]any); ok {
				field["pattern"] = "^[1-9][0-9]*$"
			}
		}
		for _, name := range []string{"count", "total", "difference"} {
			if field, ok := properties[name].(map[string]any); ok {
				field["pattern"] = "^(0|[1-9][0-9]*)$"
			}
		}
		if field, ok := properties["rate_per_second"].(map[string]any); ok {
			field["pattern"] = "^(0|[1-9][0-9]*)(/[1-9][0-9]*)?$"
		}
		if field, ok := properties["point_count"].(map[string]any); ok {
			field["minimum"] = 1
		}
		if strings.HasSuffix(command, "histogram") {
			basis := "contiguous_delta_bucket_counts"
			if command == "query metric-cumulative-histogram" {
				basis = "cumulative_bucket_count_difference"
			}
			properties["basis"].(map[string]any)["const"] = basis
			properties["validation_scope"].(map[string]any)["const"] = "Reported counts, explicit bounds and bucket counts only. Sum, min and max are ignored."
			buckets := properties["bucket_counts"].(map[string]any)
			buckets["minItems"] = 1
			buckets["items"].(map[string]any)["pattern"] = "^(0|[1-9][0-9]*)$"
		}
	}
	if strings.HasPrefix(command, "query recent ") {
		props := schema["properties"].(map[string]any)
		signal := strings.TrimPrefix(command, "query recent ")
		props["signal"] = map[string]any{"const": signal}
		var records any = []storage.TraceResult{}
		if signal == "logs" {
			records = []storage.LogResult{}
		}
		item := schemaType(reflect.TypeOf(records), false)
		item["maxItems"] = storage.MaxRecentLimit
		props["records"] = item
	}
	return schema
}

func TestRegenerateOutputSchemas(t *testing.T) {
	if os.Getenv("LOTEL_UPDATE_OUTPUT_SCHEMAS") != "1" {
		t.Skip("explicit schema regeneration only")
	}
	for command, value := range outputSchemaTypes() {
		contract, _ := outputContractFor(command)
		data, err := json.MarshalIndent(generatedOutputSchema(command, value), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("schemas", contract.file), append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func compileOutputSchema(t *testing.T, command string) *jsonschema.Schema {
	t.Helper()
	contract, ok := outputContractFor(command)
	if !ok {
		t.Fatalf("missing contract for %q", command)
	}
	data, err := outputSchemaFiles.ReadFile("schemas/" + contract.file)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource("urn:test:output", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("urn:test:output")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func validateOutput(t *testing.T, command string, data []byte) {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("%s decode %s: %v", command, data, err)
	}
	if err := compileOutputSchema(t, command).Validate(value); err != nil {
		t.Fatalf("%s output %s: %v", command, data, err)
	}
}

func schemaCLI(t *testing.T, home string, args ...string) ([]byte, []byte, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestCLIHelper$", "--"}, args...)...)
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "LOTEL_CLI_HELPER=1", "HOME="+home, "USERPROFILE="+home, "PATH="+bin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	return stdout, stderr.Bytes(), err
}

func TestOutputContractInventory(t *testing.T) {
	home := t.TempDir()
	data, stderr, err := schemaCLI(t, home, "commands")
	if err != nil {
		t.Fatalf("commands: %v %s", err, stderr)
	}
	validateOutput(t, "commands", data)
	var catalog commandCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{"output-schema --list": true}
	for _, command := range catalog.Commands {
		path := strings.TrimPrefix(command.Path, "lotel ")
		if _, ok := outputContractFor(path); !ok {
			t.Errorf("public command %q has no output contract", path)
		}
		seen[path] = true
	}
	for command := range outputContracts {
		if !seen[command] {
			t.Errorf("output contract for unregistered command %q", command)
		}
	}
	for command := range outputContracts {
		contract, _ := outputContractFor(command)
		if contract.Format != "json" {
			continue
		}
		if contract.SchemaID == "" {
			t.Errorf("missing schema ID for %q", command)
		}
		compileOutputSchema(t, command)
	}
	data, stderr, err = schemaCLI(t, home, "output-schema", "--list")
	if err != nil {
		t.Fatalf("list: %v %s", err, stderr)
	}
	validateOutput(t, "output-schema --list", data)
	if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
		t.Fatalf("discovery touched telemetry state: %v", err)
	}
}

func serializationFixture(typ reflect.Type) reflect.Value {
	if typ == reflect.TypeOf(time.Time{}) {
		return reflect.ValueOf(time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC))
	}
	if typ == reflect.TypeOf(storage.Quantile{}) {
		return reflect.ValueOf(storage.Quantile{Quantile: 0.95, Value: "Infinity"})
	}
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return reflect.ValueOf(json.RawMessage(`{"nested":[true,null,1,"text"]}`))
	}
	value := reflect.New(typ).Elem()
	switch typ.Kind() {
	case reflect.Pointer:
		value.Set(reflect.New(typ.Elem()))
		value.Elem().Set(serializationFixture(typ.Elem()))
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			if typ.Field(i).IsExported() {
				value.Field(i).Set(serializationFixture(typ.Field(i).Type))
				if typ == reflect.TypeOf(storage.MetricResult{}) && typ.Field(i).Name == "ExplicitBounds" {
					value.Field(i).Set(reflect.ValueOf([]any{1.5, "Infinity"}))
				}
			}
		}
	case reflect.Slice:
		value.Set(reflect.MakeSlice(typ, 1, 1))
		value.Index(0).Set(serializationFixture(typ.Elem()))
	case reflect.Map:
		value.Set(reflect.MakeMap(typ))
		value.SetMapIndex(reflect.ValueOf("custom.attribute"), serializationFixture(typ.Elem()))
	case reflect.Interface:
		value.Set(reflect.ValueOf(map[string]any{"nested": []any{nil, true, float64(1), "text"}}))
	case reflect.String:
		value.SetString("123")
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(1)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(1.5)
	default:
		panic("unsupported fixture type " + typ.String())
	}
	return value
}

func outputSerializationFixture(command string, prototype any) any {
	value := serializationFixture(reflect.TypeOf(prototype)).Interface()
	if strings.HasPrefix(command, "query recent ") {
		result := value.(recentResult)
		result.Signal = strings.TrimPrefix(command, "query recent ")
		if result.Signal == "traces" {
			result.Records = serializationFixture(reflect.TypeOf([]storage.TraceResult{})).Interface()
		} else {
			result.Records = serializationFixture(reflect.TypeOf([]storage.LogResult{})).Interface()
		}
		value = result
	}
	if strings.HasSuffix(command, "histogram") {
		result := value.(storage.MetricHistogram)
		result.Basis = "contiguous_delta_bucket_counts"
		if command == "query metric-cumulative-histogram" {
			result.Basis = "cumulative_bucket_count_difference"
		}
		result.ValidationScope = "Reported counts, explicit bounds and bucket counts only. Sum, min and max are ignored."
		value = result
	}
	return value
}

func TestOutputSchemasValidateSerialization(t *testing.T) {
	for command, prototype := range outputSchemaTypes() {
		t.Run(command, func(t *testing.T) {
			value := outputSerializationFixture(command, prototype)
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			validateOutput(t, command, data)
			var decoded any
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if records, ok := decoded.([]any); ok {
				decoded = records[0]
			}
			object := decoded.(map[string]any)
			var schema map[string]any
			contract, _ := outputContractFor(command)
			schemaData, _ := outputSchemaFiles.ReadFile("schemas/" + contract.file)
			if err := json.Unmarshal(schemaData, &schema); err != nil {
				t.Fatal(err)
			}
			if schema["type"] == "array" {
				schema = schema["items"].(map[string]any)
			}
			for _, required := range schema["required"].([]any) {
				name := required.(string)
				original := object[name]
				delete(object, name)
				var invalid any = object
				if reflect.TypeOf(prototype).Kind() == reflect.Slice {
					invalid = []any{object}
				}
				if err := compileOutputSchema(t, command).Validate(invalid); err == nil {
					t.Errorf("accepted missing required field %q", name)
				}
				object[name] = original
			}
		})
	}
}

func TestOutputSchemasValidateCLI(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".lotel", "data", "lotel.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	queries := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO traces(trace_id,span_id,name,kind,start_time,end_time,duration_ns,status_code,service_name,events,links,attributes,date) VALUES (?,?,?,1,?,?,42,0,'app',?,?,?,'2025-01-02')`, []any{strings.Repeat("1", 32), strings.Repeat("2", 16), "request", at, at, `[{"time":"2025-01-02T12:00:00Z","name":"event","attributes":null}]`, `[{"trace_id":"trace","span_id":"span","trace_state":"","attributes":null}]`, `{"array":[null,1,{"nested":true}],"gen_ai.operation.name":"chat","gen_ai.provider.name":"test","gen_ai.request.model":"model","gen_ai.usage.input_tokens":4,"gen_ai.input.messages":[{"role":"user","parts":[{"type":"text","content":"hi"}]}]}`}},
		{`INSERT INTO logs(timestamp,severity_number,body,body_json,service_name,attributes,date) VALUES (?,9,?,?,'app',?,'2025-01-02')`, []any{at, "log", `{"nested":[true,null]}`, `{"array":[1,"a"]}`}},
	}
	for _, q := range queries {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		command string
		flags   []string
		fails   bool
	}{
		{"query traces", nil, false}, {"query logs", nil, false}, {"query metrics", nil, false},
		{"query aggregate", []string{"--metric", "missing"}, false},
		{"query trace", []string{strings.Repeat("1", 32), "--relationships"}, false},
		{"query services", nil, false}, {"query operations", nil, false},
		{"query dependencies", []string{"--since", "2025-01-01T00:00:00Z"}, false},
		{"query span-stats", []string{"--since", "2025-01-01T00:00:00Z"}, false},
		{"query log-stats", []string{"--since", "2025-01-01T00:00:00Z"}, false},
		{"query genai calls", []string{"--content"}, false}, {"query genai usage", nil, false},
		{"query otlp", []string{"--signal", "traces"}, false}, {"query profiles", nil, false},
		{"query recent traces", []string{"--since", "2025-01-01T00:00:00Z", "--limit", "1"}, false},
		{"query recent logs", []string{"--since", "2025-01-01T00:00:00Z", "--limit", "1"}, false},
		{"ingest", nil, false}, {"prune", []string{"--all", "--dry-run"}, false},
		{"setup", []string{"--recommend-only", "--path", home}, false},
		{"diagnose runtime", nil, false},

		{"status", nil, true},
		{"assert signal", []string{"--signal", "traces", "--service", "app", "--since", "2025-01-01T00:00:00Z"}, false},
		{"assert signal", []string{"--signal", "traces", "--service", "missing", "--since", "2025-01-01T00:00:00Z"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.command+strings.Join(tc.flags, " "), func(t *testing.T) {
			data, stderr, err := schemaCLI(t, home, append(strings.Fields(tc.command), tc.flags...)...)
			if tc.command == "setup" && runtime.GOOS == "windows" {
				if err == nil || len(data) != 0 || len(stderr) == 0 {
					t.Fatalf("expected unsupported-platform text error: %v %s %s", err, data, stderr)
				}
				return
			}
			if (err != nil) != tc.fails {
				t.Fatalf("exit error %v, expected failure %t: %s", err, tc.fails, stderr)
			}
			validateOutput(t, tc.command, data)
			if tc.command == "query genai calls" || tc.command == "query genai usage" {
				var records []any
				if err := json.Unmarshal(data, &records); err != nil || len(records) == 0 {
					t.Fatalf("expected populated GenAI output: %s %v", data, err)
				}
			}
		})
	}
}

func TestOutputSchemaDiscoveryAndErrors(t *testing.T) {
	home := t.TempDir()
	for command := range outputContracts {
		contract, _ := outputContractFor(command)
		if contract.Format != "json" {
			continue
		}
		data, stderr, err := schemaCLI(t, home, append([]string{"output-schema", "--"}, strings.Fields(command)...)...)
		if err != nil {
			t.Fatalf("discover %q: %v %s", command, err, stderr)
		}
		validateOutput(t, "output-schema", data)
	}
	for _, args := range [][]string{
		{"output-schema", "health"}, {"output-schema", "unknown"},
		{"output-schema", "--schema-version", "2", "status"}, {"output-schema", "--list", "status"},
		{"query", "recent", "logs", "--since", "wrong", "--limit", "1"},
		{"query", "otlp"}, {"query", "logs", "--unknown"}, {"unknown"},
	} {
		stdout, stderr, err := schemaCLI(t, home, args...)
		if err == nil || len(stdout) != 0 || !bytes.Contains(stderr, []byte("Error:")) || json.Valid(stderr) {
			t.Errorf("incorrect text error contract for %v: %v, stdout %q, stderr %q", args, err, stdout, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".lotel")); !os.IsNotExist(err) {
		t.Fatalf("discovery/validation touched telemetry state: %v", err)
	}
}

func TestOutputSchemaTypeCoverage(t *testing.T) {
	for command, prototype := range outputSchemaTypes() {
		t.Run(command, func(t *testing.T) {
			contract, _ := outputContractFor(command)
			actual, err := outputSchemaFiles.ReadFile("schemas/" + contract.file)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := json.MarshalIndent(generatedOutputSchema(command, prototype), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(bytes.TrimSpace(actual), expected) {
				t.Fatal("output type changed without updating its published schema; review compatibility before regenerating")
			}
		})
	}
}

func TestDynamicOutputJSONValues(t *testing.T) {
	for _, value := range []any{nil, true, 1.5, "text", []any{nil, 1, "a"}, map[string]any{"nested": []any{true, nil}}} {
		for command, output := range map[string]any{
			"query logs":        []storage.LogResult{{BodyValue: value, Attributes: map[string]any{"value": value}}},
			"query genai calls": []storage.GenAICall{{InputMessages: value, OutputMessages: value, ToolCallResult: value}},
		} {
			data, err := json.Marshal(output)
			if err != nil {
				t.Fatal(err)
			}
			validateOutput(t, command, data)
		}
	}
}

func TestOutputSchemasRejectNullNormalizedCollections(t *testing.T) {
	cases := []struct{ command, path string }{
		{"commands", "commands"}, {"commands", "commands.0.flags"},
		{"setup", "candidates"}, {"setup", "skipped"}, {"setup", "candidates.0.manifests"},
		{"query trace", "spans"}, {"query trace", "logs"},
		{"query trace", "relationships.span_parents"}, {"query trace", "relationships.log_spans"}, {"query trace", "relationships.cycle_span_indices"},
		{"query dependencies", "edges"}, {"query services", "0.signals"},
		{"diagnose signals", "signals"}, {"ingest", "signals"},
		{"query metric-difference", "assumptions"}, {"query metric-delta-total", "assumptions"},
		{"query metric-delta-histogram", "bounds"}, {"query metric-delta-histogram", "bucket_counts"}, {"query metric-delta-histogram", "assumptions"},
		{"query metric-cumulative-histogram", "bounds"}, {"query metric-cumulative-histogram", "bucket_counts"},
	}
	for _, tc := range cases {
		t.Run(tc.command+"/"+tc.path, func(t *testing.T) {
			prototype := outputSchemaTypes()[tc.command]
			data, err := json.Marshal(outputSerializationFixture(tc.command, prototype))
			if err != nil {
				t.Fatal(err)
			}
			var output any
			if err := json.Unmarshal(data, &output); err != nil {
				t.Fatal(err)
			}
			cursor := output
			parts := strings.Split(tc.path, ".")
			for _, part := range parts[:len(parts)-1] {
				if array, ok := cursor.([]any); ok {
					if part != "0" {
						t.Fatal("invalid fixture path")
					}
					cursor = array[0]
				} else {
					cursor = cursor.(map[string]any)[part]
				}
			}
			cursor.(map[string]any)[parts[len(parts)-1]] = nil
			if err := compileOutputSchema(t, tc.command).Validate(output); err == nil {
				t.Fatal("accepted null normalized collection")
			}
		})
	}
	data, err := json.Marshal([]storage.TraceResult{{Events: []storage.SpanEvent{{Attributes: nil}}, Links: []storage.SpanLink{{Attributes: nil}}}})
	if err != nil {
		t.Fatal(err)
	}
	validateOutput(t, "query traces", data)
	data, err = json.Marshal([]storage.SpanStats{{Kind: nil, P50DurationNs: nil, P95DurationNs: nil}})
	if err != nil {
		t.Fatal(err)
	}
	validateOutput(t, "query span-stats", data)
}

func TestOTLPOutputPayloadObject(t *testing.T) {
	for _, command := range []string{"query otlp", "query profiles"} {
		for _, payload := range []string{`null`, `true`, `1`, `"text"`, `[]`} {
			data, err := json.Marshal([]storage.OTLPResult{{Payload: json.RawMessage(payload)}})
			if err != nil {
				t.Fatal(err)
			}
			var output any
			if err := json.Unmarshal(data, &output); err != nil {
				t.Fatal(err)
			}
			if err := compileOutputSchema(t, command).Validate(output); err == nil {
				t.Fatalf("%s accepted non-object payload %s", command, payload)
			}
		}
		data, err := json.Marshal([]storage.OTLPResult{{Payload: json.RawMessage(`{"future":[null,{"dynamic":true},1,"text"]}`)}})
		if err != nil {
			t.Fatal(err)
		}
		validateOutput(t, command, data)
	}
}

func TestMetricOutputSchemaValues(t *testing.T) {
	commands := []string{"query metric-difference", "query metric-delta-total", "query metric-delta-histogram", "query metric-cumulative-histogram"}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			data, err := json.Marshal(outputSerializationFixture(command, outputSchemaTypes()[command]))
			if err != nil {
				t.Fatal(err)
			}
			var output map[string]any
			if err := json.Unmarshal(data, &output); err != nil {
				t.Fatal(err)
			}
			schema := compileOutputSchema(t, command)
			for name, original := range output {
				var invalid []any
				switch name {
				case "from_unix_nano", "to_unix_nano", "elapsed_nanoseconds":
					invalid = []any{"0", "-1", "NaN", "abc", "1.5", "1/2", 123}
				case "count", "total", "difference":
					invalid = []any{"-1", "NaN", "abc", "1.5", "1/2", 123}
				case "rate_per_second":
					invalid = []any{"-1", "NaN", "Infinity", "abc", "1.5", "1/0", "1/-2", 123}
				case "bucket_counts":
					invalid = []any{[]any{}, []any{"-1"}, []any{"NaN"}, []any{"abc"}, []any{"1/2"}, []any{123}}
				case "basis", "validation_scope":
					invalid = []any{"wrong"}
				case "point_count":
					invalid = []any{0, -1, 1.5}
				}
				for _, value := range invalid {
					output[name] = value
					if err := schema.Validate(output); err == nil {
						t.Errorf("accepted invalid %s=%v", name, value)
					}
				}
				output[name] = original
			}
			if _, ok := output["rate_per_second"]; ok {
				for _, rate := range []string{"0", "2", "7/3", "9007199254740993/2"} {
					output["rate_per_second"] = rate
					if err := schema.Validate(output); err != nil {
						t.Errorf("rejected valid rate %q: %v", rate, err)
					}
				}
			}
		})
	}
}

func TestProjectedMetricDoubleOutputSchema(t *testing.T) {
	schema := compileOutputSchema(t, "query metrics")
	for _, field := range []string{"explicit_bounds", "quantiles"} {
		for _, value := range []any{nil, true, "garbage", map[string]any{"nested": 1}, []any{1}} {
			var optional any = []any{value}
			if field == "quantiles" {
				optional = []any{map[string]any{"quantile": 0.95, "value": value}}
			}
			data, _ := json.Marshal([]storage.MetricResult{{}})
			var output []any
			if err := json.Unmarshal(data, &output); err != nil {
				t.Fatal(err)
			}
			output[0].(map[string]any)[field] = optional
			if err := schema.Validate(output); err == nil {
				t.Errorf("accepted invalid %s value %v", field, value)
			}
		}
	}
}
