//! lotel-collector: OTLP collector for receiving and forwarding telemetry data.

pub mod config;
pub mod model;
pub mod pipeline;
pub mod receiver;

#[cfg(test)]
mod proto_check;
