# File-Based OpenTelemetry Architecture

## Overview

This document explains what's **actually required** for OpenTelemetry metrics, traces, and logs to appear on the file system in the Agent Observability Verifier.

**TL;DR**: Only the **OTel Collector** is required for file-based exports. Everything else is optional.

---

## Minimal File-Export Architecture

```
Application → OTel Collector (OTLP ports 4317/4318) → File Exporters → ./data/
```

That's it. This is the **only** required path for file-based telemetry verification.

---

## Component Requirements Matrix

| Component | Status | Role in File Exports | Required? |
|-----------|--------|---------------------|-----------|
| **OTel Collector** | ✅ WORKING | Core telemetry processor and file exporter | ✅ **YES** |
| **StatsD** | ⚠️ BROKEN | Alternative metrics input (UDP:8125) | ❌ NO |
| **Prometheus** | ⚠️ OPTIONAL | Time-series storage & scraping | ❌ NO |
| **Grafana** | ⚠️ OPTIONAL | Visualization dashboards | ❌ NO |
| **Jaeger** | ⚠️ OPTIONAL | Trace visualization & storage | ❌ NO |
| **Filebeat** | ⚠️ OPTIONAL | Post-processes exported files | ❌ NO |

---

## OTel Collector: The Only Required Service

### What It Does

The OpenTelemetry Collector is the **single point of integration** for all telemetry:

1. **Receives** telemetry via multiple protocols (OTLP, StatsD, Prometheus)
2. **Processes** telemetry (batching, filtering, enrichment)
3. **Exports** telemetry to multiple destinations (files, Jaeger, Prometheus)

### Required Configuration

**File**: `docker/configs/otel/otel-collector-config.yaml`

#### Receivers (Telemetry Input)

```yaml
receivers:
  otlp:                    # ✅ REQUIRED for OTLP telemetry
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318
```

**Optional receivers** (only needed if NOT using OTLP):
- `statsd` - For StatsD metrics (currently broken - see below)
- `prometheus` - Scrapes OTel's own metrics

#### Processors (Data Transformation)

```yaml
processors:
  batch:                   # ✅ RECOMMENDED - batches for efficiency
    send_batch_size: 1024
    send_batch_max_size: 2048

  memory_limiter:          # ✅ RECOMMENDED - prevents OOM
    limit_mib: 512

  attributes:              # ⚠️ OPTIONAL - enriches with metadata
    actions:
      - key: service.name
        value: canary-api
        action: upsert

  resource:                # ⚠️ OPTIONAL - adds host & SDK info
    attributes:
      - key: host.name
        value: local-dev
        action: upsert
```

**Minimum viable**: Just `batch` processor
**Recommended**: `batch` + `memory_limiter`
**Production**: All processors shown above

#### Exporters (Output Destinations)

**File Exporters (REQUIRED for file exports):**

```yaml
exporters:
  file/traces:
    path: /data/traces/traces.jsonl
    format: json

  file/metrics:
    path: /data/metrics/metrics.jsonl
    format: json

  file/logs:
    path: /data/logs/logs.jsonl
    format: json
```

**Optional exporters** (not needed for file exports):
- `prometheus` - Exposes metrics for Prometheus scraping
- `otlp/jaeger` - Sends traces to Jaeger UI
- `debug` - Logs samples to stdout

#### Pipelines (Connecting It All)

**Traces Pipeline:**
```yaml
traces:
  receivers: [otlp]
  processors: [memory_limiter, batch, attributes, resource]
  exporters: [file/traces, file/traces_json]  # ← File exports happen here
```

For file exports, you only need the `file/*` exporters. Remove `otlp/jaeger` and `debug` if you don't need Jaeger UI or debug logs.

**Metrics Pipeline:**
```yaml
metrics:
  receivers: [otlp]  # ← Remove statsd, prometheus if not used
  processors: [memory_limiter, batch, attributes, resource]
  exporters: [file/metrics]  # ← File exports happen here
```

**Logs Pipeline:**
```yaml
logs:
  receivers: [otlp]
  processors: [memory_limiter, batch, attributes, resource]
  exporters: [file/logs]  # ← File exports happen here
```

### Docker Volume Mapping (CRITICAL)

**File**: `docker-compose.yml`

```yaml
services:
  otel-collector:
    volumes:
      - ./data:/data    # ← THIS IS CRITICAL for file exports
      - ./docker/configs/otel/otel-collector-config.yaml:/etc/otel-collector-config.yaml
```

**Data Flow:**
1. OTel Collector writes to `/data/traces/traces.jsonl` (inside container)
2. Docker volume mount maps `/data` → `./data` (host filesystem)
3. Files appear in `./data/traces/`, `./data/metrics/`, `./data/logs/` on host

**Without this volume mount, file exports will succeed inside the container but won't be visible on your host machine.**

---

## Optional Services: What They Actually Do

### StatsD ⚠️ CURRENTLY BROKEN

**Purpose**: Alternative metrics input for applications that don't support OTLP

**Current Setup:**
- StatsD service listens on `localhost:8125` (UDP)
- StatsD forwards to `otel-collector:2003` via Graphite protocol
- **Problem**: OTel Collector has NO Graphite receiver on port 2003!

**What Actually Happens:**
```
Application (StatsD) → localhost:8125 → StatsD service
  ↓
StatsD service → otel-collector:2003 (Graphite protocol)
  ↓
❌ OTel Collector has no receiver on port 2003
  ↓
Metrics are LOST
```

**To Fix** (two options):

**Option 1: Remove StatsD service, use OTel's built-in receiver** (RECOMMENDED)
```yaml
# In docker-compose.yml - OTel Collector service:
services:
  otel-collector:
    ports:
      - "8125:8125/udp"  # Add this to expose OTel's StatsD receiver

# Remove or stop the StatsD service entirely
```

**Option 2: Add Graphite receiver to OTel Collector**
```yaml
# In otel-collector-config.yaml:
receivers:
  carbon:  # Graphite/Carbon receiver
    endpoint: 0.0.0.0:2003

service:
  pipelines:
    metrics:
      receivers: [otlp, statsd, prometheus, carbon]  # Add carbon
```

**Recommendation**: Migrate to OTLP instead (see migration guide)

### Prometheus

**Purpose**: Scrapes metrics from OTel Collector's `/metrics` endpoint (8889)

**Data Flow**:
```
OTel Collector (8889) → Prometheus scraper → Prometheus storage
```

**When needed**: For time-series queries, alerting, and Grafana dashboards

**File export impact**: ❌ NONE - Prometheus reads metrics, doesn't affect file exports

**Can remove?** ✅ YES - file exports work independently

### Grafana

**Purpose**: Visualizes Prometheus metrics in dashboards

**Data Flow**:
```
Prometheus → Grafana → User's browser
```

**When needed**: For visual monitoring and dashboards

**File export impact**: ❌ NONE - pure visualization layer

**Can remove?** ✅ YES - doesn't touch telemetry data at all

### Jaeger

**Purpose**: Trace visualization and search UI

**Data Flow**:
```
OTel Collector → Jaeger (14250 OTLP) → Jaeger storage → Jaeger UI (16686)
```

**When needed**: For visual trace exploration and debugging

**File export impact**: ❌ NONE - receives traces via `otlp/jaeger` exporter, but file exports happen via `file/traces` exporter

**Can remove?** ✅ YES - traces still written to files independently

### Filebeat

**Purpose**: Post-processes exported log files to extract correlation data

**Config**: `docker/configs/filebeat/filebeat.yml`

**Data Flow**:
```
Reads ./data/logs/*.jsonl → Processes → Writes ./data/processed/
```

**Processing**: Extracts trace IDs, span IDs, endpoint info for correlation

**File export impact**: ❌ NONE - it reads files AFTER OTel exports them

**Can remove?** ✅ YES - it's a post-processor, doesn't affect OTel exports

---

## Minimal Docker Compose Setup

To get telemetry files ONLY (no visualization), you need:

```yaml
version: '3.8'

services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    container_name: otel-collector
    command: ["--config=/etc/otel-collector-config.yaml"]
    volumes:
      - ./docker/configs/otel/otel-collector-config.yaml:/etc/otel-collector-config.yaml
      - ./data:/data  # ← CRITICAL volume mount
    ports:
      - "4317:4317"   # OTLP gRPC
      - "4318:4318"   # OTLP HTTP
      - "13133:13133" # Health check
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:13133/"]
      interval: 30s
      timeout: 10s
      retries: 3
```

**That's it.** Everything else (StatsD, Prometheus, Grafana, Jaeger, Filebeat) is optional.

---

## Minimal OTel Collector Configuration

**File**: `otel-collector-config-minimal.yaml`

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:
    timeout: 1s
    send_batch_size: 1024

exporters:
  file/traces:
    path: /data/traces/traces.jsonl
    format: json

  file/metrics:
    path: /data/metrics/metrics.jsonl
    format: json

  file/logs:
    path: /data/logs/logs.jsonl
    format: json

extensions:
  health_check:
    endpoint: 0.0.0.0:13133

service:
  extensions: [health_check]

  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [file/traces]

    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [file/metrics]

    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [file/logs]
```

**Total lines**: ~50 (vs. 137 in full config)

---

## Data Directory Structure

```
./data/
├── traces/
│   ├── traces.jsonl              # Line-delimited JSON (streaming)
│   └── traces_detailed.json      # Single JSON file (optional)
├── metrics/
│   └── metrics.jsonl             # Line-delimited JSON
├── logs/
│   └── logs.jsonl                # Line-delimited JSON
└── processed/                    # Filebeat output (optional)
```

### File Formats

**JSONL (JSON Lines)** - One JSON object per line, ideal for streaming:
```jsonl
{"timestamp":"2024-05-24T23:22:00Z","level":"INFO","service":"canary-api","message":"Server started"}
{"timestamp":"2024-05-24T23:22:01Z","level":"INFO","service":"canary-api","message":"Routes initialized"}
```

**Detailed JSON** - Single JSON array/object:
```json
{
  "resourceSpans": [
    {
      "resource": {...},
      "scopeSpans": [...]
    }
  ]
}
```

---

## Verification: Is It Working?

### 1. Check OTel Collector is Running
```bash
docker ps | grep otel-collector
curl http://localhost:13133/  # Health check
```

### 2. Check Ports are Accessible
```bash
nc -zv localhost 4317  # OTLP gRPC
nc -zv localhost 4318  # OTLP HTTP
```

### 3. Send Test Telemetry
```bash
# Example: Send a test trace via OTLP HTTP
curl -X POST http://localhost:4318/v1/traces \
  -H "Content-Type: application/json" \
  -d '{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"test-service"}}]},"scopeSpans":[{"scope":{"name":"test"},"spans":[{"traceId":"12345678901234567890123456789012","spanId":"1234567890123456","name":"test-span","kind":1,"startTimeUnixNano":"1620000000000000000","endTimeUnixNano":"1620000001000000000"}]}]}]}'
```

### 4. Check Files Exist
```bash
ls -lh data/traces/traces.jsonl
ls -lh data/metrics/metrics.jsonl
ls -lh data/logs/logs.jsonl
```

### 5. Verify Content
```bash
tail -n 10 data/traces/traces.jsonl
cat data/metrics/metrics.jsonl | jq '.' | head -20
```

---

## Full Stack Benefits

While only OTel Collector is required for file exports, the full stack provides:

| Service | Benefit |
|---------|---------|
| **StatsD** | Alternative metrics ingestion for apps without OTLP support (if fixed) |
| **Prometheus** | Time-series storage for queries, alerts, and long-term retention |
| **Grafana** | Visual dashboards for real-time monitoring and trend analysis |
| **Jaeger** | Interactive trace exploration, dependency graphs, and latency analysis |
| **Filebeat** | Log correlation, enrichment, and advanced analytics |

**Trade-off**: More complexity and resource usage vs. richer observability features

---

## Summary: What's Required?

### For File-Based Exports ONLY

| Component | Required? | Why? |
|-----------|-----------|------|
| OTel Collector | ✅ **YES** | Processes telemetry and writes files |
| Volume mount `./data:/data` | ✅ **YES** | Makes files visible on host |
| OTLP endpoint (4317/4318) | ✅ **YES** | Receives telemetry from apps |
| File exporters in config | ✅ **YES** | Defines output file paths |
| **Everything else** | ❌ **NO** | Optional visualization/processing |

### For Full Observability Platform

Add the optional services for:
- Real-time dashboards (Prometheus + Grafana)
- Trace visualization (Jaeger)
- Alternative metrics ingestion (StatsD - if fixed)
- Advanced log processing (Filebeat)

---

## Next Steps

1. **Understand the architecture**: You now know what's required vs. optional
2. **Fix StatsD** (if needed): See [StatsD to OTLP Migration Guide](./statsd-to-otlp-migration.md)
3. **Simplify your setup** (optional): Use the minimal config if you only need file exports
4. **Integrate your application**: See [Application Integration Guide](./application-integration-guide.md)

---

## Related Documentation

- [StatsD to OTLP Migration Guide](./statsd-to-otlp-migration.md) - Migrate from StatsD to OTLP
- [Application Integration Guide](./application-integration-guide.md) - Integrate your app
- [Troubleshooting Guide](../examples/common/troubleshooting.md) - Common issues

---

**Last Updated**: 2026-02-13
