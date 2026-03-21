//! lotel-collector: OTLP collector for receiving and forwarding telemetry data.

pub mod config;
pub mod exporter;
pub mod extension;
pub mod model;
pub mod pipeline;
pub mod processor;
pub mod receiver;

#[cfg(test)]
mod proto_check;
