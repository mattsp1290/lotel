#!/usr/bin/env python3
"""Verify complete OTLP preservation through the running Collector and lotel CLI."""

import collections
import copy
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request
import uuid


LOTEL = os.environ.get("LOTEL_BIN", "./lotel")
ENDPOINT = os.environ.get("OTLP_HTTP_ENDPOINT", "http://localhost:4318")
SERVICE = "verify-otlp-" + uuid.uuid4().hex[:12]
ROOTS = {
    "traces": "resourceSpans",
    "metrics": "resourceMetrics",
    "logs": "resourceLogs",
    "profiles": "resourceProfiles",
}


def run(*args):
    result = subprocess.run([LOTEL, *args], capture_output=True, text=True, timeout=300)
    if result.returncode:
        raise RuntimeError(f"lotel {' '.join(args)} failed: {result.stderr}")
    return json.loads(result.stdout)


def attr(key, value):
    return {"key": key, "value": value}


def payloads():
    now = time.time_ns()
    attributes = [
        attr("service.name", {"stringValue": SERVICE}),
        attr("exact", {"intValue": "9007199254740993"}),
        attr("bytes", {"bytesValue": "AAEC/w=="}),
        attr("nested", {"kvlistValue": {"values": [attr("items", {"arrayValue": {
            "values": [{"boolValue": True}, {"doubleValue": "NaN"}]
        }})]}}),
    ]
    resource = {"attributes": attributes, "droppedAttributesCount": 2}
    scope = {"name": "verify-otlp", "version": "1.0", "attributes": attributes[1:], "droppedAttributesCount": 3}

    def envelope(signal, scope_key, item_key, items):
        return {ROOTS[signal]: [{
            "resource": copy.deepcopy(resource), "schemaUrl": "https://example.test/resource-schema",
            scope_key: [{"scope": copy.deepcopy(scope), "schemaUrl": "https://example.test/scope-schema", item_key: items}],
        }]}

    trace_id = uuid.uuid4().hex
    span_id = uuid.uuid4().hex[:16]
    span = {
        "traceId": trace_id, "spanId": span_id, "name": "verify full OTLP", "kind": 3,
        "startTimeUnixNano": str(now), "endTimeUnixNano": str(now + 1234567),
        "traceState": "vendor=value", "flags": 257, "attributes": attributes[1:],
        "droppedAttributesCount": 4, "droppedEventsCount": 5, "droppedLinksCount": 6,
        "status": {"code": 2, "message": "fixture error"},
        "events": [{"timeUnixNano": str(now + 1), "name": "fixture event", "attributes": attributes[1:], "droppedAttributesCount": 7}],
        "links": [{"traceId": trace_id, "spanId": span_id, "traceState": "vendor=link", "flags": 257, "attributes": attributes[1:], "droppedAttributesCount": 8}],
    }
    point = {"timeUnixNano": str(now), "startTimeUnixNano": str(now - 1000), "attributes": attributes[1:], "flags": 1}
    exemplar = {"timeUnixNano": str(now + 1), "asInt": "9007199254740993", "traceId": trace_id, "spanId": span_id, "filteredAttributes": attributes[1:]}
    metrics = [
        {"name": "verify.otlp.gauge", "gauge": {"dataPoints": [dict(point, asInt="9007199254740993", exemplars=[exemplar]), dict(point, asDouble="Infinity")]}},
        {"name": "verify.otlp.sum", "sum": {"aggregationTemporality": 2, "isMonotonic": True, "dataPoints": [dict(point, asInt="9223372036854775807", exemplars=[exemplar])]}},
        {"name": "verify.otlp.histogram", "histogram": {"aggregationTemporality": 2, "dataPoints": [dict(point, count="18446744073709551615", sum=1.5, min=0.5, max=1, explicitBounds=[1], bucketCounts=["18446744073709551615", "0"], exemplars=[exemplar])]}},
        {"name": "verify.otlp.exponential", "exponentialHistogram": {"aggregationTemporality": 2, "dataPoints": [dict(point, count="2", sum=1.5, min=0.5, max=1, scale=2, zeroCount="1", zeroThreshold=0.001, positive={"offset": -1, "bucketCounts": ["1"]}, negative={"offset": 0, "bucketCounts": []}, exemplars=[exemplar])]}},
        {"name": "verify.otlp.summary", "summary": {"dataPoints": [dict(point, count="2", sum=1.5, quantileValues=[{"quantile": 0.5, "value": 0.75}])]}},
    ]
    for metric in metrics:
        metric.update(description="all supported metric data", unit="1", metadata=[attr("fixture", {"stringValue": "metadata"})])
    logs = [{
        "timeUnixNano": str(now), "observedTimeUnixNano": str(now + 1), "eventName": "verify.full.otlp",
        "severityNumber": 17, "severityText": "ERROR", "flags": 4294967295,
        "traceId": trace_id, "spanId": span_id, "body": {"kvlistValue": {"values": attributes[1:]}},
        "attributes": attributes[1:], "droppedAttributesCount": 9,
    }, {"body": {"stringValue": "timestamp-free log"}}]
    profile = {
        "profileId": uuid.uuid4().hex, "timeUnixNano": str(now), "durationNano": "1234567",
        "sampleType": {"typeStrindex": 1, "unitStrindex": 2},
        "periodType": {"typeStrindex": 1, "unitStrindex": 2}, "period": "1000",
        "samples": [{"stackIndex": 0, "values": ["9007199254740993"], "timestampsUnixNano": [str(now + 1)]}],
        "originalPayloadFormat": "fixture", "originalPayload": "AAEC/w==", "droppedAttributesCount": 2,
    }
    profiles = envelope("profiles", "scopeProfiles", "profiles", [profile])
    profiles["dictionary"] = {"stringTable": ["", "cpu", "nanoseconds"], "stackTable": [{"locationIndices": []}]}
    return {
        "traces": envelope("traces", "scopeSpans", "spans", [span]),
        "metrics": envelope("metrics", "scopeMetrics", "metrics", metrics),
        "logs": envelope("logs", "scopeLogs", "logRecords", logs),
        "profiles": profiles,
    }


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def service_name(resource, batch):
    strings = batch.get("dictionary", {}).get("stringTable", [])

    def indexed(value):
        return strings[value] if isinstance(value, int) and 0 <= value < len(strings) else None

    for attribute in resource.get("resource", {}).get("attributes", []):
        key = attribute.get("key", indexed(attribute.get("keyStrindex")))
        if key == "service.name":
            value = attribute.get("value", {})
            return value.get("stringValue", indexed(value.get("stringValueStrindex")))
    return None


def exported(signal, data_path):
    path = data_path / signal / f"{signal}.jsonl"
    found = []
    if not path.exists():
        return found
    with path.open() as source:
        for line in source:
            if not line.endswith("\n"):
                continue
            batch = json.loads(line)
            for resource in batch.get(ROOTS[signal], []):
                if service_name(resource, batch) != SERVICE:
                    continue
                envelope = dict(batch)
                envelope[ROOTS[signal]] = [resource]
                found.append(envelope)
    return found


def main():
    assert run("status").get("healthy"), "collector is not healthy"
    print(f"Verifying service {SERVICE}.", flush=True)
    state = json.loads((Path.home() / ".lotel/collector.state").read_text())
    data_path = Path(state["data_path"])
    for signal, payload in payloads().items():
        path = "/v1development/profiles" if signal == "profiles" else f"/v1/{signal}"
        request = urllib.request.Request(
            ENDPOINT + path, json.dumps(payload).encode(),
            {"Content-Type": "application/json"}, method="POST",
        )
        with urllib.request.urlopen(request, timeout=10) as response:
            reply = json.load(response)
        partial = reply.get("partialSuccess", {})
        assert not any(int(value) for key, value in partial.items() if key.startswith("rejected")), reply

    deadline = time.monotonic() + 30
    while True:
        run("ingest")
        queried = {signal: run("query", "otlp", "--signal", signal, "--service", SERVICE, "--since", "10m") for signal in ROOTS}
        expected = {signal: collections.Counter(canonical(row) for row in exported(signal, data_path)) for signal in ROOTS}
        actual = {signal: collections.Counter(canonical(row["payload"]) for row in rows) for signal, rows in queried.items()}
        if all(queried.values()) and actual == expected:
            break
        if time.monotonic() >= deadline:
            missing = [signal for signal, rows in queried.items() if not rows]
            raise AssertionError(f"telemetry missing for {SERVICE}: {missing}")
        time.sleep(1)

    for signal, rows in queried.items():
        assert len(run("query", "otlp", "--signal", signal, "--service", SERVICE, "--limit", "1")) == 1
        print(f"PASS {signal} exported resource envelopes match query payloads ({len(rows)})")

    metrics = [metric for row in queried["metrics"] for resource in row["payload"][ROOTS["metrics"]] for scope in resource["scopeMetrics"] for metric in scope["metrics"]]
    assert {m["name"] for m in metrics} >= {"verify.otlp." + name for name in ("gauge", "sum", "histogram", "exponential", "summary")}
    exact = next(m for m in metrics if m["name"] == "verify.otlp.gauge")["gauge"]["dataPoints"][0]
    assert exact["asInt"] == "9007199254740993" and exact["exemplars"][0]["asInt"] == "9007199254740993"
    histogram = next(m for m in metrics if m["name"] == "verify.otlp.histogram")["histogram"]["dataPoints"][0]
    assert histogram["count"] == "18446744073709551615"
    assert run("query", "profiles", "--service", SERVICE) == run("query", "otlp", "--signal", "profiles", "--service", SERVICE)
    before = {signal: collections.Counter(canonical(row["payload"]) for row in run("query", "otlp", "--signal", signal, "--service", SERVICE)) for signal in ROOTS}
    run("ingest")
    for signal, prior in before.items():
        after = collections.Counter(canonical(row["payload"]) for row in run("query", "otlp", "--signal", signal, "--service", SERVICE))
        assert all(after[payload] == count for payload, count in prior.items()), f"{signal} duplicated existing envelopes"
    print("PASS all metric kinds, exact numbers, exemplars, profiles alias, and repeated-ingest idempotency")
    print(f"Verified service {SERVICE}. Collector remains running.")


if __name__ == "__main__":
    main()
