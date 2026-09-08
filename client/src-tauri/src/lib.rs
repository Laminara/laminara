mod auth;
mod baked;
mod commands;
mod logging;

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::OnceLock;

use tokio::sync::Mutex;
use tokio_util::sync::CancellationToken;

use laminara_core::config::{ClientConfig, EndpointConfig};
use laminara_core::paths::LaminaraPaths;
use laminara_core::Core;

use auth::AuthManager;

pub struct AppState {
    pub core: Core,
    pub auth: AuthManager,
    pub jobs: Mutex<HashMap<String, CancellationToken>>,
    pub game_token: Mutex<Option<CancellationToken>>,
    pub authlib_jar: PathBuf,
}

const DEFAULT_CLIENT_CONFIG: &str =
    include_str!(concat!(env!("OUT_DIR"), "/embedded_client_config.json"));

static CLIENT_CONFIG: OnceLock<String> = OnceLock::new();

fn client_config() -> &'static str {
    CLIENT_CONFIG.get_or_init(|| baked::read().unwrap_or_else(|| DEFAULT_CLIENT_CONFIG.to_string()))
}

#[derive(serde::Deserialize)]
#[serde(rename_all = "camelCase")]
struct EmbeddedConfig {
    endpoints: Vec<EndpointConfig>,
    #[serde(default)]
    server_public_key_hex: String,
    #[serde(default)]
    trusted_public_keys_hex: Vec<String>,
    #[serde(default)]
    hwid_salt_hex: String,
    #[serde(default)]
    storage_name: String,
    #[serde(default)]
    branding: serde_json::Value,
}

const LEGACY_STORAGE_NAME: &str = "laminara";

fn storage_name() -> String {
    if let Ok(name) = std::env::var("LAMINARA_STORAGE_NAME") {
        if !name.trim().is_empty() {
            return sanitise_folder(&name);
        }
    }
    serde_json::from_str::<EmbeddedConfig>(client_config())
        .ok()
        .map(|config| sanitise_folder(&config.storage_name))
        .filter(|name| !name.is_empty())
        .unwrap_or_else(|| LEGACY_STORAGE_NAME.to_string())
}

fn sanitise_folder(raw: &str) -> String {
    let cleaned: String = raw
        .trim()
        .chars()
        .filter(|c| !c.is_control() && !r#"/\:*?"<>|"#.contains(*c))
        .take(48)
        .collect();
    cleaned.trim_matches(|c| c == ' ' || c == '.').to_string()
}

fn adopt_legacy(parent: Option<PathBuf>, name: &str) -> Option<PathBuf> {
    let parent = parent?;
    let target = parent.join(name);
    if name == LEGACY_STORAGE_NAME || target.exists() {
        return Some(target);
    }
    let legacy = parent.join(LEGACY_STORAGE_NAME);
    if legacy.is_dir() {
        match std::fs::rename(&legacy, &target) {
            Ok(()) => tracing::info!(from = %legacy.display(), to = %target.display(), "папка лаунчера переехала под имя проекта"),
            Err(error) => {
                tracing::warn!(%error, "не вышло перенести старую папку лаунчера — начинаю с чистой");
                return Some(target);
            }
        }
    }
    Some(target)
}

pub fn embedded_branding() -> serde_json::Value {
    serde_json::from_str::<EmbeddedConfig>(client_config())
        .map(|config| config.branding)
        .unwrap_or(serde_json::Value::Null)
}

fn load_or_bootstrap(paths: &LaminaraPaths, data_dir: &Path) -> Result<ClientConfig, String> {
    let file = paths.config_file();
    let embedded: EmbeddedConfig = serde_json::from_str(client_config())
        .map_err(|e| format!("embedded client config: {e}"))?;

    let mut endpoints = embedded.endpoints;
    if let Ok(base) = std::env::var("LAMINARA_BASE") {
        endpoints = vec![EndpointConfig {
            id: "local".into(),
            base_url: base,
        }];
    }
    if endpoints.is_empty() {
        return Err("embedded client config has no endpoints".into());
    }
    let server_public_key_hex = std::env::var("LAMINARA_SERVER_PUBKEY_HEX")
        .ok()
        .filter(|value| !value.is_empty())
        .unwrap_or(embedded.server_public_key_hex);

    if file.exists() {
        let mut config = ClientConfig::load(&file).map_err(|e| e.to_string())?;
        if let Some(account) = &config.selected_account {
            if !endpoints
                .iter()
                .any(|endpoint| endpoint.id == account.endpoint_id)
            {
                config.selected_account = None;
            }
        }
        config.endpoints = endpoints;
        config.server_public_key_hex = server_public_key_hex;
        config.trusted_public_keys_hex = embedded.trusted_public_keys_hex;
        config.hwid_salt_hex = embedded.hwid_salt_hex;
        config.save(&file).map_err(|e| e.to_string())?;
        return Ok(config);
    }

    let config = ClientConfig {
        schema_version: 1,
        endpoints,
        server_public_key_hex,
        trusted_public_keys_hex: embedded.trusted_public_keys_hex,
        hwid_salt_hex: embedded.hwid_salt_hex,
        install_dir: data_dir.join("games"),
        game_dir: None,
        selected_account: None,
        selected_profile: None,
        jvm_tuning: vec![],
        default_memory_mb: 4096,
        build_settings: std::collections::HashMap::new(),
        stale_update: None,
    };
    config.save(&file).map_err(|e| e.to_string())?;
    Ok(config)
}

fn init_state() -> Result<AppState, String> {
    let name = storage_name();
    let config_dir = adopt_legacy(dirs::config_dir(), &name).ok_or("no config dir")?;
    let data_dir = adopt_legacy(dirs::data_dir(), &name).ok_or("no data dir")?;
    std::fs::create_dir_all(&config_dir).map_err(|e| e.to_string())?;
    std::fs::create_dir_all(&data_dir).map_err(|e| e.to_string())?;

    let paths = LaminaraPaths {
        config_dir,
        data_dir: data_dir.clone(),
    };
    let config = load_or_bootstrap(&paths, &data_dir)?;
    let core = Core::new(paths, config).map_err(|e| e.to_string())?;

    let authlib_jar = std::env::var("LAMINARA_AUTHLIB_JAR")
        .map(PathBuf::from)
        .unwrap_or_else(|_| data_dir.join("authlib-injector.jar"));

    Ok(AppState {
        core,
        auth: AuthManager::default(),
        jobs: Mutex::new(HashMap::new()),
        game_token: Mutex::new(None),
        authlib_jar,
    })
}

pub fn run() {
    let log_dir = dirs::data_dir().map(|d| d.join(storage_name()).join("logs"));
    let _log_guard = log_dir.as_ref().and_then(|dir| logging::init(dir));
    tracing::info!(
        version = env!("LAMINARA_VERSION"),
        "Laminara launcher starting"
    );

    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .setup(|app| {
            use tauri::Manager;
            let state = init_state().map_err(|e| -> Box<dyn std::error::Error> {
                tracing::error!("init failed: {e}");
                e.into()
            })?;
            app.manage(state);
            laminara_core::update::swap::cleanup_stale(&laminara_core::update::detect());
            tracing::info!("launcher initialised");
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            commands::probe_endpoints,
            commands::endpoints_set,
            commands::login,
            commands::logout,
            commands::auth_status,
            commands::restore_session,
            commands::list_builds,
            commands::sync_profile,
            commands::cancel_job,
            commands::launch,
            commands::stop,
            commands::player_counts,
            commands::general_settings,
            commands::repair_build,
            commands::build_settings,
            commands::set_build_memory,
            commands::build_features,
            commands::set_build_features,
            commands::set_install_dir,
            commands::collect_garbage,
            commands::branding,
            commands::news,
            commands::open_external,
            commands::check_update,
            commands::apply_update,
            commands::report_crash,
        ])
        .run(tauri::generate_context!())
        .expect("error while running Laminara");
}

#[cfg(test)]
mod storage_tests {
    use super::{adopt_legacy, sanitise_folder, LEGACY_STORAGE_NAME};

    #[test]
    fn folder_name_survives_real_project_names() {
        assert_eq!(sanitise_folder("Мир Приключений"), "Мир Приключений");
        assert_eq!(sanitise_folder("  MagicWorld  "), "MagicWorld");
    }

    #[test]
    fn folder_name_drops_path_tricks() {
        for raw in ["../../etc", "a/b", r"c\d", "имя:двоеточие", "звёзды*?", "кавычки\"", "труба|"] {
            let cleaned = sanitise_folder(raw);
            assert!(
                !cleaned.contains('/')
                    && !cleaned.contains('\\')
                    && !cleaned.contains(':')
                    && !cleaned.contains('*')
                    && !cleaned.contains('?')
                    && !cleaned.contains('"')
                    && !cleaned.contains('|'),
                "имя {raw:?} превратилось в {cleaned:?}"
            );
        }
        assert_eq!(sanitise_folder("...."), "");
        assert_eq!(sanitise_folder(""), "");
    }

    #[test]
    fn old_folder_moves_under_the_project_name() {
        let parent = tempfile::tempdir().unwrap();
        let legacy = parent.path().join(LEGACY_STORAGE_NAME);
        std::fs::create_dir_all(legacy.join("games")).unwrap();
        std::fs::write(legacy.join("config.json"), "{}").unwrap();

        let moved = adopt_legacy(Some(parent.path().to_path_buf()), "МойПроект").unwrap();

        assert_eq!(moved, parent.path().join("МойПроект"));
        assert!(moved.join("games").is_dir(), "сборки игрока потерялись при переезде");
        assert!(moved.join("config.json").is_file(), "настройки потерялись при переезде");
        assert!(!legacy.exists(), "старая папка осталась дублировать данные");
    }

    #[test]
    fn existing_project_folder_is_left_alone() {
        let parent = tempfile::tempdir().unwrap();
        let legacy = parent.path().join(LEGACY_STORAGE_NAME);
        let mine = parent.path().join("МойПроект");
        std::fs::create_dir_all(&legacy).unwrap();
        std::fs::create_dir_all(&mine).unwrap();
        std::fs::write(mine.join("config.json"), "{\"свой\":true}").unwrap();

        let chosen = adopt_legacy(Some(parent.path().to_path_buf()), "МойПроект").unwrap();

        assert_eq!(chosen, mine);
        assert_eq!(std::fs::read_to_string(mine.join("config.json")).unwrap(), "{\"свой\":true}");
        assert!(legacy.exists(), "чужую папку трогать не нужно");
    }
}
