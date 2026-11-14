use super::*;

fn file(path: &str, bytes: &[u8], policy: FilePolicy) -> ManifestFile {
    ManifestFile {
        path: path.into(),
        policy: policy as i32,
        object: Some(crate::proto::core::v1::ObjectRef {
            hash: Some(crate::proto::core::v1::Hash {
                algo: HashAlgo::Blake3 as i32,
                value: blake3::hash(bytes).as_bytes().to_vec(),
            }),
            size: bytes.len() as u64,
        }),
        ..Default::default()
    }
}

fn owned(policy: FilePolicy) -> OwnedEntry {
    OwnedEntry {
        object_hash: blake3::hash(b"object").to_hex().to_string(),
        class: policy as i32,
        placement: Placement::Copy,
        released: false,
        size: 6,
        algo: HashAlgo::Blake3 as i32,
    }
}

async fn apply(root: &Path, files: Vec<ManifestFile>) -> Result<SyncOutcome, CoreError> {
    sync(
        SyncPlan {
            transport: &Transport::default(),
            base_url: "http://127.0.0.1:1",
            profile_dir: &root.join("profile"),
            state_dir: &root.join("profile/.laminara"),
            cas_dir: &root.join("cas"),
            manifest: &Manifest {
                files,
                ..Default::default()
            },
            selection: &FeatureSelection::default(),
            max_parallel: 1,
            cancel: CancellationToken::new(),
        },
        |_| {},
    )
    .await
}

#[test]
fn rejects_windows_aliases_and_devices_on_every_host() {
    for path in [
        "",
        "../escape",
        "/absolute",
        "C:/outside",
        "C:relative",
        "mods/a.jar:stream",
        "a\\b",
        "mods/NUL.jar",
        "mods/con",
        "COM1.cfg",
        "LPT9.txt",
        "COM¹.txt",
        "LPT².ini",
        "CONIN$",
        "conout$.txt",
        "CON .txt",
        "mods/x. /a",
        "mods/a.jar.",
        "mods/a.jar ",
        "mods/a\0.jar",
        "mods/a\n.jar",
        ".LAMINARA",
        ".Laminara/state",
        ".lamınara/installed.json",
        "a//b",
        "a/./b",
    ] {
        assert!(validate_manifest_path(path).is_err(), "accepted {path:?}");
    }
    for path in [
        "mods/a.jar",
        "config/настройки.cfg",
        "runtime/bin/java",
        "config/COM10.txt",
        "auxiliary.txt",
    ] {
        validate_manifest_path(path).unwrap();
    }
}

#[tokio::test]
async fn rejects_case_aliases_and_file_directory_conflicts_before_installing() {
    for (a, b) in [
        ("mods/A.jar", "mods/a.jar"),
        ("mods", "mods/a.jar"),
        ("mods/A", "MODS/a/config"),
        ("config/σ.cfg", "CONFIG/ς.CFG"),
    ] {
        let temp = tempfile::tempdir().unwrap();
        assert!(apply(
            temp.path(),
            vec![
                file(a, b"a", FilePolicy::Unspecified),
                file(b, b"b", FilePolicy::Unspecified)
            ]
        )
        .await
        .is_err());
        assert!(!temp.path().join("profile").join(a).exists());
        assert!(!install_unfinished(&temp.path().join("profile/.laminara")));
    }
}

#[cfg(unix)]
#[test]
fn prune_refuses_symlinked_parents_and_traversal() {
    let temp = tempfile::tempdir().unwrap();
    let profile = temp.path().join("profile");
    let outside = temp.path().join("outside");
    std::fs::create_dir_all(&profile).unwrap();
    std::fs::create_dir_all(&outside).unwrap();
    std::fs::write(outside.join("old.jar"), b"unrelated").unwrap();
    std::os::unix::fs::symlink(&outside, profile.join("mods")).unwrap();
    for path in ["mods/old.jar", "../outside/old.jar"] {
        let ledger = Ledger::from([(path.into(), owned(FilePolicy::Unspecified))]);
        assert!(prune(&profile, &ledger, &HashSet::new()).is_err());
        assert_eq!(
            std::fs::read(outside.join("old.jar")).unwrap(),
            b"unrelated"
        );
    }
}

#[test]
fn repair_preflights_the_whole_batch_and_preserves_player_files() {
    let temp = tempfile::tempdir().unwrap();
    let profile = temp.path().join("profile");
    let state = profile.join(".laminara");
    let cas = temp.path().join("cas");
    std::fs::create_dir_all(profile.join("mods")).unwrap();
    std::fs::write(profile.join("mods/a.jar"), b"managed").unwrap();
    std::fs::write(profile.join("options.txt"), b"player").unwrap();
    let mut ledger = Ledger::from([("mods/a.jar".into(), owned(FilePolicy::Unspecified))]);
    save_ledger(&state.join(LEDGER_FILE), &ledger).unwrap();
    assert!(discard_installed(
        &profile,
        &state,
        &cas,
        &["mods/a.jar".into(), "options.txt".into()]
    )
    .is_err());
    assert_eq!(
        std::fs::read(profile.join("mods/a.jar")).unwrap(),
        b"managed"
    );
    ledger.insert("options.txt".into(), owned(FilePolicy::UserWritable));
    save_ledger(&state.join(LEDGER_FILE), &ledger).unwrap();
    assert!(discard_installed(&profile, &state, &cas, &["options.txt".into()]).is_err());
    ledger.get_mut("options.txt").unwrap().class = FilePolicy::Enforced as i32;
    ledger.get_mut("options.txt").unwrap().released = true;
    save_ledger(&state.join(LEDGER_FILE), &ledger).unwrap();
    assert!(discard_installed(&profile, &state, &cas, &["options.txt".into()]).is_err());
    assert_eq!(
        std::fs::read(profile.join("options.txt")).unwrap(),
        b"player"
    );
}

#[test]
fn malformed_ownership_hash_is_an_error_before_repair_deletes_anything() {
    let temp = tempfile::tempdir().unwrap();
    let profile = temp.path().join("profile");
    let state = profile.join(".laminara");
    std::fs::create_dir_all(&profile).unwrap();
    std::fs::write(profile.join("a.jar"), b"keep").unwrap();
    let mut entry = owned(FilePolicy::Unspecified);
    entry.object_hash = "00".into();
    save_ledger(
        &state.join(LEDGER_FILE),
        &Ledger::from([("a.jar".into(), entry)]),
    )
    .unwrap();
    assert!(discard_installed(
        &profile,
        &state,
        &temp.path().join("cas"),
        &["a.jar".into()]
    )
    .is_err());
    assert_eq!(std::fs::read(profile.join("a.jar")).unwrap(), b"keep");
}

#[cfg(unix)]
#[test]
fn partial_links_never_modify_the_other_file() {
    let temp = tempfile::tempdir().unwrap();
    let outside = temp.path().join("data");
    let partial = temp.path().join("object.part");
    std::fs::write(&outside, b"player data").unwrap();
    std::os::unix::fs::symlink(&outside, &partial).unwrap();
    assert!(lock_partial(&partial).is_err());
    std::fs::remove_file(&partial).unwrap();
    std::fs::hard_link(&outside, &partial).unwrap();
    assert!(lock_partial(&partial).is_err());
    assert_eq!(std::fs::read(&outside).unwrap(), b"player data");
}

#[cfg(unix)]
#[test]
fn partial_special_files_are_rejected_without_waiting_for_io() {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("object.part");
    let _socket = std::os::unix::net::UnixListener::bind(&path).unwrap();
    assert!(lock_partial(&path).is_err());
}

#[test]
fn a_valid_partial_remains_exclusively_locked() {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("object.part");
    let first = lock_partial(&path).unwrap().unwrap();
    assert!(lock_partial(&path).unwrap().is_none());
    drop(first);
    assert!(lock_partial(&path).unwrap().is_some());
}

#[cfg(unix)]
#[test]
fn unfinished_markers_and_ledger_writes_do_not_follow_links() {
    let temp = tempfile::tempdir().unwrap();
    let outside = temp.path().join("data");
    std::fs::write(&outside, b"player data").unwrap();
    let marker = temp.path().join(UNFINISHED_FILE);
    std::os::unix::fs::symlink(&outside, &marker).unwrap();
    assert!(install_unfinished(temp.path()));
    assert!(mark_unfinished(temp.path()).is_err());
    std::fs::remove_file(&marker).unwrap();
    std::fs::hard_link(&outside, &marker).unwrap();
    assert!(mark_unfinished(temp.path()).is_err());
    std::fs::remove_file(&marker).unwrap();
    mark_unfinished(temp.path()).unwrap();
    mark_unfinished(temp.path()).unwrap();
    mark_finished(temp.path()).unwrap();
    let ledger = temp.path().join(LEDGER_FILE);
    std::os::unix::fs::symlink(&outside, &ledger).unwrap();
    assert!(save_ledger(&ledger, &Ledger::new()).is_err());
    assert!(read_ledger(&ledger).is_err());
    std::fs::remove_file(&ledger).unwrap();
    let old_tmp = ledger.with_extension("json.tmp");
    std::os::unix::fs::symlink(&outside, &old_tmp).unwrap();
    save_ledger(&ledger, &Ledger::new()).unwrap();
    assert!(read_ledger(&ledger).unwrap().is_empty());
    assert_eq!(std::fs::read(outside).unwrap(), b"player data");
}

#[cfg(unix)]
#[tokio::test]
async fn sync_refuses_writable_leaf_links_and_linked_roots() {
    let temp = tempfile::tempdir().unwrap();
    let outside = temp.path().join("data");
    let profile = temp.path().join("profile");
    std::fs::create_dir(&profile).unwrap();
    std::fs::write(&outside, b"player data").unwrap();
    std::os::unix::fs::symlink(&outside, profile.join("options.txt")).unwrap();
    assert!(apply(
        temp.path(),
        vec![file("options.txt", b"defaults", FilePolicy::UserWritable)]
    )
    .await
    .is_err());
    assert_eq!(std::fs::read(&outside).unwrap(), b"player data");
    let temp = tempfile::tempdir().unwrap();
    std::os::unix::fs::symlink(&profile, temp.path().join("profile")).unwrap();
    assert!(apply(temp.path(), Vec::new()).await.is_err());
}

#[cfg(unix)]
#[tokio::test]
async fn sync_refuses_a_symlinked_cache_parent_before_changing_its_target() {
    let temp = tempfile::tempdir().unwrap();
    let outside = temp.path().join("outside");
    std::fs::create_dir(&outside).unwrap();
    std::fs::create_dir(temp.path().join("cas")).unwrap();
    std::os::unix::fs::symlink(&outside, temp.path().join("cas/blake3")).unwrap();
    assert!(apply(
        temp.path(),
        vec![file("mods/a.jar", b"data", FilePolicy::Unspecified)]
    )
    .await
    .is_err());
    assert_eq!(std::fs::read_dir(outside).unwrap().count(), 0);
}

#[cfg(unix)]
#[test]
fn cache_maintenance_ignores_directory_links_and_active_partials() {
    for scrub in [false, true] {
        let temp = tempfile::tempdir().unwrap();
        let cas = temp.path().join("cas");
        let outside = temp.path().join("outside");
        std::fs::create_dir(&cas).unwrap();
        std::fs::create_dir(&outside).unwrap();
        std::fs::write(outside.join("foreign"), b"player data").unwrap();
        std::os::unix::fs::symlink(&outside, cas.join("linked")).unwrap();
        std::fs::write(cas.join("object.part"), b"partial").unwrap();
        std::fs::write(cas.join("orphan"), b"bad object").unwrap();
        std::fs::create_dir(cas.join(".lam-backup")).unwrap();
        std::fs::write(cas.join(".lam-backup/original"), b"backup").unwrap();
        if scrub {
            assert_eq!(scrub_cas(&cas).unwrap(), (1, 1));
        } else {
            assert_eq!(gc_cas(&cas, &temp.path().join("profiles")).unwrap(), 1);
        }
        assert_eq!(
            std::fs::read(outside.join("foreign")).unwrap(),
            b"player data"
        );
        assert_eq!(std::fs::read(cas.join("object.part")).unwrap(), b"partial");
        assert_eq!(
            std::fs::read(cas.join(".lam-backup/original")).unwrap(),
            b"backup"
        );
    }
}

#[test]
fn corrupt_ownership_ledger_stops_gc_instead_of_deleting_referenced_objects() {
    let temp = tempfile::tempdir().unwrap();
    let state = temp.path().join("profiles/build/.laminara");
    let cas = temp.path().join("cas");
    std::fs::create_dir_all(&state).unwrap();
    std::fs::create_dir(&cas).unwrap();
    std::fs::write(state.join(LEDGER_FILE), b"not json").unwrap();
    std::fs::write(cas.join("object"), b"keep").unwrap();
    assert!(gc_cas(&cas, &temp.path().join("profiles")).is_err());
    assert_eq!(std::fs::read(cas.join("object")).unwrap(), b"keep");
}

#[tokio::test]
async fn case_only_path_changes_do_not_delete_the_new_file_or_leave_a_duplicate() {
    for (old, new) in [("mods/A.jar", "mods/a.jar"), ("Mods/a.jar", "mods/a.jar")] {
        let temp = tempfile::tempdir().unwrap();
        let initial = file(old, b"object", FilePolicy::Unspecified);
        let hash = initial.object.as_ref().unwrap().hash.as_ref().unwrap();
        let cached = temp
            .path()
            .join("cas")
            .join(object_key(hash.algo, &hash.value));
        std::fs::create_dir_all(cached.parent().unwrap()).unwrap();
        std::fs::write(&cached, b"object").unwrap();
        apply(temp.path(), vec![initial]).await.unwrap();
        apply(
            temp.path(),
            vec![file(new, b"object", FilePolicy::Unspecified)],
        )
        .await
        .unwrap();
        assert_eq!(
            std::fs::read(temp.path().join("profile").join(new)).unwrap(),
            b"object"
        );
        let mut jars = 0;
        for directory in std::fs::read_dir(temp.path().join("profile")).unwrap() {
            let directory = directory.unwrap();
            if !directory.file_type().unwrap().is_dir() {
                continue;
            }
            for entry in std::fs::read_dir(directory.path()).unwrap() {
                jars += usize::from(
                    entry
                        .unwrap()
                        .path()
                        .extension()
                        .is_some_and(|extension| extension == "jar"),
                );
            }
        }
        assert_eq!(jars, 1);
    }
}

#[cfg(unix)]
#[test]
fn repair_refuses_a_symlinked_cache_before_removing_the_installed_file() {
    let temp = tempfile::tempdir().unwrap();
    let profile = temp.path().join("profile");
    let state = profile.join(".laminara");
    let cas = temp.path().join("cas");
    let outside = temp.path().join("outside");
    std::fs::create_dir_all(profile.join("mods")).unwrap();
    std::fs::create_dir(&cas).unwrap();
    std::fs::create_dir(&outside).unwrap();
    std::fs::write(profile.join("mods/a.jar"), b"keep").unwrap();
    std::os::unix::fs::symlink(&outside, cas.join("blake3")).unwrap();
    save_ledger(
        &state.join(LEDGER_FILE),
        &Ledger::from([("mods/a.jar".into(), owned(FilePolicy::Unspecified))]),
    )
    .unwrap();
    assert!(discard_installed(&profile, &state, &cas, &["mods/a.jar".into()]).is_err());
    assert_eq!(std::fs::read(profile.join("mods/a.jar")).unwrap(), b"keep");
}

#[test]
fn separate_hardlink_names_are_not_the_same_directory_entry() {
    let temp = tempfile::tempdir().unwrap();
    let first = temp.path().join("first.jar");
    let second = temp.path().join("second.jar");
    std::fs::write(&first, b"object").unwrap();
    std::fs::hard_link(&first, &second).unwrap();
    assert!(safety::same_entry(&first, &first).unwrap());
    assert!(!safety::same_entry(&first, &second).unwrap());
    assert!(!safety::same_entry(&first, &temp.path().join("absent.jar")).unwrap());
}
