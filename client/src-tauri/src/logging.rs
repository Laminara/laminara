use std::path::{Path, PathBuf};

use tracing_appender::non_blocking::WorkerGuard;
use tracing_subscriber::prelude::*;
use tracing_subscriber::EnvFilter;

const LOG_FILE: &str = "launcher.log";
const PREVIOUS_LOG_FILE: &str = "launcher.previous.log";
const ROTATE_AT_BYTES: u64 = 4 * 1024 * 1024;

pub fn log_dir(data_dir: &Path) -> PathBuf {
    data_dir.join("logs")
}

pub fn log_file(log_dir: &Path) -> PathBuf {
    log_dir.join(LOG_FILE)
}

fn rotate(log_dir: &Path) {
    let current = log_file(log_dir);
    let oversized = std::fs::metadata(&current).is_ok_and(|meta| meta.len() > ROTATE_AT_BYTES);
    if oversized {
        let _ = std::fs::rename(&current, log_dir.join(PREVIOUS_LOG_FILE));
    }
}

pub fn tail(path: &Path, limit: usize) -> std::io::Result<String> {
    let bytes = std::fs::read(path)?;
    let start = bytes.len().saturating_sub(limit);
    let text = String::from_utf8_lossy(&bytes[start..]);
    if start == 0 {
        return Ok(text.into_owned());
    }
    Ok(match text.find('\n') {
        Some(end) => text[end + 1..].to_string(),
        None => text.into_owned(),
    })
}

pub fn init(log_dir: &Path) -> Option<WorkerGuard> {
    if std::fs::create_dir_all(log_dir).is_err() {
        return None;
    }
    rotate(log_dir);
    let appender = tracing_appender::rolling::never(log_dir, LOG_FILE);
    let (writer, guard) = tracing_appender::non_blocking(appender);

    let filter = EnvFilter::try_from_env("LAMINARA_LOG")
        .unwrap_or_else(|_| EnvFilter::new("info,laminara_lib=debug,laminara_core=debug"));

    let file_layer = tracing_subscriber::fmt::layer()
        .with_ansi(false)
        .with_target(true)
        .with_writer(writer);
    let console_layer = tracing_subscriber::fmt::layer().with_target(false);

    let _ = tracing_subscriber::registry()
        .with(filter)
        .with(file_layer)
        .with(console_layer)
        .try_init();

    std::panic::set_hook(Box::new(|info| {
        tracing::error!(target: "laminara_lib", "panic: {info}");
    }));

    tracing::info!(target: "laminara_lib", "launcher log started at {}", log_file(log_dir).display());
    Some(guard)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_grown_log_is_set_aside_on_start() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(
            log_file(dir.path()),
            vec![b'x'; ROTATE_AT_BYTES as usize + 1],
        )
        .unwrap();
        rotate(dir.path());
        assert!(!log_file(dir.path()).exists());
        assert!(dir.path().join(PREVIOUS_LOG_FILE).exists());
    }

    #[test]
    fn a_small_log_keeps_growing() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(log_file(dir.path()), b"line\n").unwrap();
        rotate(dir.path());
        assert!(log_file(dir.path()).exists());
    }

    #[test]
    fn the_tail_starts_on_a_whole_line() {
        let dir = tempfile::tempdir().unwrap();
        let path = log_file(dir.path());
        std::fs::write(&path, "первая строка\nвторая строка\nтретья\n").unwrap();
        assert_eq!(tail(&path, 20).unwrap(), "третья\n");
        assert_eq!(
            tail(&path, 1024).unwrap(),
            "первая строка\nвторая строка\nтретья\n"
        );
    }
}
