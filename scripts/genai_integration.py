#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.10"
# dependencies = [
#   "openai>=1.60",
#   "opentelemetry-sdk==1.45.2",
#   "opentelemetry-exporter-otlp-proto-http==1.45.2",
#   "opentelemetry-instrumentation-genai-openai==1.3b0",
# ]
# ///
"""Optional real OpenCode Go telemetry test.

Run: zsh -c 'source ~/.profile && LOTEL_BIN=./lotel uv run scripts/genai_integration.py'
Without OPENCODE_GO_API_KEY, print SKIP and exit successfully.
Set LOTEL_IT_REQUIRE=1 to require credentials.
"""
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.request
import uuid

KEY = os.environ.get("OPENCODE_GO_API_KEY", "")
LOTEL = os.environ.get("LOTEL_BIN", "lotel")
ENDPOINT = os.environ.get("OTLP_HTTP_ENDPOINT", "http://localhost:4318").rstrip("/")
BASE = os.environ.get("LOTEL_IT_OPENCODE_BASE_URL", "https://opencode.ai/inference/go/openai/v1").rstrip("/")
MODEL = os.environ.get("LOTEL_IT_OPENCODE_MODEL", "glm-5.3-flash")
SERVICE = f"genai-it-{uuid.uuid4().hex[:8]}"
passed = failed = warnings = 0


def redact(text):
    return str(text).replace(KEY, "<redacted>") if KEY else str(text)


def run(*args, check=True):
    env = {k: v for k, v in os.environ.items() if k != "OPENCODE_GO_API_KEY"}
    result = subprocess.run([LOTEL, *args], capture_output=True, text=True,
                            timeout=300 if args[0] == "ingest" else 60, env=env)
    if check and result.returncode:
        raise RuntimeError(redact(result.stderr))
    return result


def query(*args):
    return json.loads(run("query", *args, "--service", SERVICE).stdout)


def test(name, fn):
    global passed, failed
    try:
        fn()
        passed += 1
        print(f"PASS: {name}")
    except Exception as exc:
        failed += 1
        print(f"FAIL: {name}: {redact(exc)}")


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def warn(message):
    global warnings
    warnings += 1
    print(f"WARN: {redact(message)}")


def scan_for_key():
    home = Path.home() / ".lotel"
    data = home / "data"
    paths = [home / "collector.log", data / "lotel.db", data / "lotel.db.wal"]
    for signal in ("traces", "metrics", "logs"):
        paths.extend((data / signal).glob("*.jsonl"))
    needle = KEY.encode()
    for path in paths:
        if path.exists():
            # Keep overlap so a key crossing chunks cannot evade the scan.
            with path.open("rb") as source:
                tail = b""
                while chunk := source.read(1 << 20):
                    content = tail + chunk
                    require(needle not in content, f"credential leak in {path}")
                    tail = content[-max(len(needle) - 1, 0):]
    logs = subprocess.run(["docker", "logs", "lotel-collector"], capture_output=True, timeout=30)
    require(needle not in logs.stdout + logs.stderr, "credential leak in collector container logs")


def main():
    global failed
    if not KEY:
        print("SKIP: OPENCODE_GO_API_KEY not set")
        return 1 if os.environ.get("LOTEL_IT_REQUIRE") == "1" else 0

    started_here = False
    providers = []
    client = None
    try:
        state_file = Path.home() / ".lotel/collector.state"
        if state_file.exists():
            state = json.loads(state_file.read_text())
            expected = str(Path.home() / ".lotel/collector-config.yaml")
            require(state.get("config_path") == expected, "collector running with custom config; stop it first")
        started_here = run("health", check=False).returncode != 0
        run("start", "--wait")
        image = subprocess.run(["docker", "inspect", "-f", "{{.Config.Image}}", "lotel-collector"],
                               capture_output=True, text=True, timeout=30)
        require(image.returncode == 0 and image.stdout.strip().endswith(":0.165.0"), "stale collector image")

        catalog_url = BASE.removesuffix("/openai/v1") + "/v1/models"
        request = urllib.request.Request(catalog_url, headers={"User-Agent": "lotel-genai-integration/1.0"})
        with urllib.request.urlopen(request, timeout=10) as response:
            catalog = json.load(response)
        models = catalog.get("data", catalog.get("models", []))
        selected = next((m for m in models if m.get("id") == MODEL), None)
        require(selected is not None, f"model absent; available: {[m.get('id') for m in models[:10]]}")

        import openai
        client = openai.OpenAI(
            base_url=BASE, api_key=KEY, timeout=60, max_retries=0,
            default_headers={"x-opencode-session": f"lotel-it-{uuid.uuid4().hex[:8]}",
                             "User-Agent": "lotel-genai-integration/1.0"},
        )
        try:
            client.chat.completions.create(model=MODEL, messages=[{"role": "user", "content": "Say hello."}],
                                           max_tokens=256)
        except openai.APIStatusError as exc:
            label = "auth rejected" if exc.status_code in (401, 403) else "request rejected"
            raise RuntimeError(f"{label} (HTTP {exc.status_code})") from None

        os.environ["OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"] = "span_and_event"
        from opentelemetry import trace, metrics
        from opentelemetry._logs import set_logger_provider
        from opentelemetry.sdk.resources import Resource
        from opentelemetry.sdk.trace import TracerProvider
        from opentelemetry.sdk.trace.export import BatchSpanProcessor
        from opentelemetry.sdk.metrics import MeterProvider
        from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
        from opentelemetry.sdk._logs import LoggerProvider
        from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
        from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
        from opentelemetry.instrumentation.genai.openai import OpenAIInstrumentor

        resource = Resource({"service.name": SERVICE})
        tracer_provider = TracerProvider(resource=resource)
        tracer_provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter(endpoint=ENDPOINT + "/v1/traces")))
        providers.append(tracer_provider)
        meter_provider = MeterProvider(resource=resource, metric_readers=[
            PeriodicExportingMetricReader(OTLPMetricExporter(endpoint=ENDPOINT + "/v1/metrics"),
                                          export_interval_millis=2000)])
        providers.append(meter_provider)
        logger_provider = LoggerProvider(resource=resource)
        logger_provider.add_log_record_processor(BatchLogRecordProcessor(OTLPLogExporter(endpoint=ENDPOINT + "/v1/logs")))
        providers.append(logger_provider)
        trace.set_tracer_provider(tracer_provider)
        metrics.set_meter_provider(meter_provider)
        set_logger_provider(logger_provider)
        OpenAIInstrumentor().instrument()

        nonce = uuid.uuid4().hex
        with trace.get_tracer("lotel-integration").start_as_current_span("lotel-it scenario") as parent:
            trace_id = f"{parent.get_span_context().trace_id:032x}"
            client.chat.completions.create(model=MODEL, max_tokens=256, temperature=0,
                messages=[{"role": "user", "content": f"Reply with exactly this token and nothing else: {nonce}"}])
            stream = client.chat.completions.create(model=MODEL, max_tokens=256, stream=True,
                stream_options={"include_usage": True}, messages=[{"role": "user", "content": "Say hello."}])
            for _ in stream:
                pass
            client.chat.completions.create(model=MODEL, max_tokens=256, tool_choice="auto",
                tools=[{"type": "function", "function": {"name": "get_weather", "description": "Get weather",
                       "parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}}}],
                messages=[{"role": "user", "content": "What is the weather in London?"}])
        for provider in providers:
            provider.force_flush()
            provider.shutdown()
        providers.clear()

        deadline = time.monotonic() + 45
        calls = []
        while time.monotonic() < deadline:
            run("ingest")
            calls = query("genai", "calls", "--content")
            spanmetrics = query("metrics", "--name", "traces.span.metrics.calls",
                                "--attr", f"gen_ai.request.model={MODEL}")
            if len(calls) >= 3 and spanmetrics:
                break
            time.sleep(3)

        def check_calls():
            require(len(calls) >= 3, "expected at least three instrumented calls")
            for call in calls:
                require(call["trace_id"] == trace_id and call["request_model"] == MODEL, "wrong trace or model")
                require(call["operation"] == "chat" and call["provider"], "missing operation or provider")
            print("Observed:", [(c["span_name"], c["provider"]) for c in calls])

        test("instrumented model calls", check_calls)
        call_a = next((c for c in calls if nonce in json.dumps(c.get("input_messages"))), {})
        test("non-streaming token counts", lambda: require(
            (call_a.get("input_tokens") or 0) > 0 and (call_a.get("output_tokens") or 0) > 0, "missing token usage"))
        if any(c.get("input_tokens") is None for c in calls):
            warn("some calls lack usage; streaming providers may omit it")
        test("JSON-string span content", lambda: require(
            isinstance(call_a.get("input_messages"), list) and call_a.get("content_source") == "span",
            "span content was not decoded"))
        logs = query("logs", "--event-name", "gen_ai.client.inference.operation.details")
        test("structured GenAI event content", lambda: require(any(
            log.get("span_id") == call_a.get("span_id") and
            isinstance(log.get("attributes", {}).get("gen_ai.input.messages"), list) for log in logs),
            "missing correlated structured event"))
        def histogram():
            rows = query("metrics", "--name", "gen_ai.client.operation.duration")
            if not rows:
                rows = query("metrics", "--name", "gen_ai.client.inference.duration")
            require(any(len(r.get("bucket_counts", [])) == len(r.get("explicit_bounds", [])) + 1 for r in rows),
                    "missing duration histogram buckets")
        test("duration histogram", histogram)
        test("token metrics", lambda: require(
            query("metrics", "--name", "gen_ai.client.token.usage", "--attr", "gen_ai.token.type=input") or
            query("metrics", "--name", "gen_ai.client.inference.usage.input_tokens"), "missing token metric"))
        test("spanmetrics", lambda: require(spanmetrics, "missing spanmetrics calls"))
        def usage():
            rows = query("genai", "usage", "--group-by", "model")
            row = next(r for r in rows if r.get("model") == MODEL)
            require(row["input_tokens"] == sum(c.get("input_tokens") or 0 for c in calls), "usage total mismatch")
        test("grouped token usage", usage)
        test("credential leak scan", scan_for_key)
    except Exception as exc:
        failed += 1
        print(f"FAIL: {redact(exc)}")
    finally:
        for provider in providers:
            try:
                provider.shutdown()
            except Exception as exc:
                failed += 1
                print(f"FAIL: shutdown: {redact(exc)}")
        if client is not None:
            client.close()
        for args in [("prune", "--older-than", "0h", "--service", SERVICE)] + ([("stop",)] if started_here else []):
            try:
                run(*args)
            except Exception as exc:
                failed += 1
                print(f"FAIL: cleanup: {redact(exc)}")
    print(f"Results: {passed} passed, {failed} failed, {warnings} warnings")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
