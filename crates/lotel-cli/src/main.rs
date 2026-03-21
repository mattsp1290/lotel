mod daemon;
mod time;

use std::path::PathBuf;
use std::time::Duration;

use anyhow::{Result, bail};
use clap::{Parser, Subcommand};
use serde::Serialize;

#[derive(Parser)]
#[command(
    name = "lotel",
    about = "Local OpenTelemetry — manage a collector and query telemetry"
)]
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
        Command::Ingest => cmd_ingest()?,
        Command::Query { subcommand } => cmd_query(subcommand)?,
        Command::Prune {
            older_than,
            service,
            dry_run,
            all,
        } => cmd_prune(older_than, service, dry_run, all)?,
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
    let data_path = lotel_collector::config::data_path().map_err(|e| anyhow::anyhow!("{e}"))?;

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
            let healthy = if running { check_health_sync() } else { false };
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

fn cmd_run_collector(config: &std::path::Path) -> Result<()> {
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

fn cmd_ingest() -> Result<()> {
    let data_path = lotel_collector::config::data_path().map_err(|e| anyhow::anyhow!("{e}"))?;
    let conn = lotel_storage::default_db()?;
    lotel_storage::ingest_all(&conn, &data_path)?;
    eprintln!("Ingestion complete.");
    Ok(())
}

fn cmd_query(subcommand: QueryCommand) -> Result<()> {
    let conn = lotel_storage::default_db()?;

    match subcommand {
        QueryCommand::Traces {
            service,
            since,
            until,
            limit,
        } => {
            let opts = build_query_opts(service, since, until, limit)?;
            let results = lotel_storage::query_traces(&conn, &opts)?;
            print_json(&results);
        }
        QueryCommand::Metrics {
            service,
            since,
            until,
            limit,
        } => {
            let opts = build_query_opts(service, since, until, limit)?;
            let results = lotel_storage::query_metrics(&conn, &opts)?;
            print_json(&results);
        }
        QueryCommand::Logs {
            service,
            since,
            until,
            limit,
        } => {
            let opts = build_query_opts(service, since, until, limit)?;
            let results = lotel_storage::query_logs(&conn, &opts)?;
            print_json(&results);
        }
        QueryCommand::Aggregate {
            metric,
            service,
            since,
            until,
        } => {
            let opts = build_query_opts(service, since, until, None)?;
            let result = lotel_storage::aggregate_metrics(&conn, &opts, &metric)?;
            print_json(&result);
        }
    }
    Ok(())
}

fn cmd_prune(
    older_than: Option<String>,
    service: Option<String>,
    dry_run: bool,
    all: bool,
) -> Result<()> {
    if all && older_than.is_some() {
        bail!("--all and --older-than are mutually exclusive");
    }
    if !all && older_than.is_none() {
        bail!("--older-than or --all is required (e.g., '7d', '24h')");
    }

    let cutoff = if all {
        // Future cutoff catches everything.
        chrono::Utc::now().naive_utc() + chrono::Duration::hours(1)
    } else {
        let dur = time::parse_duration(older_than.as_deref().unwrap())?;
        chrono::Utc::now().naive_utc() - dur
    };

    let conn = lotel_storage::default_db()?;
    let reports = lotel_storage::prune(&conn, cutoff, service.as_deref(), dry_run)?;

    if dry_run {
        eprintln!("Dry run — no data was deleted.");
    }
    print_json(&reports);
    Ok(())
}

fn build_query_opts(
    service: Option<String>,
    since: Option<String>,
    until: Option<String>,
    limit: Option<usize>,
) -> Result<lotel_storage::QueryOptions> {
    let since_dt = since.map(|s| time::parse_time(&s)).transpose()?;
    let until_dt = until.map(|s| time::parse_time(&s)).transpose()?;
    Ok(lotel_storage::QueryOptions {
        service,
        since: since_dt,
        until: until_dt,
        limit,
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
