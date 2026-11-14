use std::time::Instant;

use tokio_util::sync::CancellationToken;

use super::{cancelled, SyncProgress, SyncStage, Wanted, PROGRESS_EVERY};
use crate::error::CoreError;
use crate::proto::core::v1::ManifestFile;

pub(super) async fn disk_work<T: Send + 'static>(
    cancel: &CancellationToken,
    report: &(dyn Fn(SyncProgress) + Send + Sync),
    work: impl FnOnce(
            CancellationToken,
            tokio::sync::watch::Sender<Option<SyncProgress>>,
        ) -> Result<T, CoreError>
        + Send
        + 'static,
) -> Result<T, CoreError> {
    let cancel = cancel.child_token();
    let _guard = cancel.clone().drop_guard();
    let (send, mut receive) = tokio::sync::watch::channel(None);
    let worker = tokio::task::spawn_blocking(move || work(cancel, send));
    while receive.changed().await.is_ok() {
        let event = receive.borrow_and_update().clone();
        if let Some(event) = event {
            report(event);
        }
    }
    worker
        .await
        .map_err(|error| CoreError::Sync(format!("проверка файлов прервана: {error}")))?
}

pub(super) struct CheckProgress<'a> {
    report: &'a (dyn Fn(SyncProgress) + Send + Sync),
    cancel: &'a CancellationToken,
    files_done: u64,
    files_total: u64,
    bytes_done: u64,
    bytes_total: u64,
    current_bytes: u64,
    current_path: Option<String>,
    last: Option<Instant>,
}

impl<'a> CheckProgress<'a> {
    pub(super) fn new(
        files: &[&ManifestFile],
        cancel: &'a CancellationToken,
        report: &'a (dyn Fn(SyncProgress) + Send + Sync),
    ) -> Self {
        Self {
            report,
            cancel,
            files_done: 0,
            files_total: files.len() as u64,
            bytes_done: 0,
            bytes_total: files
                .iter()
                .filter_map(|file| file.object.as_ref())
                .map(|object| object.size)
                .sum(),
            current_bytes: 0,
            current_path: None,
            last: None,
        }
    }

    pub(super) fn start(&mut self, path: &str) {
        self.current_path = Some(path.into());
        self.current_bytes = 0;
        self.publish(false);
    }

    pub(super) fn advance(&mut self, bytes: u64) -> Result<(), CoreError> {
        cancelled(self.cancel)?;
        self.current_bytes = bytes;
        self.publish(false);
        Ok(())
    }

    pub(super) fn finish(&mut self, size: u64) {
        self.files_done += 1;
        self.bytes_done = self.bytes_done.saturating_add(size);
        self.current_bytes = 0;
        self.publish(false);
    }

    pub(super) fn extend<'b>(&mut self, objects: impl Iterator<Item = &'b Wanted>) {
        for object in objects {
            self.files_total += 1;
            self.bytes_total = self.bytes_total.saturating_add(object.size);
        }
    }

    pub(super) fn publish(&mut self, force: bool) {
        if !force && self.last.is_some_and(|at| at.elapsed() < PROGRESS_EVERY) {
            return;
        }
        self.last = Some(Instant::now());
        (self.report)(SyncProgress {
            stage: SyncStage::Planning,
            files_done: self.files_done,
            files_total: self.files_total,
            bytes_done: self
                .bytes_done
                .saturating_add(self.current_bytes)
                .min(self.bytes_total),
            bytes_total: self.bytes_total,
            current_path: self.current_path.clone(),
        });
    }
}
