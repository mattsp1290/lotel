# Known Issues

## ⚠️ StatsD Integration is Broken

**Status**: ❌ BROKEN
**Severity**: Medium
**Impact**: StatsD/DogStatsD metrics sent to `localhost:8125` do not reach file exports
**Workaround**: Use OTLP instead (recommended)
**Fix**: See [StatsD to OTLP Migration Guide](./statsd-to-otlp-migration.md)

### Description

The StatsD service is misconfigured and metrics sent to it are silently dropped.

**Current broken flow:**
```
Application → localhost:8125 → StatsD service
  ↓
StatsD service → otel-collector:2003 (Graphite protocol)
  ↓
❌ OTel Collector has NO Graphite receiver on port 2003
  ↓
Metrics are LOST
```

### Root Cause

1. **StatsD service** forwards metrics to `otel-collector:2003` via Graphite protocol
2. **OTel Collector** has NO Graphite/Carbon receiver configured on port 2003
3. **Port conflict**: StatsD service occupies port 8125, blocking OTel's built-in StatsD receiver

**Configuration files involved:**
- `docker/configs/statsd/config.js` - Lines 24-25 (graphiteHost, graphitePort)
- `docker/configs/otel/otel-collector-config.yaml` - Missing Carbon receiver
- `docker-compose.yml` - Lines 36 (StatsD port mapping blocks OTel's receiver)

### Impact

**Who is affected:**
- Applications using StatsD client libraries
- Applications using DogStatsD (DataDog StatsD) libraries
- Anyone sending metrics to `localhost:8125` via UDP

**What works:**
- ✅ OTLP metrics (gRPC port 4317, HTTP port 4318)
- ✅ Traces via OTLP
- ✅ Logs via OTLP
- ✅ All file exports for OTLP telemetry

**What's broken:**
- ❌ StatsD metrics sent to `localhost:8125`
- ❌ DogStatsD metrics sent to `localhost:8125`

### Fix Options

#### Option 1: Migrate to OTLP (RECOMMENDED)

**Why:**
- ✅ Already working
- ✅ Supports traces, metrics, AND logs
- ✅ Better correlation (automatic trace/span IDs in metrics)
- ✅ Richer metadata (resource attributes)
- ✅ Industry standard (CNCF OpenTelemetry)

**How:**
See [StatsD to OTLP Migration Guide](./statsd-to-otlp-migration.md)

**Effort:** 30-60 minutes for typical application

---

#### Option 2: Remove StatsD Service, Use OTel's Built-in Receiver

**Changes required:**

1. **Update `docker-compose.yml`:**
   ```yaml
   services:
     otel-collector:
       ports:
         - "8125:8125/udp"  # ← ADD THIS

     # statsd:  # ← REMOVE THIS SERVICE
   ```

2. **Restart:**
   ```bash
   docker-compose down
   docker-compose up -d
   ```

**Result:** Application → OTel:8125 (StatsD receiver) → File exports ✅

**Trade-offs:**
- ✅ Simpler (one less service)
- ✅ StatsD metrics work
- ❌ Still limited to metrics only (no traces/logs)
- ❌ No correlation with traces

---

#### Option 3: Add Graphite Receiver to OTel Collector

**Changes required:**

1. **Update `docker/configs/otel/otel-collector-config.yaml`:**
   ```yaml
   receivers:
     carbon:  # ← ADD THIS
       endpoint: 0.0.0.0:2003
       transport: tcp

   service:
     pipelines:
       metrics:
         receivers: [otlp, statsd, prometheus, carbon]  # ← Add 'carbon'
   ```

2. **Restart:**
   ```bash
   docker-compose restart otel-collector
   ```

**Result:** Application → StatsD:8125 → OTel:2003 (Graphite) → File exports ✅

**Trade-offs:**
- ✅ StatsD metrics work
- ❌ More complex (extra receiver)
- ❌ Still limited to metrics only
- ❌ No correlation with traces

---

### Timeline

**Discovered:** 2026-02-13
**Root cause identified:** 2026-02-13
**Fix available:** Yes (multiple options)
**ETA for permanent fix:** N/A (migration to OTLP recommended)

---

## 🟢 Everything Else Works

All other functionality is working as expected:

- ✅ OTLP traces, metrics, and logs (ports 4317/4318)
- ✅ File exports to `./data/` directory
- ✅ Prometheus scraping and storage
- ✅ Grafana dashboards
- ✅ Jaeger trace visualization
- ✅ Filebeat log processing
- ✅ All verification scripts
- ✅ Docker health checks

---

## Reporting New Issues

If you encounter issues:

1. Check the [Troubleshooting Guide](../examples/common/troubleshooting.md)
2. Run health check: `./scripts/verification/bash/check_telemetry_health.sh`
3. Check service logs: `docker-compose logs <service-name>`
4. Create an issue with:
   - Description of the problem
   - Steps to reproduce
   - Relevant logs
   - Expected vs. actual behavior

---

**Last Updated**: 2026-02-13
