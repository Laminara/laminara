use std::path::Path;

use tracing_appender::non_blocking::WorkerGuard;
use tracing_subscriber::prelude::*;
use tracing_subscriber::EnvFilter;

pub fn init(log_dir: &Path) -> Option<WorkerGuard> {
    if std::fs::create_dir_all(log_dir).is_err() {
        return None;
    }
    let appender = tracing_appender::rolling::never(log_dir, "launcher.log");
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

    tracing::info!(target: "laminara_lib", "launcher log started at {}", log_dir.join("launcher.log").display());
    Some(guard)
}
