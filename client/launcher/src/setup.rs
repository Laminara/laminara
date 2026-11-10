use std::path::{Path, PathBuf};
use std::sync::OnceLock;

use tokio::sync::Mutex;
use tokio_util::sync::CancellationToken;

use laminara_core::Core;
use laminara_core::config::{ClientConfig, EndpointConfig};
use laminara_core::paths::LaminaraPaths;

use crate::auth::AuthManager;
use crate::{baked, logging};

pub const REPORT_LOG_BYTES: usize = 256 * 1024;

pub struct RunningGame {
    pub session: u64,
    pub token: CancellationToken,
}

pub struct SyncJob {
    pub id: u64,
    pub build: String,
    pub token: CancellationToken,
}

pub struct AppState {
    pub core: Core,
    pub auth: AuthManager,
    pub sync_job: std::sync::Mutex<Option<SyncJob>>,
    pub sync_jobs_started: std::sync::atomic::AtomicU64,
    pub game: Mutex<Option<RunningGame>>,
    pub game_log: crate::console::SharedLog,
    pub face: Mutex<Option<(String, String)>>,
    pub log_dir: PathBuf,
    pub data_dir: PathBuf,
    pub authlib_jar: PathBuf,
}

const DEFAULT_CLIENT_CONFIG: &str =
    include_str!(concat!(env!("OUT_DIR"), "/embedded_client_config.json"));

static CLIENT_CONFIG: OnceLock<Result<EmbeddedConfig, String>> = OnceLock::new();

fn client_config() -> Result<&'static EmbeddedConfig, String> {
    CLIENT_CONFIG
        .get_or_init(|| {
            let baked = baked::read();
            serde_json::from_str(baked.as_deref().unwrap_or(DEFAULT_CLIENT_CONFIG))
                .map_err(|error| format!("embedded client config: {error}"))
        })
        .as_ref()
        .map_err(Clone::clone)
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
    branding: Branding,
}

#[derive(serde::Deserialize, Default)]
#[serde(rename_all = "camelCase", default)]
pub struct Branding {
    pub name: String,
    pub window_title: String,
    pub tagline: String,
    pub primary_color: String,
    pub primary_ink: String,
    pub background_color: String,
    pub logo_data_uri: String,
    pub hero_media_data_uri: TakenOnce,
}

#[derive(Default)]
pub struct TakenOnce(std::sync::Mutex<Option<String>>);

impl TakenOnce {
    pub fn take(&self) -> String {
        self.0
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .take()
            .unwrap_or_default()
    }
}

impl<'de> serde::Deserialize<'de> for TakenOnce {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        Ok(TakenOnce(std::sync::Mutex::new(Some(String::deserialize(
            deserializer,
        )?))))
    }
}

const LEGACY_STORAGE_NAME: &str = "laminara";

pub fn storage_name() -> String {
    if let Ok(name) = std::env::var("LAMINARA_STORAGE_NAME")
        && !name.trim().is_empty()
    {
        return sanitise_folder(&name);
    }
    client_config()
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
    cleaned.trim_end_matches([' ', '.']).to_string()
}

pub fn adopt_legacy(parent: Option<PathBuf>, name: &str) -> Option<PathBuf> {
    let parent = parent?;
    let target = parent.join(name);
    if name == LEGACY_STORAGE_NAME || target.exists() {
        return Some(target);
    }
    let legacy = parent.join(LEGACY_STORAGE_NAME);
    if legacy.is_dir() {
        match std::fs::rename(&legacy, &target) {
            Ok(()) => {
                tracing::info!(from = %legacy.display(), to = %target.display(), "папка лаунчера переехала под имя проекта")
            }
            Err(error) => {
                tracing::warn!(%error, "не вышло перенести старую папку лаунчера — начинаю с чистой");
                return Some(target);
            }
        }
    }
    Some(target)
}

pub fn embedded_branding() -> &'static Branding {
    static FALLBACK: OnceLock<Branding> = OnceLock::new();
    client_config()
        .map(|config| &config.branding)
        .unwrap_or_else(|_| FALLBACK.get_or_init(Branding::default))
}

fn load_or_bootstrap(paths: &LaminaraPaths, data_dir: &Path) -> Result<ClientConfig, String> {
    let file = paths.config_file();
    let embedded = client_config()?;

    let mut endpoints = embedded.endpoints.clone();
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
        .unwrap_or_else(|| embedded.server_public_key_hex.clone());

    if file.exists() {
        let mut config = ClientConfig::load(&file).map_err(|e| e.to_string())?;
        if let Some(account) = &config.selected_account
            && !endpoints
                .iter()
                .any(|endpoint| endpoint.id == account.endpoint_id)
        {
            config.selected_account = None;
        }
        config.endpoints = endpoints;
        config.server_public_key_hex = server_public_key_hex;
        config
            .trusted_public_keys_hex
            .clone_from(&embedded.trusted_public_keys_hex);
        config.hwid_salt_hex.clone_from(&embedded.hwid_salt_hex);
        config.save(&file).map_err(|e| e.to_string())?;
        return Ok(config);
    }

    let config = ClientConfig {
        schema_version: 1,
        endpoints,
        server_public_key_hex,
        trusted_public_keys_hex: embedded.trusted_public_keys_hex.clone(),
        hwid_salt_hex: embedded.hwid_salt_hex.clone(),
        install_dir: data_dir.join("games"),
        game_dir: None,
        selected_account: None,
        saved_accounts: Vec::new(),
        selected_profile: None,
        jvm_tuning: vec![],
        default_memory_mb: 4096,
        build_settings: std::collections::HashMap::new(),
        stale_update: None,
        game_console: false,
    };
    config.save(&file).map_err(|e| e.to_string())?;
    Ok(config)
}

pub fn init_state() -> Result<AppState, String> {
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
        sync_job: std::sync::Mutex::new(None),
        sync_jobs_started: std::sync::atomic::AtomicU64::new(0),
        game: Mutex::new(None),
        game_log: crate::console::SharedLog::default(),
        face: Mutex::new(None),
        log_dir: logging::log_dir(&data_dir),
        data_dir,
        authlib_jar,
    })
}

pub fn decode_data_uri(uri: &str) -> Option<Vec<u8>> {
    use base64::Engine;
    let payload = uri.strip_prefix("data:")?.split_once(";base64,")?.1;
    base64::engine::general_purpose::STANDARD
        .decode(payload.trim())
        .ok()
}

#[cfg(test)]
mod storage_tests {
    use super::{LEGACY_STORAGE_NAME, adopt_legacy, sanitise_folder};

    #[test]
    fn folder_name_survives_real_project_names() {
        assert_eq!(sanitise_folder("Мир Приключений"), "Мир Приключений");
        assert_eq!(sanitise_folder("  MagicWorld  "), "MagicWorld");
    }

    #[test]
    fn folder_name_drops_path_tricks() {
        for raw in [
            "../../etc",
            "a/b",
            r"c\d",
            "имя:двоеточие",
            "звёзды*?",
            "кавычки\"",
            "труба|",
        ] {
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
        assert!(
            moved.join("games").is_dir(),
            "сборки игрока потерялись при переезде"
        );
        assert!(
            moved.join("config.json").is_file(),
            "настройки потерялись при переезде"
        );
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
        assert_eq!(
            std::fs::read_to_string(mine.join("config.json")).unwrap(),
            "{\"свой\":true}"
        );
        assert!(legacy.exists(), "чужую папку трогать не нужно");
    }
}
