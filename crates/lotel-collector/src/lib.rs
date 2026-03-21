//! lotel-collector: OTLP collector for receiving and forwarding telemetry data.

pub mod config;
pub mod model;

#[cfg(test)]
mod proto_check;
