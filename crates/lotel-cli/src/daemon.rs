use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::Duration;

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CollectorState {
    pub pid: u32,
    pub started_at: String,
    pub config_path: String,
    pub data_path: String,
}

fn state_file_path() -> Result<PathBuf> {
    let home = dirs::home_dir().context("cannot determine home directory")?;
    Ok(home.join(".lotel").join("collector.state"))
}

fn lotel_dir() -> Result<PathBuf> {
    let home = dirs::home_dir().context("cannot determine home directory")?;
    let dir = home.join(".lotel");
    fs::create_dir_all(&dir)?;
    Ok(dir)
}

pub fn read_state() -> Result<Option<CollectorState>> {
    let path = state_file_path()?;
    if !path.exists() {
        return Ok(None);
    }
    let content = fs::read_to_string(&path)?;
    match serde_json::from_str(&content) {
        Ok(state) => Ok(Some(state)),
        Err(_) => {
            // State file is from an incompatible version; discard it.
            eprintln!("Warning: collector.state has incompatible format, removing it.");
            fs::remove_file(&path)?;
            Ok(None)
        }
    }
}

pub fn write_state(state: &CollectorState) -> Result<()> {
    let path = state_file_path()?;
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }
    // Write to temp file then rename for atomicity.
    let tmp_path = path.with_extension("tmp");
    let mut file = fs::File::create(&tmp_path)?;
    file.write_all(serde_json::to_string_pretty(state)?.as_bytes())?;
    file.flush()?;
    fs::rename(&tmp_path, &path)?;
    Ok(())
}

pub fn remove_state() -> Result<()> {
    let path = state_file_path()?;
    if path.exists() {
        fs::remove_file(&path)?;
    }
    Ok(())
}

pub fn is_pid_alive(pid: u32) -> bool {
    let cmdline_path = format!("/proc/{pid}/cmdline");
    if let Ok(cmdline) = fs::read_to_string(&cmdline_path) {
        return cmdline.contains("lotel");
    }
    // Fallback: check if process exists via kill(0).
    unsafe { libc::kill(pid as i32, 0) == 0 }
}

pub fn stop_process(pid: u32, timeout: Duration) -> Result<()> {
    // Send SIGTERM.
    unsafe {
        libc::kill(pid as i32, libc::SIGTERM);
    }

    let start = std::time::Instant::now();
    while start.elapsed() < timeout {
        if !is_pid_alive(pid) {
            return Ok(());
        }
        std::thread::sleep(Duration::from_millis(100));
    }

    // SIGKILL if still alive.
    if is_pid_alive(pid) {
        unsafe {
            libc::kill(pid as i32, libc::SIGKILL);
        }
        std::thread::sleep(Duration::from_millis(500));
    }

    Ok(())
}

pub fn cleanup_stale_state() -> Result<()> {
    if let Some(state) = read_state()?
        && !is_pid_alive(state.pid)
    {
        remove_state()?;
    }
    Ok(())
}

pub fn spawn_collector(config_path: &Path, data_path: &Path) -> Result<u32> {
    let exe = std::env::current_exe().context("cannot determine current executable")?;
    let lotel_dir = lotel_dir()?;
    let log_file = fs::File::create(lotel_dir.join("collector.log"))?;
    let stderr_file = log_file.try_clone()?;

    let child = Command::new(exe)
        .arg("run-collector")
        .arg("--config")
        .arg(config_path)
        .arg("--data")
        .arg(data_path)
        .stdout(Stdio::from(log_file))
        .stderr(Stdio::from(stderr_file))
        .spawn()
        .context("failed to spawn collector process")?;

    Ok(child.id())
}
