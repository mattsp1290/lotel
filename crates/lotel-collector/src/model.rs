use chrono::NaiveDateTime;
use opentelemetry_proto::tonic::common::v1::{AnyValue, KeyValue};
use serde::{Deserialize, Deserializer, Serialize};

/// A flattened span record ready for DuckDB insertion.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SpanRecord {
    pub trace_id: String,
    pub span_id: String,
    pub parent_span_id: Option<String>,
    pub name: String,
    pub kind: i32,
    pub start_time: NaiveDateTime,
    pub end_time: Option<NaiveDateTime>,
    pub duration_ns: i64,
    pub status_code: i32,
    pub service_name: String,
    pub attributes: serde_json::Value,
}

/// A flattened metric record ready for DuckDB insertion.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MetricRecord {
    pub metric_name: String,
    pub metric_type: String, // "sum", "gauge", "histogram"
    pub value: f64,
    pub timestamp: NaiveDateTime,
    pub service_name: String,
    pub aggregation_temporality: Option<i32>,
    pub is_monotonic: Option<bool>,
    pub unit: Option<String>,
    pub attributes: serde_json::Value,
}

/// A flattened log record ready for DuckDB insertion.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LogRecord {
    pub timestamp: NaiveDateTime,
    pub severity: Option<String>,
    pub severity_number: Option<i32>,
    pub body: Option<String>,
    pub service_name: String,
    pub trace_id: Option<String>,
    pub span_id: Option<String>,
    pub attributes: serde_json::Value,
}

/// Nanosecond timestamp that handles both string and integer JSON representations.
/// Matches Go `otlpNano` in internal/storage/ingest.go.
#[derive(Debug, Clone, Copy, Default)]
pub struct OtlpNano(pub i64);

impl OtlpNano {
    pub fn to_datetime(self) -> Option<NaiveDateTime> {
        if self.0 == 0 {
            return None;
        }
        let secs = self.0 / 1_000_000_000;
        let nsecs = (self.0 % 1_000_000_000) as u32;
        chrono::DateTime::from_timestamp(secs, nsecs).map(|dt| dt.naive_utc())
    }
}

impl<'de> Deserialize<'de> for OtlpNano {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        use serde::de;

        struct OtlpNanoVisitor;

        impl<'de> de::Visitor<'de> for OtlpNanoVisitor {
            type Value = OtlpNano;

            fn expecting(&self, formatter: &mut std::fmt::Formatter) -> std::fmt::Result {
                formatter.write_str("a nanosecond timestamp as string or integer")
            }

            fn visit_i64<E: de::Error>(self, v: i64) -> Result<OtlpNano, E> {
                Ok(OtlpNano(v))
            }

            fn visit_u64<E: de::Error>(self, v: u64) -> Result<OtlpNano, E> {
                Ok(OtlpNano(v as i64))
            }

            fn visit_str<E: de::Error>(self, v: &str) -> Result<OtlpNano, E> {
                let n: i64 = v.parse().unwrap_or(0);
                Ok(OtlpNano(n))
            }
        }

        deserializer.deserialize_any(OtlpNanoVisitor)
    }
}

impl Serialize for OtlpNano {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        serializer.serialize_i64(self.0)
    }
}

/// Extract service.name from resource attributes, defaulting to "unknown".
pub fn extract_service_name(attrs: &[KeyValue]) -> String {
    for kv in attrs {
        if kv.key == "service.name"
            && let Some(ref val) = kv.value
        {
            return any_value_to_string(val);
        }
    }
    "unknown".to_string()
}

/// Flatten a list of KeyValue attributes into a JSON object with string values.
pub fn flatten_attrs(attrs: &[KeyValue]) -> serde_json::Value {
    let mut map = serde_json::Map::new();
    for kv in attrs {
        let value = kv
            .value
            .as_ref()
            .map(any_value_to_string)
            .unwrap_or_default();
        map.insert(kv.key.clone(), serde_json::Value::String(value));
    }
    serde_json::Value::Object(map)
}

fn any_value_to_string(val: &AnyValue) -> String {
    use opentelemetry_proto::tonic::common::v1::any_value::Value;
    match &val.value {
        Some(Value::StringValue(s)) => s.clone(),
        Some(Value::IntValue(i)) => i.to_string(),
        Some(Value::BoolValue(b)) => b.to_string(),
        Some(Value::DoubleValue(d)) => format!("{d}"),
        Some(Value::BytesValue(b)) => format!("{b:?}"),
        Some(Value::ArrayValue(arr)) => format!("{arr:?}"),
        Some(Value::KvlistValue(kv)) => format!("{kv:?}"),
        None => String::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use opentelemetry_proto::tonic::common::v1::any_value::Value;

    #[test]
    fn otlp_nano_from_string() {
        let json = r#""1710000000000000000""#;
        let nano: OtlpNano = serde_json::from_str(json).unwrap();
        assert_eq!(nano.0, 1710000000000000000);
        let dt = nano.to_datetime().unwrap();
        assert_eq!(dt.to_string(), "2024-03-09 16:00:00");
    }

    #[test]
    fn otlp_nano_from_integer() {
        let json = "1710000000000000000";
        let nano: OtlpNano = serde_json::from_str(json).unwrap();
        assert_eq!(nano.0, 1710000000000000000);
    }

    #[test]
    fn otlp_nano_zero_returns_none() {
        let nano = OtlpNano(0);
        assert!(nano.to_datetime().is_none());
    }

    #[test]
    fn extract_service_name_found() {
        let attrs = vec![KeyValue {
            key: "service.name".into(),
            value: Some(AnyValue {
                value: Some(Value::StringValue("my-svc".into())),
            }),
        }];
        assert_eq!(extract_service_name(&attrs), "my-svc");
    }

    #[test]
    fn extract_service_name_missing() {
        let attrs = vec![KeyValue {
            key: "other.key".into(),
            value: Some(AnyValue {
                value: Some(Value::StringValue("val".into())),
            }),
        }];
        assert_eq!(extract_service_name(&attrs), "unknown");
    }

    #[test]
    fn flatten_attrs_basic() {
        let attrs = vec![
            KeyValue {
                key: "http.method".into(),
                value: Some(AnyValue {
                    value: Some(Value::StringValue("GET".into())),
                }),
            },
            KeyValue {
                key: "http.status".into(),
                value: Some(AnyValue {
                    value: Some(Value::IntValue(200)),
                }),
            },
        ];
        let flat = flatten_attrs(&attrs);
        assert_eq!(flat["http.method"], "GET");
        assert_eq!(flat["http.status"], "200");
    }

    #[test]
    fn span_record_json_roundtrip() {
        let record = SpanRecord {
            trace_id: "abc123".into(),
            span_id: "def456".into(),
            parent_span_id: None,
            name: "test-span".into(),
            kind: 1,
            start_time: chrono::NaiveDateTime::parse_from_str(
                "2024-01-15 10:00:00",
                "%Y-%m-%d %H:%M:%S",
            )
            .unwrap(),
            end_time: None,
            duration_ns: 1000000,
            status_code: 0,
            service_name: "test-svc".into(),
            attributes: serde_json::json!({}),
        };
        let json = serde_json::to_string(&record).unwrap();
        let deserialized: SpanRecord = serde_json::from_str(&json).unwrap();
        assert_eq!(deserialized.trace_id, "abc123");
        assert_eq!(deserialized.name, "test-span");
    }
}
