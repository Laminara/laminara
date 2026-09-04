use std::path::{Path, PathBuf};
use std::sync::Arc;

use arc_swap::ArcSwap;
use ed25519_dalek::VerifyingKey;
use tokio_util::sync::CancellationToken;

use crate::account::{self, GameSession};
use crate::config::{ClientConfig, EndpointConfig};
use crate::endpoint::{EndpointPool, EndpointStatus};
use crate::error::CoreError;
use crate::launch::profile::LaunchProfile;
use crate::launch::{build_argv, extract_natives, LaunchInputs};
use crate::manifest::{verify_and_decode, VerifiedManifest};
use crate::paths::LaminaraPaths;
use crate::proto::api::v1::{ProfileSummary, Tokens};
use crate::proto::core::v1::LauncherRelease;
use crate::sync::{self, SyncOutcome, SyncProgress};
use crate::transport::Transport;
use crate::update;

pub struct Account {
    pub uuid: String,
    pub name: String,
    pub endpoint_id: String,
}

pub struct LoginResult {
    pub account: Account,
    pub tokens: Tokens,
    pub session: GameSession,
    pub machine: Option<crate::proto::api::v1::MachineVerdict>,
}

pub struct Core {
    paths: LaminaraPaths,
    config: ArcSwap<ClientConfig>,
    transport: Transport,
    pool: EndpointPool,
    verifying_keys: Vec<VerifyingKey>,
    client_token: String,
    machine_salt: Option<[u8; 32]>,
    machine_facts: tokio::sync::OnceCell<Arc<crate::machine::MachineFacts>>,
    manifests: std::sync::Mutex<std::collections::HashMap<String, Arc<VerifiedManifest>>>,
}

impl Core {
    pub fn new(paths: LaminaraPaths, config: ClientConfig) -> Result<Core, CoreError> {
        let verifying_keys = config.verifying_keys()?;
        let transport = Transport::default();
        let pool = EndpointPool::new(transport.clone(), config.endpoints.clone());
        let client_token = account::ensure_client_token(&paths.config_dir.join("client-token"))?;
        let machine_salt = crate::machine::parse_salt(&config.hwid_salt_hex);
        Ok(Core {
            paths,
            config: ArcSwap::from_pointee(config),
            transport,
            pool,
            verifying_keys,
            client_token,
            machine_salt,
            machine_facts: tokio::sync::OnceCell::new(),
            manifests: std::sync::Mutex::new(std::collections::HashMap::new()),
        })
    }

    pub fn config(&self) -> Arc<ClientConfig> {
        self.config.load_full()
    }

    pub fn current_base_url(&self) -> Option<String> {
        self.pool.current_base_url()
    }

    pub async fn probe_endpoints(&self) -> Vec<EndpointStatus> {
        self.pool.probe().await
    }

    async fn machine_facts(&self) -> Option<Arc<crate::machine::MachineFacts>> {
        let salt = self.machine_salt?;
        Some(
            self.machine_facts
                .get_or_init(|| async move {
                    Arc::new(crate::machine::MachineFacts::collect(&salt).await)
                })
                .await
                .clone(),
        )
    }

    pub async fn login(
        &self,
        username: &str,
        password: &str,
        two_factor_code: &str,
    ) -> Result<LoginResult, CoreError> {
        let facts = self.machine_facts().await;
        let response = self
            .pool
            .login(
                username.to_string(),
                password.to_string(),
                two_factor_code.to_string(),
                facts,
                env!("LAMINARA_VERSION").to_string(),
            )
            .await?;
        let tokens = response.tokens.ok_or_else(|| CoreError::App {
            code: "login".into(),
            message: "response missing tokens".into(),
        })?;
        let base = self.pool.current_base_url().ok_or(CoreError::NoEndpoint)?;
        self.transport.set_machine_ticket(
            response
                .machine
                .as_ref()
                .map(|verdict| verdict.machine_ticket.clone()),
        );
        let session = account::authenticate(
            &self.transport,
            &base,
            username,
            password,
            two_factor_code,
            &self.client_token,
        )
        .await?;
        self.transport.set_access_token(Some(tokens.access.clone()));
        let account = Account {
            uuid: session.uuid.clone(),
            name: session.name.clone(),
            endpoint_id: self.pool.id_for(&base).unwrap_or_default(),
        };
        Ok(LoginResult {
            account,
            tokens,
            session,
            machine: response.machine,
        })
    }

    pub async fn report_machine(&self) -> Result<(), CoreError> {
        let Some(facts) = self.machine_facts().await else {
            return Ok(());
        };
        let verdict = self
            .pool
            .report_machine(facts, env!("LAMINARA_VERSION").to_string())
            .await?;
        self.transport
            .set_machine_ticket(verdict.map(|verdict| verdict.machine_ticket));
        Ok(())
    }

    pub async fn report_crash(
        &self,
        build: String,
        build_version: String,
        loader: String,
        exit_code: i32,
        log: String,
    ) -> Result<String, CoreError> {
        let mut details = std::collections::HashMap::new();
        details.insert("launcher".to_string(), env!("LAMINARA_VERSION").to_string());
        details.insert(
            "platform".to_string(),
            crate::platform::current().as_str_name().to_string(),
        );
        details.insert("os".to_string(), crate::machine::os_version());

        let happened_at_unix_nanos = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|elapsed| elapsed.as_nanos() as i64)
            .unwrap_or_default();

        let response = self
            .pool
            .report_crash(crate::proto::api::v1::CrashReport {
                build,
                build_version,
                loader,
                exit_code,
                log,
                details,
                happened_at_unix_nanos,
            })
            .await?;
        if !response.accepted {
            return Err(CoreError::App {
                code: "crash".into(),
                message: response.message,
            });
        }
        Ok(response.message)
    }

    pub async fn refresh(&self, refresh: &str) -> Result<Tokens, CoreError> {
        let tokens = self.pool.refresh(refresh.to_string()).await?;
        self.transport.set_access_token(Some(tokens.access.clone()));
        Ok(tokens)
    }

    pub fn sign_out(&self) {
        self.transport.set_access_token(None);
    }

    pub fn base_url_for(&self, endpoint_id: &str) -> Option<String> {
        self.config
            .load()
            .endpoints
            .iter()
            .find(|e| e.id == endpoint_id)
            .map(|e| e.base_url.clone())
    }

    pub async fn refresh_session(
        &self,
        base_url: &str,
        access_token: &str,
        client_token: &str,
    ) -> Result<GameSession, CoreError> {
        account::refresh(&self.transport, base_url, access_token, client_token).await
    }

    pub async fn news(&self) -> Result<Vec<crate::proto::api::v1::NewsItem>, CoreError> {
        self.pool.news().await
    }

    pub async fn list_profiles(&self) -> Result<Vec<ProfileSummary>, CoreError> {
        self.pool.list_profiles().await
    }

    pub async fn verified_manifest(
        &self,
        profile: &str,
    ) -> Result<Arc<VerifiedManifest>, CoreError> {
        let response = self.pool.get_manifest(profile.to_string()).await?;
        let verified = Arc::new(verify_and_decode(
            &self.verifying_keys,
            &response.manifest,
            &response.signature,
        )?);
        if let Ok(mut cache) = self.manifests.lock() {
            cache.insert(profile.to_string(), verified.clone());
        }
        Ok(verified)
    }

    fn cached_manifest(&self, profile: &str) -> Option<Arc<VerifiedManifest>> {
        self.manifests.lock().ok()?.get(profile).cloned()
    }

    pub fn profile_dir(&self, profile: &str) -> PathBuf {
        self.config.load().install_dir.join(profile)
    }

    fn state_dir(&self, profile: &str) -> PathBuf {
        self.profile_dir(profile).join(".laminara")
    }

    fn cas_dir(&self) -> PathBuf {
        let install_dir = self.config.load().install_dir.clone();
        let shared = self.paths.data_dir.join("objects");
        if same_volume(&install_dir, &self.paths.data_dir) {
            shared
        } else {
            install_dir.join(".laminara-cas")
        }
    }

    pub async fn check_update(
        &self,
        current_version: &str,
    ) -> Result<Option<update::AvailableUpdate>, CoreError> {
        let response = self.pool.check_update(current_version.to_string()).await?;
        if response.release.is_empty() {
            return Ok(None);
        }
        let release: LauncherRelease = crate::manifest::verify_signed(
            &self.verifying_keys,
            &response.release,
            &response.signature,
        )?;
        if !update::is_newer(&release.version, current_version) {
            return Ok(None);
        }

        let layout = update::detect();
        let Some(artifact) = update::artifact_for(&release, crate::platform::current(), &layout)
        else {
            return Ok(None);
        };
        let object = artifact
            .object
            .as_ref()
            .ok_or_else(|| CoreError::Launch("release artifact has no object".into()))?;
        let hash = object
            .hash
            .as_ref()
            .ok_or_else(|| CoreError::Launch("release artifact has no hash".into()))?;

        let installer_only =
            artifact.kind == crate::proto::core::v1::LauncherArtifactKind::Installer as i32;
        let blocked_reason = match &layout {
            update::InstallLayout::Managed { reason } => Some(reason.clone()),
            _ if installer_only => Some("установка через инсталлятор".into()),
            _ => None,
        };

        Ok(Some(update::AvailableUpdate {
            version: release.version.clone(),
            notes: release.notes.clone(),
            mandatory: !release.minimum_version.is_empty()
                && update::compare(current_version, &release.minimum_version) < 0,
            file_name: artifact.file_name.clone(),
            size: object.size,
            object_key: crate::manifest::object_key(hash.algo, &hash.value),
            algo: hash.algo,
            hash_hex: hex::encode(&hash.value),
            can_install: blocked_reason.is_none(),
            blocked_reason,
        }))
    }

    pub async fn apply_update(
        &self,
        current_version: &str,
        on_bytes: impl Fn(u64, u64) + Send + Sync,
    ) -> Result<PathBuf, CoreError> {
        let base = self.pool.current_base_url().ok_or(CoreError::NoEndpoint)?;
        let response = self.pool.check_update(current_version.to_string()).await?;
        if response.release.is_empty() {
            return Err(CoreError::Launch("no launcher release published".into()));
        }
        let release: LauncherRelease = crate::manifest::verify_signed(
            &self.verifying_keys,
            &response.release,
            &response.signature,
        )?;
        if !update::is_newer(&release.version, current_version) {
            return Err(CoreError::Launch("already up to date".into()));
        }

        let layout = update::detect();
        let target = layout
            .target()
            .ok_or_else(|| {
                CoreError::Launch("this installation is managed and cannot self-update".into())
            })?
            .to_path_buf();
        let artifact = update::artifact_for(&release, crate::platform::current(), &layout)
            .ok_or_else(|| CoreError::Launch("no launcher build for this platform".into()))?;
        if artifact.kind == crate::proto::core::v1::LauncherArtifactKind::Installer as i32 {
            return Err(CoreError::Launch(
                "this release ships an installer; run it manually".into(),
            ));
        }

        let staging = update::staging_dir(&target);
        let staged =
            update::download_artifact(&self.transport, &base, artifact, &staging, on_bytes).await?;
        let staged = update::stage_payload(artifact, &staged, &staging)?;
        update::swap::apply(&layout, &staged)?;
        let _ = std::fs::remove_dir_all(&staging);
        Ok(update::relaunch_target(&layout))
    }

    pub async fn collect_garbage(&self) -> Result<u64, CoreError> {
        let cas_dir = self.cas_dir();
        let install_dir = self.config.load().install_dir.clone();
        offload(move || sync::gc_cas(&cas_dir, &install_dir)).await
    }

    pub async fn verify_installed(&self, profile: &str) -> Result<Vec<String>, CoreError> {
        let profile_dir = self.profile_dir(profile);
        let state_dir = self.state_dir(profile);
        offload(move || Ok(sync::verify_installed(&profile_dir, &state_dir))).await
    }

    pub async fn discard_broken(&self, profile: &str, deep: bool) -> Result<u64, CoreError> {
        let profile_dir = self.profile_dir(profile);
        let state_dir = self.state_dir(profile);
        let cas_dir = self.cas_dir();
        offload(move || {
            let broken = if deep {
                sync::verify_contents(&profile_dir, &state_dir)
            } else {
                sync::verify_installed(&profile_dir, &state_dir)
            };
            sync::discard_installed(&profile_dir, &state_dir, &cas_dir, &broken)
        })
        .await
    }

    pub async fn sync_profile(
        &self,
        profile: &str,
        manifest: &VerifiedManifest,
        cancel: CancellationToken,
        on_progress: impl Fn(SyncProgress) + Send + Sync,
    ) -> Result<SyncOutcome, CoreError> {
        let base = self.pool.current_base_url().ok_or(CoreError::NoEndpoint)?;
        let profile_dir = self.profile_dir(profile);
        let state_dir = self.state_dir(profile);
        let cas_dir = self.cas_dir();
        let parallel = manifest
            .manifest
            .fetch_tuning
            .as_ref()
            .map(|t| t.max_parallel as usize)
            .unwrap_or(0);
        let selection = self.build_settings(profile).feature_selection;
        sync::sync(
            sync::SyncPlan {
                transport: &self.transport,
                base_url: &base,
                profile_dir: &profile_dir,
                state_dir: &state_dir,
                cas_dir: &cas_dir,
                manifest: &manifest.manifest,
                selection: &selection,
                max_parallel: parallel,
                cancel,
            },
            on_progress,
        )
        .await
    }

    pub async fn launch(
        &self,
        profile: &str,
        session: &GameSession,
        authlib_jar: &Path,
        client_version: &str,
    ) -> Result<tokio::process::Child, CoreError> {
        let config = self.config.load();
        let profile_dir = self.profile_dir(profile);
        let launch_profile =
            LaunchProfile::load(&profile_dir.join(crate::launch::LAUNCH_PROFILE_NAME))?;

        let natives_dir = self
            .state_dir(profile)
            .join("natives")
            .join(&launch_profile.platform_key);
        extract_natives(&launch_profile, &profile_dir, &natives_dir)?;

        let base = self.pool.current_base_url().ok_or(CoreError::NoEndpoint)?;
        let prefetch = account::prefetch(&self.transport, &base).await?;
        let yggdrasil_root = format!("{}/yggdrasil/", base.trim_end_matches('/'));

        let java_bin = java_binary(&profile_dir, &launch_profile);
        let game_dir = config
            .game_dir
            .clone()
            .unwrap_or_else(|| profile_dir.clone());
        let jvm = self.effective_jvm(profile);

        let manifest = match self.cached_manifest(profile) {
            Some(manifest) => manifest,
            None => self.verified_manifest(profile).await?,
        };
        let extras = crate::features::resolve_extras(
            &manifest.manifest.features,
            &self.build_settings(profile).feature_selection,
        );

        let authlib_jar = authlib_from(&profile_dir, authlib_jar)?;

        let argv = build_argv(&LaunchInputs {
            profile: &launch_profile,
            profile_dir: &profile_dir,
            game_dir: &game_dir,
            java_bin: &java_bin,
            natives_dir: &natives_dir,
            yggdrasil_root: &yggdrasil_root,
            authlib_jar: &authlib_jar,
            prefetch_b64: &prefetch,
            session,
            jvm_tuning: &jvm,
            extras: &extras,
            client_version,
        });

        let mut command = crate::process::async_command(&argv[0]);
        command
            .args(&argv[1..])
            .current_dir(&game_dir)
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped());
        command
            .spawn()
            .map_err(|e| CoreError::Launch(format!("spawn java: {e}")))
    }

    pub fn set_endpoints(&self, endpoints: Vec<EndpointConfig>) -> Result<(), CoreError> {
        let mut next = ClientConfig::clone(&self.config.load());
        next.endpoints = endpoints;
        next.save(&self.paths.config_file())?;
        self.config.store(Arc::new(next));
        Ok(())
    }

    pub fn default_memory_mb(&self) -> u32 {
        self.config.load().default_memory_mb
    }

    pub fn install_dir(&self) -> PathBuf {
        self.config.load().install_dir.clone()
    }

    pub fn set_install_dir(&self, path: PathBuf) -> Result<(), CoreError> {
        let mut next = ClientConfig::clone(&self.config.load());
        next.install_dir = path;
        next.save(&self.paths.config_file())?;
        self.config.store(Arc::new(next));
        Ok(())
    }

    pub fn endpoints(&self) -> Vec<EndpointConfig> {
        self.config.load().endpoints.clone()
    }

    pub fn stale_update(&self) -> Option<String> {
        self.config.load().stale_update.clone()
    }

    pub fn remember_stale_update(&self, version: &str) -> Result<(), CoreError> {
        let mut next = ClientConfig::clone(&self.config.load());
        next.stale_update = Some(version.to_string());
        next.save(&self.paths.config_file())?;
        self.config.store(Arc::new(next));
        Ok(())
    }

    pub fn build_settings(&self, profile: &str) -> crate::config::BuildSettings {
        self.config
            .load()
            .build_settings
            .get(profile)
            .cloned()
            .unwrap_or_default()
    }

    pub fn set_build_memory(
        &self,
        profile: &str,
        max_memory_mb: Option<u32>,
    ) -> Result<(), CoreError> {
        let mut next = ClientConfig::clone(&self.config.load());
        let entry = next.build_settings.entry(profile.to_string()).or_default();
        entry.max_memory_mb = max_memory_mb.map(|m| m.clamp(512, 65536));
        next.save(&self.paths.config_file())?;
        self.config.store(Arc::new(next));
        Ok(())
    }

    pub fn set_feature_selection(
        &self,
        profile: &str,
        selection: crate::features::FeatureSelection,
    ) -> Result<(), CoreError> {
        let mut next = ClientConfig::clone(&self.config.load());
        let entry = next.build_settings.entry(profile.to_string()).or_default();
        entry.feature_selection = selection;
        next.save(&self.paths.config_file())?;
        self.config.store(Arc::new(next));
        Ok(())
    }

    fn effective_jvm(&self, profile: &str) -> Vec<String> {
        let config = self.config.load();
        let memory = self
            .build_settings(profile)
            .max_memory_mb
            .unwrap_or(config.default_memory_mb);
        let mut jvm = vec![format!("-Xmx{memory}m")];
        jvm.extend(
            config
                .jvm_tuning
                .iter()
                .filter(|a| !a.starts_with("-Xmx") && !a.starts_with("-Xms"))
                .cloned(),
        );
        jvm
    }

    pub fn save_selection(
        &self,
        account: Option<crate::config::SelectedAccount>,
        profile: Option<String>,
    ) -> Result<(), CoreError> {
        let mut next = ClientConfig::clone(&self.config.load());
        if account.is_some() {
            next.selected_account = account;
        }
        if profile.is_some() {
            next.selected_profile = profile;
        }
        next.save(&self.paths.config_file())?;
        self.config.store(Arc::new(next));
        Ok(())
    }
}

async fn offload<T, F>(work: F) -> Result<T, CoreError>
where
    F: FnOnce() -> Result<T, CoreError> + Send + 'static,
    T: Send + 'static,
{
    match tokio::task::spawn_blocking(work).await {
        Ok(result) => result,
        Err(e) => Err(CoreError::Io(e.to_string())),
    }
}

pub const AUTHLIB_INJECTOR_NAME: &str = "authlib-injector.jar";

fn authlib_from(profile_dir: &Path, fallback: &Path) -> Result<PathBuf, CoreError> {
    let shipped = profile_dir.join(AUTHLIB_INJECTOR_NAME);
    if shipped.is_file() {
        return Ok(shipped);
    }
    if fallback.is_file() {
        return Ok(fallback.to_path_buf());
    }
    Err(CoreError::Launch(format!(
        "в сборке нет {AUTHLIB_INJECTOR_NAME} — без него игра не сможет войти на сервер; положите его в сборку и опубликуйте её заново"
    )))
}

fn java_binary(profile_dir: &Path, profile: &LaunchProfile) -> PathBuf {
    if !profile.java_bin.is_empty() {
        let mut path = profile_dir.to_path_buf();
        for segment in profile.java_bin.split('/') {
            path.push(segment);
        }
        return path;
    }
    let bin = profile_dir
        .join("runtime")
        .join(&profile.platform_key)
        .join("bin");
    if profile.platform_key.starts_with("windows") {
        bin.join("javaw.exe")
    } else {
        bin.join("java")
    }
}

fn same_volume(a: &Path, b: &Path) -> bool {
    let anchor = |path: &Path| {
        let mut current = path.to_path_buf();
        loop {
            if current.exists() {
                return Some(current);
            }
            if !current.pop() {
                return None;
            }
        }
    };
    let (Some(a), Some(b)) = (anchor(a), anchor(b)) else {
        return true;
    };

    #[cfg(unix)]
    {
        use std::os::unix::fs::MetadataExt;
        if let (Ok(am), Ok(bm)) = (std::fs::metadata(&a), std::fs::metadata(&b)) {
            return am.dev() == bm.dev();
        }
        true
    }
    #[cfg(not(unix))]
    {
        a.components().next() == b.components().next()
    }
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeMap;

    use super::*;
    use crate::proto::core::v1::FilePolicy;

    fn test_core(tmp: &Path) -> Core {
        let key = ed25519_dalek::SigningKey::from_bytes(&[7u8; 32]);
        Core::new(
            LaminaraPaths {
                config_dir: tmp.join("config"),
                data_dir: tmp.join("data"),
            },
            ClientConfig {
                schema_version: 1,
                endpoints: Vec::new(),
                server_public_key_hex: hex::encode(key.verifying_key().as_bytes()),
                trusted_public_keys_hex: Vec::new(),
                hwid_salt_hex: String::new(),
                install_dir: tmp.join("install"),
                game_dir: None,
                selected_account: None,
                selected_profile: None,
                jvm_tuning: Vec::new(),
                default_memory_mb: 4096,
                build_settings: std::collections::HashMap::new(),
                stale_update: None,
            },
        )
        .unwrap()
    }

    fn ledger_entry(hash: &str) -> sync::OwnedEntry {
        sync::OwnedEntry {
            object_hash: hash.to_string(),
            class: FilePolicy::Unspecified as i32,
            placement: sync::Placement::Hardlink,
            released: false,
            size: 0,
            algo: 0,
        }
    }

    fn write_ledger(profile: &Path, hashes: &[&str]) {
        let mut ledger: sync::Ledger = BTreeMap::new();
        for (index, hash) in hashes.iter().enumerate() {
            ledger.insert(format!("mods/file{index}.bin"), ledger_entry(hash));
        }
        sync::save_ledger(&profile.join(".laminara").join(sync::LEDGER_FILE), &ledger).unwrap();
    }

    #[test]
    fn the_injector_that_came_with_the_build_wins() {
        let tmp = tempfile::tempdir().unwrap();
        let profile = tmp.path().join("profile");
        std::fs::create_dir_all(&profile).unwrap();
        let fallback = tmp.path().join(AUTHLIB_INJECTOR_NAME);
        std::fs::write(&fallback, b"local").unwrap();

        assert_eq!(authlib_from(&profile, &fallback).unwrap(), fallback);

        let shipped = profile.join(AUTHLIB_INJECTOR_NAME);
        std::fs::write(&shipped, b"signed").unwrap();
        assert_eq!(authlib_from(&profile, &fallback).unwrap(), shipped);

        std::fs::remove_file(&shipped).unwrap();
        std::fs::remove_file(&fallback).unwrap();
        assert!(authlib_from(&profile, &fallback).is_err());
    }

    #[tokio::test]
    async fn offload_returns_the_worker_result() {
        let value = offload(|| Ok::<u8, CoreError>(7)).await;
        assert!(matches!(value, Ok(7)));
    }

    #[tokio::test]
    async fn offload_survives_a_panicking_worker() {
        let value: Result<u8, CoreError> = offload(|| panic!("worker failed")).await;
        assert!(matches!(value, Err(CoreError::Io(_))));
    }

    #[tokio::test]
    async fn verify_installed_accepts_read_only_and_reports_broken_immutables() {
        let tmp = tempfile::tempdir().unwrap();
        let core = test_core(tmp.path());
        let profile = core.profile_dir("Adventure");
        std::fs::create_dir_all(profile.join(".laminara")).unwrap();
        write_ledger(&profile, &["intact", "tampered", "missing"]);

        for (name, contents) in [
            ("file0.bin", &b"intact"[..]),
            ("file1.bin", &b"tampered"[..]),
        ] {
            let path = profile.join("mods").join(name);
            std::fs::create_dir_all(path.parent().unwrap()).unwrap();
            std::fs::write(&path, contents).unwrap();
        }
        sync::set_mode(&profile.join("mods/file0.bin"), 0o444);

        let mut broken = core.verify_installed("Adventure").await.unwrap();
        broken.sort();
        assert_eq!(
            broken,
            vec!["mods/file1.bin".to_string(), "mods/file2.bin".to_string(),]
        );
    }

    #[tokio::test]
    async fn collect_garbage_keeps_objects_named_by_a_ledger() {
        let tmp = tempfile::tempdir().unwrap();
        let core = test_core(tmp.path());

        let profile = core.profile_dir("Adventure");
        std::fs::create_dir_all(profile.join(".laminara")).unwrap();
        write_ledger(&profile, &["kept"]);

        let objects = tmp.path().join("data/objects/blake3/ab/cd");
        std::fs::create_dir_all(&objects).unwrap();
        std::fs::write(objects.join("kept"), b"kept").unwrap();
        std::fs::write(objects.join("dropped"), b"dropped").unwrap();

        let removed = core.collect_garbage().await.unwrap();

        assert_eq!(removed, 1);
        assert!(objects.join("kept").exists());
        assert!(!objects.join("dropped").exists());
    }
}
