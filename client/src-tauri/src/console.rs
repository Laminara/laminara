use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::time::Duration;

use laminara_core::gamelog::{self, GameLine, GameLog, Stream};
use serde::Serialize;
use tauri::{AppHandle, Emitter, EventTarget, Manager, WebviewUrl, WebviewWindowBuilder};
use tokio::process::Child;
use tokio_util::sync::CancellationToken;

use crate::{AppState, RunningGame};

pub const WINDOW_LABEL: &str = "console";
const FLUSH_EVERY: Duration = Duration::from_millis(60);
const DRAIN_GRACE: Duration = Duration::from_secs(2);

pub type SharedLog = Arc<Mutex<GameLog>>;

pub fn lock(log: &SharedLog) -> MutexGuard<'_, GameLog> {
    log.lock().unwrap_or_else(PoisonError::into_inner)
}

#[derive(Serialize, Clone)]
#[serde(rename_all = "camelCase")]
struct Started {
    session: u64,
    build: String,
}

#[derive(Serialize, Clone)]
#[serde(rename_all = "camelCase")]
struct Batch {
    session: u64,
    lines: Vec<GameLine>,
}

#[derive(Serialize, Clone)]
#[serde(rename_all = "camelCase")]
struct Exit {
    session: u64,
    build: String,
    code: i32,
    stopped: bool,
}

pub fn watch(app: AppHandle, log: SharedLog, build: &str, mut child: Child) -> RunningGame {
    let session = lock(&log).begin(build);
    let token = CancellationToken::new();
    let build = build.to_string();
    let _ = app.emit(
        "game:started",
        Started {
            session,
            build: build.clone(),
        },
    );

    let stdout = child.stdout.take();
    let stderr = child.stderr.take();
    let cancelled = token.clone();
    tauri::async_runtime::spawn(async move {
        let mut pumps = Vec::new();
        if let Some(pipe) = stdout {
            let log = log.clone();
            pumps.push(tokio::spawn(async move {
                gamelog::pump(pipe, Stream::Out, &log, session).await
            }));
        }
        if let Some(pipe) = stderr {
            let log = log.clone();
            pumps.push(tokio::spawn(async move {
                gamelog::pump(pipe, Stream::Err, &log, session).await
            }));
        }
        let flusher = {
            let app = app.clone();
            let log = log.clone();
            tokio::spawn(async move {
                let mut tick = tokio::time::interval(FLUSH_EVERY);
                loop {
                    tick.tick().await;
                    flush(&app, &log);
                }
            })
        };

        let (status, stopped) = tokio::select! {
            status = child.wait() => (status.ok(), false),
            _ = cancelled.cancelled() => {
                let _ = child.kill().await;
                (child.wait().await.ok(), true)
            }
        };
        let drained =
            tokio::time::timeout(DRAIN_GRACE, futures::future::join_all(pumps.iter_mut())).await;
        if drained.is_err() {
            tracing::warn!(
                "вывод игры не закрылся за {DRAIN_GRACE:?} после выхода — журнал может быть неполным"
            );
            for pump in &pumps {
                pump.abort();
            }
        }
        flusher.abort();
        let _ = flusher.await;
        flush(&app, &log);

        let code = status.and_then(|status| status.code()).unwrap_or(-1);
        lock(&log).finish(session, code, stopped);
        tracing::info!(code, stopped, "игра «{build}» завершилась");
        {
            let state = app.state::<AppState>();
            let mut running = state.game.lock().await;
            if running.as_ref().is_some_and(|game| game.session == session) {
                *running = None;
            }
        }
        let _ = app.emit(
            "game:exit",
            Exit {
                session,
                build,
                code,
                stopped,
            },
        );
    });

    RunningGame { session, token }
}

fn flush(app: &AppHandle, log: &SharedLog) {
    let (session, lines) = {
        let mut log = lock(log);
        (log.session(), log.unsent())
    };
    if lines.is_empty() {
        return;
    }
    let _ = app.emit_to(
        EventTarget::webview_window(WINDOW_LABEL),
        "game:log",
        Batch { session, lines },
    );
}

pub fn open_window(app: &AppHandle) -> tauri::Result<()> {
    if let Some(window) = app.get_webview_window(WINDOW_LABEL) {
        window.unminimize()?;
        window.show()?;
        return window.set_focus();
    }
    let mut builder =
        WebviewWindowBuilder::new(app, WINDOW_LABEL, WebviewUrl::App("index.html".into()))
            .title(crate::window_title(app, "консоль игры"))
            .inner_size(1080.0, 640.0)
            .min_inner_size(720.0, 420.0)
            .decorations(false)
            .resizable(true)
            .center()
            .visible(false);
    if let Some(icon) = crate::branding_icon() {
        builder = builder.icon(icon)?;
    }
    builder.build()?;
    Ok(())
}

pub fn close_window(app: &AppHandle) {
    if let Some(window) = app.get_webview_window(WINDOW_LABEL) {
        if let Err(error) = window.destroy() {
            tracing::warn!(%error, "окно консоли не закрылось вместе с лаунчером");
        }
    }
}
