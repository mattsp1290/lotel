# StatsD to OTLP Migration Guide

## Overview

This guide explains:
1. Why StatsD is **currently broken** in this project
2. How to **fix** the StatsD integration (if needed)
3. Why you should **migrate to OTLP** instead
4. **How to migrate** your code from StatsD/DogStatsD to OTLP

---

## Current StatsD Issue ⚠️

### The Problem

**StatsD metrics sent to `localhost:8125` do NOT reach file exports.**

### Why It's Broken

**Current data flow:**
```
Application (StatsD/DogStatsD) → localhost:8125 → StatsD service
  ↓
StatsD service → otel-collector:2003 (Graphite protocol)
  ↓
❌ OTel Collector has NO receiver on port 2003
  ↓
Metrics are SILENTLY DROPPED
```

**The configuration mismatch:**

1. **StatsD service** (`docker/configs/statsd/config.js`):
   ```javascript
   graphiteHost: "otel-collector",
   graphitePort: 2003,  // ← Tries to send here
   ```

2. **OTel Collector** (`docker/configs/otel/otel-collector-config.yaml`):
   ```yaml
   receivers:
     statsd:
       endpoint: 0.0.0.0:8125  # ← Has StatsD receiver
     # NO Graphite/Carbon receiver on port 2003!
   ```

3. **Docker Compose** (`docker-compose.yml`):
   ```yaml
   statsd:
     ports:
       - "8125:8125/udp"  # ← StatsD service occupies port 8125
   ```

Result: OTel Collector's StatsD receiver on `0.0.0.0:8125` is unreachable from host because the StatsD service is using that port.

---

## How to Fix StatsD (If You Really Want It)

### Option 1: Remove StatsD Service, Use OTel's Built-in Receiver (RECOMMENDED)

This is simpler and removes a unnecessary service.

**1. Update `docker-compose.yml`:**

```yaml
services:
  otel-collector:
    ports:
      - "4317:4317"   # OTLP gRPC
      - "4318:4318"   # OTLP HTTP
      - "13133:13133" # Health check
      - "8889:8889"   # Prometheus metrics
      - "8125:8125/udp"  # ← ADD THIS: Expose OTel's StatsD receiver

  # statsd:  # ← REMOVE OR COMMENT OUT this entire service
  #   image: statsd/statsd:latest
  #   ...
```

**2. OTel Collector config stays the same:**

The `statsd` receiver is already configured in `otel-collector-config.yaml`:
```yaml
receivers:
  statsd:
    endpoint: 0.0.0.0:8125  # Already configured
    aggregation_interval: 10s
```

**3. Restart services:**
```bash
docker-compose down
docker-compose up -d
```

**4. Test:**
```python
from datadog import DogStatsd

statsd = DogStatsd(host='localhost', port=8125)
statsd.increment('test.metric', 1)
# Metrics now flow: App → OTel:8125 → File exports ✅
```

---

### Option 2: Add Graphite Receiver to OTel Collector

Keep the StatsD service and add a Graphite receiver.

**1. Update `docker/configs/otel/otel-collector-config.yaml`:**

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

  statsd:
    endpoint: 0.0.0.0:8125
    aggregation_interval: 10s

  carbon:  # ← ADD THIS: Graphite/Carbon receiver
    endpoint: 0.0.0.0:2003
    transport: tcp

service:
  pipelines:
    metrics:
      receivers: [otlp, statsd, prometheus, carbon]  # ← Add 'carbon'
      processors: [memory_limiter, batch, attributes, filter, resource]
      exporters: [file/metrics, prometheus, debug]
```

**2. Restart OTel Collector:**
```bash
docker-compose restart otel-collector
```

**3. Test:**
```python
from datadog import DogStatsd

statsd = DogStatsd(host='localhost', port=8125)
statsd.increment('test.metric', 1)
# Metrics now flow: App → StatsD:8125 → OTel:2003 (Graphite) → File exports ✅
```

---

## Why Migrate to OTLP Instead? (RECOMMENDED)

### StatsD Limitations

| Feature | StatsD/DogStatsD | OTLP |
|---------|------------------|------|
| **Metrics** | ✅ Yes | ✅ Yes |
| **Traces** | ❌ No | ✅ Yes |
| **Logs** | ❌ No | ✅ Yes |
| **Correlation** | ❌ No | ✅ Yes (trace/span IDs in metrics/logs) |
| **Metadata** | ⚠️ Limited (tags) | ✅ Rich (resource attributes) |
| **Data Types** | ⚠️ Basic (counter, gauge, timer, set) | ✅ Advanced (histograms, exponential histograms) |
| **Transport** | UDP only (fire-and-forget) | gRPC + HTTP (with acknowledgments) |
| **Error Handling** | ❌ Fire-and-forget | ✅ Retries and backoff |
| **Standardization** | ⚠️ Multiple dialects | ✅ CNCF standard |
| **This Project** | ❌ **BROKEN** | ✅ **WORKING** |

### OTLP Benefits

1. **Unified Observability**: One protocol for traces, metrics, AND logs
2. **Automatic Correlation**: Metrics recorded inside trace spans include trace/span IDs
3. **Richer Metadata**: Resource attributes (service name, version, environment, host)
4. **Better Data Types**: Native histograms, exponential histograms, summaries
5. **Industry Standard**: OpenTelemetry is the CNCF standard for observability
6. **Already Working**: OTLP is configured and working in this project

### Example: Correlation (OTLP Only)

```python
from opentelemetry import trace, metrics

tracer = trace.get_tracer(__name__)
meter = metrics.get_meter(__name__)
request_counter = meter.create_counter("http.server.requests")

# Metrics recorded inside a span automatically include trace context
with tracer.start_as_current_span("handle_request") as span:
    request_counter.add(1, {"http.route": "/api/users"})
    # This metric now has:
    # - trace_id: 12345678901234567890123456789012
    # - span_id: 1234567890123456
    # You can correlate slow metrics with specific traces!
```

**With StatsD, this correlation is impossible.** You'd need to manually extract and tag trace IDs, which is error-prone and inefficient.

---

## Migration Steps

### Step 1: Install OpenTelemetry Libraries

**Python:**
```bash
# Remove StatsD
pip uninstall statsd datadog

# Install OpenTelemetry (if not already installed)
pip install opentelemetry-api \
            opentelemetry-sdk \
            opentelemetry-exporter-otlp-proto-http \
            opentelemetry-instrumentation
```

**Node.js:**
```bash
# Remove StatsD
npm uninstall node-statsd hot-shots

# Install OpenTelemetry (if not already installed)
npm install @opentelemetry/api \
            @opentelemetry/sdk-node \
            @opentelemetry/auto-instrumentations-node \
            @opentelemetry/exporter-metrics-otlp-http
```

---

### Step 2: Update Initialization Code

#### Python Example

**Before (StatsD):**
```python
import statsd

# Initialize StatsD client
STATSD_HOST = os.getenv("STATSD_HOST", "localhost")
STATSD_PORT = int(os.getenv("STATSD_PORT", "8125"))

statsd_client = statsd.StatsClient(
    host=STATSD_HOST,
    port=STATSD_PORT,
    prefix=f'{SERVICE_NAME.replace("-", "_")}'
)
```

**After (OTLP):**
```python
from opentelemetry import metrics
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
from opentelemetry.sdk.resources import Resource

# Create resource with service metadata
resource = Resource.create({
    "service.name": SERVICE_NAME,
    "service.version": SERVICE_VERSION,
    "deployment.environment": os.getenv("ENVIRONMENT", "development")
})

# Setup metrics provider
metric_exporter = OTLPMetricExporter(
    endpoint=os.getenv("OTLP_ENDPOINT_METRICS", "http://localhost:4318/v1/metrics")
)
metric_reader = PeriodicExportingMetricReader(
    metric_exporter,
    export_interval_millis=10000  # Export every 10 seconds
)
metric_provider = MeterProvider(
    resource=resource,
    metric_readers=[metric_reader]
)
metrics.set_meter_provider(metric_provider)

# Get meter instance
meter = metrics.get_meter(__name__, SERVICE_VERSION)
```

#### Node.js Example

**Before (StatsD):**
```javascript
const StatsD = require('node-statsd');

const statsd = new StatsD({
  host: process.env.STATSD_HOST || 'localhost',
  port: process.env.STATSD_PORT || 8125,
  prefix: 'canary.'
});
```

**After (OTLP):**
```javascript
const { MeterProvider, PeriodicExportingMetricReader } = require('@opentelemetry/sdk-metrics');
const { OTLPMetricExporter } = require('@opentelemetry/exporter-metrics-otlp-http');
const { Resource } = require('@opentelemetry/resources');

// Create resource with service metadata
const resource = new Resource({
  'service.name': process.env.SERVICE_NAME || 'canary-api',
  'service.version': process.env.SERVICE_VERSION || '1.0.0',
  'deployment.environment': process.env.ENVIRONMENT || 'development'
});

// Setup metrics provider
const metricExporter = new OTLPMetricExporter({
  url: process.env.OTLP_ENDPOINT_METRICS || 'http://localhost:4318/v1/metrics'
});

const metricReader = new PeriodicExportingMetricReader({
  exporter: metricExporter,
  exportIntervalMillis: 10000  // Export every 10 seconds
});

const meterProvider = new MeterProvider({
  resource: resource,
  readers: [metricReader]
});

// Get meter instance
const meter = meterProvider.getMeter('canary-api', '1.0.0');
```

---

### Step 3: Define Metric Instruments

Create metric instruments once at module level.

#### Python

**Before (StatsD):**
```python
# StatsD: No setup needed, just call methods
statsd_client.incr('requests')
statsd_client.timing('request_duration', 42)
statsd_client.gauge('active_connections', 10)
```

**After (OTLP):**
```python
# OTLP: Define instruments once
request_counter = meter.create_counter(
    name="http.server.requests",
    description="Total HTTP requests",
    unit="1"
)

request_duration = meter.create_histogram(
    name="http.server.request.duration",
    description="HTTP request duration",
    unit="ms"
)

active_connections = meter.create_up_down_counter(
    name="http.server.active_connections",
    description="Active HTTP connections",
    unit="1"
)
```

#### Node.js

**Before (StatsD):**
```javascript
// StatsD: No setup needed
statsd.increment('requests');
statsd.timing('request_duration', 42);
statsd.gauge('active_connections', 10);
```

**After (OTLP):**
```javascript
// OTLP: Define instruments once
const requestCounter = meter.createCounter('http.server.requests', {
  description: 'Total HTTP requests',
  unit: '1'
});

const requestDuration = meter.createHistogram('http.server.request.duration', {
  description: 'HTTP request duration',
  unit: 'ms'
});

const activeConnections = meter.createUpDownCounter('http.server.active_connections', {
  description: 'Active HTTP connections',
  unit: '1'
});
```

---

### Step 4: Update Metric Recording

#### Python

**Before (StatsD):**
```python
# Increment counter
statsd_client.incr('requests', tags=['endpoint:/chirp', 'method:GET'])

# Record timing
statsd_client.timing('request_duration', 42, tags=['endpoint:/chirp', 'method:GET'])

# Set gauge
statsd_client.gauge('active_connections', 10, tags=['endpoint:/chirp'])

# Decrement gauge
statsd_client.decr('active_connections', tags=['endpoint:/chirp'])
```

**After (OTLP):**
```python
# Increment counter
request_counter.add(1, {"http.route": "/chirp", "http.method": "GET"})

# Record histogram
request_duration.record(42, {"http.route": "/chirp", "http.method": "GET"})

# Increment gauge
active_connections.add(1, {"http.route": "/chirp"})

# Decrement gauge
active_connections.add(-1, {"http.route": "/chirp"})
```

#### Node.js

**Before (StatsD):**
```javascript
// Increment counter
statsd.increment('requests', 1, ['endpoint:/chirp', 'method:GET']);

// Record timing
statsd.timing('request_duration', 42, ['endpoint:/chirp', 'method:GET']);

// Set gauge
statsd.gauge('active_connections', 10, ['endpoint:/chirp']);
```

**After (OTLP):**
```javascript
// Increment counter
requestCounter.add(1, { 'http.route': '/chirp', 'http.method': 'GET' });

// Record histogram
requestDuration.record(42, { 'http.route': '/chirp', 'http.method': 'GET' });

// Set gauge
activeConnections.add(1, { 'http.route': '/chirp' });
```

---

### Step 5: Use Semantic Conventions (Best Practice)

OpenTelemetry defines **semantic conventions** - standardized naming and attributes for common scenarios.

**Benefits:**
- Tools automatically recognize metrics
- Consistent naming across services
- Better observability insights

#### HTTP Server Metrics

**Instead of custom names:**
```python
# ❌ Custom names (harder for tools to understand)
statsd_client.incr('api_requests')
statsd_client.timing('response_time', 42)
```

**Use semantic conventions:**
```python
# ✅ Semantic conventions (standard names)
request_counter = meter.create_counter(
    "http.server.requests",  # ← Standard name
    unit="1"
)
request_duration = meter.create_histogram(
    "http.server.request.duration",  # ← Standard name
    unit="ms"
)

# Use standard attributes
request_counter.add(1, {
    "http.method": "GET",      # ← Standard attribute
    "http.route": "/api/users", # ← Standard attribute
    "http.status_code": 200     # ← Standard attribute
})
```

**See**: [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/)

---

## Complete Migration Example: Python FastAPI

This shows how to migrate the `examples/python-fastapi/app.py` from StatsD to OTLP.

### Before (StatsD)

```python
# app.py (excerpt)
import statsd

# Initialize StatsD
STATSD_HOST = os.getenv("STATSD_HOST", "localhost")
STATSD_PORT = int(os.getenv("STATSD_PORT", "8125"))
statsd_client = statsd.StatsClient(
    host=STATSD_HOST,
    port=STATSD_PORT,
    prefix='canary_api'
)

@app.get("/chirp")
async def chirp():
    start_time = time.time()

    # Increment request counter
    statsd_client.incr('requests', tags=['endpoint:chirp', 'method:GET'])

    # ... business logic ...

    # Record duration
    elapsed = (time.time() - start_time) * 1000
    statsd_client.timing('request_duration', elapsed, tags=['endpoint:chirp'])

    return {"chirp": "tweet!"}

@app.post("/nest")
async def create_nest(nest: Nest):
    statsd_client.incr('requests', tags=['endpoint:nest', 'method:POST'])
    statsd_client.gauge('nest_count', len(nest_storage))
    statsd_client.incr('nests_created', tags=[f'type:{nest.type}'])
    return {"status": "created"}
```

### After (OTLP)

```python
# app.py (excerpt)
from opentelemetry import metrics
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
from opentelemetry.sdk.resources import Resource

# Initialize OTLP metrics (in init_telemetry() function)
def init_telemetry():
    """Initialize OpenTelemetry tracing AND metrics"""
    # Create resource with service info
    resource = Resource.create({
        "service.name": SERVICE_NAME,
        "service.version": SERVICE_VERSION,
        "deployment.environment": os.getenv("ENVIRONMENT", "development")
    })

    # Setup traces (existing code)
    # ... trace setup ...

    # Setup metrics
    metric_exporter = OTLPMetricExporter(
        endpoint=os.getenv("OTLP_ENDPOINT_METRICS", "http://localhost:4318/v1/metrics")
    )
    metric_reader = PeriodicExportingMetricReader(
        metric_exporter,
        export_interval_millis=10000
    )
    metric_provider = MeterProvider(
        resource=resource,
        metric_readers=[metric_reader]
    )
    metrics.set_meter_provider(metric_provider)

    return provider, metric_provider

# Get meter and create instruments (at module level)
meter = metrics.get_meter(__name__, SERVICE_VERSION)

request_counter = meter.create_counter(
    "http.server.requests",
    description="Total HTTP requests",
    unit="1"
)
request_duration = meter.create_histogram(
    "http.server.request.duration",
    description="HTTP request duration",
    unit="ms"
)
nest_count_gauge = meter.create_up_down_counter(
    "app.nests.count",
    description="Current number of nests",
    unit="1"
)
nest_created_counter = meter.create_counter(
    "app.nests.created",
    description="Total nests created",
    unit="1"
)

# Use in endpoints
@app.get("/chirp")
async def chirp():
    start_time = time.time()

    # Increment request counter
    request_counter.add(1, {
        "http.route": "/chirp",
        "http.method": "GET"
    })

    # ... business logic ...

    # Record duration
    elapsed = (time.time() - start_time) * 1000
    request_duration.record(elapsed, {
        "http.route": "/chirp",
        "http.method": "GET"
    })

    return {"chirp": "tweet!"}

@app.post("/nest")
async def create_nest(nest: Nest):
    request_counter.add(1, {
        "http.route": "/nest",
        "http.method": "POST"
    })
    nest_count_gauge.add(1)
    nest_created_counter.add(1, {"nest.type": nest.type})
    return {"status": "created"}
```

---

## Migration Checklist

- [ ] Install OpenTelemetry libraries
- [ ] Remove StatsD/DogStatsD libraries
- [ ] Update initialization code to use OTLP
- [ ] Define metric instruments (counter, histogram, gauge)
- [ ] Replace `statsd_client.incr()` with `counter.add()`
- [ ] Replace `statsd_client.timing()` with `histogram.record()`
- [ ] Replace `statsd_client.gauge()` with `up_down_counter.add()`
- [ ] Update metric names to semantic conventions
- [ ] Update attributes (tags → attributes dict)
- [ ] Test metrics appear in `./data/metrics/metrics.jsonl`
- [ ] Remove StatsD service from `docker-compose.yml` (optional)
- [ ] Update environment variables (remove `STATSD_*`, add `OTLP_ENDPOINT_METRICS`)

---

## Testing Your Migration

### 1. Start the Stack
```bash
docker-compose up -d
```

### 2. Send Test Metrics

**Python:**
```bash
python -c "
from opentelemetry import metrics
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter

exporter = OTLPMetricExporter(endpoint='http://localhost:4318/v1/metrics')
reader = PeriodicExportingMetricReader(exporter)
provider = MeterProvider(metric_readers=[reader])
metrics.set_meter_provider(provider)

meter = metrics.get_meter('test')
counter = meter.create_counter('test.requests')
counter.add(1, {'endpoint': '/test'})
print('Metric sent!')
"
```

### 3. Verify Files
```bash
# Wait a few seconds for export interval
sleep 15

# Check file exists and has content
ls -lh data/metrics/metrics.jsonl
tail -20 data/metrics/metrics.jsonl
```

### 4. Check Metric Content
```bash
# Pretty-print last metric
tail -1 data/metrics/metrics.jsonl | jq '.'
```

You should see your metrics with resource attributes:
```json
{
  "resourceMetrics": [{
    "resource": {
      "attributes": [
        {"key": "service.name", "value": {"stringValue": "test"}},
        {"key": "telemetry.sdk.name", "value": {"stringValue": "opentelemetry"}}
      ]
    },
    "scopeMetrics": [{
      "metrics": [{
        "name": "test.requests",
        "sum": {
          "dataPoints": [{
            "asInt": "1",
            "attributes": [
              {"key": "endpoint", "value": {"stringValue": "/test"}}
            ]
          }]
        }
      }]
    }]
  }]
}
```

---

## Troubleshooting

### Metrics Not Appearing in Files

1. **Check OTel Collector is running:**
   ```bash
   curl http://localhost:13133/
   ```

2. **Check OTLP endpoint is accessible:**
   ```bash
   nc -zv localhost 4318
   ```

3. **Check OTel Collector logs:**
   ```bash
   docker-compose logs otel-collector | grep -i metric
   ```

4. **Verify export interval has passed:**
   ```bash
   # Wait for export interval (default 10 seconds)
   sleep 15
   ```

### Metrics Have Wrong Format

**Check your endpoint URL:**
```python
# ✅ Correct
OTLPMetricExporter(endpoint="http://localhost:4318/v1/metrics")

# ❌ Wrong (missing /v1/metrics path)
OTLPMetricExporter(endpoint="http://localhost:4318")
```

### Resource Attributes Missing

**Ensure you create a Resource:**
```python
from opentelemetry.sdk.resources import Resource

resource = Resource.create({
    "service.name": "my-service",
    "service.version": "1.0.0"
})

metric_provider = MeterProvider(
    resource=resource,  # ← Don't forget this!
    metric_readers=[reader]
)
```

---

## Summary

| Aspect | StatsD | OTLP |
|--------|--------|------|
| **Status in this project** | ❌ Broken | ✅ Working |
| **Metrics** | ✅ Yes | ✅ Yes |
| **Traces** | ❌ No | ✅ Yes |
| **Logs** | ❌ No | ✅ Yes |
| **Correlation** | ❌ No | ✅ Automatic |
| **Metadata** | ⚠️ Limited | ✅ Rich |
| **Setup effort** | Low | Medium |
| **Long-term value** | Low | High |
| **Recommendation** | ❌ Avoid | ✅ **Use this!** |

**Bottom line**: Migrate to OTLP for unified observability and better features.

---

## Next Steps

1. ✅ Understand why StatsD is broken
2. ✅ Decide: Fix StatsD or migrate to OTLP
3. 🔄 Follow migration steps in this guide
4. ✅ Test metrics appear in `./data/metrics/metrics.jsonl`
5. ✅ Remove StatsD service from `docker-compose.yml`
6. 📚 Learn more: [OpenTelemetry Python Docs](https://opentelemetry.io/docs/languages/python/)

---

## Related Documentation

- [File-Based Architecture](./file-based-architecture.md) - Understand required vs. optional services
- [Application Integration Guide](./application-integration-guide.md) - Integrate OTLP into your app
- [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/) - Standard metric names

---

**Last Updated**: 2026-02-13
