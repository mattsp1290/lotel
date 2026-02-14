# OpenTelemetry Quick Reference

## What's Required for File Exports?

**Only the OTel Collector is required.** Everything else is optional.

```
Application → OTel Collector (ports 4317/4318) → ./data/ files
```

## Component Status

| Component | Required? | Status | Purpose |
|-----------|-----------|--------|---------|
| **OTel Collector** | ✅ YES | ✅ Working | Receives and exports telemetry |
| StatsD | ❌ NO | ⚠️ Broken | Alternative metrics (use OTLP instead) |
| Prometheus | ❌ NO | ⚠️ Optional | Time-series storage |
| Grafana | ❌ NO | ⚠️ Optional | Dashboards |
| Jaeger | ❌ NO | ⚠️ Optional | Trace visualization |
| Filebeat | ❌ NO | ⚠️ Optional | Log post-processing |

## Telemetry Endpoints

**Send telemetry to these ports:**

| Protocol | Port | URL |
|----------|------|-----|
| OTLP gRPC | 4317 | `localhost:4317` |
| OTLP HTTP | 4318 | `http://localhost:4318` |
| StatsD | 8125 | ⚠️ Broken - use OTLP instead |

**Health check:** `http://localhost:13133/`

## File Output Locations

```
./data/
├── traces/
│   ├── traces.jsonl         # Line-delimited JSON
│   └── traces_detailed.json # Single JSON file
├── metrics/
│   └── metrics.jsonl        # Line-delimited JSON
└── logs/
    └── logs.jsonl           # Line-delimited JSON
```

## OTLP Code Examples

### Python

```python
from opentelemetry import trace, metrics
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
from opentelemetry.sdk.resources import Resource

# Create resource
resource = Resource.create({"service.name": "my-service"})

# Setup tracing
trace.set_tracer_provider(TracerProvider(resource=resource))
trace.get_tracer_provider().add_span_processor(
    BatchSpanProcessor(OTLPSpanExporter(
        endpoint="http://localhost:4318/v1/traces"
    ))
)

# Setup metrics
metrics.set_meter_provider(MeterProvider(
    resource=resource,
    metric_readers=[PeriodicExportingMetricReader(
        OTLPMetricExporter(endpoint="http://localhost:4318/v1/metrics")
    )]
))

# Use
tracer = trace.get_tracer(__name__)
meter = metrics.get_meter(__name__)

with tracer.start_as_current_span("operation"):
    counter = meter.create_counter("requests")
    counter.add(1, {"endpoint": "/api"})
```

### Node.js

```javascript
const { NodeSDK } = require('@opentelemetry/sdk-node');
const { OTLPTraceExporter } = require('@opentelemetry/exporter-trace-otlp-http');
const { OTLPMetricExporter } = require('@opentelemetry/exporter-metrics-otlp-http');

const sdk = new NodeSDK({
  serviceName: 'my-service',
  traceExporter: new OTLPTraceExporter({
    url: 'http://localhost:4318/v1/traces'
  }),
  metricReader: new PeriodicExportingMetricReader({
    exporter: new OTLPMetricExporter({
      url: 'http://localhost:4318/v1/metrics'
    })
  })
});

sdk.start();
```

## Verification Commands

```bash
# Check OTel Collector health
curl http://localhost:13133/

# Check ports are open
nc -zv localhost 4317  # OTLP gRPC
nc -zv localhost 4318  # OTLP HTTP

# View recent traces
tail -20 data/traces/traces.jsonl

# View recent metrics
tail -20 data/metrics/metrics.jsonl | jq '.'

# Check file sizes
ls -lh data/traces/ data/metrics/ data/logs/

# Full health check
./scripts/verification/bash/check_telemetry_health.sh
```

## Common Issues

### No data in files?

1. Check OTel Collector is running: `docker ps | grep otel`
2. Check health endpoint: `curl http://localhost:13133/`
3. Verify OTLP endpoint is correct: `http://localhost:4318`
4. Check OTel logs: `docker-compose logs otel-collector`

### StatsD metrics not appearing?

**StatsD is broken in this project.** Migrate to OTLP instead.

See: [StatsD to OTLP Migration Guide](./statsd-to-otlp-migration.md)

### Permission denied on data files?

```bash
chmod -R 755 data/
```

## Minimal Setup

**To run ONLY file exports (no visualization):**

```yaml
# docker-compose-minimal.yml
version: '3.8'
services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    command: ["--config=/etc/otel-collector-config.yaml"]
    volumes:
      - ./docker/configs/otel/otel-collector-config-minimal.yaml:/etc/otel-collector-config.yaml
      - ./data:/data
    ports:
      - "4317:4317"
      - "4318:4318"
      - "13133:13133"
```

Then use `otel-collector-config-minimal.yaml` (50 lines vs. 137 lines full config).

## Documentation

- **[File-Based Architecture](./file-based-architecture.md)** - What's required vs. optional
- **[StatsD to OTLP Migration](./statsd-to-otlp-migration.md)** - Fix StatsD or migrate to OTLP
- **[Application Integration Guide](./application-integration-guide.md)** - Integrate your app
- **[Troubleshooting](../examples/common/troubleshooting.md)** - Common issues

## Semantic Conventions

Use standard metric/span names for better tool integration:

**HTTP Server:**
- `http.server.requests` - Request counter
- `http.server.request.duration` - Request duration histogram
- `http.server.active_requests` - Active requests gauge

**Attributes:**
- `http.method` - HTTP method (GET, POST, etc.)
- `http.route` - Route pattern (/api/users/:id)
- `http.status_code` - Response status (200, 404, etc.)

**See:** [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/)

---

**Last Updated**: 2026-02-13
