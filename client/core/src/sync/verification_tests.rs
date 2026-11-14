use super::*;

#[tokio::test(flavor = "current_thread")]
async fn file_workers_leave_the_async_executor_available() {
    let (started, running) = tokio::sync::oneshot::channel();
    let (release, released) = std::sync::mpsc::sync_channel(1);
    let sibling = tokio::spawn(async move {
        running.await.unwrap();
        release.send(()).unwrap();
    });
    disk_work(&CancellationToken::new(), &|_| {}, move |_, _| {
        started.send(()).unwrap();
        released
            .recv_timeout(Duration::from_secs(2))
            .map_err(|error| CoreError::Sync(error.to_string()))?;
        Ok(())
    })
    .await
    .unwrap();
    sibling.await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn dropping_verification_cancels_its_worker() {
    let (started, running) = tokio::sync::oneshot::channel();
    let (finished, stopped) = tokio::sync::oneshot::channel();
    let task = tokio::spawn(async move {
        disk_work(&CancellationToken::new(), &|_| {}, move |cancel, _| {
            started.send(()).unwrap();
            while !cancel.is_cancelled() {
                std::thread::yield_now();
            }
            finished.send(()).unwrap();
            Err::<(), _>(CoreError::Cancelled)
        })
        .await
    });
    running.await.unwrap();
    task.abort();
    tokio::time::timeout(Duration::from_secs(2), stopped)
        .await
        .unwrap()
        .unwrap();
    assert!(task.await.unwrap_err().is_cancelled());
}

#[test]
fn verification_stops_between_read_blocks() {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("large.jar");
    let bytes = vec![42u8; 4 * HASH_READ_BYTES];
    std::fs::write(&path, &bytes).unwrap();
    let hash = blake3::hash(&bytes).to_hex().to_string();
    let cancel = CancellationToken::new();
    let mut read = 0;
    let result = file_matches_with(
        &path,
        HashAlgo::Blake3 as i32,
        &hash,
        Some(bytes.len() as u64),
        |count| {
            read = count;
            if count > 0 {
                cancel.cancel();
            }
            cancelled(&cancel)
        },
    );
    assert!(matches!(result, Err(CoreError::Cancelled)));
    assert_eq!(read, HASH_READ_BYTES as u64);
}

struct InstalledBuild {
    root: tempfile::TempDir,
    manifest: Manifest,
}

impl InstalledBuild {
    async fn new() -> Self {
        let root = tempfile::tempdir().unwrap();
        let bytes = vec![42u8; 8 * 1024 * 1024];
        let hash = blake3::hash(&bytes);
        let object = root
            .path()
            .join("objects")
            .join(object_key(HashAlgo::Blake3 as i32, hash.as_bytes()).unwrap());
        std::fs::create_dir_all(object.parent().unwrap()).unwrap();
        std::fs::write(object, &bytes).unwrap();
        let build = Self {
            root,
            manifest: Manifest {
                files: vec![ManifestFile {
                    path: "mods/large.jar".into(),
                    object: Some(crate::proto::core::v1::ObjectRef {
                        hash: Some(Hash {
                            algo: HashAlgo::Blake3 as i32,
                            value: hash.as_bytes().to_vec(),
                        }),
                        size: bytes.len() as u64,
                    }),
                    ..Default::default()
                }],
                ..Default::default()
            },
        };
        build.run(CancellationToken::new(), |_| {}).await.unwrap();
        build
    }

    async fn run(
        &self,
        cancel: CancellationToken,
        report: impl Fn(SyncProgress) + Send + Sync,
    ) -> Result<SyncOutcome, CoreError> {
        sync(
            SyncPlan {
                transport: &Transport::default(),
                base_url: "http://127.0.0.1:1",
                profile_dir: &self.root.path().join("profile"),
                state_dir: &self.root.path().join("profile/.laminara"),
                cas_dir: &self.root.path().join("objects"),
                manifest: &self.manifest,
                selection: &FeatureSelection::default(),
                max_parallel: 1,
                cancel,
            },
            report,
        )
        .await
    }
}

#[tokio::test(flavor = "current_thread")]
async fn checking_reports_bytes_and_can_be_cancelled_without_changing_the_installation() {
    let build = InstalledBuild::new().await;
    let state = build.root.path().join("profile/.laminara");
    let previous = std::fs::read(state.join(LEDGER_FILE)).unwrap();
    let cancel = CancellationToken::new();
    let result = build
        .run(cancel.clone(), |event| {
            if event.stage == SyncStage::Planning && event.bytes_done > 0 {
                cancel.cancel();
            }
        })
        .await;
    assert!(
        matches!(result, Err(CoreError::Cancelled)),
        "checking did not respond to cancellation: {result:?}"
    );
    assert_eq!(std::fs::read(state.join(LEDGER_FILE)).unwrap(), previous);
    assert!(install_unfinished(&state));
    assert!(verify_contents(&build.root.path().join("profile"), &state).is_empty());
    let outcome = build.run(CancellationToken::new(), |_| {}).await.unwrap();
    assert_eq!(outcome.downloaded, 0);
    assert_eq!(outcome.skipped, 1);
    assert!(!install_unfinished(&state));
}
