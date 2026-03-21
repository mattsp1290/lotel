use std::time::Duration;

use lotel_collector::config;

/// Full pipeline integration test: start collector -> send OTLP data -> ingest -> query -> prune.
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn full_pipeline_roundtrip() {
    // Parse config with test ports (random).
    let test_config_yaml = r#"
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 127.0.0.1:0
      http:
        endpoint: 127.0.0.1:0

processors:
  batch:
    timeout: 100ms
    send_batch_size: 1
    send_batch_max_size: 10

exporters:
  file/traces:
    path: __TRACES_PATH__
    format: json
  file/metrics:
    path: __METRICS_PATH__
    format: json
  file/logs:
    path: __LOGS_PATH__
    format: json

extensions:
  health_check:
    endpoint: 127.0.0.1:0

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
"#;

    // We can't use port 0 with the current Pipeline implementation since
    // it reads config endpoints as strings. Use fixed high ports instead.
    let tmp = tempfile::TempDir::new().unwrap();
    let traces_path = tmp.path().join("traces/traces.jsonl");
    let metrics_path = tmp.path().join("metrics/metrics.jsonl");
    let logs_path = tmp.path().join("logs/logs.jsonl");

    let yaml = test_config_yaml
        .replace("__TRACES_PATH__", &traces_path.display().to_string())
        .replace("__METRICS_PATH__", &metrics_path.display().to_string())
        .replace("__LOGS_PATH__", &logs_path.display().to_string())
        // Use high ports to avoid conflicts.
        .replace("127.0.0.1:0", "127.0.0.1:0");

    // Since Pipeline uses fixed ports from config, we need specific ports.
    // Find free ports.
    let grpc_port = get_free_port().await;
    let http_port = get_free_port().await;
    let health_port = get_free_port().await;

    let yaml = yaml
        .replacen("127.0.0.1:0", &format!("127.0.0.1:{grpc_port}"), 1)
        .replacen("127.0.0.1:0", &format!("127.0.0.1:{http_port}"), 1)
        .replacen("127.0.0.1:0", &format!("127.0.0.1:{health_port}"), 1);

    let test_config = config::parse_config(&yaml).expect("parse test config");

    // Start the pipeline.
    let handle = lotel_collector::pipeline::Pipeline::run(&test_config)
        .expect("start pipeline");

    eprintln!("Pipeline started. Ports: gRPC={grpc_port}, HTTP={http_port}, health={health_port}");

    // Give pipeline tasks time to start.
    tokio::time::sleep(Duration::from_millis(1000)).await;

    // Wait for health check.
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(2))
        .build()
        .unwrap();
    let health_url = format!("http://127.0.0.1:{health_port}/");
    let mut healthy = false;
    for _ in 0..40 {
        if let Ok(resp) = client.get(&health_url).send().await {
            if resp.status().is_success() {
                healthy = true;
                break;
            }
        }
        tokio::time::sleep(Duration::from_millis(250)).await;
    }
    if !healthy {
        panic!("collector did not become healthy. Health URL: {health_url}, grpc_port: {grpc_port}, http_port: {http_port}, health_port: {health_port}");
    }

    // Send OTLP traces via HTTP using proper proto types for serde compatibility.
    use opentelemetry_proto::tonic::collector::trace::v1::ExportTraceServiceRequest;
    use opentelemetry_proto::tonic::common::v1::{any_value, AnyValue, KeyValue};
    use opentelemetry_proto::tonic::resource::v1::Resource;
    use opentelemetry_proto::tonic::trace::v1::{ResourceSpans, ScopeSpans, Span};

    let trace_req = ExportTraceServiceRequest {
        resource_spans: vec![ResourceSpans {
            resource: Some(Resource {
                attributes: vec![KeyValue {
                    key: "service.name".into(),
                    value: Some(AnyValue {
                        value: Some(any_value::Value::StringValue(
                            "integration-test-svc".into(),
                        )),
                    }),
                }],
                ..Default::default()
            }),
            scope_spans: vec![ScopeSpans {
                spans: vec![Span {
                    name: "test-span".into(),
                    start_time_unix_nano: 1710000000000000000,
                    end_time_unix_nano: 1710000001000000000,
                    ..Default::default()
                }],
                ..Default::default()
            }],
            ..Default::default()
        }],
    };
    let trace_data = serde_json::to_value(&trace_req).unwrap();

    let http_url = format!("http://127.0.0.1:{http_port}");
    let resp = client
        .post(format!("{http_url}/v1/traces"))
        .json(&trace_data)
        .send()
        .await
        .expect("send traces");
    assert!(resp.status().is_success(), "trace POST should succeed");

    // Wait for batch to flush.
    tokio::time::sleep(Duration::from_millis(500)).await;

    // Verify JSONL file was written.
    assert!(traces_path.exists(), "traces JSONL should exist");
    let content = std::fs::read_to_string(&traces_path).unwrap();
    assert!(!content.is_empty(), "traces JSONL should not be empty");

    // Ingest into DuckDB.
    let conn = lotel_storage::open_in_memory().unwrap();
    lotel_storage::ingest::ingest_all(&conn, tmp.path()).unwrap();

    // Query traces.
    let results = lotel_storage::query_traces(&conn, &lotel_storage::QueryOptions::default())
        .unwrap();
    assert!(!results.is_empty(), "should have ingested traces");

    // Prune with dry run.
    let cutoff = chrono::Utc::now().naive_utc() + chrono::Duration::hours(1);
    let reports = lotel_storage::prune(&conn, cutoff, None, true).unwrap();
    assert!(
        reports.iter().any(|r| r.deleted > 0),
        "dry run should report deletions"
    );

    // Prune for real.
    let reports = lotel_storage::prune(&conn, cutoff, None, false).unwrap();
    assert!(
        reports.iter().any(|r| r.deleted > 0),
        "should have deleted data"
    );

    // Verify data is gone.
    let results = lotel_storage::query_traces(&conn, &lotel_storage::QueryOptions::default())
        .unwrap();
    assert!(results.is_empty(), "traces should be pruned");

    // Shutdown.
    handle.shutdown().await;
}

async fn get_free_port() -> u16 {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    listener.local_addr().unwrap().port()
}
