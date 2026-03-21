mod daemon;
mod time;

use std::path::PathBuf;
use std::time::Duration;

use anyhow::{bail, Result};
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
        #[arg(long)]
        service: Option<String>,
        #[arg(long)]
        since: Option<String>,
        #[arg(long)]
        until: Option<String>,
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
        Command::Start { wait } => cmd_start(wait)?,
        Command::Stop => cmd_stop()?,
        Command::Status => cmd_status()?,
        Command::Health => cmd_health()?,
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
        Command::RunCollector { config, data: _ } => {
            cmd_run_collector(&config)?;
        }
    }

    Ok(())
}

fn cmd_start(wait: bool) -> Result<()> {
    daemon::cleanup_stale_state()?;

    if let Some(state) = daemon::read_state()? {
        if daemon::is_pid_alive(state.pid) {
            eprintln!("Collector is already running (PID {}).", state.pid);
            return Ok(());
        }
        daemon::remove_state()?;
    }

    let config_path =
        lotel_collector::config::resolve_config_path().map_err(|e| anyhow::anyhow!("{e}"))?;
    let data_path =
        lotel_collector::config::data_path().map_err(|e| anyhow::anyhow!("{e}"))?;

    let pid = daemon::spawn_collector(&config_path, &data_path)?;

    let state = daemon::CollectorState {
        pid,
        started_at: chrono::Utc::now().to_rfc3339(),
        config_path: config_path.display().to_string(),
        data_path: data_path.display().to_string(),
    };
    daemon::write_state(&state)?;

    eprintln!("Collector started (PID {pid}).");

    if wait {
        eprint!("Waiting for collector to become healthy...");
        let rt = tokio::runtime::Runtime::new()?;
        let healthy = rt.block_on(async {
            let client = reqwest::Client::new();
            let start = std::time::Instant::now();
            loop {
                if start.elapsed() > Duration::from_secs(30) {
                    return false;
                }
                match client.get("http://localhost:13133/").send().await {
                    Ok(resp) if resp.status().is_success() => return true,
                    _ => {}
                }
                tokio::time::sleep(Duration::from_millis(500)).await;
            }
        });
        if healthy {
            eprintln!(" OK");
        } else {
            eprintln!(" FAILED");
            bail!("collector did not become healthy within 30s");
        }
    }

    Ok(())
}

fn cmd_stop() -> Result<()> {
    let state = daemon::read_state()?;
    match state {
        Some(state) if daemon::is_pid_alive(state.pid) => {
            daemon::stop_process(state.pid, Duration::from_secs(10))?;
            daemon::remove_state()?;
            eprintln!("Collector stopped.");
        }
        Some(_) => {
            daemon::remove_state()?;
            eprintln!("Collector was not running (cleaned up stale state).");
        }
        None => {
            eprintln!("Collector is not running.");
        }
    }
    Ok(())
}

fn cmd_status() -> Result<()> {
    let state = daemon::read_state()?;
    match state {
        Some(state) => {
            let running = daemon::is_pid_alive(state.pid);
            let healthy = if running {
                check_health_sync()
            } else {
                false
            };
            print_json(&serde_json::json!({
                "running": running,
                "healthy": healthy,
                "pid": state.pid,
                "started_at": state.started_at,
                "config_path": state.config_path,
                "data_path": state.data_path,
            }));
            if !running {
                std::process::exit(1);
            }
        }
        None => {
            print_json(&serde_json::json!({
                "running": false,
                "healthy": false,
            }));
            std::process::exit(1);
        }
    }
    Ok(())
}

fn cmd_health() -> Result<()> {
    let state = daemon::read_state()?;
    match state {
        Some(state) if daemon::is_pid_alive(state.pid) => {
            if check_health_sync() {
                eprintln!("Collector is healthy.");
            } else {
                eprintln!("Collector is running but not healthy.");
                std::process::exit(1);
            }
        }
        _ => {
            eprintln!("Collector is not running.");
            std::process::exit(1);
        }
    }
    Ok(())
}

fn cmd_run_collector(config: &PathBuf) -> Result<()> {
    let rt = tokio::runtime::Runtime::new()?;
    rt.block_on(async {
        let collector = lotel_collector::Collector::from_config_file(config)
            .map_err(|e| anyhow::anyhow!("{e}"))?;
        let handle = collector.start().map_err(|e| anyhow::anyhow!("{e}"))?;

        // Wait for SIGTERM/SIGINT.
        tokio::signal::ctrl_c().await?;
        eprintln!("Shutting down collector...");
        handle.shutdown().await;
        Ok(())
    })
}

fn check_health_sync() -> bool {
    let rt = match tokio::runtime::Runtime::new() {
        Ok(rt) => rt,
        Err(_) => return false,
    };
    rt.block_on(async {
        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(2))
            .build()
            .ok()?;
        let resp = client.get("http://localhost:13133/").send().await.ok()?;
        Some(resp.status().is_success())
    })
    .unwrap_or(false)
}
