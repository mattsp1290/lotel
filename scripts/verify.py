#!/usr/bin/env python3
"""
lotel end-to-end verification script.

Verifies the full pipeline: start → ingest → query → prune.
Uses a UUID-based service.name to isolate test data.

Usage:
    python3 scripts/verify.py

Prerequisites:
    - lotel binary built and on PATH (or ./lotel)
    - Docker running
    - pip install requests (for OTLP HTTP submission)
"""

import json
import os
import subprocess
import sys
import time
import uuid

try:
    import requests
except ImportError:
    print("ERROR: 'requests' package required. Install with: pip install requests")
    sys.exit(1)

LOTEL = os.environ.get("LOTEL_BIN", "lotel")
OTLP_HTTP = os.environ.get("OTLP_HTTP_ENDPOINT", "http://localhost:4318")
SERVICE_NAME = f"verify-{uuid.uuid4().hex[:8]}"

passed = 0
failed = 0


def run(args, check=True, timeout=30):
    """Run a lotel CLI command and return stdout."""
    result = subprocess.run(
        [LOTEL] + args,
        capture_output=True,
        text=True,
        timeout=timeout,
    )
    if check and result.returncode != 0:
        print(f"  STDERR: {result.stderr.strip()}")
    return result


def test(name, fn):
    """Run a test function and track pass/fail."""
    global passed, failed
    try:
        fn()
        print(f"  PASS: {name}")
        passed += 1
    except Exception as e:
        print(f"  FAIL: {name}: {e}")
        failed += 1


def check_collector_health():
    """Verify collector is running and healthy."""
    result = run(["health"], check=False)
    if result.returncode != 0:
        raise RuntimeError("Collector is not healthy. Run 'lotel start --wait' first.")


def send_otlp_traces():
    """Send test traces via OTLP HTTP."""
    now_ns = str(int(time.time() * 1_000_000_000))
    end_ns = str(int((time.time() + 0.025) * 1_000_000_000))
    data = {
        "resourceSpans": [{
            "resource": {
                "attributes": [
                    {"key": "service.name", "value": {"stringValue": SERVICE_NAME}}
                ]
            },
            "scopeSpans": [{
                "spans": [{
                    "traceId": uuid.uuid4().hex,
                    "spanId": uuid.uuid4().hex[:16],
                    "name": "GET /verify",
                    "kind": 2,
                    "startTimeUnixNano": now_ns,
                    "endTimeUnixNano": end_ns,
                    "status": {"code": 1},
                    "attributes": [
                        {"key": "http.method", "value": {"stringValue": "GET"}},
                        {"key": "test.run", "value": {"stringValue": SERVICE_NAME}},
                    ],
                }]
            }]
        }]
    }
    resp = requests.post(f"{OTLP_HTTP}/v1/traces", json=data, timeout=10)
    if resp.status_code != 200:
        raise RuntimeError(f"OTLP traces: HTTP {resp.status_code}: {resp.text}")


def send_otlp_metrics():
    """Send test metrics via OTLP HTTP."""
    now_ns = str(int(time.time() * 1_000_000_000))
    data = {
        "resourceMetrics": [{
            "resource": {
                "attributes": [
                    {"key": "service.name", "value": {"stringValue": SERVICE_NAME}}
                ]
            },
            "scopeMetrics": [{
                "metrics": [{
                    "name": "verify_requests_total",
                    "unit": "1",
                    "sum": {
                        "dataPoints": [{
                            "timeUnixNano": now_ns,
                            "asInt": "1",
                            "attributes": [],
                        }],
                        "aggregationTemporality": 2,
                        "isMonotonic": True,
                    }
                }]
            }]
        }]
    }
    resp = requests.post(f"{OTLP_HTTP}/v1/metrics", json=data, timeout=10)
    if resp.status_code != 200:
        raise RuntimeError(f"OTLP metrics: HTTP {resp.status_code}: {resp.text}")


def send_otlp_logs():
    """Send test logs via OTLP HTTP."""
    now_ns = str(int(time.time() * 1_000_000_000))
    data = {
        "resourceLogs": [{
            "resource": {
                "attributes": [
                    {"key": "service.name", "value": {"stringValue": SERVICE_NAME}}
                ]
            },
            "scopeLogs": [{
                "logRecords": [{
                    "timeUnixNano": now_ns,
                    "severityText": "INFO",
                    "severityNumber": 9,
                    "body": {"stringValue": f"verification log from {SERVICE_NAME}"},
                    "attributes": [],
                }]
            }]
        }]
    }
    resp = requests.post(f"{OTLP_HTTP}/v1/logs", json=data, timeout=10)
    if resp.status_code != 200:
        raise RuntimeError(f"OTLP logs: HTTP {resp.status_code}: {resp.text}")


def wait_and_ingest():
    """Wait for collector to flush, then ingest JSONL into DuckDB."""
    time.sleep(3)
    result = run(["ingest"], timeout=300)
    if result.returncode != 0:
        raise RuntimeError(f"Ingest failed: {result.stderr}")


def query_traces():
    """Verify traces can be queried by service name."""
    result = run(["query", "traces", "--service", SERVICE_NAME])
    if result.returncode != 0:
        raise RuntimeError(f"Query traces failed: {result.stderr}")
    data = json.loads(result.stdout)
    if not data or len(data) == 0:
        raise RuntimeError(f"Expected traces for {SERVICE_NAME}, got empty result")


def query_metrics():
    """Verify metrics can be queried by service name."""
    result = run(["query", "metrics", "--service", SERVICE_NAME])
    if result.returncode != 0:
        raise RuntimeError(f"Query metrics failed: {result.stderr}")
    data = json.loads(result.stdout)
    if not data or len(data) == 0:
        raise RuntimeError(f"Expected metrics for {SERVICE_NAME}, got empty result")


def query_logs():
    """Verify logs can be queried by service name."""
    result = run(["query", "logs", "--service", SERVICE_NAME])
    if result.returncode != 0:
        raise RuntimeError(f"Query logs failed: {result.stderr}")
    data = json.loads(result.stdout)
    if not data or len(data) == 0:
        raise RuntimeError(f"Expected logs for {SERVICE_NAME}, got empty result")


def query_aggregate():
    """Verify metric aggregation works."""
    result = run(["query", "aggregate", "--metric", "verify_requests_total", "--service", SERVICE_NAME])
    if result.returncode != 0:
        raise RuntimeError(f"Query aggregate failed: {result.stderr}")
    data = json.loads(result.stdout)
    if data.get("count", 0) == 0:
        raise RuntimeError(f"Expected non-zero count for aggregation")


def prune_dry_run():
    """Verify prune --dry-run reports what would be deleted."""
    result = run(["prune", "--older-than", "0h", "--service", SERVICE_NAME, "--dry-run"])
    if result.returncode != 0:
        raise RuntimeError(f"Prune dry-run failed: {result.stderr}")
    data = json.loads(result.stdout)
    if not any(r.get("deleted", 0) > 0 for r in data):
        raise RuntimeError("Dry-run reported no deletions")


def prune_execute():
    """Verify prune actually deletes data."""
    result = run(["prune", "--older-than", "0h", "--service", SERVICE_NAME])
    if result.returncode != 0:
        raise RuntimeError(f"Prune failed: {result.stderr}")


def verify_pruned():
    """Verify data is gone after prune."""
    result = run(["query", "traces", "--service", SERVICE_NAME])
    data = json.loads(result.stdout)
    if data and len(data) > 0:
        raise RuntimeError(f"Expected 0 traces after prune, got {len(data)}")


def start_collector():
    """Start the collector and wait for it to become healthy."""
    result = run(["start", "--wait"], check=False)
    if result.returncode != 0:
        raise RuntimeError(f"Failed to start collector: {result.stderr.strip()}")


def stop_collector():
    """Stop the collector."""
    result = run(["stop"], check=False)
    if result.returncode != 0:
        raise RuntimeError(f"Failed to stop collector: {result.stderr.strip()}")



MODEL = f"verify-model-{uuid.uuid4().hex[:8]}"


def check_existing_config():
    from pathlib import Path
    state_path = Path.home() / ".lotel/collector.state"
    if state_path.exists():
        state = json.loads(state_path.read_text())
        expected = str(Path.home() / ".lotel/collector-config.yaml")
        if state.get("config_path") != expected:
            raise RuntimeError("collector running with custom config; stop it first")


def check_collector_pinned():
    from pathlib import Path
    state_path = Path.home() / ".lotel/collector.state"
    if state_path.exists():
        state = json.loads(state_path.read_text())
        expected = str(Path.home() / ".lotel/collector-config.yaml")
        if state.get("config_path") != expected:
            raise RuntimeError("collector running with custom config; stop it first")
    result = subprocess.run(
        ["docker", "inspect", "-f", "{{.Config.Image}}", "lotel-collector"],
        capture_output=True, text=True, timeout=30,
    )
    assert result.returncode == 0 and result.stdout.strip().endswith(":0.165.0"), "stale collector"


def genai_query(*args):
    result = run(["query", *args, "--service", SERVICE_NAME])
    if result.returncode:
        raise RuntimeError(result.stderr)
    return json.loads(result.stdout)


def ingest_until(predicate, timeout=30):
    deadline = time.monotonic() + timeout
    while True:
        result = run(["ingest"], timeout=300)
        if result.returncode:
            raise RuntimeError(result.stderr)
        if predicate():
            return
        if time.monotonic() >= deadline:
            raise RuntimeError("telemetry did not arrive before timeout")
        time.sleep(2)


def send_genai_payloads():
    from pathlib import Path
    fixtures = Path(__file__).resolve().parents[1] / "internal/storage/testdata/genai"
    now = time.time_ns()
    def refresh(value):
        if isinstance(value, dict):
            for key, child in value.items():
                if key in ("timeUnixNano", "observedTimeUnixNano", "startTimeUnixNano"):
                    value[key] = str(now)
                elif key == "endTimeUnixNano":
                    value[key] = str(now + 1000000)
                elif key == "stringValue" and child == "genai-fixture":
                    value[key] = SERVICE_NAME
                elif key == "stringValue" and child == "fixture-model":
                    value[key] = MODEL
                else:
                    refresh(child)
        elif isinstance(value, list):
            for child in value:
                refresh(child)

    for signal, root in (("traces", "resourceSpans"), ("metrics", "resourceMetrics"), ("logs", "resourceLogs")):
        payload = json.loads((fixtures / f"{signal}.jsonl").read_text())
        refresh(payload)
        if signal == "traces":
            spans = payload[root][0]["scopeSpans"][0]["spans"]
            oi = {
                "traceId": uuid.uuid4().hex, "spanId": uuid.uuid4().hex[:16],
                "name": "verify OpenInference", "kind": 3,
                "startTimeUnixNano": str(now), "endTimeUnixNano": str(now + 1000000),
                "attributes": [
                    {"key": "openinference.span.kind", "value": {"stringValue": "LLM"}},
                    {"key": "llm.model_name", "value": {"stringValue": MODEL + "-oi"}},
                    {"key": "llm.token_count.prompt", "value": {"intValue": "11"}},
                    {"key": "llm.token_count.completion", "value": {"intValue": "7"}},
                ],
            }
            spans.append(oi)
        response = requests.post(f"{OTLP_HTTP}/v1/{signal}", json=payload, timeout=10)
        response.raise_for_status()


def check_genai_calls():
    ingest_until(lambda: len(genai_query("genai", "calls")) >= 5)
    calls = genai_query("genai", "calls", "--content")
    native = next(c for c in calls if c["span_id"] == "1" * 16)
    assert native["provider"] == "openai" and native["input_tokens"] == 5
    assert native["input_messages"][0]["parts"][0]["content"] == "fixture prompt"
    assert native["event_count"] == 1
    oi = next(c for c in calls if c["span_name"] == "verify OpenInference")
    assert oi["input_tokens"] == 11 and oi["output_tokens"] == 7


def check_genai_logs():
    rows = genai_query("logs", "--event-name", "gen_ai.client.inference.operation.details")
    assert len(rows) == 1 and rows[0]["span_id"] == "1" * 16
    assert isinstance(rows[0]["attributes"]["gen_ai.input.messages"], list)


def check_genai_histograms():
    rows = genai_query("metrics", "--name", "gen_ai.client.token.usage")
    assert rows
    for row in rows:
        assert row["bucket_counts"] == [0, 0, 1, 0]
        assert len(row["bucket_counts"]) == len(row["explicit_bounds"]) + 1
        assert row["count"] == 1 and row["sum"] == 5


def check_genai_spanmetrics():
    ingest_until(lambda: bool(genai_query(
        "metrics", "--name", "traces.span.metrics.calls",
        "--attr", f"gen_ai.request.model={MODEL}",
    )))


def check_genai_usage():
    row = next(r for r in genai_query("genai", "usage", "--group-by", "model") if r.get("model") == MODEL)
    assert row["input_tokens"] == 5 and row["output_tokens"] == 3


def check_ingest_idempotent():
    before = [len(genai_query(signal)) for signal in ("traces", "logs")]
    result = run(["ingest"], timeout=300)
    assert result.returncode == 0, result.stderr
    after = [len(genai_query(signal)) for signal in ("traces", "logs")]
    assert before == after, (before, after)


def main():
    print(f"lotel end-to-end verification")
    print(f"Service tag: {SERVICE_NAME}")
    print()

    print("[0/6] Start collector")
    test("Check existing config", check_existing_config)
    if failed:
        sys.exit(1)
    test("Start collector", start_collector)
    test("Pinned collector", check_collector_pinned)

    print("\n[1/6] Collector health")
    test("Collector is healthy", check_collector_health)

    print("\n[2/6] OTLP ingestion")
    test("Send OTLP traces", send_otlp_traces)
    test("Send OTLP metrics", send_otlp_metrics)
    test("Send OTLP logs", send_otlp_logs)
    test("Wait and ingest", wait_and_ingest)

    print("\n[3/6] Query verification")
    test("Query traces by service", query_traces)
    test("Query metrics by service", query_metrics)
    test("Query logs by service", query_logs)
    test("Metric aggregation", query_aggregate)

    print("\n[4/6] GenAI verification")
    test("Send GenAI payloads", send_genai_payloads)
    test("GenAI calls and normalizer", check_genai_calls)
    test("GenAI event logs", check_genai_logs)
    test("GenAI histogram buckets", check_genai_histograms)
    test("GenAI spanmetrics", check_genai_spanmetrics)
    test("GenAI usage", check_genai_usage)
    test("Incremental ingest idempotency", check_ingest_idempotent)

    print("\n[5/6] Prune verification")
    test("Prune dry-run", prune_dry_run)
    test("Prune execute", prune_execute)
    test("Verify pruned", verify_pruned)

    print("\n[6/6] Stop collector")
    test("Stop collector", stop_collector)

    print(f"\nResults: {passed} passed, {failed} failed")
    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
