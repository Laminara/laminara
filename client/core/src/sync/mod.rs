pub mod statcache;

use std::collections::{BTreeMap, HashMap, HashSet};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{Duration, Instant};

use futures::stream::{self, StreamExt};
use serde::{Deserialize, Serialize};
use sha2::Digest;
use tokio_util::sync::CancellationToken;

use crate::backoff;
use crate::error::{describe, CoreError};
use crate::features::{self, FeatureSelection};
use crate::manifest::object_key;
use crate::proto::core::v1::{FilePolicy, HashAlgo, Manifest, ManifestFile};
use crate::transport::Transport;

const DEFAULT_PARALLEL: usize = 8;
const PARTIAL_SUFFIX: &str = "part";
pub(crate) const LEDGER_FILE: &str = "installed.json";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SyncStage {
    Planning,
    Downloading,
    Done,
}

#[derive(Debug, Clone)]
pub struct SyncProgress {
    pub stage: SyncStage,
    pub files_done: u64,
    pub files_total: u64,
    pub bytes_done: u64,
    pub bytes_total: u64,
    pub current_path: Option<String>,
}

#[derive(Debug, Default, Clone)]
pub struct SyncOutcome {
    pub downloaded: u64,
    pub skipped: u64,
    pub linked: u64,
    pub bytes_downloaded: u64,
    pub pruned: u64,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
pub(crate) enum Placement {
    Hardlink,
    Copy,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub(crate) struct OwnedEntry {
    pub object_hash: String,
    pub class: i32,
    pub placement: Placement,
    pub released: bool,
    #[serde(default)]
    pub size: u64,
    #[serde(default)]
    pub algo: i32,
}

pub(crate) type Ledger = BTreeMap<String, OwnedEntry>;

fn load_ledger(path: &Path) -> Ledger {
    std::fs::read_to_string(path)
        .ok()
        .and_then(|t| serde_json::from_str(&t).ok())
        .unwrap_or_default()
}

pub(crate) const UNFINISHED_FILE: &str = "unfinished";

pub fn install_unfinished(state_dir: &Path) -> bool {
    state_dir.join(UNFINISHED_FILE).exists()
}

fn mark_unfinished(state_dir: &Path) -> Result<(), CoreError> {
    std::fs::write(state_dir.join(UNFINISHED_FILE), b"")
        .map_err(|e| CoreError::Sync(format!("не отметить начало установки: {e}")))
}

fn mark_finished(state_dir: &Path) {
    let _ = std::fs::remove_file(state_dir.join(UNFINISHED_FILE));
}

pub(crate) fn save_ledger(path: &Path, ledger: &Ledger) -> Result<(), CoreError> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let text = serde_json::to_string(ledger).map_err(|e| CoreError::Sync(e.to_string()))?;
    let tmp = path.with_extension("json.tmp");
    std::fs::write(&tmp, text)?;
    std::fs::rename(&tmp, path)?;
    Ok(())
}

enum Hasher {
    Blake3(Box<blake3::Hasher>),
    Sha256(sha2::Sha256),
    Sha1(sha1::Sha1),
}

impl Hasher {
    fn for_algo(algo: i32) -> Hasher {
        match HashAlgo::try_from(algo).unwrap_or(HashAlgo::Blake3) {
            HashAlgo::Sha256 => Hasher::Sha256(sha2::Sha256::new()),
            HashAlgo::Sha1 => Hasher::Sha1(sha1::Sha1::new()),
            _ => Hasher::Blake3(Box::new(blake3::Hasher::new())),
        }
    }
    fn update(&mut self, data: &[u8]) {
        match self {
            Hasher::Blake3(h) => {
                h.update(data);
            }
            Hasher::Sha256(h) => h.update(data),
            Hasher::Sha1(h) => h.update(data),
        }
    }
    fn finalize_hex(self) -> String {
        match self {
            Hasher::Blake3(h) => h.finalize().to_hex().to_string(),
            Hasher::Sha256(h) => hex::encode(h.finalize()),
            Hasher::Sha1(h) => hex::encode(h.finalize()),
        }
    }
}

pub(crate) fn set_mode(path: &Path, mode: u32) {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(path, std::fs::Permissions::from_mode(mode));
    }
    #[cfg(not(unix))]
    {
        if let Ok(meta) = std::fs::metadata(path) {
            let mut perms = meta.permissions();
            perms.set_readonly(mode & 0o222 == 0);
            let _ = std::fs::set_permissions(path, perms);
        }
    }
}

fn same_file(a: &Path, b: &Path) -> bool {
    same_file::is_same_file(a, b).unwrap_or(false)
}

fn clone_or_copy(source: &Path, dest: &Path) -> Result<(), CoreError> {
    match reflink_copy::reflink_or_copy(source, dest) {
        Ok(_) => Ok(()),
        Err(e) => Err(CoreError::Sync(format!("copy object: {e}"))),
    }
}

fn ensure_safe_parents(root: &Path, dest: &Path) -> Result<(), CoreError> {
    let parent = dest
        .parent()
        .ok_or_else(|| CoreError::Sync("dest has no parent".into()))?;
    let relative = parent
        .strip_prefix(root)
        .map_err(|_| CoreError::Sync(format!("path escapes the profile: {}", dest.display())))?;

    let mut current = root.to_path_buf();
    for segment in relative.components() {
        current.push(segment);
        match std::fs::symlink_metadata(&current) {
            Ok(meta) if meta.file_type().is_symlink() => {
                return Err(CoreError::Sync(format!(
                    "refusing to write through a symlinked directory: {}",
                    current.display()
                )));
            }
            Ok(meta) if meta.is_dir() => {}
            Ok(_) => {
                return Err(CoreError::Sync(format!(
                    "expected a directory: {}",
                    current.display()
                )))
            }
            Err(_) => std::fs::create_dir(&current)
                .map_err(|e| CoreError::Sync(format!("create {}: {e}", current.display())))?,
        }
    }
    Ok(())
}

fn is_read_only(meta: &std::fs::Metadata) -> bool {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        meta.permissions().mode() & 0o222 == 0
    }
    #[cfg(not(unix))]
    {
        meta.permissions().readonly()
    }
}

const OBJECT_ATTEMPTS: u32 = 6;
const CORRUPT_LIMIT: u32 = 2;
const RETRY_FIRST: Duration = Duration::from_secs(1);
const RETRY_LONGEST: Duration = Duration::from_secs(15);
const PROGRESS_EVERY: Duration = Duration::from_millis(100);
const HASH_READ_BYTES: usize = 64 * 1024;

struct Wanted {
    hex: String,
    algo: i32,
    value: Vec<u8>,
    executable: bool,
    size: u64,
    path: String,
}

struct Meter<'a> {
    report: &'a (dyn Fn(SyncProgress) + Send + Sync),
    files_total: u64,
    bytes_total: u64,
    files_done: AtomicU64,
    bytes_done: AtomicU64,
    current: std::sync::Mutex<Option<String>>,
    last: std::sync::Mutex<Option<Instant>>,
}

impl Meter<'_> {
    fn started(&self, path: &str) {
        if let Ok(mut current) = self.current.lock() {
            *current = Some(path.to_string());
        }
    }

    fn gained(&self, bytes: u64) {
        self.bytes_done.fetch_add(bytes, Ordering::Relaxed);
        self.publish(false);
    }

    fn lost(&self, bytes: u64) {
        let _ = self
            .bytes_done
            .fetch_update(Ordering::Relaxed, Ordering::Relaxed, |done| {
                Some(done.saturating_sub(bytes))
            });
    }

    fn reconcile(&self, counted: &mut u64, actual: u64) {
        if actual > *counted {
            self.gained(actual - *counted);
        } else {
            self.lost(*counted - actual);
        }
        *counted = actual;
    }

    fn finished_file(&self) {
        self.files_done.fetch_add(1, Ordering::Relaxed);
        self.publish(false);
    }

    fn publish(&self, force: bool) {
        {
            let Ok(mut last) = self.last.lock() else {
                return;
            };
            if !force && last.is_some_and(|at| at.elapsed() < PROGRESS_EVERY) {
                return;
            }
            *last = Some(Instant::now());
        }
        let current_path = self.current.lock().ok().and_then(|current| current.clone());
        (self.report)(SyncProgress {
            stage: SyncStage::Downloading,
            files_done: self.files_done.load(Ordering::Relaxed),
            files_total: self.files_total,
            bytes_done: self.bytes_done.load(Ordering::Relaxed),
            bytes_total: self.bytes_total,
            current_path,
        });
    }
}

enum Attempt {
    Again(CoreError),
    Corrupt(CoreError),
    Final(CoreError),
}

fn object_mode(executable: bool) -> u32 {
    if executable {
        0o555
    } else {
        0o444
    }
}

fn partial_path(cas_path: &Path) -> PathBuf {
    cas_path.with_extension(PARTIAL_SUFFIX)
}

fn partial_len(partial: &Path) -> u64 {
    std::fs::metadata(partial)
        .map(|meta| meta.len())
        .unwrap_or(0)
}

fn retryable_status(status: reqwest::StatusCode) -> bool {
    status.is_server_error() || matches!(status.as_u16(), 401 | 408 | 425 | 429)
}

fn network_error(wanted: &Wanted, error: &reqwest::Error) -> CoreError {
    CoreError::Transport(format!("{}: {}", wanted.path, describe(error)))
}

fn disk_error(wanted: &Wanted, error: std::io::Error) -> Attempt {
    Attempt::Final(CoreError::Io(format!("{}: {error}", wanted.path)))
}

fn cancelled(token: &CancellationToken) -> Result<(), CoreError> {
    if token.is_cancelled() {
        Err(CoreError::Cancelled)
    } else {
        Ok(())
    }
}

async fn download_or_abort(
    transport: &Transport,
    base_url: &str,
    cas_dir: &Path,
    object: &Wanted,
    meter: &Meter<'_>,
    abort: &CancellationToken,
) -> Result<u64, CoreError> {
    if abort.is_cancelled() {
        return Err(CoreError::Cancelled);
    }
    let result = tokio::select! {
        _ = abort.cancelled() => Err(CoreError::Cancelled),
        result = download(transport, base_url, cas_dir, object, meter, abort) => result,
    };
    if result.is_err() {
        abort.cancel();
    }
    result.map(|()| object.size)
}

async fn download(
    transport: &Transport,
    base_url: &str,
    cas_dir: &Path,
    wanted: &Wanted,
    meter: &Meter<'_>,
    cancel: &CancellationToken,
) -> Result<(), CoreError> {
    let cas_path = cas_dir.join(object_key(wanted.algo, &wanted.value));
    let partial = partial_path(&cas_path);
    let parent = cas_path
        .parent()
        .ok_or_else(|| CoreError::Sync("cas path has no parent".into()))?;
    tokio::fs::create_dir_all(parent).await?;
    meter.started(&wanted.path);
    let mut counted = partial_len(&partial).min(wanted.size);
    let mut attempt = 0;
    let mut corrupt = 0;
    loop {
        attempt += 1;
        let outcome = fetch(
            transport,
            base_url,
            wanted,
            &cas_path,
            &partial,
            &mut counted,
            meter,
        )
        .await;
        let error = match outcome {
            Ok(()) => {
                meter.finished_file();
                return Ok(());
            }
            Err(Attempt::Final(error)) => {
                tracing::error!(path = %wanted.path, hex = %wanted.hex, %error, "файл не скачался, и повтор тут не поможет");
                return Err(error);
            }
            Err(Attempt::Corrupt(error)) => {
                corrupt += 1;
                if corrupt >= CORRUPT_LIMIT {
                    tracing::error!(path = %wanted.path, hex = %wanted.hex, %error, "файл дважды пришёл целиком и оба раза не сошёлся с хешем: на сервере он испорчен");
                    return Err(error);
                }
                error
            }
            Err(Attempt::Again(error)) => error,
        };
        if attempt == OBJECT_ATTEMPTS {
            tracing::error!(path = %wanted.path, hex = %wanted.hex, %error, "файл так и не скачался за {OBJECT_ATTEMPTS} попыток");
            return Err(error);
        }
        let pause = backoff::pause(attempt, RETRY_FIRST, RETRY_LONGEST);
        tracing::warn!(path = %wanted.path, hex = %wanted.hex, attempt, ?pause, %error, "файл не скачался, пробую ещё раз");
        tokio::select! {
            _ = cancel.cancelled() => return Err(CoreError::Cancelled),
            _ = tokio::time::sleep(pause) => {}
        }
    }
}

fn lock_partial(partial: &Path) -> std::io::Result<Option<tokio::fs::File>> {
    let file = std::fs::OpenOptions::new()
        .read(true)
        .write(true)
        .create(true)
        .truncate(false)
        .open(partial)?;
    match file.try_lock() {
        Ok(()) => Ok(Some(tokio::fs::File::from_std(file))),
        Err(std::fs::TryLockError::WouldBlock) => Ok(None),
        Err(std::fs::TryLockError::Error(error)) => Err(error),
    }
}

async fn hash_from_start(file: &mut tokio::fs::File, hasher: &mut Hasher) -> std::io::Result<u64> {
    use tokio::io::{AsyncReadExt, AsyncSeekExt};

    file.seek(std::io::SeekFrom::Start(0)).await?;
    let mut buffer = vec![0u8; HASH_READ_BYTES];
    let mut total = 0u64;
    loop {
        let read = file.read(&mut buffer).await?;
        if read == 0 {
            return Ok(total);
        }
        hasher.update(&buffer[..read]);
        total += read as u64;
    }
}

async fn empty_partial(
    file: &mut tokio::fs::File,
    counted: &mut u64,
    meter: &Meter<'_>,
) -> std::io::Result<()> {
    use tokio::io::AsyncSeekExt;

    file.set_len(0).await?;
    file.seek(std::io::SeekFrom::Start(0)).await?;
    meter.reconcile(counted, 0);
    Ok(())
}

fn range_starts_at(response: &reqwest::Response, offset: u64) -> bool {
    response
        .headers()
        .get(reqwest::header::CONTENT_RANGE)
        .and_then(|value| value.to_str().ok())
        .is_some_and(|value| value.starts_with(&format!("bytes {offset}-")))
}

async fn fetch(
    transport: &Transport,
    base_url: &str,
    wanted: &Wanted,
    cas_path: &Path,
    partial: &Path,
    counted: &mut u64,
    meter: &Meter<'_>,
) -> Result<(), Attempt> {
    use tokio::io::{AsyncSeekExt, AsyncWriteExt};

    let mode = object_mode(wanted.executable);
    if cas_path.exists() {
        set_mode(cas_path, mode);
        meter.reconcile(counted, wanted.size);
        return Ok(());
    }
    let Some(mut file) = lock_partial(partial).map_err(|e| disk_error(wanted, e))? else {
        return Err(Attempt::Again(CoreError::Sync(format!(
            "{}: этот файл сейчас качает другой лаунчер",
            wanted.path
        ))));
    };

    let mut offset = file
        .metadata()
        .await
        .map_err(|e| disk_error(wanted, e))?
        .len();
    meter.reconcile(counted, offset.min(wanted.size));
    if offset >= wanted.size {
        let mut hasher = Hasher::for_algo(wanted.algo);
        let length = hash_from_start(&mut file, &mut hasher)
            .await
            .map_err(|e| disk_error(wanted, e))?;
        if length == wanted.size && hasher.finalize_hex() == wanted.hex {
            drop(file);
            return settle_object(partial, cas_path, mode).map_err(Attempt::Again);
        }
        empty_partial(&mut file, counted, meter)
            .await
            .map_err(|e| disk_error(wanted, e))?;
        offset = 0;
    }

    let url = format!(
        "{}/objects/{}",
        base_url.trim_end_matches('/'),
        object_key(wanted.algo, &wanted.value)
    );
    let mut request = transport.authorize(transport.client().get(&url));
    if offset > 0 {
        request = request.header(reqwest::header::RANGE, format!("bytes={offset}-"));
    }
    let response = request
        .send()
        .await
        .map_err(|e| Attempt::Again(network_error(wanted, &e)))?;
    let status = response.status();
    if status == reqwest::StatusCode::RANGE_NOT_SATISFIABLE {
        empty_partial(&mut file, counted, meter)
            .await
            .map_err(|e| disk_error(wanted, e))?;
        return Err(Attempt::Again(CoreError::Sync(format!(
            "{}: сервер не отдал хвост файла, качаю заново",
            wanted.path
        ))));
    }
    if !status.is_success() {
        let reason = format!("{}: сервер ответил {status}", wanted.path);
        return Err(if retryable_status(status) {
            Attempt::Again(CoreError::Transport(reason))
        } else {
            Attempt::Final(CoreError::Sync(reason))
        });
    }
    let resumed = offset > 0
        && status == reqwest::StatusCode::PARTIAL_CONTENT
        && range_starts_at(&response, offset);
    let whole = status == reqwest::StatusCode::OK
        && (offset == 0 || response.content_length() == Some(wanted.size));
    if !resumed && !whole {
        return Err(Attempt::Again(CoreError::Sync(format!(
            "{}: сервер ответил не тем куском ({status}), недокачанное сохранено",
            wanted.path
        ))));
    }

    let mut hasher = Hasher::for_algo(wanted.algo);
    if resumed {
        hash_from_start(&mut file, &mut hasher)
            .await
            .map_err(|e| disk_error(wanted, e))?;
        file.seek(std::io::SeekFrom::End(0))
            .await
            .map_err(|e| disk_error(wanted, e))?;
    } else if offset > 0 {
        empty_partial(&mut file, counted, meter)
            .await
            .map_err(|e| disk_error(wanted, e))?;
    }

    let mut stream = response.bytes_stream();
    let mut broken = None;
    while let Some(chunk) = stream.next().await {
        let chunk = match chunk {
            Ok(chunk) => chunk,
            Err(error) => {
                broken = Some(network_error(wanted, &error));
                break;
            }
        };
        if *counted + chunk.len() as u64 > wanted.size {
            empty_partial(&mut file, counted, meter)
                .await
                .map_err(|e| disk_error(wanted, e))?;
            return Err(Attempt::Corrupt(CoreError::Sync(format!(
                "{}: сервер прислал больше, чем весит файл",
                wanted.path
            ))));
        }
        hasher.update(&chunk);
        file.write_all(&chunk)
            .await
            .map_err(|e| disk_error(wanted, e))?;
        *counted += chunk.len() as u64;
        meter.gained(chunk.len() as u64);
    }
    file.flush().await.map_err(|e| disk_error(wanted, e))?;
    if let Some(error) = broken {
        return Err(Attempt::Again(error));
    }
    if *counted < wanted.size {
        return Err(Attempt::Again(CoreError::Transport(format!(
            "{}: ответ оборвался на {} из {} байт",
            wanted.path, *counted, wanted.size
        ))));
    }
    if hasher.finalize_hex() != wanted.hex {
        empty_partial(&mut file, counted, meter)
            .await
            .map_err(|e| disk_error(wanted, e))?;
        return Err(Attempt::Corrupt(CoreError::Sync(format!(
            "{}: содержимое не совпало с хешем",
            wanted.path
        ))));
    }
    drop(file);
    settle_object(partial, cas_path, mode).map_err(Attempt::Again)
}

fn settle_object(from: &Path, cas_path: &Path, object_mode: u32) -> Result<(), CoreError> {
    if let Err(e) = std::fs::rename(from, cas_path) {
        if !cas_path.exists() {
            return Err(CoreError::Sync(format!("persist object: {e}")));
        }
        let _ = std::fs::remove_file(from);
    }
    set_mode(cas_path, object_mode);
    Ok(())
}

fn materialize_immutable(
    root: &Path,
    cas_path: &Path,
    dest: &Path,
    executable: bool,
) -> Result<Placement, CoreError> {
    ensure_safe_parents(root, dest)?;
    let parent = dest
        .parent()
        .ok_or_else(|| CoreError::Sync("dest has no parent".into()))?;
    let tmp = parent.join(format!(".lam-link-{}", unique_suffix(dest)));
    let _ = remove_stubborn_file(&tmp);

    let placement = match std::fs::hard_link(cas_path, &tmp) {
        Ok(_) => Placement::Hardlink,
        Err(_) => {
            clone_or_copy(cas_path, &tmp)?;
            set_mode(&tmp, object_mode(executable));
            Placement::Copy
        }
    };
    replace_file(&tmp, dest)?;
    Ok(placement)
}

fn materialize_writable(
    root: &Path,
    cas_path: &Path,
    dest: &Path,
    executable: bool,
) -> Result<(), CoreError> {
    ensure_safe_parents(root, dest)?;
    let parent = dest
        .parent()
        .ok_or_else(|| CoreError::Sync("dest has no parent".into()))?;
    let tmp = parent.join(format!(".lam-seed-{}", unique_suffix(dest)));
    let _ = remove_stubborn_file(&tmp);
    clone_or_copy(cas_path, &tmp).map_err(|e| CoreError::Sync(format!("seed {e}")))?;
    set_mode(&tmp, if executable { 0o755 } else { 0o644 });
    replace_file(&tmp, dest)?;
    Ok(())
}

fn remove_stubborn_file(path: &Path) -> std::io::Result<()> {
    match std::fs::remove_file(path) {
        Ok(()) => Ok(()),
        Err(e) => {
            set_mode(path, 0o644);
            match std::fs::remove_file(path) {
                Ok(()) => Ok(()),
                Err(_) => Err(e),
            }
        }
    }
}

fn replace_file(tmp: &Path, dest: &Path) -> Result<(), CoreError> {
    if std::fs::rename(tmp, dest).is_ok() {
        return Ok(());
    }
    let _ = remove_stubborn_file(dest);
    std::fs::rename(tmp, dest).map_err(|e| {
        let _ = std::fs::remove_file(tmp);
        CoreError::Sync(format!("place {}: {e}", dest.display()))
    })
}

fn unique_suffix(dest: &Path) -> String {
    let name = dest.file_name().and_then(|n| n.to_str()).unwrap_or("f");
    format!("{}-{}", name, dest.as_os_str().len())
}

enum Action {
    Skip,
    Link,
    Seed,
    Leave,
}

struct PlanItem {
    path: String,
    dest: PathBuf,
    policy: FilePolicy,
    algo: i32,
    hex: String,
    value: Vec<u8>,
    size: u64,
    executable: bool,
    action: Action,
    released: bool,
}

pub(crate) fn validate_manifest_path(path: &str) -> Result<(), CoreError> {
    if path.is_empty() || path.starts_with('/') || path.contains('\\') {
        return Err(CoreError::Sync(format!("unsafe manifest path: {path}")));
    }
    for segment in path.split('/') {
        if segment.is_empty() || segment == "." || segment == ".." {
            return Err(CoreError::Sync(format!(
                "unsafe segment in manifest path: {path}"
            )));
        }
    }
    if path == ".laminara" || path.starts_with(".laminara/") {
        return Err(CoreError::Sync(format!(
            "manifest path targets client state: {path}"
        )));
    }
    Ok(())
}

pub struct SyncPlan<'a> {
    pub transport: &'a Transport,
    pub base_url: &'a str,
    pub profile_dir: &'a Path,
    pub state_dir: &'a Path,
    pub cas_dir: &'a Path,
    pub manifest: &'a Manifest,
    pub selection: &'a FeatureSelection,
    pub max_parallel: usize,
    pub cancel: CancellationToken,
}

pub async fn sync(
    plan: SyncPlan<'_>,
    on_progress: impl Fn(SyncProgress) + Send + Sync,
) -> Result<SyncOutcome, CoreError> {
    let SyncPlan {
        transport,
        base_url,
        profile_dir,
        state_dir,
        cas_dir,
        manifest,
        selection,
        max_parallel,
        cancel,
    } = plan;
    let parallel = if max_parallel == 0 {
        DEFAULT_PARALLEL
    } else {
        max_parallel
    };
    tokio::fs::create_dir_all(profile_dir)
        .await
        .map_err(|e| CoreError::Sync(format!("create {}: {e}", profile_dir.display())))?;
    tokio::fs::create_dir_all(state_dir)
        .await
        .map_err(|e| CoreError::Sync(format!("create {}: {e}", state_dir.display())))?;
    let ledger_path = state_dir.join(LEDGER_FILE);
    let previous = load_ledger(&ledger_path);
    mark_unfinished(state_dir)?;

    let optional = features::optional_paths(&manifest.features);
    let (_, active_files) = features::resolve_active(&manifest.features, selection);
    let included: Vec<&ManifestFile> = manifest
        .files
        .iter()
        .filter(|file| !optional.contains(&file.path) || active_files.contains(&file.path))
        .collect();

    on_progress(SyncProgress {
        stage: SyncStage::Planning,
        files_done: 0,
        files_total: 0,
        bytes_done: 0,
        bytes_total: 0,
        current_path: None,
    });

    let mut plan: Vec<PlanItem> = Vec::with_capacity(included.len());
    let mut current_paths: HashSet<String> = HashSet::with_capacity(included.len());
    let mut skipped = 0u64;

    for &file in &included {
        cancelled(&cancel)?;
        validate_manifest_path(&file.path)?;
        let object = file
            .object
            .as_ref()
            .ok_or_else(|| CoreError::Sync("file missing object".into()))?;
        let hash = object
            .hash
            .as_ref()
            .ok_or_else(|| CoreError::Sync("object missing hash".into()))?;
        let hex = hex::encode(&hash.value);
        let policy = FilePolicy::try_from(file.policy).unwrap_or(FilePolicy::Unspecified);
        let dest = profile_dir.join(&file.path);
        current_paths.insert(file.path.clone());

        let (action, released) = match policy {
            FilePolicy::UserWritable => plan_user_writable(&dest, previous.get(&file.path)),
            _ => (
                plan_immutable(
                    cas_dir,
                    hash.algo,
                    &hash.value,
                    &hex,
                    &dest,
                    previous.get(&file.path),
                ),
                false,
            ),
        };
        if matches!(action, Action::Skip | Action::Leave) {
            skipped += 1;
        }
        plan.push(PlanItem {
            path: file.path.clone(),
            dest,
            policy,
            algo: hash.algo,
            hex,
            value: hash.value.clone(),
            size: object.size,
            executable: file.executable,
            action,
            released,
        });
    }

    let mut needed: HashMap<String, Wanted> = HashMap::new();
    for item in &plan {
        if matches!(item.action, Action::Link | Action::Seed) {
            needed
                .entry(item.hex.clone())
                .and_modify(|object| object.executable |= item.executable)
                .or_insert_with(|| Wanted {
                    hex: item.hex.clone(),
                    algo: item.algo,
                    value: item.value.clone(),
                    executable: item.executable,
                    size: item.size,
                    path: item.path.clone(),
                });
        }
    }

    let mut wanted: Vec<Wanted> = Vec::with_capacity(needed.len());
    let mut resumable = 0u64;
    for object in needed.into_values() {
        let cas_path = cas_dir.join(object_key(object.algo, &object.value));
        if cas_path.exists() {
            set_mode(&cas_path, object_mode(object.executable));
            continue;
        }
        resumable += partial_len(&partial_path(&cas_path)).min(object.size);
        wanted.push(object);
    }
    wanted.sort_by(|left, right| left.path.cmp(&right.path));

    let meter = Meter {
        report: &on_progress,
        files_total: wanted.len() as u64,
        bytes_total: wanted.iter().map(|object| object.size).sum(),
        files_done: AtomicU64::new(0),
        bytes_done: AtomicU64::new(resumable),
        current: std::sync::Mutex::new(None),
        last: std::sync::Mutex::new(None),
    };
    meter.publish(true);

    let abort = cancel.child_token();
    let results: Vec<Result<u64, CoreError>> = stream::iter(0..wanted.len())
        .map(|index| {
            download_or_abort(transport, base_url, cas_dir, &wanted[index], &meter, &abort)
        })
        .buffer_unordered(parallel)
        .collect()
        .await;
    meter.publish(true);

    let mut downloaded = 0u64;
    let mut bytes_downloaded = 0u64;
    let mut failure: Option<CoreError> = None;
    for result in results {
        match result {
            Ok(size) => {
                downloaded += 1;
                bytes_downloaded += size;
            }
            Err(error) => {
                let replaces = match &failure {
                    None => true,
                    Some(existing) => {
                        matches!(existing, CoreError::Cancelled)
                            && !matches!(error, CoreError::Cancelled)
                    }
                };
                if replaces {
                    failure = Some(error);
                }
            }
        }
    }
    if let Some(error) = failure {
        return Err(error);
    }

    let mut ledger: Ledger = Ledger::new();
    let mut linked = 0u64;
    for item in &plan {
        cancelled(&cancel)?;
        let cas_path = cas_dir.join(object_key(item.algo, &item.value));
        let (placement, released) = match item.action {
            Action::Skip => (
                previous
                    .get(&item.path)
                    .map(|e| e.placement)
                    .unwrap_or(Placement::Hardlink),
                false,
            ),
            Action::Leave => (
                Placement::Copy,
                item.released || item.dest.symlink_metadata().is_ok(),
            ),
            Action::Link => {
                let placement =
                    materialize_immutable(profile_dir, &cas_path, &item.dest, item.executable)?;
                linked += 1;
                (placement, false)
            }
            Action::Seed => {
                materialize_writable(profile_dir, &cas_path, &item.dest, item.executable)?;
                linked += 1;
                (Placement::Copy, true)
            }
        };
        ledger.insert(
            item.path.clone(),
            OwnedEntry {
                object_hash: item.hex.clone(),
                class: item.policy as i32,
                placement,
                released,
                size: item.size,
                algo: item.algo,
            },
        );
    }

    let pruned = prune(profile_dir, &previous, &current_paths)?;

    save_ledger(&ledger_path, &ledger)?;
    mark_finished(state_dir);
    on_progress(SyncProgress {
        stage: SyncStage::Done,
        files_done: meter.files_total,
        files_total: meter.files_total,
        bytes_done: meter.bytes_total,
        bytes_total: meter.bytes_total,
        current_path: None,
    });

    Ok(SyncOutcome {
        downloaded,
        skipped,
        linked,
        bytes_downloaded,
        pruned,
    })
}

pub fn verify_installed(profile_dir: &Path, state_dir: &Path) -> Vec<String> {
    verify(profile_dir, state_dir, false)
}

pub fn verify_contents(profile_dir: &Path, state_dir: &Path) -> Vec<String> {
    verify(profile_dir, state_dir, true)
}

fn verify(profile_dir: &Path, state_dir: &Path, hash_contents: bool) -> Vec<String> {
    let ledger = load_ledger(&state_dir.join(LEDGER_FILE));
    let mut broken = Vec::new();
    for (path, entry) in &ledger {
        if entry.class == FilePolicy::UserWritable as i32 || entry.released {
            continue;
        }
        let target = profile_dir.join(path);
        let intact = match std::fs::symlink_metadata(&target) {
            Ok(meta) if meta.file_type().is_symlink() => false,
            Ok(meta) if !meta.is_file() => false,
            Ok(meta) => {
                is_read_only(&meta)
                    && (entry.size == 0 || meta.len() == entry.size)
                    && (!(hash_contents || carries_code(path)) || content_matches(&target, entry))
            }
            Err(_) => false,
        };
        if !intact {
            broken.push(path.clone());
        }
    }
    broken
}

fn carries_code(path: &str) -> bool {
    let lower = path.to_ascii_lowercase();
    lower.ends_with(".jar")
        || lower.ends_with(".dll")
        || lower.ends_with(".so")
        || lower.ends_with(".dylib")
}

fn content_matches(path: &Path, entry: &OwnedEntry) -> bool {
    let Ok(bytes) = std::fs::read(path) else {
        return false;
    };
    let mut hasher = Hasher::for_algo(entry.algo);
    hasher.update(&bytes);
    hasher.finalize_hex() == entry.object_hash
}

pub fn discard_installed(
    profile_dir: &Path,
    state_dir: &Path,
    cas_dir: &Path,
    paths: &[String],
) -> Result<u64, CoreError> {
    let ledger = load_ledger(&state_dir.join(LEDGER_FILE));
    let mut discarded = 0u64;
    for path in paths {
        let target = profile_dir.join(path);
        if remove_stubborn_file(&target).is_ok() {
            discarded += 1;
        }
        let Some(entry) = ledger.get(path) else {
            continue;
        };
        let Ok(value) = hex::decode(&entry.object_hash) else {
            continue;
        };
        let _ = remove_stubborn_file(&cas_dir.join(object_key(entry.algo, &value)));
    }
    Ok(discarded)
}

pub fn gc_cas(cas_dir: &Path, install_dir: &Path) -> Result<u64, CoreError> {
    let mut referenced: HashSet<String> = HashSet::new();
    if let Ok(entries) = std::fs::read_dir(install_dir) {
        for entry in entries.flatten() {
            let ledger = load_ledger(&entry.path().join(".laminara").join(LEDGER_FILE));
            for owned in ledger.values() {
                referenced.insert(owned.object_hash.clone());
            }
        }
    }

    let mut removed = 0u64;
    let mut stack = vec![cas_dir.to_path_buf()];
    while let Some(dir) = stack.pop() {
        let Ok(entries) = std::fs::read_dir(&dir) else {
            continue;
        };
        for entry in entries.flatten() {
            let path = entry.path();
            let Ok(meta) = entry.metadata() else { continue };
            if meta.is_dir() {
                stack.push(path);
                continue;
            }
            let Some(name) = path.file_name().and_then(|n| n.to_str()) else {
                continue;
            };
            if name.starts_with(".lam-") {
                continue;
            }
            if !referenced.contains(name) && remove_stubborn_file(&path).is_ok() {
                removed += 1;
            }
        }
    }
    Ok(removed)
}

pub fn scrub_cas(cas_dir: &Path) -> Result<(u64, u64), CoreError> {
    let mut checked = 0u64;
    let mut repaired = 0u64;
    let mut stack = vec![cas_dir.to_path_buf()];
    while let Some(dir) = stack.pop() {
        let Ok(entries) = std::fs::read_dir(&dir) else {
            continue;
        };
        for entry in entries.flatten() {
            let path = entry.path();
            let Ok(meta) = entry.metadata() else { continue };
            if meta.is_dir() {
                stack.push(path);
                continue;
            }
            let Some(name) = path
                .file_name()
                .and_then(|n| n.to_str())
                .map(str::to_string)
            else {
                continue;
            };
            if name.starts_with(".lam-") {
                continue;
            }
            let algo = algo_from_cas_path(&path, cas_dir);
            let Ok(bytes) = std::fs::read(&path) else {
                continue;
            };
            let mut hasher = Hasher::for_algo(algo);
            hasher.update(&bytes);
            checked += 1;
            if hasher.finalize_hex() != name && remove_stubborn_file(&path).is_ok() {
                repaired += 1;
            }
        }
    }
    Ok((checked, repaired))
}

fn algo_from_cas_path(path: &Path, cas_dir: &Path) -> i32 {
    let relative = path.strip_prefix(cas_dir).unwrap_or(path);
    match relative
        .components()
        .next()
        .and_then(|c| c.as_os_str().to_str())
    {
        Some("sha256") => HashAlgo::Sha256 as i32,
        Some("sha1") => HashAlgo::Sha1 as i32,
        _ => HashAlgo::Blake3 as i32,
    }
}

fn plan_user_writable(dest: &Path, previous: Option<&OwnedEntry>) -> (Action, bool) {
    if dest.symlink_metadata().is_ok() {
        return (Action::Leave, true);
    }
    if previous.map(|e| e.released).unwrap_or(false) {
        return (Action::Leave, true);
    }
    (Action::Seed, false)
}

fn plan_immutable(
    cas_dir: &Path,
    algo: i32,
    value: &[u8],
    hex: &str,
    dest: &Path,
    previous: Option<&OwnedEntry>,
) -> Action {
    let cas_path = cas_dir.join(object_key(algo, value));
    let (Ok(dest_meta), Ok(cas_meta)) = (dest.symlink_metadata(), std::fs::metadata(&cas_path))
    else {
        return Action::Link;
    };
    if !dest_meta.is_file() || !is_read_only(&dest_meta) {
        return Action::Link;
    }
    if same_file(dest, &cas_path) {
        return Action::Skip;
    }
    match previous {
        Some(entry)
            if entry.placement == Placement::Copy
                && entry.object_hash == hex
                && !entry.released
                && dest_meta.len() == cas_meta.len() =>
        {
            Action::Skip
        }
        _ => Action::Link,
    }
}

fn prune(
    profile_dir: &Path,
    previous: &Ledger,
    current: &HashSet<String>,
) -> Result<u64, CoreError> {
    let mut pruned = 0u64;
    for (path, entry) in previous {
        if current.contains(path) {
            continue;
        }
        if entry.class == FilePolicy::UserWritable as i32 {
            continue;
        }
        let target = profile_dir.join(path);
        if let Ok(meta) = target.symlink_metadata() {
            if meta.is_file() && remove_stubborn_file(&target).is_ok() {
                pruned += 1;
            }
        }
    }
    Ok(pruned)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;

    #[test]
    fn user_writable_is_private_per_build() {
        let tmp = tempfile::tempdir().unwrap();
        let cas_obj = tmp.path().join("cas-options");
        std::fs::write(&cas_obj, b"default-keybinds").unwrap();

        let a = tmp.path().join("A/options.txt");
        let b = tmp.path().join("B/options.txt");

        materialize_writable(tmp.path(), &cas_obj, &a, false).unwrap();
        materialize_writable(tmp.path(), &cas_obj, &b, false).unwrap();

        #[cfg(unix)]
        {
            use std::os::unix::fs::MetadataExt;
            assert_ne!(
                std::fs::metadata(&a).unwrap().ino(),
                std::fs::metadata(&b).unwrap().ino(),
                "each build must get its OWN private options.txt inode, not a shared one"
            );
        }
        assert_eq!(std::fs::read(&a).unwrap(), b"default-keybinds");

        std::fs::write(&a, b"layout-A").unwrap();
        std::fs::write(&b, b"layout-B").unwrap();

        assert!(matches!(plan_user_writable(&a, None).0, Action::Leave));
        assert!(matches!(plan_user_writable(&b, None).0, Action::Leave));

        assert_eq!(
            std::fs::read(&a).unwrap(),
            b"layout-A",
            "build A keeps its own keybinds"
        );
        assert_eq!(
            std::fs::read(&b).unwrap(),
            b"layout-B",
            "build B keeps its own keybinds"
        );
        assert_eq!(
            std::fs::read(&cas_obj).unwrap(),
            b"default-keybinds",
            "shared CAS object untouched"
        );
    }

    #[cfg(unix)]
    #[test]
    fn refuses_to_write_through_a_symlinked_ancestor() {
        let tmp = tempfile::tempdir().unwrap();
        let root = tmp.path().join("profile");
        let outside = tmp.path().join("outside");
        std::fs::create_dir_all(&root).unwrap();
        std::fs::create_dir_all(&outside).unwrap();
        std::os::unix::fs::symlink(&outside, root.join("mods")).unwrap();

        let err = ensure_safe_parents(&root, &root.join("mods/evil.jar")).unwrap_err();
        assert!(format!("{err}").contains("symlink"), "{err}");
        assert!(!outside.join("evil.jar").exists());
    }

    #[test]
    fn creates_missing_parents_inside_the_profile() {
        let tmp = tempfile::tempdir().unwrap();
        let root = tmp.path().join("profile");
        std::fs::create_dir_all(&root).unwrap();
        ensure_safe_parents(&root, &root.join("a/b/c/file.jar")).unwrap();
        assert!(root.join("a/b/c").is_dir());
    }

    #[test]
    fn an_object_that_lands_in_the_cas_is_read_only() {
        let tmp = tempfile::tempdir().unwrap();
        let cas_path = tmp.path().join("object");
        let partial = partial_path(&cas_path);
        std::fs::write(&partial, b"object").unwrap();

        settle_object(&partial, &cas_path, 0o444).unwrap();

        let meta = std::fs::metadata(&cas_path).unwrap();
        assert!(
            is_read_only(&meta),
            "объект в CAS должен быть только для чтения, иначе проверка целостности назовёт битым весь клиент"
        );
    }

    #[test]
    fn replaces_a_read_only_file() {
        let tmp = tempfile::tempdir().unwrap();
        let cas_obj = tmp.path().join("obj");
        std::fs::write(&cas_obj, b"v2").unwrap();
        let dest = tmp.path().join("dest.jar");
        std::fs::write(&dest, b"v1").unwrap();
        set_mode(&dest, 0o444);

        materialize_immutable(tmp.path(), &cas_obj, &dest, false).unwrap();
        assert_eq!(std::fs::read(&dest).unwrap(), b"v2");
    }

    #[test]
    fn a_file_edited_in_place_is_caught() {
        let tmp = tempfile::tempdir().unwrap();
        let profile = tmp.path().join("profile");
        let state = profile.join(".laminara");
        std::fs::create_dir_all(&state).unwrap();

        let jar = profile.join("mods/signed.jar");
        std::fs::create_dir_all(jar.parent().unwrap()).unwrap();
        let original = b"jar with a signature inside".to_vec();
        std::fs::write(&jar, &original).unwrap();
        set_mode(&jar, 0o444);

        let mut hasher = Hasher::for_algo(HashAlgo::Blake3 as i32);
        hasher.update(&original);
        let mut ledger: Ledger = BTreeMap::new();
        ledger.insert(
            "mods/signed.jar".into(),
            OwnedEntry {
                object_hash: hasher.finalize_hex(),
                class: FilePolicy::Unspecified as i32,
                placement: Placement::Hardlink,
                released: false,
                size: original.len() as u64,
                algo: HashAlgo::Blake3 as i32,
            },
        );
        save_ledger(&state.join(LEDGER_FILE), &ledger).unwrap();

        assert!(verify_installed(&profile, &state).is_empty());
        assert!(verify_contents(&profile, &state).is_empty());

        set_mode(&jar, 0o644);
        std::fs::write(&jar, b"jar with the signature stripped").unwrap();
        set_mode(&jar, 0o444);

        assert_eq!(
            verify_installed(&profile, &state),
            vec!["mods/signed.jar".to_string()],
            "антивирус переписал файл — размер разошёлся, проверка обязана это увидеть"
        );

        set_mode(&jar, 0o644);
        let mut same_length = original.clone();
        *same_length.last_mut().unwrap() = b'!';
        std::fs::write(&jar, &same_length).unwrap();
        set_mode(&jar, 0o444);

        assert_eq!(
            verify_installed(&profile, &state),
            vec!["mods/signed.jar".to_string()],
            "подмена той же длины ловится и быстрой проверкой: у jar всегда сверяется содержимое"
        );
        assert_eq!(
            verify_contents(&profile, &state),
            vec!["mods/signed.jar".to_string()],
            "полная проверка обязана поймать подмену той же длины"
        );
    }

    #[test]
    fn an_asset_of_the_same_size_is_not_rehashed_on_every_launch() {
        let tmp = tempfile::tempdir().unwrap();
        let profile = tmp.path().join("profile");
        let state = profile.join(".laminara");
        std::fs::create_dir_all(&state).unwrap();

        let asset = profile.join("assets/objects/ab/abcdef");
        std::fs::create_dir_all(asset.parent().unwrap()).unwrap();
        std::fs::write(&asset, b"sound").unwrap();
        set_mode(&asset, 0o444);

        let mut ledger: Ledger = BTreeMap::new();
        ledger.insert(
            "assets/objects/ab/abcdef".into(),
            OwnedEntry {
                object_hash: "не тот хеш".into(),
                class: FilePolicy::Unspecified as i32,
                placement: Placement::Hardlink,
                released: false,
                size: 5,
                algo: HashAlgo::Blake3 as i32,
            },
        );
        save_ledger(&state.join(LEDGER_FILE), &ledger).unwrap();

        assert!(
            verify_installed(&profile, &state).is_empty(),
            "ассеты перед каждым запуском не пересчитываются — иначе старт упрётся в диск"
        );
        assert_eq!(verify_contents(&profile, &state).len(), 1);
    }

    #[test]
    fn discarding_a_broken_file_drops_its_object_too() {
        let tmp = tempfile::tempdir().unwrap();
        let profile = tmp.path().join("profile");
        let state = profile.join(".laminara");
        let cas = tmp.path().join("objects");
        std::fs::create_dir_all(&state).unwrap();

        let value = vec![0xAAu8, 0xBB];
        let hex = hex::encode(&value);
        let object = cas.join(object_key(HashAlgo::Blake3 as i32, &value));
        std::fs::create_dir_all(object.parent().unwrap()).unwrap();
        std::fs::write(&object, b"payload").unwrap();
        set_mode(&object, 0o444);

        let jar = profile.join("mods/broken.jar");
        std::fs::create_dir_all(jar.parent().unwrap()).unwrap();
        std::fs::write(&jar, b"payload").unwrap();
        set_mode(&jar, 0o444);

        let mut ledger: Ledger = BTreeMap::new();
        ledger.insert(
            "mods/broken.jar".into(),
            OwnedEntry {
                object_hash: hex,
                class: FilePolicy::Unspecified as i32,
                placement: Placement::Hardlink,
                released: false,
                size: 7,
                algo: HashAlgo::Blake3 as i32,
            },
        );
        save_ledger(&state.join(LEDGER_FILE), &ledger).unwrap();

        let discarded =
            discard_installed(&profile, &state, &cas, &["mods/broken.jar".to_string()]).unwrap();

        assert_eq!(discarded, 1);
        assert!(!jar.exists(), "испорченный файл удалён");
        assert!(
            !object.exists(),
            "объект тоже удалён, иначе синхронизация снова разложит ту же порчу"
        );
    }

    #[test]
    fn verify_installed_reports_missing_and_writable_immutables() {
        let tmp = tempfile::tempdir().unwrap();
        let profile = tmp.path().join("profile");
        let state = profile.join(".laminara");
        std::fs::create_dir_all(&state).unwrap();

        let present = profile.join("mods/ok.jar");
        std::fs::create_dir_all(present.parent().unwrap()).unwrap();
        std::fs::write(&present, b"x").unwrap();
        set_mode(&present, 0o444);

        let tampered = profile.join("mods/tampered.jar");
        std::fs::write(&tampered, b"x").unwrap();

        let user = profile.join("options.txt");
        std::fs::write(&user, b"x").unwrap();

        let mut of_x = Hasher::for_algo(HashAlgo::Blake3 as i32);
        of_x.update(b"x");
        let hash_of_x = of_x.finalize_hex();

        let mut ledger: Ledger = BTreeMap::new();
        for (path, class) in [
            ("mods/ok.jar", FilePolicy::Unspecified as i32),
            ("mods/tampered.jar", FilePolicy::Unspecified as i32),
            ("mods/gone.jar", FilePolicy::Unspecified as i32),
            ("options.txt", FilePolicy::UserWritable as i32),
        ] {
            ledger.insert(
                path.to_string(),
                OwnedEntry {
                    object_hash: hash_of_x.clone(),
                    class,
                    placement: Placement::Hardlink,
                    released: false,
                    size: 0,
                    algo: HashAlgo::Blake3 as i32,
                },
            );
        }
        save_ledger(&state.join(LEDGER_FILE), &ledger).unwrap();

        let mut broken = verify_installed(&profile, &state);
        broken.sort();
        assert_eq!(
            broken,
            vec!["mods/gone.jar".to_string(), "mods/tampered.jar".to_string()]
        );
    }

    #[test]
    fn gc_cas_keeps_referenced_objects_only() {
        let tmp = tempfile::tempdir().unwrap();
        let cas = tmp.path().join("objects/blake3/ab/cd");
        let install = tmp.path().join("games");
        std::fs::create_dir_all(&cas).unwrap();

        let kept = cas.join("aabb");
        let orphan = cas.join("ccdd");
        std::fs::write(&kept, b"k").unwrap();
        std::fs::write(&orphan, b"o").unwrap();

        let state = install.join("Survival/.laminara");
        std::fs::create_dir_all(&state).unwrap();
        let mut ledger: Ledger = BTreeMap::new();
        ledger.insert(
            "mods/a.jar".into(),
            OwnedEntry {
                object_hash: "aabb".into(),
                class: 0,
                placement: Placement::Hardlink,
                released: false,
                size: 0,
                algo: 0,
            },
        );
        save_ledger(&state.join(LEDGER_FILE), &ledger).unwrap();

        let removed = gc_cas(&tmp.path().join("objects"), &install).unwrap();
        assert_eq!(removed, 1);
        assert!(kept.exists(), "referenced object must survive");
        assert!(!orphan.exists(), "unreferenced object must be collected");
    }

    #[test]
    fn scrub_drops_corrupted_objects() {
        let tmp = tempfile::tempdir().unwrap();
        let cas = tmp.path().join("blake3/ab/cd");
        std::fs::create_dir_all(&cas).unwrap();

        let good_bytes = b"healthy object";
        let good_name = blake3::hash(good_bytes).to_hex().to_string();
        let good = cas.join(&good_name);
        std::fs::write(&good, good_bytes).unwrap();

        let rotten = cas.join("deadbeefdeadbeef");
        std::fs::write(&rotten, b"corrupted").unwrap();

        let (checked, repaired) = scrub_cas(tmp.path()).unwrap();
        assert_eq!(checked, 2);
        assert_eq!(repaired, 1);
        assert!(good.exists(), "intact object must survive the scrub");
        assert!(!rotten.exists(), "corrupted object must be dropped");
    }

    #[tokio::test]
    async fn syncs_into_a_directory_that_does_not_exist_yet() {
        let temp = tempfile::tempdir().unwrap();
        let profile = temp.path().join("fresh-install");
        let state = profile.join(".laminara");
        let cas = temp.path().join("objects");
        let outcome = sync(
            SyncPlan {
                transport: &Transport::default(),
                base_url: "http://127.0.0.1:1",
                profile_dir: &profile,
                state_dir: &state,
                cas_dir: &cas,
                manifest: &Manifest::default(),
                selection: &FeatureSelection::default(),
                max_parallel: 2,
                cancel: CancellationToken::new(),
            },
            |_| {},
        )
        .await
        .expect("an empty manifest must still lay out the profile");
        assert_eq!(outcome.downloaded, 0);
        assert!(profile.is_dir(), "the profile directory must be created");
        assert!(state.is_dir(), "the state directory must be created");
    }

    struct ObjectServer {
        base_url: String,
        requests: Arc<std::sync::Mutex<Vec<String>>>,
    }

    #[derive(Clone)]
    enum Reply {
        Serve,
        Cut(usize),
        Whole,
        Wrong(Vec<u8>),
        NotFound,
    }

    async fn object_server(content: Vec<u8>, script: Vec<Reply>) -> ObjectServer {
        use tokio::io::{AsyncReadExt, AsyncWriteExt};
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let base_url = format!("http://{}", listener.local_addr().unwrap());
        let requests = Arc::new(std::sync::Mutex::new(Vec::new()));
        let seen = requests.clone();
        tokio::spawn(async move {
            let mut served = 0usize;
            while let Ok((mut socket, _)) = listener.accept().await {
                let mut head = Vec::new();
                let mut byte = [0u8; 1];
                while !head.ends_with(b"\r\n\r\n") {
                    if socket.read_exact(&mut byte).await.is_err() {
                        break;
                    }
                    head.push(byte[0]);
                }
                let text = String::from_utf8_lossy(&head).to_ascii_lowercase();
                seen.lock().unwrap().push(text.clone());
                let reply = script[served.min(script.len() - 1)].clone();
                served += 1;
                let requested = text.lines().find_map(|line| {
                    line.strip_prefix("range: bytes=")
                        .and_then(|range| range.trim_end_matches('-').parse::<usize>().ok())
                });
                let (status, extra, body, cut) = match reply {
                    Reply::NotFound => ("404 Not Found", String::new(), Vec::new(), None),
                    Reply::Wrong(body) => ("200 OK", String::new(), body, None),
                    Reply::Whole => ("200 OK", String::new(), content.clone(), None),
                    Reply::Serve | Reply::Cut(_) => {
                        let cut = match reply {
                            Reply::Cut(at) => Some(at),
                            _ => None,
                        };
                        match requested {
                            Some(from) => (
                                "206 Partial Content",
                                format!(
                                    "Content-Range: bytes {from}-{}/{}\r\n",
                                    content.len() - 1,
                                    content.len()
                                ),
                                content[from..].to_vec(),
                                cut,
                            ),
                            None => ("200 OK", String::new(), content.clone(), cut),
                        }
                    }
                };
                let header = format!(
                    "HTTP/1.1 {status}\r\nContent-Length: {}\r\n{extra}Connection: close\r\n\r\n",
                    body.len()
                );
                let _ = socket.write_all(header.as_bytes()).await;
                let _ = socket.write_all(&body[..cut.unwrap_or(body.len())]).await;
                let _ = socket.shutdown().await;
            }
        });
        ObjectServer { base_url, requests }
    }

    fn one_file_manifest(path: &str, content: &[u8]) -> Manifest {
        Manifest {
            files: vec![ManifestFile {
                path: path.into(),
                object: Some(crate::proto::core::v1::ObjectRef {
                    hash: Some(crate::proto::core::v1::Hash {
                        algo: HashAlgo::Blake3 as i32,
                        value: blake3::hash(content).as_bytes().to_vec(),
                    }),
                    size: content.len() as u64,
                }),
                ..Default::default()
            }],
            ..Default::default()
        }
    }

    fn partial_of(root: &Path, content: &[u8]) -> PathBuf {
        let key = object_key(HashAlgo::Blake3 as i32, blake3::hash(content).as_bytes());
        partial_path(&root.join("objects").join(key))
    }

    fn leave_partial(root: &Path, content: &[u8], len: usize) {
        let partial = partial_of(root, content);
        std::fs::create_dir_all(partial.parent().unwrap()).unwrap();
        std::fs::write(&partial, &content[..len]).unwrap();
    }

    async fn sync_one(
        base_url: &str,
        root: &Path,
        manifest: &Manifest,
    ) -> (Result<SyncOutcome, CoreError>, Vec<SyncProgress>) {
        let events = std::sync::Mutex::new(Vec::new());
        let outcome = sync(
            SyncPlan {
                transport: &Transport::default(),
                base_url,
                profile_dir: &root.join("build"),
                state_dir: &root.join("build").join(".laminara"),
                cas_dir: &root.join("objects"),
                manifest,
                selection: &FeatureSelection::default(),
                max_parallel: 1,
                cancel: CancellationToken::new(),
            },
            |progress| events.lock().unwrap().push(progress),
        )
        .await;
        (outcome, events.into_inner().unwrap())
    }

    fn patterned(len: u32) -> Vec<u8> {
        (0..len).map(|index| (index % 251) as u8).collect()
    }

    fn downloading(events: &[SyncProgress]) -> Vec<&SyncProgress> {
        events
            .iter()
            .filter(|event| event.stage == SyncStage::Downloading)
            .collect()
    }

    #[tokio::test]
    async fn a_download_cut_halfway_resumes_from_where_it_stopped() {
        let content = patterned(200_000);
        let server = object_server(content.clone(), vec![Reply::Cut(90_000), Reply::Serve]).await;
        let temp = tempfile::tempdir().unwrap();
        let (outcome, events) = sync_one(
            &server.base_url,
            temp.path(),
            &one_file_manifest("mods/big.jar", &content),
        )
        .await;

        outcome.expect("the cut download must finish on the next attempt");
        let requests = server.requests.lock().unwrap().clone();
        assert_eq!(requests.len(), 2, "one cut request and one resumed request");
        assert!(
            requests[1].contains("range: bytes=90000-"),
            "the second request must ask only for the missing tail: {}",
            requests[1]
        );
        assert_eq!(
            std::fs::read(temp.path().join("build/mods/big.jar")).unwrap(),
            content
        );
        let progress = downloading(&events);
        assert!(progress
            .iter()
            .all(|event| event.bytes_done <= event.bytes_total));
        assert_eq!(progress.last().unwrap().bytes_done, content.len() as u64);
        assert_eq!(
            progress.last().unwrap().current_path.as_deref(),
            Some("mods/big.jar")
        );
    }

    #[tokio::test]
    async fn a_server_without_ranges_gets_a_clean_restart() {
        let content = patterned(120_000);
        let server = object_server(content.clone(), vec![Reply::Cut(50_000), Reply::Whole]).await;
        let temp = tempfile::tempdir().unwrap();
        let (outcome, events) = sync_one(
            &server.base_url,
            temp.path(),
            &one_file_manifest("mods/a.jar", &content),
        )
        .await;

        outcome.expect("a full answer to a range request must restart the file, not corrupt it");
        assert_eq!(
            std::fs::read(temp.path().join("build/mods/a.jar")).unwrap(),
            content
        );
        assert!(downloading(&events)
            .iter()
            .all(|event| event.bytes_done <= event.bytes_total));
        assert!(
            !partial_of(temp.path(), &content).exists(),
            "a finished object leaves no partial behind"
        );
    }

    #[tokio::test]
    async fn a_missing_object_fails_at_once_without_retries() {
        let content = patterned(1_000);
        let server = object_server(content.clone(), vec![Reply::NotFound]).await;
        let temp = tempfile::tempdir().unwrap();
        let (outcome, _) = sync_one(
            &server.base_url,
            temp.path(),
            &one_file_manifest("mods/gone.jar", &content),
        )
        .await;

        let error = outcome.expect_err("a 404 cannot be fixed by asking again");
        assert!(
            error.to_string().contains("404"),
            "the reason must name the answer: {error}"
        );
        assert_eq!(server.requests.lock().unwrap().len(), 1);
    }

    #[tokio::test]
    async fn a_complete_partial_is_installed_without_asking_the_server() {
        let content = patterned(30_000);
        let temp = tempfile::tempdir().unwrap();
        leave_partial(temp.path(), &content, content.len());
        let server = object_server(content.clone(), vec![Reply::Serve]).await;
        let (outcome, events) = sync_one(
            &server.base_url,
            temp.path(),
            &one_file_manifest("mods/done.jar", &content),
        )
        .await;

        outcome.expect("a partial that already holds the whole file must simply be kept");
        assert!(server.requests.lock().unwrap().is_empty());
        assert_eq!(
            std::fs::read(temp.path().join("build/mods/done.jar")).unwrap(),
            content
        );
        assert_eq!(
            downloading(&events).last().unwrap().bytes_done,
            content.len() as u64
        );
    }

    #[tokio::test]
    async fn a_stranger_answering_the_range_request_does_not_erase_the_partial() {
        let content = patterned(150_000);
        let temp = tempfile::tempdir().unwrap();
        leave_partial(temp.path(), &content, 60_000);
        let portal = b"<html>please sign in to the wifi</html>".to_vec();
        let server = object_server(content.clone(), vec![Reply::Wrong(portal), Reply::Serve]).await;
        let (outcome, events) = sync_one(
            &server.base_url,
            temp.path(),
            &one_file_manifest("mods/c.jar", &content),
        )
        .await;

        outcome.expect("after the portal lets go the download must continue");
        let requests = server.requests.lock().unwrap().clone();
        assert_eq!(requests.len(), 2);
        assert!(
            requests[1].contains("range: bytes=60000-"),
            "the kept partial must be resumed, not refetched: {}",
            requests[1]
        );
        assert_eq!(
            std::fs::read(temp.path().join("build/mods/c.jar")).unwrap(),
            content
        );
        assert!(downloading(&events)
            .iter()
            .all(|event| event.bytes_done <= event.bytes_total));
    }

    #[tokio::test]
    async fn an_object_broken_on_the_server_is_given_up_after_two_whole_downloads() {
        let content = patterned(40_000);
        let mut rotten = content.clone();
        rotten[123] ^= 0xff;
        let server = object_server(content.clone(), vec![Reply::Wrong(rotten)]).await;
        let temp = tempfile::tempdir().unwrap();
        let (outcome, events) = sync_one(
            &server.base_url,
            temp.path(),
            &one_file_manifest("mods/rot.jar", &content),
        )
        .await;

        let error = outcome.expect_err("an object that never matches its hash cannot be installed");
        assert!(error.to_string().contains("хеш"), "{error}");
        assert_eq!(server.requests.lock().unwrap().len(), 2);
        assert!(downloading(&events)
            .iter()
            .all(|event| event.bytes_done <= event.bytes_total));
    }

    #[test]
    fn rejects_unsafe_paths() {
        assert!(validate_manifest_path("mods/a.jar").is_ok());
        assert!(validate_manifest_path("../escape").is_err());
        assert!(validate_manifest_path("/abs").is_err());
        assert!(validate_manifest_path("a\\b").is_err());
        assert!(validate_manifest_path(".laminara/state").is_err());
        assert!(validate_manifest_path("a/../b").is_err());
    }

    #[test]
    fn prune_removes_deselected_enforced_keeps_user_writable() {
        let dir = tempfile::tempdir().unwrap();
        let profile = dir.path();

        let enforced = "config/anticheat/rules.toml";
        let enforced_target = profile.join(enforced);
        std::fs::create_dir_all(enforced_target.parent().unwrap()).unwrap();
        std::fs::write(&enforced_target, b"x").unwrap();

        let writable = "options.txt";
        let writable_target = profile.join(writable);
        std::fs::write(&writable_target, b"y").unwrap();

        let mut previous: Ledger = BTreeMap::new();
        previous.insert(
            enforced.to_string(),
            OwnedEntry {
                object_hash: String::new(),
                class: FilePolicy::Enforced as i32,
                placement: Placement::Copy,
                released: false,
                size: 0,
                algo: 0,
            },
        );
        previous.insert(
            writable.to_string(),
            OwnedEntry {
                object_hash: String::new(),
                class: FilePolicy::UserWritable as i32,
                placement: Placement::Copy,
                released: false,
                size: 0,
                algo: 0,
            },
        );

        let pruned = prune(profile, &previous, &HashSet::new()).unwrap();
        assert_eq!(pruned, 1);
        assert!(
            !enforced_target.exists(),
            "deselected enforced optional file must be pruned"
        );
        assert!(
            writable_target.exists(),
            "user_writable file must be preserved"
        );
    }
}

#[cfg(test)]
mod live {
    use super::*;
    use crate::config::EndpointConfig;
    use crate::endpoint::EndpointPool;
    use crate::manifest::verify_and_decode;
    use crate::transport::Transport;
    use ed25519_dalek::SigningKey;

    #[tokio::test]
    async fn cas_dedup_across_builds() {
        if std::env::var("LAMINARA_CLIENT_E2E").is_err() {
            return;
        }
        let base =
            std::env::var("LAMINARA_BASE").unwrap_or_else(|_| "http://127.0.0.1:8099".into());
        let key_path = std::env::var("LAMINARA_KEY").expect("LAMINARA_KEY");
        let seed: [u8; 32] = hex::decode(std::fs::read_to_string(&key_path).unwrap().trim())
            .unwrap()
            .as_slice()
            .try_into()
            .unwrap();
        let verifying_key = SigningKey::from_bytes(&seed).verifying_key();

        let transport = Transport::default();
        let pool = EndpointPool::new(
            transport.clone(),
            vec![EndpointConfig {
                id: "eu".into(),
                base_url: base.clone(),
            }],
        );
        pool.login(
            "neo".into(),
            "matrix".into(),
            String::new(),
            None,
            "0.1.0-test".into(),
        )
        .await
        .expect("login");
        let name = pool
            .list_profiles()
            .await
            .expect("list")
            .first()
            .expect("profile")
            .name
            .clone();
        let response = pool.get_manifest(name).await.expect("manifest");
        let verified = verify_and_decode(&[verifying_key], &response.manifest, &response.signature)
            .expect("verify");

        let temp = tempfile::tempdir().unwrap();
        let cas = temp.path().join("objects");
        let cancel = CancellationToken::new();

        let a_dir = temp.path().join("A");
        let a_state = a_dir.join(".laminara");
        let first = sync(
            SyncPlan {
                transport: &transport,
                base_url: &base,
                profile_dir: &a_dir,
                state_dir: &a_state,
                cas_dir: &cas,
                manifest: &verified.manifest,
                selection: &FeatureSelection::default(),
                max_parallel: 8,
                cancel: cancel.clone(),
            },
            |_| {},
        )
        .await
        .expect("A");
        eprintln!(
            "A: downloaded={} linked={} skipped={}",
            first.downloaded, first.linked, first.skipped
        );
        assert!(first.downloaded > 0 && first.linked > 0);

        let b_dir = temp.path().join("B");
        let b_state = b_dir.join(".laminara");
        let second = sync(
            SyncPlan {
                transport: &transport,
                base_url: &base,
                profile_dir: &b_dir,
                state_dir: &b_state,
                cas_dir: &cas,
                manifest: &verified.manifest,
                selection: &FeatureSelection::default(),
                max_parallel: 8,
                cancel: cancel.clone(),
            },
            |_| {},
        )
        .await
        .expect("B");
        eprintln!(
            "B: downloaded={} linked={} skipped={}",
            second.downloaded, second.linked, second.skipped
        );
        assert_eq!(
            second.downloaded, 0,
            "build B must reuse the shared CAS with zero downloads"
        );
        assert!(second.linked > 0);

        let third = sync(
            SyncPlan {
                transport: &transport,
                base_url: &base,
                profile_dir: &a_dir,
                state_dir: &a_state,
                cas_dir: &cas,
                manifest: &verified.manifest,
                selection: &FeatureSelection::default(),
                max_parallel: 8,
                cancel,
            },
            |_| {},
        )
        .await
        .expect("A2");
        eprintln!(
            "A2: downloaded={} skipped={}",
            third.downloaded, third.skipped
        );
        assert_eq!(third.downloaded, 0);
        assert_eq!(third.skipped as usize, verified.manifest.files.len());

        #[cfg(unix)]
        {
            use std::os::unix::fs::MetadataExt;
            let sample = &verified.manifest.files[0].path;
            let am = std::fs::metadata(a_dir.join(sample)).unwrap();
            let bm = std::fs::metadata(b_dir.join(sample)).unwrap();
            assert_eq!(
                am.ino(),
                bm.ino(),
                "A and B share one inode (cross-build dedup)"
            );
            assert!(is_read_only(&am), "immutable file is read-only");
        }
    }
}
