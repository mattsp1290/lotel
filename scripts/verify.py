#!/usr/bin/env python3
"""
lotel end-to-end verification script.

Verifies the full pipeline: start → ingest → query → prune.
Uses a UUID-based service.name to isolate test data.

Usage:
    python3 scripts/verify.py

Prerequisites:
    - lotel binary built and on PATH (or ./lotel)
    - otelcol-contrib installed and on PATH
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


def run(args, check=True):
    """Run a lotel CLI command and return stdout."""
    result = subprocess.run(
        [LOTEL] + args,
        capture_output=True,
        text=True,
        timeout=30,
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
    result = run(["ingest"])
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


def main():
    print(f"lotel end-to-end verification")
    print(f"Service tag: {SERVICE_NAME}")
    print()

    print("[1/4] Collector health")
    test("Collector is healthy", check_collector_health)

    print("\n[2/4] OTLP ingestion")
    test("Send OTLP traces", send_otlp_traces)
    test("Send OTLP metrics", send_otlp_metrics)
    test("Send OTLP logs", send_otlp_logs)
    test("Wait and ingest", wait_and_ingest)

    print("\n[3/4] Query verification")
    test("Query traces by service", query_traces)
    test("Query metrics by service", query_metrics)
    test("Query logs by service", query_logs)
    test("Metric aggregation", query_aggregate)

    print("\n[4/4] Prune verification")
    test("Prune dry-run", prune_dry_run)
    test("Prune execute", prune_execute)
    test("Verify pruned", verify_pruned)

    print(f"\nResults: {passed} passed, {failed} failed")
    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
