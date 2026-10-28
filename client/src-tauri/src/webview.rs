use tauri::{AppHandle, Runtime, WebviewWindowBuilder};

#[cfg(windows)]
const UNNEEDED_PROFILE_PARTS: &[&str] = &[
    "Subresource Filter",
    "Speech Recognition",
    "hyphen-data",
    "Trust Protection Lists",
    "MEIPreload",
    "WidevineCdm",
    "PKIMetadata",
    "component_crx_cache",
    "ShaderCache",
    "GrShaderCache",
    "Default/GPUCache",
    "Default/DawnGraphiteCache",
    "Default/DawnWebGPUCache",
];

pub fn with_shared_browser_args<'a, R: Runtime, M: tauri::Manager<R>>(
    app: &AppHandle<R>,
    builder: WebviewWindowBuilder<'a, R, M>,
) -> WebviewWindowBuilder<'a, R, M> {
    let shared = app
        .config()
        .app
        .windows
        .iter()
        .find_map(|window| window.additional_browser_args.as_deref());
    match shared {
        Some(args) => builder.additional_browser_args(args),
        None => builder,
    }
}

#[cfg(windows)]
pub fn prune_profile(identifier: &str) {
    let Some(profile) = dirs::data_local_dir().map(|dir| dir.join(identifier).join("EBWebView"))
    else {
        return;
    };
    for part in UNNEEDED_PROFILE_PARTS {
        let path = profile.join(part);
        match std::fs::remove_dir_all(&path) {
            Ok(()) => {
                tracing::info!(path = %path.display(), "из профиля окна убран лишний компонент")
            }
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {}
            Err(error) => {
                tracing::warn!(path = %path.display(), %error, "лишний компонент профиля окна не удалился")
            }
        }
    }
}

#[cfg(not(windows))]
pub fn prune_profile(_identifier: &str) {}
