use std::path::Path;

use crate::error::CoreError;
use crate::update::layout::InstallLayout;

pub fn apply(layout: &InstallLayout, staged: &Path) -> Result<(), CoreError> {
    let target = layout.target().ok_or_else(|| {
        CoreError::Launch("this installation is managed and cannot self-update".into())
    })?;

    match layout {
        InstallLayout::MacBundle { .. } => {
            if !staged.is_dir() {
                return Err(CoreError::Launch(
                    "staged update is not an .app bundle".into(),
                ));
            }
            replace_directory(staged, target)
        }
        _ => {
            if staged.is_dir() {
                return Err(CoreError::Launch(
                    "staged update is not a launcher executable".into(),
                ));
            }
            replace_file(staged, target)
        }
    }
}

fn replace_file(staged: &Path, target: &Path) -> Result<(), CoreError> {
    make_executable(staged);
    let backup = with_suffix(target, ".old");
    let _ = std::fs::remove_file(&backup);

    let had_target = target.exists();
    if had_target {
        std::fs::rename(target, &backup)
            .map_err(|e| CoreError::Launch(format!("move the running launcher aside: {e}")))?;
    }
    if let Err(e) = std::fs::rename(staged, target) {
        if had_target {
            let _ = std::fs::rename(&backup, target);
        }
        return Err(CoreError::Launch(format!("install the new launcher: {e}")));
    }
    let _ = std::fs::remove_file(&backup);
    Ok(())
}

fn replace_directory(staged: &Path, target: &Path) -> Result<(), CoreError> {
    let backup = with_suffix(target, ".old");
    let _ = std::fs::remove_dir_all(&backup);

    let had_target = target.exists();
    if had_target {
        std::fs::rename(target, &backup)
            .map_err(|e| CoreError::Launch(format!("move the bundle aside: {e}")))?;
    }
    if let Err(e) = std::fs::rename(staged, target) {
        if had_target {
            let _ = std::fs::rename(&backup, target);
        }
        return Err(CoreError::Launch(format!("install the new bundle: {e}")));
    }
    let _ = std::fs::remove_dir_all(&backup);
    Ok(())
}

pub fn cleanup_stale(layout: &InstallLayout) {
    let Some(target) = layout.target() else {
        return;
    };
    let backup = with_suffix(target, ".old");
    let _ = std::fs::remove_file(&backup);
    let _ = std::fs::remove_dir_all(&backup);
}

fn with_suffix(path: &Path, suffix: &str) -> std::path::PathBuf {
    let mut name = path.as_os_str().to_os_string();
    name.push(suffix);
    std::path::PathBuf::from(name)
}

fn make_executable(path: &Path) {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o755));
    }
    #[cfg(not(unix))]
    {
        let _ = path;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn replaces_a_portable_binary_and_clears_the_backup() {
        let tmp = tempfile::tempdir().unwrap();
        let target = tmp.path().join("laminara");
        std::fs::write(&target, b"old").unwrap();
        let staged = tmp.path().join(".laminara-update/laminara");
        std::fs::create_dir_all(staged.parent().unwrap()).unwrap();
        std::fs::write(&staged, b"new").unwrap();

        apply(
            &InstallLayout::Portable {
                target: target.clone(),
            },
            &staged,
        )
        .unwrap();

        assert_eq!(std::fs::read(&target).unwrap(), b"new");
        assert!(
            !tmp.path().join("laminara.old").exists(),
            "backup must be cleaned up"
        );
        assert!(!staged.exists(), "staged file must have been consumed");
    }

    #[test]
    fn a_managed_install_refuses_to_swap() {
        let tmp = tempfile::tempdir().unwrap();
        let staged = tmp.path().join("new");
        std::fs::write(&staged, b"new").unwrap();
        let err = apply(
            &InstallLayout::Managed {
                reason: "package manager".into(),
            },
            &staged,
        )
        .unwrap_err();
        assert!(format!("{err}").contains("managed"), "{err}");
    }

    #[test]
    fn refuses_to_put_an_archive_where_a_bundle_belongs() {
        let tmp = tempfile::tempdir().unwrap();
        let bundle = tmp.path().join("Laminara.app");
        std::fs::create_dir_all(bundle.join("Contents/MacOS")).unwrap();
        std::fs::write(bundle.join("Contents/MacOS/laminara"), b"real").unwrap();
        let tarball = tmp.path().join("Laminara.app.tar.gz");
        std::fs::write(&tarball, b"gzip-bytes").unwrap();

        let err = apply(
            &InstallLayout::MacBundle {
                target: bundle.clone(),
            },
            &tarball,
        )
        .unwrap_err();
        assert!(format!("{err}").contains("bundle"), "{err}");
        assert!(
            bundle.join("Contents/MacOS/laminara").exists(),
            "the installed bundle must survive"
        );
    }

    #[test]
    fn refuses_to_put_a_directory_where_an_executable_belongs() {
        let tmp = tempfile::tempdir().unwrap();
        let target = tmp.path().join("laminara");
        std::fs::write(&target, b"old").unwrap();
        let staged_dir = tmp.path().join("staged-dir");
        std::fs::create_dir_all(&staged_dir).unwrap();

        let err = apply(
            &InstallLayout::Portable {
                target: target.clone(),
            },
            &staged_dir,
        )
        .unwrap_err();
        assert!(format!("{err}").contains("executable"), "{err}");
        assert_eq!(std::fs::read(&target).unwrap(), b"old");
    }

    #[test]
    fn a_failed_swap_restores_the_original() {
        let tmp = tempfile::tempdir().unwrap();
        let target = tmp.path().join("laminara");
        std::fs::write(&target, b"old").unwrap();
        let missing = tmp.path().join("does-not-exist");

        let err = apply(
            &InstallLayout::Portable {
                target: target.clone(),
            },
            &missing,
        )
        .unwrap_err();
        assert!(
            format!("{err}").contains("install the new launcher"),
            "{err}"
        );
        assert_eq!(
            std::fs::read(&target).unwrap(),
            b"old",
            "the original launcher must survive a failed update"
        );
    }
}
