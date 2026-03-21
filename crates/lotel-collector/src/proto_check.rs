//! Compile-time and runtime validation that the opentelemetry-proto crate
//! provides all the OTLP types needed by lotel-collector.

#[cfg(test)]
mod tests {
    // Trace types
    use opentelemetry_proto::tonic::collector::trace::v1::ExportTraceServiceRequest;
    use opentelemetry_proto::tonic::trace::v1::{ResourceSpans, ScopeSpans, Span};

    // Metric types
    use opentelemetry_proto::tonic::collector::metrics::v1::ExportMetricsServiceRequest;
    use opentelemetry_proto::tonic::metrics::v1::{
        Gauge, Histogram, Metric, ResourceMetrics, ScopeMetrics, Sum,
    };

    // Log types
    use opentelemetry_proto::tonic::collector::logs::v1::ExportLogsServiceRequest;
    use opentelemetry_proto::tonic::logs::v1::{LogRecord, ResourceLogs, ScopeLogs};

    // Common types
    use opentelemetry_proto::tonic::common::v1::{AnyValue, KeyValue};

    // gRPC server traits
    use opentelemetry_proto::tonic::collector::logs::v1::logs_service_server::LogsServiceServer;
    use opentelemetry_proto::tonic::collector::metrics::v1::metrics_service_server::MetricsServiceServer;
    use opentelemetry_proto::tonic::collector::trace::v1::trace_service_server::TraceServiceServer;

    #[test]
    fn trace_types_instantiate() {
        let span = Span::default();
        assert!(span.trace_id.is_empty());

        let scope_spans = ScopeSpans::default();
        assert!(scope_spans.spans.is_empty());

        let resource_spans = ResourceSpans::default();
        assert!(resource_spans.scope_spans.is_empty());

        let req = ExportTraceServiceRequest::default();
        assert!(req.resource_spans.is_empty());
    }

    #[test]
    fn metric_types_instantiate() {
        let metric = Metric::default();
        assert!(metric.name.is_empty());

        let _sum = Sum::default();
        let _gauge = Gauge::default();
        let _histogram = Histogram::default();

        let scope_metrics = ScopeMetrics::default();
        assert!(scope_metrics.metrics.is_empty());

        let resource_metrics = ResourceMetrics::default();
        assert!(resource_metrics.scope_metrics.is_empty());

        let req = ExportMetricsServiceRequest::default();
        assert!(req.resource_metrics.is_empty());
    }

    #[test]
    fn log_types_instantiate() {
        let log_record = LogRecord::default();
        assert!(log_record.body.is_none());

        let scope_logs = ScopeLogs::default();
        assert!(scope_logs.log_records.is_empty());

        let resource_logs = ResourceLogs::default();
        assert!(resource_logs.scope_logs.is_empty());

        let req = ExportLogsServiceRequest::default();
        assert!(req.resource_logs.is_empty());
    }

    #[test]
    fn common_types_instantiate() {
        let kv = KeyValue {
            key: "test.key".into(),
            value: Some(AnyValue {
                value: Some(
                    opentelemetry_proto::tonic::common::v1::any_value::Value::StringValue(
                        "hello".into(),
                    ),
                ),
            }),
        };
        assert_eq!(kv.key, "test.key");
    }

    #[test]
    fn grpc_server_traits_exist() {
        // These assertions just verify the types are usable — we cannot
        // instantiate servers without an implementation, but the trait
        // being importable is what we need to confirm.
        fn _assert_trace_server<
            S: opentelemetry_proto::tonic::collector::trace::v1::trace_service_server::TraceService,
        >() {
            let _ = TraceServiceServer::<S>::new;
        }
        fn _assert_metrics_server<S: opentelemetry_proto::tonic::collector::metrics::v1::metrics_service_server::MetricsService>(){
            let _ = MetricsServiceServer::<S>::new;
        }
        fn _assert_logs_server<
            S: opentelemetry_proto::tonic::collector::logs::v1::logs_service_server::LogsService,
        >() {
            let _ = LogsServiceServer::<S>::new;
        }
    }

    #[test]
    fn serde_json_roundtrip() {
        // Verify with-serde feature enables JSON serialization.
        let span = Span {
            name: "test-span".into(),
            ..Default::default()
        };
        let json = serde_json::to_string(&span).expect("serialize span to JSON");
        let deserialized: Span = serde_json::from_str(&json).expect("deserialize span from JSON");
        assert_eq!(deserialized.name, "test-span");
    }
}
