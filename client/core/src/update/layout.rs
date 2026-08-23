use std::path::{Path, PathBuf};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum InstallLayout {
    Portable { target: PathBuf },
    AppImage { target: PathBuf },
    MacBundle { target: PathBuf },
    Managed { reason: String },
}

impl InstallLayout {
    pub fn target(&self) -> Option<&Path> {
        match self {
            InstallLayout::Portable { target }
            | InstallLayout::AppImage { target }
            | InstallLayout::MacBundle { target } => Some(target),
            InstallLayout::Managed { .. } => None,
        }
    }

    pub fn is_managed(&self) -> bool {
        matches!(self, InstallLayout::Managed { .. })
    }
}

pub fn detect() -> InstallLayout {
    let app_image = std::env::var_os("APPIMAGE").map(PathBuf::from);
    let current = std::env::current_exe().ok();
    classify(app_image, current)
}

pub fn classify(app_image: Option<PathBuf>, current_exe: Option<PathBuf>) -> InstallLayout {
    if let Some(image) = app_image {
        return InstallLayout::AppImage { target: image };
    }
    let Some(exe) = current_exe else {
        return InstallLayout::Managed {
            reason: "unknown executable path".into(),
        };
    };
    let exe = std::fs::canonicalize(&exe).unwrap_or(exe);
    let text = exe.to_string_lossy().replace('\\', "/");

    if text.contains("/AppTranslocation/") {
        return InstallLayout::Managed {
            reason: "app translocation".into(),
        };
    }
    for system in [
        "/usr/",
        "/opt/",
        "/nix/store/",
        "/snap/",
        "/var/lib/flatpak/",
    ] {
        if text.starts_with(system) {
            return InstallLayout::Managed {
                reason: format!("installed under {system}"),
            };
        }
    }
    if cfg!(target_os = "windows") {
        for var in ["ProgramFiles", "ProgramFiles(x86)"] {
            if let Some(root) = std::env::var_os(var) {
                let root = root.to_string_lossy().replace('\\', "/").to_lowercase();
                if !root.is_empty() && text.to_lowercase().starts_with(&root) {
                    return InstallLayout::Managed {
                        reason: "installed for all users".into(),
                    };
                }
            }
        }
    }
    if let Some(bundle) = exe
        .ancestors()
        .find(|path| path.extension().is_some_and(|ext| ext == "app"))
    {
        return InstallLayout::MacBundle {
            target: bundle.to_path_buf(),
        };
    }
    InstallLayout::Portable { target: exe }
}

pub fn staging_dir(target: &Path) -> PathBuf {
    target
        .parent()
        .unwrap_or_else(|| Path::new("."))
        .join(".laminara-update")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn appimage_wins_over_the_mounted_executable() {
        let layout = classify(
            Some(PathBuf::from("/home/p/Laminara.AppImage")),
            Some(PathBuf::from("/tmp/.mount_abc/AppRun")),
        );
        assert_eq!(
            layout,
            InstallLayout::AppImage {
                target: PathBuf::from("/home/p/Laminara.AppImage")
            }
        );
    }

    #[test]
    fn system_paths_are_managed() {
        assert!(classify(None, Some(PathBuf::from("/usr/bin/laminara"))).is_managed());
        assert!(classify(None, Some(PathBuf::from("/nix/store/abc/bin/laminara"))).is_managed());
        assert!(classify(
            None,
            Some(PathBuf::from(
                "/Applications/Laminara.app/Contents/MacOS/../../../AppTranslocation/x"
            ))
        )
        .is_managed());
        assert!(classify(None, None).is_managed());
    }

    #[test]
    fn mac_bundle_is_detected_from_an_ancestor() {
        let layout = classify(
            None,
            Some(PathBuf::from(
                "/Users/p/Apps/Laminara.app/Contents/MacOS/laminara",
            )),
        );
        assert_eq!(
            layout,
            InstallLayout::MacBundle {
                target: PathBuf::from("/Users/p/Apps/Laminara.app")
            }
        );
    }

    #[test]
    fn loose_binary_is_portable_and_stages_next_to_itself() {
        let layout = classify(None, Some(PathBuf::from("/home/p/games/laminara")));
        assert_eq!(
            layout,
            InstallLayout::Portable {
                target: PathBuf::from("/home/p/games/laminara")
            }
        );
        assert_eq!(
            staging_dir(layout.target().unwrap()),
            PathBuf::from("/home/p/games/.laminara-update")
        );
    }
}
