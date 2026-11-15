package collector_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattsp1290/lotel/internal/config"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNativeCLI(t *testing.T) {
	bin := os.Getenv("LOTEL_NATIVE_TEST_BIN")
	if bin == "" {
		t.Skip("set LOTEL_NATIVE_TEST_BIN to a built lotel executable")
	}
	bin, err := filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	home, cwd := t.TempDir(), t.TempDir()
	ports := make([]string, 3)
	listeners := make([]net.Listener, 3)
	for i := range ports {
		listeners[i], err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ports[i] = listeners[i].Addr().String()
	}
	text := strings.ReplaceAll(config.DefaultConfig, "0.0.0.0:4317", ports[0])
	text = strings.ReplaceAll(text, "0.0.0.0:4318", ports[1])
	text = strings.ReplaceAll(text, "0.0.0.0:13133", ports[2])
	text = strings.ReplaceAll(text, "timeout: 1s", "timeout: 100ms")
	text = strings.ReplaceAll(text, "metrics_flush_interval: 5s", "metrics_flush_interval: 200ms")
	text += "\ningestion:\n  enabled: true\n  interval: 150ms\n"
	configPath := filepath.Join(cwd, "lotel-collector.yaml")
	if err := os.WriteFile(configPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, listener := range listeners {
		listener.Close()
	}
	runAt := func(root string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "HOME="+root, "PATH="+filepath.Join(root, "no-executables"))
		out, err := cmd.CombinedOutput()
		return out, err
	}
	run := func(args ...string) []byte {
		t.Helper()
		out, err := runAt(home, args...)
		if err != nil {
			log, _ := os.ReadFile(filepath.Join(home, ".lotel", "collector.log"))
			t.Fatalf("lotel %v: %v\n%s\nCollector log:\n%s", args, err, out, log)
		}
		return out
	}
	defer func() {
		if out, err := runAt(home, "stop"); err != nil {
			t.Errorf("cleanup stop: %v\n%s", err, out)
		}
	}()
	if out, err := runAt(t.TempDir(), "query", "traces"); err != nil || strings.TrimSpace(string(out)) != "[]" {
		t.Fatalf("query on a fresh home: %v, %s", err, out)
	}
	run("start", "--backend", "native", "--wait")
	status := run("status")
	var before map[string]any
	if err := json.Unmarshal(status, &before); err != nil || before["running"] != true || before["healthy"] != true {
		t.Fatalf("native status: %s (%v)", status, err)
	}
	run("start", "--backend", "native", "--wait")
	var after map[string]any
	if err := json.Unmarshal(run("status"), &after); err != nil {
		t.Fatal(err)
	}
	if before["pid"] != after["pid"] || before["pid"] == nil {
		t.Fatalf("repeated start changed process: %v -> %v", before, after)
	}
	run("health")
	otherHome := t.TempDir()
	if out, err := runAt(otherHome, "start", "--backend", "native", "--wait"); err == nil {
		runAt(otherHome, "stop")
		t.Fatalf("occupied ports accepted another collector's health: %s", out)
	}
	if out, err := runAt(otherHome, "status"); err == nil {
		t.Fatalf("failed startup left a running instance: %s", out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(ports[0], grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	service := "lotel-native-cli-test"
	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", service)
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("chat native-model")
	span.SetTraceID(pcommon.TraceID{1, 2, 3})
	span.SetSpanID(pcommon.SpanID{1, 2, 3})
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(10 * time.Millisecond)))
	span.Attributes().PutStr("gen_ai.operation.name", "chat")
	span.Attributes().PutStr("gen_ai.provider.name", "openai")
	span.Attributes().PutStr("gen_ai.request.model", "native-model")
	span.Attributes().PutInt("gen_ai.usage.input_tokens", 5)
	span.Attributes().PutInt("gen_ai.usage.output_tokens", 3)
	req := ptraceotlp.NewExportRequestFromTraces(traces)
	jsonBody, err := req.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	post := func(body []byte, contentType string) {
		t.Helper()
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Post("http://"+ports[1]+"/v1/traces", contentType, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("HTTP OTLP returned %s", resp.Status)
		}
	}
	post(jsonBody, "application/json")
	span.SetSpanID(pcommon.SpanID{4, 5, 6})
	protoBody, err := req.MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	post(protoBody, "application/x-protobuf")
	span.SetSpanID(pcommon.SpanID{7, 8, 9})
	span.Attributes().Clear()
	span.Attributes().PutStr("openinference.span.kind", "LLM")
	span.Attributes().PutStr("llm.provider", "openai")
	span.Attributes().PutStr("llm.model_name", "native-model")
	span.Attributes().PutInt("llm.token_count.prompt", 5)
	span.Attributes().PutInt("llm.token_count.completion", 3)
	if _, err := ptraceotlp.NewGRPCClient(conn).Export(ctx, req); err != nil {
		t.Fatal(err)
	}
	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", service)
	metric := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	metric.SetName("native.test.count")
	point := metric.SetEmptyGauge().DataPoints().AppendEmpty()
	point.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	point.SetIntValue(42)
	if _, err := pmetricotlp.NewGRPCClient(conn).Export(ctx, pmetricotlp.NewExportRequestFromMetrics(metrics)); err != nil {
		t.Fatal(err)
	}
	logs := plog.NewLogs()
	rl := logs.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", service)
	record := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	record.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	record.Body().SetStr("native log")
	if _, err := plogotlp.NewGRPCClient(conn).Export(ctx, plogotlp.NewExportRequestFromLogs(logs)); err != nil {
		t.Fatal(err)
	}
	query := func(kind string) ([]json.RawMessage, error) {
		out, err := runAt(home, "query", kind, "--service", service, "--since", "1m")
		if err != nil {
			return nil, fmt.Errorf("query %s: %w: %s", kind, err, out)
		}
		var rows []json.RawMessage
		err = json.Unmarshal(out, &rows)
		return rows, err
	}
	waitCount := func(kind string, count int) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for {
			rows, err := query(kind)
			if err == nil && len(rows) == count {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("automatic %s ingestion: count %d, want %d, error %v", kind, len(rows), count, err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	waitCount("traces", 3)
	waitCount("logs", 1)
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows, err := query("metrics")
		if err == nil && len(rows) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("automatic metrics ingestion: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	var calls []map[string]any
	if err := json.Unmarshal(run("query", "genai", "calls", "--service", service, "--since", "1m"), &calls); err != nil || len(calls) != 3 {
		t.Fatalf("GenAI calls: %v, %v", calls, err)
	}
	for _, call := range calls {
		if call["input_tokens"] == nil || call["output_tokens"] == nil {
			t.Fatalf("missing GenAI usage: %v", call)
		}
	}
	for i := 0; i < 20; i++ {
		if _, err := query("traces"); err != nil {
			t.Fatal(err)
		}
	}

	text = strings.Replace(text, "timeout: 100ms", "timeout: 5s", 1)
	if err := os.WriteFile(configPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	run("start", "--backend", "native", "--wait")
	span.SetSpanID(pcommon.SpanID{10, 11, 12})
	if _, err := ptraceotlp.NewGRPCClient(conn).Export(ctx, req); err != nil {
		t.Fatal(err)
	}
	run("stop")
	waitCount("traces", 4)
	run("prune", "--all")
	waitCount("traces", 0)
	run("ingest", "--full")
	waitCount("traces", 4)
	run("ingest", "--full")
	waitCount("traces", 4)
	run("ingest")
	waitCount("traces", 4)
	if out, err := runAt(home, "status"); err == nil {
		t.Fatalf("stopped collector reported running: %s", out)
	}
	text = strings.ReplaceAll(text, "enabled: true", "enabled: false")
	text = strings.ReplaceAll(text, "timeout: 5s", "timeout: 100ms")
	if err := os.WriteFile(configPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	run("start", "--backend", "native", "--wait")
	record.Body().SetStr("manual ingestion only")
	if _, err := plogotlp.NewGRPCClient(conn).Export(ctx, plogotlp.NewExportRequestFromLogs(logs), grpc.WaitForReady(true)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	waitCount("logs", 1)
	run("stop")
	waitCount("logs", 1)
	run("ingest")
	waitCount("logs", 2)
	run("start", "--backend", "native", "--wait")
	var crashed struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(run("status"), &crashed); err != nil || crashed.PID <= 0 {
		t.Fatalf("native PID: %v, %v", crashed, err)
	}
	process, err := os.FindProcess(crashed.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if _, err := runAt(home, "status"); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("crashed Collector still reported running")
		}
		time.Sleep(25 * time.Millisecond)
	}
	run("stop")
	run("start", "--backend", "native", "--wait")
	run("stop")
}
