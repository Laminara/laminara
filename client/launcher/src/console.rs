use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::time::Duration;

use laminara_core::gamelog::{self, GameLog, Stream};
use tokio::process::Child;
use tokio_util::sync::CancellationToken;

use crate::setup::{AppState, RunningGame};

const FLUSH_EVERY: Duration = Duration::from_millis(60);
const DRAIN_GRACE: Duration = Duration::from_secs(2);

pub type SharedLog = Arc<Mutex<GameLog>>;

pub fn lock(log: &SharedLog) -> MutexGuard<'_, GameLog> {
    log.lock().unwrap_or_else(PoisonError::into_inner)
}

#[derive(Clone)]
pub struct GameExit {
    pub session: u64,
    pub build: String,
    pub code: i32,
    pub stopped: bool,
}

pub fn watch(state: Arc<AppState>, build: &str, mut child: Child) -> RunningGame {
    let log = state.game_log.clone();
    let session = lock(&log).begin(build);
    let token = CancellationToken::new();
    let build = build.to_string();
    crate::ui::game_started(session, build.clone());

    let stdout = child.stdout.take();
    let stderr = child.stderr.take();
    let cancelled = token.clone();
    tokio::spawn(async move {
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
            let log = log.clone();
            tokio::spawn(async move {
                let mut tick = tokio::time::interval(FLUSH_EVERY);
                let mut announced = 0;
                loop {
                    tick.tick().await;
                    announced = announce(&log, announced);
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

        let code = status.and_then(|status| status.code()).unwrap_or(-1);
        lock(&log).finish(session, code, stopped);
        crate::ui::game_log_grew();
        tracing::info!(code, stopped, "игра «{build}» завершилась");
        {
            let mut running = state.game.lock().await;
            if running.as_ref().is_some_and(|game| game.session == session) {
                *running = None;
            }
        }
        crate::ui::game_exited(GameExit {
            session,
            build,
            code,
            stopped,
        });
    });

    RunningGame { session, token }
}

fn announce(log: &SharedLog, announced: u64) -> u64 {
    let latest = lock(log).next_seq();
    if latest != announced {
        crate::ui::game_log_grew();
    }
    latest
}
