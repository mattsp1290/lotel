# 🦅 Agent Observability Verifier

A comprehensive Docker-based telemetry verification environment designed for AI agents to validate traces, metrics, and logs are properly collected from any application. Features OpenTelemetry Collector, StatsD, Prometheus, Grafana, Jaeger, and Filebeat with file-based exports for integration testing.

## 🚀 Quick Start

### Start the Stack
```bash
# 1. Setup environment (first time only)
./scripts/setup/setup-telemetry-env.sh

# 2. Start all services
./scripts/setup/start-telemetry-stack.sh

# 3. Verify everything is working
./scripts/verification/bash/check_telemetry_health.sh
```

### Access Dashboards
- **Jaeger (Traces)**: http://localhost:16686
- **Grafana (Metrics)**: http://localhost:3000 (admin/admin)
- **Prometheus**: http://localhost:9090
- **OTel Collector Health**: http://localhost:13133

### Stop the Stack
```bash
./scripts/setup/stop-telemetry-stack.sh

# Or with data cleanup
./scripts/setup/stop-telemetry-stack.sh --clean-data
```

## 📋 Overview

This environment provides a complete local telemetry verification stack that:

- ✅ **Collects traces, metrics, and logs** from any application via OpenTelemetry
- ✅ **Exports data to files** for integration testing and AI agent verification
- ✅ **Provides real-time visualization** with Grafana and Jaeger (optional)
- ⚠️ **StatsD metrics support** (currently broken - migrate to OTLP recommended)
- ✅ **Processes logs** with Filebeat for correlation and analysis (optional)
- ✅ **Runs entirely in Docker** for consistent environments
- ✅ **Includes verification scripts** in Python, Go, and Bash
- ✅ **AI Agent Optimized** - designed for automated observability verification

**Important**: Only the **OTel Collector** is required for file-based exports. All other services (StatsD, Prometheus, Grafana, Jaeger, Filebeat) are optional. See [Architecture Documentation](docs/file-based-architecture.md) for details.

## 🏗️ Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│ Your Application│───▶│ OpenTelemetry    │───▶│ File Exports    │
│  (Canary API)   │    │ Collector        │    │ (JSON/CSV/JSONL)│
└─────────────────┘    └──────────────────┘    └─────────────────┘
         │                       │                       │
         │                       ▼                       ▼
         │              ┌─────────────────┐    ┌─────────────────┐
         │              │   Prometheus    │    │    Filebeat     │
         │              │   (Metrics)     │    │ (Log Processing)│
         │              └─────────────────┘    └─────────────────┘
         │                       │                       │
         ▼                       ▼                       ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     StatsD      │    │     Grafana     │    │ Processed Logs  │
│   (UDP:8125)    │    │  (Dashboards)   │    │   (Files)       │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │
         ▼                       │
┌─────────────────┐              │
│     Jaeger      │◀─────────────┘
│   (Tracing)     │
└─────────────────┘
```

## 📁 Directory Structure

```
local-otel/
├── docker/
│   ├── docker-compose.yml          # Main orchestration file
│   └── configs/                    # Service configurations
│       ├── otel/                   # OpenTelemetry Collector config
│       ├── statsd/                 # StatsD server config
│       ├── prometheus/             # Prometheus config
│       ├── grafana/                # Grafana provisioning
│       └── filebeat/               # Filebeat config
├── data/                           # Telemetry data exports
│   ├── traces/                     # Trace files (JSON, JSONL)
│   ├── metrics/                    # Metric files (JSON, Prometheus)
│   ├── logs/                       # Log files (JSON, text)
│   └── processed/                  # Filebeat processed logs
├── scripts/
│   ├── setup/                      # Environment setup scripts
│   ├── verification/               # Verification scripts
│   │   ├── python/                 # Python verification scripts
│   │   ├── go/                     # Go verification programs
│   │   └── bash/                   # Bash verification scripts
│   └── automation/                 # Additional automation
└── docs/
    └── application-integration-guide.md  # Application integration guide
```

## 🔧 Services

| Service | Port | Purpose | Required? | Health Check |
|---------|------|---------|-----------|--------------|
| **OTel Collector** | 4317 (gRPC), 4318 (HTTP) | Central telemetry collection + disk export | ✅ **YES** | http://localhost:13133 |
| **StatsD** | 8125 (UDP), 8126 (Admin) | Alternative metrics input | ❌ NO (broken) | http://localhost:8126 |
| **Prometheus** | 9090 | Metrics storage and querying | ❌ NO | http://localhost:9090/-/healthy |
| **Grafana** | 3000 | Visualization dashboards | ❌ NO | http://localhost:3000/api/health |
| **Jaeger** | 16686 (UI), 14250 (gRPC) | Distributed tracing visualization | ❌ NO | http://localhost:16686 |
| **Filebeat** | 5066 (HTTP) | Log post-processing | ❌ NO | Internal health checks |

**Note**: For file-based exports only, you need just the OTel Collector. See [docs/file-based-architecture.md](docs/file-based-architecture.md).

## 📊 Telemetry Endpoints

### For Application Integration

Send telemetry data to these endpoints:

- **OTLP Traces/Metrics/Logs (gRPC)**: `localhost:4317` ✅ **RECOMMENDED**
- **OTLP Traces/Metrics/Logs (HTTP)**: `localhost:4318` ✅ **RECOMMENDED**
- **StatsD Metrics (UDP)**: `localhost:8125` ⚠️ **BROKEN** - See [migration guide](docs/statsd-to-otlp-migration.md)

**OTLP (OpenTelemetry Protocol)** is the recommended and working protocol. StatsD integration is currently broken due to a configuration mismatch. See the [StatsD to OTLP Migration Guide](docs/statsd-to-otlp-migration.md) for migration instructions or fixes.

### Example Usage

```python
# Python OpenTelemetry example
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

# Setup tracing
trace.set_tracer_provider(TracerProvider())
tracer = trace.get_tracer(__name__)

# Configure OTLP exporter - use standard ports
otlp_exporter = OTLPSpanExporter(
    endpoint="http://localhost:4318/v1/traces",
)
span_processor = BatchSpanProcessor(otlp_exporter)
trace.get_tracer_provider().add_span_processor(span_processor)

# Create spans
with tracer.start_as_current_span("canary_chirp"):
    # Your application logic here
    pass
```

```javascript
// Node.js StatsD example
const StatsD = require('node-statsd');
const client = new StatsD({
  host: 'localhost',
  port: 8125,
  prefix: 'canary.'
});

// Send metrics
client.increment('requests_total', 1, {endpoint: '/chirp'});
client.timing('response_duration', 42, {endpoint: '/chirp'});
```

## 📄 File Exports

All telemetry data is exported to files for integration testing:

### Traces
- `data/traces/traces.jsonl` - JSONL format for streaming
- `data/traces/traces_detailed.json` - Full JSON with all details

### Metrics
- `data/metrics/metrics.jsonl` - JSONL format
- `data/metrics/metrics.prom` - Prometheus exposition format

### Logs
- `data/logs/logs.jsonl` - Structured JSON logs
- `data/processed/filebeat-processed*` - Filebeat processed logs

## 🧪 Verification Scripts

### Bash Scripts
```bash
# Health check all services
./scripts/verification/bash/check_telemetry_health.sh

# Validate file outputs
./scripts/verification/bash/validate_file_outputs.sh
```

### Python Scripts
```bash
# Test metrics pipeline
python3 scripts/verification/python/test_metrics_pipeline.py

# Verify Filebeat processing
python3 scripts/verification/python/verify_filebeat.py

# Validate traces
python3 scripts/verification/python/trace_validator.py
```

### Go Programs
```bash
# Load test metrics
go run scripts/verification/go/metrics_load_test.go

# Generate test traces
go run scripts/verification/go/trace_generator.go

# Parse logs for performance
go run scripts/verification/go/log_parser.go
```

## 🔍 Monitoring and Debugging

### View Real-time Logs
```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f otel-collector
docker-compose logs -f statsd
```

### Check Container Status
```bash
docker-compose ps
```

### Inspect Data Files
```bash
# Recent traces
ls -la data/traces/
tail -f data/traces/traces.jsonl

# Recent metrics
ls -la data/metrics/
tail -f data/metrics/metrics.jsonl

# Processed logs
ls -la data/processed/
```

## 🛠️ Management Scripts

### Start/Stop Services
```bash
# Start all services
./scripts/setup/start-telemetry-stack.sh

# Stop all services
./scripts/setup/stop-telemetry-stack.sh

# Stop and clean data
./scripts/setup/stop-telemetry-stack.sh --clean-data

# Full reset
./scripts/setup/stop-telemetry-stack.sh --clean-data --remove-volumes
./scripts/setup/setup-telemetry-env.sh
```

## 🎯 Examples

The `examples/` directory contains fully instrumented applications demonstrating best practices:

### Available Examples

#### 🐍 Python FastAPI Example
- **Location**: `examples/python-fastapi/`
- **Features**: OpenTelemetry auto-instrumentation, StatsD metrics, structured JSON logging
- **Endpoints**: `/chirp` (health), `/nest` (create), `/flock` (list)
- **Quick Start**:
  ```bash
  cd examples/python-fastapi
  docker-compose up --build
  python test_telemetry.py  # Verify telemetry
  ```

### 📚 Common Documentation
- **[Telemetry Patterns](examples/common/telemetry-patterns.md)** - Universal patterns for traces, metrics, and logs
- **[Troubleshooting Guide](examples/common/troubleshooting.md)** - Diagnose and fix common telemetry issues
- **[Performance Tips](examples/common/performance-tips.md)** - Optimize telemetry overhead

## 🤖 AI Agent Usage

This environment is specifically designed for AI agents to add and verify observability:

### For AI Agents
1. Read `AGENT_QUICKSTART.md` for a concise overview
2. Use the verification scripts to confirm telemetry is working
3. Follow the patterns in `examples/` for different languages
4. Check `data/` directories for exported telemetry data

### Common AI Agent Tasks
```bash
# Add observability to a web service
# 1. Instrument the code (see examples/)
# 2. Start the telemetry stack
# 3. Run the application
# 4. Verify with:
./scripts/verification/bash/check_telemetry_health.sh

# Debug missing telemetry
# 1. Check service health
# 2. Verify endpoints are correct
# 3. Check data files for output
ls -la data/traces/ data/metrics/ data/logs/
```

## 🐦 Example: Canary API Integration

Our example "Canary API" demonstrates common web service patterns:

### Endpoints
- `/chirp` - Quick health check endpoint
- `/nest` - Data creation endpoint  
- `/flock` - Batch operations endpoint

### Metrics
- `canary_requests_total` - Request counter by method/endpoint/status
- `canary_response_duration_seconds` - Response time histogram
- `canary_active_connections` - Current connection gauge
- `canary_error_rate` - Error percentage by endpoint

## 🐛 Troubleshooting

### Common Issues

**Services not starting:**
```bash
# Check Docker is running
docker info

# Check port conflicts
lsof -i :4317 -i :4318 -i :8125 -i :9090 -i :3000

# Reset everything
./scripts/setup/stop-telemetry-stack.sh --clean-data --remove-volumes
./scripts/setup/setup-telemetry-env.sh
```

**No telemetry data:**
```bash
# Check OpenTelemetry Collector health
curl http://localhost:13133/

# Check logs for errors
docker-compose logs otel-collector | grep -i "error"

# Verify configuration
./scripts/verification/bash/check_telemetry_health.sh
```

**Permission issues:**
```bash
# Fix data directory permissions
chmod -R 755 data/
```

### Getting Help

1. Run the health check script for detailed diagnostics
2. Check service logs for specific error messages
3. Verify all configuration files are present and valid
4. Ensure Docker has sufficient resources (4GB RAM minimum)

## 🚀 Performance

The telemetry environment is optimized for development use:

- **Low latency**: Sub-second data processing
- **High throughput**: Handles thousands of metrics/traces per second
- **Minimal overhead**: <5% performance impact on your application
- **Efficient storage**: Compressed file exports with rotation

## 📚 Documentation

### Start Here

- **[Quick Reference](docs/QUICK_REFERENCE.md)** - Essential commands and code examples
- **[Known Issues](docs/KNOWN_ISSUES.md)** - ⚠️ Current issues (StatsD is broken)

### Core Documentation

- **[File-Based Architecture](docs/file-based-architecture.md)** - What's required vs. optional for file exports
- **[StatsD to OTLP Migration Guide](docs/statsd-to-otlp-migration.md)** - Fix StatsD or migrate to OTLP (recommended)
- **[Application Integration Guide](docs/application-integration-guide.md)** - Integrate your application

### Example Documentation

- **[Telemetry Patterns](examples/common/telemetry-patterns.md)** - Universal patterns for traces, metrics, and logs
- **[Troubleshooting Guide](examples/common/troubleshooting.md)** - Diagnose and fix common issues
- **[Performance Tips](examples/common/performance-tips.md)** - Optimize telemetry overhead

### Configuration Files

- **[Full OTel Config](docker/configs/otel/otel-collector-config.yaml)** - Production-ready configuration (137 lines)
- **[Minimal OTel Config](docker/configs/otel/otel-collector-config-minimal.yaml)** - File exports only (50 lines)

### Implementation Details

- **[Implementation Summary](docs/IMPLEMENTATION_SUMMARY.md)** - Documentation implementation notes

## 🔮 Future Enhancements

- [ ] Language-specific instrumentation examples
- [ ] Pre-built Grafana dashboards for common patterns
- [ ] Cloud provider migration guides
- [ ] Performance regression testing suite
- [ ] Multi-service distributed tracing examples
- [ ] Advanced log correlation features

## 📝 License

This project is open source and available under the [MIT License](LICENSE).
