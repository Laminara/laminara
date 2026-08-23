use std::collections::HashMap;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use futures::future::join_all;
use serde::{Deserialize, Serialize};
use tauri::ipc::Channel;
use tauri::{AppHandle, Emitter, Manager, State};
use tokio_util::sync::CancellationToken;
use zeroize::Zeroizing;

use laminara_core::config::EndpointConfig;
use laminara_core::features::{self, FeatureSelection};
use laminara_core::launch::LAUNCH_PROFILE_NAME;
use laminara_core::proto::core::v1::{
    FeatureGroup, FeatureModel, FeatureOption, Platform, SelectionType,
};
use laminara_core::slp;
use laminara_core::sync::{SyncProgress, SyncStage};

use crate::auth::{AuthStatus, Session};
use crate::AppState;

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct EndpointStatusDto {
    id: String,
    base_url: String,
    healthy: bool,
    latency_ms: Option<u32>,
    is_current: bool,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AccountDto {
    uuid: String,
    name: String,
    endpoint_id: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BuildDto {
    name: String,
    version: String,
    loader: Option<String>,
    size_bytes: u64,
    install: String,
    has_features: bool,
    available: bool,
    platforms: Vec<String>,
    locked: bool,
    lock_reason: Option<String>,
}

#[derive(Deserialize)]
pub struct EndpointInput {
    id: String,
    base_url: String,
}

#[derive(Serialize, Clone)]
#[serde(tag = "event", content = "data", rename_all = "camelCase")]
pub enum SyncEvent {
    #[serde(rename_all = "camelCase")]
    Started {
        files_total: u64,
        bytes_total: u64,
    },
    #[serde(rename_all = "camelCase")]
    Progress {
        stage: String,
        files_done: u64,
        files_total: u64,
        bytes_done: u64,
        bytes_total: u64,
        current_path: Option<String>,
    },
    #[serde(rename_all = "camelCase")]
    Finished {
        downloaded: u64,
        skipped: u64,
        pruned: u64,
    },
    Failed {
        message: String,
    },
}

#[tauri::command]
pub async fn probe_endpoints(state: State<'_, AppState>) -> Result<Vec<EndpointStatusDto>, String> {
    let statuses = state.core.probe_endpoints().await;
    Ok(statuses
        .into_iter()
        .map(|s| EndpointStatusDto {
            id: s.id,
            base_url: s.base_url,
            healthy: s.healthy,
            latency_ms: s.latency_ms,
            is_current: s.is_current,
        })
        .collect())
}

#[tauri::command]
pub async fn endpoints_set(
    state: State<'_, AppState>,
    endpoints: Vec<EndpointInput>,
) -> Result<(), String> {
    let list = endpoints
        .into_iter()
        .map(|e| EndpointConfig {
            id: e.id,
            base_url: e.base_url,
        })
        .collect();
    state
        .core
        .set_endpoints(list)
        .map_err(|e| player_error("set endpoints", e))
}

fn player_message(error: &laminara_core::CoreError) -> String {
    use laminara_core::CoreError;
    match error {
        CoreError::App { message, .. } => {
            for (sentinel, russian) in [
                ("invalid credentials", "Неверный логин или пароль"),
                (
                    "too many attempts",
                    "Слишком много попыток. Подождите несколько минут",
                ),
                ("session expired", "Сессия истекла, войдите заново"),
            ] {
                if message.contains(sentinel) {
                    return russian.into();
                }
            }
            message.clone()
        }
        CoreError::NoEndpoint | CoreError::Transport(_) => {
            "Сервер не отвечает. Проверьте подключение к интернету".into()
        }
        CoreError::Untrusted => "Подпись сборки не совпала — скачайте лаунчер заново".into(),
        CoreError::Sync(_) => "Не удалось установить сборку. Что именно — в логе лаунчера".into(),
        CoreError::Launch(_) => "Не удалось запустить игру. Что именно — в логе лаунчера".into(),
        CoreError::Config(_) | CoreError::Io(_) => {
            "Ошибка на этом компьютере. Что именно — в логе лаунчера".into()
        }
    }
}

fn player_error(context: &str, error: laminara_core::CoreError) -> String {
    tracing::error!("{context}: {error}");
    player_message(&error)
}

fn now_unix_nanos() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos() as i64)
        .unwrap_or_default()
}

const TOKEN_REFRESH_LEAD: i64 = 60_000_000_000;

fn spawn_token_keeper(app: AppHandle, generation: u64) {
    tauri::async_runtime::spawn(async move {
        loop {
            let wait = {
                let state = app.state::<AppState>();
                if state.auth.generation() != generation {
                    return;
                }
                let Some(expiry) = state.auth.access_expiry() else {
                    return;
                };
                (expiry - now_unix_nanos() - TOKEN_REFRESH_LEAD).max(1_000_000_000) as u64
            };
            tokio::time::sleep(Duration::from_nanos(wait)).await;

            let state = app.state::<AppState>();
            if state.auth.generation() != generation {
                return;
            }
            let Some((endpoint_id, uuid, _)) = state.auth.identity() else {
                return;
            };
            let Some(refresh) = state.auth.load_refresh(&endpoint_id, &uuid) else {
                return;
            };
            match state.core.refresh(&refresh).await {
                Ok(tokens) => {
                    let _ = state
                        .auth
                        .store_refresh(&endpoint_id, &uuid, &tokens.refresh);
                    state
                        .auth
                        .update_access(tokens.access, tokens.access_expires_unix_nanos);
                }
                Err(e) => {
                    tracing::warn!("token keeper stopping: {e}");
                    return;
                }
            }
        }
    });
}

#[tauri::command]
pub async fn login(
    app: AppHandle,
    state: State<'_, AppState>,
    username: String,
    password: String,
) -> Result<AccountDto, String> {
    let result = state.core.login(&username, &password).await.map_err(|e| {
        tracing::error!("login failed for {username}: {e:?}");
        player_message(&e)
    })?;
    tracing::info!("signed in as {}", result.account.name);
    let endpoint_id = result.account.endpoint_id.clone();
    let uuid = result.account.uuid.clone();
    let name = result.account.name.clone();

    let _ = state
        .auth
        .store_refresh(&endpoint_id, &uuid, &result.tokens.refresh);
    let _ = state.auth.store_game(
        &endpoint_id,
        &uuid,
        &result.session.access_token,
        &result.session.client_token,
    );
    let _ = state.core.save_selection(
        Some(laminara_core::config::SelectedAccount {
            endpoint_id: endpoint_id.clone(),
            uuid: uuid.clone(),
            name: name.clone(),
        }),
        None,
    );

    let dto = AccountDto {
        uuid: uuid.clone(),
        name: name.clone(),
        endpoint_id: endpoint_id.clone(),
    };
    let generation = state.auth.set_session(Session {
        account: result.account,
        access: Zeroizing::new(result.tokens.access),
        access_expires_unix_nanos: result.tokens.access_expires_unix_nanos,
        game: result.session,
    });
    spawn_token_keeper(app, generation);
    Ok(dto)
}

#[tauri::command]
pub async fn logout(state: State<'_, AppState>) -> Result<(), String> {
    if let Some((endpoint_id, uuid, _)) = state.auth.identity() {
        state.auth.clear_refresh(&endpoint_id, &uuid);
        state.auth.clear_game(&endpoint_id, &uuid);
    }
    state.auth.clear_session();
    state.core.sign_out();
    Ok(())
}

#[tauri::command]
pub fn auth_status(state: State<'_, AppState>) -> AuthStatus {
    state.auth.status()
}

#[tauri::command]
pub async fn restore_session(
    app: AppHandle,
    state: State<'_, AppState>,
) -> Result<AuthStatus, String> {
    if state.auth.status().signed_in {
        return Ok(state.auth.status());
    }
    let Some(account) = state.core.config().selected_account.clone() else {
        return Ok(state.auth.status());
    };
    let Some((access, client)) = state.auth.load_game(&account.endpoint_id, &account.uuid) else {
        return Ok(state.auth.status());
    };
    let Some(base) = state.core.base_url_for(&account.endpoint_id) else {
        return Ok(state.auth.status());
    };

    let mut launcher_access = String::new();
    let mut launcher_access_expires = 0i64;
    if let Some(refresh) = state.auth.load_refresh(&account.endpoint_id, &account.uuid) {
        match state.core.refresh(&refresh).await {
            Ok(tokens) => {
                let _ =
                    state
                        .auth
                        .store_refresh(&account.endpoint_id, &account.uuid, &tokens.refresh);
                launcher_access = tokens.access;
                launcher_access_expires = tokens.access_expires_unix_nanos;
            }
            Err(e) => tracing::warn!("launcher session refresh failed: {e}"),
        }
    }

    if let Err(e) = state.core.report_machine().await {
        tracing::warn!("machine check failed during restore: {e:?}");
        return Ok(state.auth.status());
    }

    match state.core.refresh_session(&base, &access, &client).await {
        Ok(session) => {
            tracing::info!("session restored for {}", session.name);
            let _ = state.auth.store_game(
                &account.endpoint_id,
                &session.uuid,
                &session.access_token,
                &session.client_token,
            );
            let generation = state.auth.set_session(Session {
                account: laminara_core::Account {
                    uuid: session.uuid.clone(),
                    name: session.name.clone(),
                    endpoint_id: account.endpoint_id.clone(),
                },
                access: Zeroizing::new(launcher_access),
                access_expires_unix_nanos: launcher_access_expires,
                game: session,
            });
            spawn_token_keeper(app, generation);
        }
        Err(e) => tracing::warn!("session restore failed: {e}"),
    }
    Ok(state.auth.status())
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct NewsItemDto {
    id: String,
    title: String,
    body: String,
    published_at_unix_nanos: i64,
    tag: Option<String>,
    link: Option<String>,
    banner_data_uri: Option<String>,
}

#[tauri::command]
pub async fn news(state: State<'_, AppState>) -> Result<Vec<NewsItemDto>, String> {
    let items = state.core.news().await.unwrap_or_default();
    Ok(items
        .into_iter()
        .map(|item| NewsItemDto {
            id: item.id,
            title: item.title,
            body: item.body,
            published_at_unix_nanos: item.published_at_unix_nanos,
            tag: if item.tag.is_empty() {
                None
            } else {
                Some(item.tag)
            },
            link: if item.link.is_empty() {
                None
            } else {
                Some(item.link)
            },
            banner_data_uri: if item.banner_data_uri.is_empty() {
                None
            } else {
                Some(item.banner_data_uri)
            },
        })
        .collect())
}

#[tauri::command]
pub async fn open_external(url: String) -> Result<(), String> {
    let lower = url.to_lowercase();
    if !lower.starts_with("http://") && !lower.starts_with("https://") {
        return Err("only http and https links can be opened".into());
    }
    #[cfg(target_os = "linux")]
    let command = ("xdg-open", vec![url]);
    #[cfg(target_os = "macos")]
    let command = ("/usr/bin/open", vec![url]);
    #[cfg(target_os = "windows")]
    let command = (
        "rundll32.exe",
        vec!["url.dll,FileProtocolHandler".to_string(), url],
    );

    laminara_core::process::async_command(command.0)
        .args(command.1)
        .spawn()
        .map(|_| ())
        .map_err(|e| {
            tracing::error!("open external link: {e}");
            "Не удалось открыть ссылку в браузере".to_string()
        })
}

#[tauri::command]
pub async fn list_builds(state: State<'_, AppState>) -> Result<Vec<BuildDto>, String> {
    let profiles = state
        .core
        .list_profiles()
        .await
        .map_err(|e| player_error("list profiles", e))?;
    let mine = laminara_core::platform::current();
    Ok(profiles
        .into_iter()
        .map(|profile| {
            let installed = state
                .core
                .profile_dir(&profile.name)
                .join(LAUNCH_PROFILE_NAME)
                .exists();
            BuildDto {
                name: profile.name,
                version: profile.version,
                loader: if profile.loader.trim().is_empty() {
                    None
                } else {
                    Some(profile.loader)
                },
                size_bytes: profile.total_size,
                install: if installed {
                    "installed".into()
                } else {
                    "missing".into()
                },
                has_features: profile.has_features,
                available: profile.platforms.is_empty()
                    || profile.platforms.contains(&(mine as i32)),
                platforms: profile
                    .platforms
                    .iter()
                    .filter_map(|value| {
                        Platform::try_from(*value)
                            .ok()
                            .and_then(laminara_core::platform::key)
                    })
                    .collect(),
                locked: profile.locked,
                lock_reason: if profile.lock_reason.trim().is_empty() {
                    None
                } else {
                    Some(profile.lock_reason)
                },
            }
        })
        .collect())
}

#[tauri::command]
pub async fn sync_profile(
    state: State<'_, AppState>,
    profile: String,
    on_event: Channel<SyncEvent>,
) -> Result<(), String> {
    tracing::info!("sync started for {profile}");
    let manifest = state
        .core
        .verified_manifest(&profile)
        .await
        .map_err(|e| player_error(&format!("manifest verify failed for {profile}"), e))?;
    let _ = on_event.send(SyncEvent::Started {
        files_total: manifest.manifest.files.len() as u64,
        bytes_total: manifest.manifest.total_size,
    });

    let cancel = CancellationToken::new();
    state
        .jobs
        .lock()
        .await
        .insert(profile.clone(), cancel.clone());

    let channel = on_event.clone();
    let result = state
        .core
        .sync_profile(&profile, &manifest, cancel, move |p: SyncProgress| {
            let stage = match p.stage {
                SyncStage::Planning => "planning",
                SyncStage::Downloading => "downloading",
                SyncStage::Done => "done",
            };
            let _ = channel.send(SyncEvent::Progress {
                stage: stage.into(),
                files_done: p.files_done,
                files_total: p.files_total,
                bytes_done: p.bytes_done,
                bytes_total: p.bytes_total,
                current_path: p.current_path,
            });
        })
        .await;

    state.jobs.lock().await.remove(&profile);
    match result {
        Ok(outcome) => {
            tracing::info!(
                "sync finished for {profile}: downloaded={} linked={} skipped={} pruned={}",
                outcome.downloaded,
                outcome.linked,
                outcome.skipped,
                outcome.pruned
            );
            let _ = on_event.send(SyncEvent::Finished {
                downloaded: outcome.downloaded,
                skipped: outcome.skipped,
                pruned: outcome.pruned,
            });
            Ok(())
        }
        Err(err) => {
            tracing::error!("sync failed for {profile}: {err}");
            let _ = on_event.send(SyncEvent::Failed {
                message: err.to_string(),
            });
            Err(err.to_string())
        }
    }
}

#[tauri::command]
pub async fn cancel_job(state: State<'_, AppState>, job: String) -> Result<(), String> {
    if let Some(token) = state.jobs.lock().await.get(&job) {
        token.cancel();
    }
    Ok(())
}

#[tauri::command]
pub async fn launch(
    app: AppHandle,
    state: State<'_, AppState>,
    profile: String,
) -> Result<(), String> {
    let game = state.auth.game_session().ok_or("not signed in")?;
    let authlib = state.authlib_jar.clone();

    state
        .core
        .report_machine()
        .await
        .map_err(|e| player_error("report machine", e))?;

    let broken = state.core.verify_installed(&profile);
    if !broken.is_empty() {
        tracing::error!(
            "integrity check failed for {profile}: {} file(s), first: {:?}",
            broken.len(),
            broken.first()
        );
        return Err(format!(
            "Файлы сборки повреждены ({}). Нажмите «Обновить», чтобы восстановить.",
            broken.len()
        ));
    }
    tracing::info!("launching {profile}");

    let mut child = state
        .core
        .launch(&profile, &game, &authlib, env!("CARGO_PKG_VERSION"))
        .await
        .map_err(|e| player_error(&format!("launch failed for {profile}"), e))?;

    let stdout = child.stdout.take();
    let stderr = child.stderr.take();
    let token = CancellationToken::new();
    *state.game_token.lock().await = Some(token.clone());

    tokio::spawn(async move {
        use tokio::io::{AsyncBufReadExt, BufReader};
        if let Some(out) = stdout {
            let app = app.clone();
            tokio::spawn(async move {
                let mut lines = BufReader::new(out).lines();
                while let Ok(Some(line)) = lines.next_line().await {
                    let _ = app.emit("game:log", line);
                }
            });
        }
        if let Some(err) = stderr {
            let app = app.clone();
            tokio::spawn(async move {
                let mut lines = BufReader::new(err).lines();
                while let Ok(Some(line)) = lines.next_line().await {
                    let _ = app.emit("game:log", line);
                }
            });
        }
        let status = tokio::select! {
            status = child.wait() => status.ok(),
            _ = token.cancelled() => {
                let _ = child.kill().await;
                child.wait().await.ok()
            }
        };
        let code = status.and_then(|s| s.code()).unwrap_or(-1);
        let _ = app.emit("game:exit", code);
    });

    Ok(())
}

#[tauri::command]
pub async fn stop(state: State<'_, AppState>) -> Result<(), String> {
    if let Some(token) = state.game_token.lock().await.take() {
        token.cancel();
    }
    Ok(())
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct EndpointDto {
    id: String,
    base_url: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GeneralSettings {
    install_dir: String,
    default_memory_mb: u32,
    endpoints: Vec<EndpointDto>,
    version: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BuildSettingsDto {
    max_memory_mb: Option<u32>,
    default_memory_mb: u32,
}

#[tauri::command]
pub fn general_settings(state: State<'_, AppState>) -> GeneralSettings {
    GeneralSettings {
        install_dir: state.core.install_dir().to_string_lossy().into_owned(),
        default_memory_mb: state.core.default_memory_mb(),
        endpoints: state
            .core
            .endpoints()
            .into_iter()
            .map(|e| EndpointDto {
                id: e.id,
                base_url: e.base_url,
            })
            .collect(),
        version: env!("CARGO_PKG_VERSION").into(),
    }
}

#[tauri::command]
pub async fn set_default_memory(state: State<'_, AppState>, mb: u32) -> Result<(), String> {
    state
        .core
        .set_default_memory(mb)
        .map_err(|e| player_error("set default memory", e))
}

#[tauri::command]
pub fn branding() -> serde_json::Value {
    crate::embedded_branding()
}

#[tauri::command]
pub async fn collect_garbage(state: State<'_, AppState>) -> Result<u64, String> {
    let removed = state
        .core
        .collect_garbage()
        .map_err(|e| player_error("collect garbage", e))?;
    tracing::info!("cas gc removed {removed} object(s)");
    Ok(removed)
}

#[tauri::command]
pub async fn set_install_dir(state: State<'_, AppState>, path: String) -> Result<(), String> {
    state
        .core
        .set_install_dir(path.into())
        .map_err(|e| player_error("set install dir", e))
}

#[tauri::command]
pub fn build_settings(state: State<'_, AppState>, profile: String) -> BuildSettingsDto {
    BuildSettingsDto {
        max_memory_mb: state.core.build_settings(&profile).max_memory_mb,
        default_memory_mb: state.core.default_memory_mb(),
    }
}

#[tauri::command]
pub async fn set_build_memory(
    state: State<'_, AppState>,
    profile: String,
    max_memory_mb: Option<u32>,
) -> Result<(), String> {
    state
        .core
        .set_build_memory(&profile, max_memory_mb)
        .map_err(|e| player_error("set build memory", e))
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FeatureMetaDto {
    icon: String,
    badge: String,
    added_size: u64,
    requires: Vec<String>,
    incompatible_with: Vec<String>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FeatureOptionDto {
    id: String,
    title: String,
    description: String,
    default_enabled: bool,
    files: Vec<String>,
    groups: Vec<FeatureGroupDto>,
    meta: FeatureMetaDto,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FeatureGroupDto {
    id: String,
    title: String,
    description: String,
    selection: String,
    required: bool,
    options: Vec<FeatureOptionDto>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BuildFeaturesDto {
    model: Vec<FeatureGroupDto>,
    selection: FeatureSelection,
    active: Vec<String>,
}

fn group_dto(group: &FeatureGroup) -> FeatureGroupDto {
    FeatureGroupDto {
        id: group.id.clone(),
        title: group.title.clone(),
        description: group.description.clone(),
        selection: match group.selection() {
            SelectionType::Single => "single",
            _ => "multi",
        }
        .into(),
        required: group.required,
        options: group.options.iter().map(option_dto).collect(),
    }
}

fn option_dto(option: &FeatureOption) -> FeatureOptionDto {
    let meta = option.meta.as_ref();
    FeatureOptionDto {
        id: option.id.clone(),
        title: option.title.clone(),
        description: option.description.clone(),
        default_enabled: option.default_enabled,
        files: option.files.clone(),
        groups: option.groups.iter().map(group_dto).collect(),
        meta: FeatureMetaDto {
            icon: meta.map(|m| m.icon.clone()).unwrap_or_default(),
            badge: meta.map(|m| m.badge.clone()).unwrap_or_default(),
            added_size: meta.map(|m| m.added_size).unwrap_or_default(),
            requires: meta.map(|m| m.requires.clone()).unwrap_or_default(),
            incompatible_with: meta
                .map(|m| m.incompatible_with.clone())
                .unwrap_or_default(),
        },
    }
}

#[tauri::command]
pub async fn build_features(
    state: State<'_, AppState>,
    profile: String,
) -> Result<BuildFeaturesDto, String> {
    let manifest = state
        .core
        .verified_manifest(&profile)
        .await
        .map_err(|e| player_error("verify manifest", e))?;
    let model: &Option<FeatureModel> = &manifest.manifest.features;
    let selection = state.core.build_settings(&profile).feature_selection;
    let (active_set, _) = features::resolve_active(model, &selection);
    let mut active: Vec<String> = active_set.into_iter().collect();
    active.sort();
    Ok(BuildFeaturesDto {
        model: model
            .as_ref()
            .map(|m| m.groups.iter().map(group_dto).collect())
            .unwrap_or_default(),
        selection,
        active,
    })
}

#[tauri::command]
pub async fn set_build_features(
    state: State<'_, AppState>,
    profile: String,
    selection: FeatureSelection,
) -> Result<(), String> {
    state
        .core
        .set_feature_selection(&profile, selection)
        .map_err(|e| player_error("set feature selection", e))
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct PlayerCounts {
    per_build: HashMap<String, i64>,
    total: i64,
}

#[tauri::command]
pub async fn player_counts(state: State<'_, AppState>) -> Result<PlayerCounts, String> {
    let profiles = state
        .core
        .list_profiles()
        .await
        .map_err(|e| player_error("list profiles", e))?;
    let with_address: Vec<(String, String)> = profiles
        .into_iter()
        .filter(|p| !p.server_address.trim().is_empty())
        .map(|p| (p.name, p.server_address.trim().to_string()))
        .collect();

    let mut addresses: Vec<String> = with_address.iter().map(|(_, addr)| addr.clone()).collect();
    addresses.sort();
    addresses.dedup();

    let pinged = join_all(addresses.into_iter().map(|addr| async move {
        let (host, port) = slp::split_address(&addr);
        let online = slp::ping(&host, port).await.map(|s| s.online);
        (addr, online)
    }))
    .await;

    let online_by_address: HashMap<String, i64> = pinged
        .into_iter()
        .filter_map(|(addr, online)| online.map(|value| (addr, value)))
        .collect();

    let mut per_build = HashMap::new();
    for (name, addr) in &with_address {
        if let Some(&online) = online_by_address.get(addr) {
            per_build.insert(name.clone(), online);
        }
    }
    let total: i64 = online_by_address.values().sum();

    Ok(PlayerCounts { per_build, total })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sync_event_fields_are_camel_case() {
        let started = serde_json::to_string(&SyncEvent::Started {
            files_total: 4432,
            bytes_total: 1104859674,
        })
        .unwrap();
        assert!(started.contains("\"event\":\"started\""), "{started}");
        assert!(started.contains("\"filesTotal\":4432"), "{started}");
        assert!(started.contains("\"bytesTotal\":1104859674"), "{started}");

        let progress = serde_json::to_string(&SyncEvent::Progress {
            stage: "downloading".into(),
            files_done: 10,
            files_total: 20,
            bytes_done: 5,
            bytes_total: 100,
            current_path: Some("mods/a.jar".into()),
        })
        .unwrap();
        for key in [
            "\"filesDone\"",
            "\"filesTotal\"",
            "\"bytesDone\"",
            "\"bytesTotal\"",
            "\"currentPath\"",
        ] {
            assert!(progress.contains(key), "missing {key} in {progress}");
        }
    }
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct UpdateDto {
    version: String,
    notes: String,
    mandatory: bool,
    size: u64,
    file_name: String,
    can_install: bool,
    blocked_reason: Option<String>,
}

#[tauri::command]
pub async fn check_update(state: State<'_, AppState>) -> Result<Option<UpdateDto>, String> {
    match state.core.check_update(env!("CARGO_PKG_VERSION")).await {
        Ok(Some(update)) => {
            tracing::info!("launcher update available: {}", update.version);
            Ok(Some(UpdateDto {
                version: update.version,
                notes: update.notes,
                mandatory: update.mandatory,
                size: update.size,
                file_name: update.file_name,
                can_install: update.can_install,
                blocked_reason: update.blocked_reason,
            }))
        }
        Ok(None) => Ok(None),
        Err(e) => {
            tracing::warn!("update check failed: {e}");
            Err(player_message(&e))
        }
    }
}

#[tauri::command]
pub async fn apply_update(
    app: AppHandle,
    state: State<'_, AppState>,
    on_event: Channel<UpdateProgress>,
) -> Result<(), String> {
    if state.game_token.lock().await.is_some() {
        return Err("Сначала закройте игру".into());
    }
    let channel = on_event.clone();
    let target = state
        .core
        .apply_update(env!("CARGO_PKG_VERSION"), move |done, total| {
            let _ = channel.send(UpdateProgress {
                bytes_done: done,
                bytes_total: total,
            });
        })
        .await
        .map_err(|e| player_error("update failed", e))?;

    tracing::info!("launcher updated, relaunching {}", target.display());
    let _ = laminara_core::process::command(&target).spawn();
    app.exit(0);
    Ok(())
}

#[derive(Serialize, Clone)]
#[serde(rename_all = "camelCase")]
pub struct UpdateProgress {
    bytes_done: u64,
    bytes_total: u64,
}
