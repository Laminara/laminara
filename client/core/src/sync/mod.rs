pub mod statcache;

use std::collections::{BTreeMap, HashMap, HashSet};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

use futures::stream::{self, StreamExt};
use serde::{Deserialize, Serialize};
use sha2::Digest;
use tokio_util::sync::CancellationToken;

use crate::error::CoreError;
use crate::features::{self, FeatureSelection};
use crate::manifest::object_key;
use crate::proto::core::v1::{FilePolicy, HashAlgo, Manifest, ManifestFile};
use crate::transport::Transport;

const DEFAULT_PARALLEL: usize = 8;
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
}

pub(crate) type Ledger = BTreeMap<String, OwnedEntry>;

fn load_ledger(path: &Path) -> Ledger {
    std::fs::read_to_string(path)
        .ok()
        .and_then(|t| serde_json::from_str(&t).ok())
        .unwrap_or_default()
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

async fn ensure_object(
    transport: &Transport,
    base_url: &str,
    cas_dir: &Path,
    algo: i32,
    expected_hex: &str,
    value: &[u8],
    executable: bool,
) -> Result<(PathBuf, u64, bool), CoreError> {
    let key = object_key(algo, value);
    let cas_path = cas_dir.join(&key);
    let object_mode = if executable { 0o555 } else { 0o444 };

    if let Ok(meta) = std::fs::metadata(&cas_path) {
        if executable {
            set_mode(&cas_path, object_mode);
        }
        return Ok((cas_path, meta.len(), false));
    }

    let parent = cas_path
        .parent()
        .ok_or_else(|| CoreError::Sync("cas path has no parent".into()))?;
    tokio::fs::create_dir_all(parent).await?;

    let url = format!("{}/objects/{}", base_url.trim_end_matches('/'), key);
    let response = transport
        .authorize(transport.client().get(&url))
        .send()
        .await
        .map_err(|e| CoreError::Sync(format!("get {expected_hex}: {e}")))?
        .error_for_status()
        .map_err(|e| CoreError::Sync(format!("get {expected_hex}: {e}")))?;

    let mut temp = tempfile::Builder::new()
        .prefix(".lam-")
        .tempfile_in(parent)
        .map_err(|e| CoreError::Sync(e.to_string()))?;
    let mut hasher = Hasher::for_algo(algo);
    let mut size = 0u64;
    let mut stream = response.bytes_stream();
    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|e| CoreError::Sync(format!("stream {expected_hex}: {e}")))?;
        hasher.update(&chunk);
        size += chunk.len() as u64;
        temp.as_file_mut()
            .write_all(&chunk)
            .map_err(|e| CoreError::Sync(e.to_string()))?;
    }
    if hasher.finalize_hex() != expected_hex {
        return Err(CoreError::Sync(format!(
            "hash mismatch for object {expected_hex}"
        )));
    }
    set_mode(temp.path(), object_mode);

    match temp.persist(&cas_path) {
        Ok(_) => {}
        Err(e) => {
            if !cas_path.exists() {
                return Err(CoreError::Sync(format!("persist object: {e}")));
            }
        }
    }
    Ok((cas_path, size, true))
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
            set_mode(&tmp, if executable { 0o555 } else { 0o444 });
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

fn cancelled(token: &CancellationToken) -> Result<(), CoreError> {
    if token.is_cancelled() {
        Err(CoreError::Sync("cancelled".into()))
    } else {
        Ok(())
    }
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

    let optional = features::optional_paths(&manifest.features);
    let (_, active_files) = features::resolve_active(&manifest.features, selection);
    let included: Vec<&ManifestFile> = manifest
        .files
        .iter()
        .filter(|file| !optional.contains(&file.path) || active_files.contains(&file.path))
        .collect();

    let files_total = included.len() as u64;
    let bytes_total: u64 = included
        .iter()
        .filter_map(|f| f.object.as_ref())
        .map(|o| o.size)
        .sum();
    on_progress(SyncProgress {
        stage: SyncStage::Planning,
        files_done: 0,
        files_total,
        bytes_done: 0,
        bytes_total,
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
            executable: file.executable,
            action,
            released,
        });
    }

    let mut needed: HashMap<String, (i32, Vec<u8>, bool)> = HashMap::new();
    for item in &plan {
        if matches!(item.action, Action::Link | Action::Seed) {
            let entry =
                needed
                    .entry(item.hex.clone())
                    .or_insert((item.algo, item.value.clone(), false));
            entry.2 |= item.executable;
        }
    }

    let bytes_done = Arc::new(AtomicU64::new(0));
    let downloaded_count = Arc::new(AtomicU64::new(0));
    on_progress(SyncProgress {
        stage: SyncStage::Downloading,
        files_done: skipped,
        files_total,
        bytes_done: 0,
        bytes_total,
        current_path: None,
    });

    let ingests: Vec<Result<(u64, bool), CoreError>> = stream::iter(needed.into_iter().map(|(hex, (algo, value, executable))| {
        let transport = transport.clone();
        let base = base_url.to_string();
        let cas_dir = cas_dir.to_path_buf();
        let cancel = cancel.clone();
        let bytes_done = bytes_done.clone();
        let downloaded_count = downloaded_count.clone();
        let progress = &on_progress;
        async move {
            tokio::select! {
                _ = cancel.cancelled() => Err(CoreError::Sync("cancelled".into())),
                result = ensure_object(&transport, &base, &cas_dir, algo, &hex, &value, executable) => {
                    let (_, size, downloaded) = result?;
                    if downloaded {
                        let total = bytes_done.fetch_add(size, Ordering::Relaxed) + size;
                        let done = downloaded_count.fetch_add(1, Ordering::Relaxed) + 1;
                        progress(SyncProgress { stage: SyncStage::Downloading, files_done: skipped + done, files_total, bytes_done: total, bytes_total, current_path: None });
                    }
                    Ok((size, downloaded))
                }
            }
        }
    }))
    .buffer_unordered(parallel)
    .collect()
    .await;

    let mut downloaded = 0u64;
    let mut bytes_downloaded = 0u64;
    for ingest in ingests {
        let (size, was_downloaded) = ingest?;
        if was_downloaded {
            downloaded += 1;
            bytes_downloaded += size;
        }
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
            },
        );
    }

    let pruned = prune(profile_dir, &previous, &current_paths)?;

    save_ledger(&ledger_path, &ledger)?;
    on_progress(SyncProgress {
        stage: SyncStage::Done,
        files_done: files_total,
        files_total,
        bytes_done: bytes_total,
        bytes_total,
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
    let ledger = load_ledger(&state_dir.join(LEDGER_FILE));
    let mut broken = Vec::new();
    for (path, entry) in &ledger {
        if entry.class == FilePolicy::UserWritable as i32 || entry.released {
            continue;
        }
        let target = profile_dir.join(path);
        match std::fs::symlink_metadata(&target) {
            Ok(meta) if meta.file_type().is_symlink() => broken.push(path.clone()),
            Ok(meta) if !meta.is_file() => broken.push(path.clone()),
            Ok(meta) => {
                if !is_read_only(&meta) {
                    broken.push(path.clone());
                }
            }
            Err(_) => broken.push(path.clone()),
        }
    }
    broken
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
                    object_hash: "h".into(),
                    class,
                    placement: Placement::Hardlink,
                    released: false,
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
            },
        );
        previous.insert(
            writable.to_string(),
            OwnedEntry {
                object_hash: String::new(),
                class: FilePolicy::UserWritable as i32,
                placement: Placement::Copy,
                released: false,
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
