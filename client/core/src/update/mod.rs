pub mod layout;
pub mod swap;

use crate::proto::core::v1::{LauncherArtifact, LauncherArtifactKind, LauncherRelease, Platform};

pub use layout::{detect, staging_dir, InstallLayout};

pub fn relaunch_target(layout: &InstallLayout) -> PathBuf {
    let Some(target) = layout.target() else {
        return PathBuf::new();
    };
    if let InstallLayout::MacBundle { .. } = layout {
        let macos = target.join("Contents/MacOS");
        if let Ok(entries) = std::fs::read_dir(&macos) {
            if let Some(binary) = entries
                .filter_map(Result::ok)
                .map(|e| e.path())
                .find(|p| p.is_file())
            {
                return binary;
            }
        }
    }
    target.to_path_buf()
}

#[derive(Debug, Clone)]
pub struct AvailableUpdate {
    pub version: String,
    pub notes: String,
    pub mandatory: bool,
    pub file_name: String,
    pub size: u64,
    pub object_key: String,
    pub algo: i32,
    pub hash_hex: String,
    pub can_install: bool,
    pub blocked_reason: Option<String>,
}

pub fn artifact_for<'a>(
    release: &'a LauncherRelease,
    platform: Platform,
    layout: &InstallLayout,
) -> Option<&'a LauncherArtifact> {
    let wanted_kind = match layout {
        InstallLayout::AppImage { .. } => LauncherArtifactKind::AppImage,
        InstallLayout::MacBundle { .. } => LauncherArtifactKind::AppBundleTarGz,
        _ => LauncherArtifactKind::RawExecutable,
    };
    release
        .artifacts
        .iter()
        .find(|artifact| {
            artifact.platform == platform as i32 && artifact.kind == wanted_kind as i32
        })
        .or_else(|| {
            release.artifacts.iter().find(|artifact| {
                artifact.platform == platform as i32
                    && artifact.kind == LauncherArtifactKind::Installer as i32
            })
        })
}

pub fn stage_payload(
    artifact: &LauncherArtifact,
    downloaded: &Path,
    staging: &Path,
) -> Result<PathBuf, CoreError> {
    if artifact.kind != LauncherArtifactKind::AppBundleTarGz as i32 {
        return Ok(downloaded.to_path_buf());
    }
    let unpacked = staging.join("bundle");
    let _ = std::fs::remove_dir_all(&unpacked);
    std::fs::create_dir_all(&unpacked)
        .map_err(|e| CoreError::Launch(format!("create staging: {e}")))?;

    let file = std::fs::File::open(downloaded)
        .map_err(|e| CoreError::Launch(format!("open update: {e}")))?;
    let mut archive = tar::Archive::new(flate2::read::GzDecoder::new(file));
    archive.set_preserve_permissions(true);
    archive
        .unpack(&unpacked)
        .map_err(|e| CoreError::Launch(format!("unpack bundle: {e}")))?;

    let bundle = std::fs::read_dir(&unpacked)
        .map_err(|e| CoreError::Launch(format!("read staging: {e}")))?
        .filter_map(Result::ok)
        .map(|entry| entry.path())
        .find(|path| path.is_dir() && path.extension().is_some_and(|ext| ext == "app"))
        .ok_or_else(|| CoreError::Launch("release archive has no .app bundle".into()))?;
    let _ = std::fs::remove_file(downloaded);
    Ok(bundle)
}

pub fn is_newer(candidate: &str, current: &str) -> bool {
    compare(candidate, current) > 0
}

pub fn compare(a: &str, b: &str) -> i32 {
    let part = |value: &str, index: usize| -> i64 {
        value
            .trim_start_matches('v')
            .split('.')
            .nth(index)
            .and_then(|piece| piece.split(['-', '+']).next())
            .and_then(|piece| piece.parse::<i64>().ok())
            .unwrap_or(0)
    };
    for index in 0..3 {
        let (left, right) = (part(a, index), part(b, index));
        if left != right {
            return if left < right { -1 } else { 1 };
        }
    }
    0
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::proto::core::v1::{Hash, ObjectRef};

    fn artifact(platform: Platform, kind: LauncherArtifactKind, name: &str) -> LauncherArtifact {
        LauncherArtifact {
            platform: platform as i32,
            kind: kind as i32,
            object: Some(ObjectRef {
                hash: Some(Hash {
                    algo: 1,
                    value: vec![1, 2, 3],
                }),
                size: 10,
            }),
            file_name: name.into(),
        }
    }

    #[test]
    fn versions_compare_numerically() {
        assert!(is_newer("0.2.0", "0.1.9"));
        assert!(is_newer("1.0.0", "0.9.9"));
        assert!(is_newer("0.10.0", "0.9.0"));
        assert!(!is_newer("0.1.0", "0.1.0"));
        assert!(
            !is_newer("0.1.0", "0.2.0"),
            "downgrades must never be offered"
        );
        assert!(is_newer("v1.2.3", "1.2.2"));
    }

    #[test]
    fn installer_only_release_never_masquerades_as_a_portable_build() {
        let release = LauncherRelease {
            artifacts: vec![artifact(
                Platform::WindowsX64,
                LauncherArtifactKind::Installer,
                "setup.exe",
            )],
            ..Default::default()
        };
        let portable = InstallLayout::Portable {
            target: "/c/laminara.exe".into(),
        };
        let picked = artifact_for(&release, Platform::WindowsX64, &portable).unwrap();
        assert_eq!(
            picked.kind,
            LauncherArtifactKind::Installer as i32,
            "an installer may only be reported, never swapped"
        );
    }

    #[test]
    fn a_bundle_release_is_not_offered_to_a_portable_install() {
        let release = LauncherRelease {
            artifacts: vec![artifact(
                Platform::MacOsArm64,
                LauncherArtifactKind::AppBundleTarGz,
                "Laminara.app.tar.gz",
            )],
            ..Default::default()
        };
        let portable = InstallLayout::Portable {
            target: "/Users/p/laminara".into(),
        };
        assert!(
            artifact_for(&release, Platform::MacOsArm64, &portable).is_none(),
            "a tarball must never be renamed over a loose binary"
        );
    }

    #[test]
    fn artifact_matches_the_installed_layout() {
        let release = LauncherRelease {
            artifacts: vec![
                artifact(
                    Platform::Linux,
                    LauncherArtifactKind::RawExecutable,
                    "laminara-linux",
                ),
                artifact(
                    Platform::Linux,
                    LauncherArtifactKind::AppImage,
                    "Laminara.AppImage",
                ),
                artifact(
                    Platform::WindowsX64,
                    LauncherArtifactKind::RawExecutable,
                    "laminara.exe",
                ),
            ],
            ..Default::default()
        };

        let portable = InstallLayout::Portable {
            target: "/home/p/laminara".into(),
        };
        assert_eq!(
            artifact_for(&release, Platform::Linux, &portable)
                .unwrap()
                .file_name,
            "laminara-linux"
        );

        let appimage = InstallLayout::AppImage {
            target: "/home/p/Laminara.AppImage".into(),
        };
        assert_eq!(
            artifact_for(&release, Platform::Linux, &appimage)
                .unwrap()
                .file_name,
            "Laminara.AppImage"
        );

        assert_eq!(
            artifact_for(&release, Platform::WindowsX64, &portable)
                .unwrap()
                .file_name,
            "laminara.exe"
        );
        assert!(artifact_for(&release, Platform::MacOsArm64, &portable).is_none());
    }
}

use crate::error::CoreError;
use crate::transport::Transport;
use std::path::{Path, PathBuf};

pub async fn download_artifact(
    transport: &Transport,
    base_url: &str,
    artifact: &LauncherArtifact,
    staging: &Path,
    on_bytes: impl Fn(u64, u64) + Send + Sync,
) -> Result<PathBuf, CoreError> {
    use futures::StreamExt;
    use std::io::Write;

    let object = artifact
        .object
        .as_ref()
        .ok_or_else(|| CoreError::Launch("release artifact has no object".into()))?;
    let hash = object
        .hash
        .as_ref()
        .ok_or_else(|| CoreError::Launch("release artifact has no hash".into()))?;
    let expected = hex::encode(&hash.value);
    let key = crate::manifest::object_key(hash.algo, &hash.value);

    std::fs::create_dir_all(staging)
        .map_err(|e| CoreError::Launch(format!("create staging: {e}")))?;
    let dest = staging.join(&artifact.file_name);
    let _ = std::fs::remove_file(&dest);

    let url = format!("{}/objects/{}", base_url.trim_end_matches('/'), key);
    let response = transport
        .client()
        .get(&url)
        .send()
        .await
        .map_err(|e| CoreError::Launch(format!("download launcher: {e}")))?
        .error_for_status()
        .map_err(|e| CoreError::Launch(format!("download launcher: {e}")))?;

    let limit = object.size;
    let mut file = std::fs::File::create(&dest)
        .map_err(|e| CoreError::Launch(format!("create {}: {e}", dest.display())))?;
    let mut hasher = blake3::Hasher::new();
    let mut done = 0u64;
    let mut stream = response.bytes_stream();
    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|e| CoreError::Launch(format!("download launcher: {e}")))?;
        hasher.update(&chunk);
        done += chunk.len() as u64;
        if done > limit {
            drop(file);
            let _ = std::fs::remove_file(&dest);
            return Err(CoreError::Untrusted);
        }
        file.write_all(&chunk)
            .map_err(|e| CoreError::Launch(format!("write update: {e}")))?;
        on_bytes(done, limit);
    }
    drop(file);

    if hasher.finalize().to_hex().to_string() != expected {
        let _ = std::fs::remove_file(&dest);
        return Err(CoreError::Untrusted);
    }
    Ok(dest)
}
