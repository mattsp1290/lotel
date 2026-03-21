mod time;

use std::path::PathBuf;

use anyhow::Result;
use clap::{Parser, Subcommand};
use serde::Serialize;

#[derive(Parser)]
#[command(name = "lotel", about = "Local OpenTelemetry — manage a collector and query telemetry")]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// Start the OTel Collector
    Start {
        /// Wait for collector to become healthy before returning
        #[arg(long)]
        wait: bool,
    },
    /// Stop the OTel Collector
    Stop,
    /// Show collector status (JSON)
    Status,
    /// Check collector health (exit 0 if healthy, 1 if not)
    Health,
    /// Ingest JSONL telemetry files into the query database
    Ingest,
    /// Query telemetry data
    Query {
        #[command(subcommand)]
        subcommand: QueryCommand,
    },
    /// Delete telemetry data older than a threshold
    Prune {
        /// Age threshold (e.g., '7d', '24h', '1h')
        #[arg(long)]
        older_than: Option<String>,
        /// Limit pruning to a specific service
        #[arg(long)]
        service: Option<String>,
        /// Show what would be pruned without deleting
        #[arg(long)]
        dry_run: bool,
        /// Delete all telemetry data
        #[arg(long)]
        all: bool,
    },
    /// Run the collector directly (internal, used for daemon self-spawn)
    #[command(hide = true)]
    RunCollector {
        /// Path to collector config file
        #[arg(long)]
        config: PathBuf,
        /// Path to data directory
        #[arg(long)]
        data: PathBuf,
    },
}

#[derive(Subcommand)]
enum QueryCommand {
    /// Query traces (JSON output)
    Traces {
        /// Filter by service.name
        #[arg(long)]
        service: Option<String>,
        /// Start time (RFC3339 or relative like '1h', '24h')
        #[arg(long)]
        since: Option<String>,
        /// End time (RFC3339)
        #[arg(long)]
        until: Option<String>,
        /// Max results (0 = unlimited)
        #[arg(long)]
        limit: Option<usize>,
    },
    /// Query metrics (JSON output)
    Metrics {
        #[arg(long)]
        service: Option<String>,
        #[arg(long)]
        since: Option<String>,
        #[arg(long)]
        until: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
    },
    /// Query logs (JSON output)
    Logs {
        #[arg(long)]
        service: Option<String>,
        #[arg(long)]
        since: Option<String>,
        #[arg(long)]
        until: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
    },
    /// Compute avg/min/max for a metric over a time window
    Aggregate {
        /// Metric name to aggregate (required)
        #[arg(long)]
        metric: String,
        #[arg(long)]
        service: Option<String>,
        #[arg(long)]
        since: Option<String>,
        #[arg(long)]
        until: Option<String>,
    },
}

fn print_json<T: Serialize>(value: &T) {
    let data = serde_json::to_string_pretty(value).expect("json serialization");
    println!("{data}");
}

fn main() -> Result<()> {
    let cli = Cli::parse();

    match cli.command {
        Command::Start { wait: _ } => {
            eprintln!("start: not yet implemented");
        }
        Command::Stop => {
            eprintln!("stop: not yet implemented");
        }
        Command::Status => {
            eprintln!("status: not yet implemented");
        }
        Command::Health => {
            eprintln!("health: not yet implemented");
        }
        Command::Ingest => {
            eprintln!("ingest: not yet implemented");
        }
        Command::Query { subcommand } => match subcommand {
            QueryCommand::Traces { .. } => {
                print_json(&serde_json::Value::Array(vec![]));
            }
            QueryCommand::Metrics { .. } => {
                print_json(&serde_json::Value::Array(vec![]));
            }
            QueryCommand::Logs { .. } => {
                print_json(&serde_json::Value::Array(vec![]));
            }
            QueryCommand::Aggregate { .. } => {
                print_json(&serde_json::json!({}));
            }
        },
        Command::Prune { .. } => {
            eprintln!("prune: not yet implemented");
        }
        Command::RunCollector { .. } => {
            eprintln!("run-collector: not yet implemented");
        }
    }

    Ok(())
}
