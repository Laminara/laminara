use std::collections::HashMap;
use std::ffi::OsString;
use std::path::{Path, PathBuf};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use std::sync::Arc;

use futures::future::join_all;
use tokio_util::sync::CancellationToken;
use zeroize::Zeroizing;

use laminara_core::CoreError;
use laminara_core::features::FeatureSelection;
use laminara_core::gamelog::GameLine;
use laminara_core::launch::LAUNCH_PROFILE_NAME;
use laminara_core::proto::core::v1::FeatureModel;
use laminara_core::slp;
use laminara_core::sync::SyncProgress;

use crate::auth::{AuthStatus, Session};
use crate::setup::{AppState, SyncJob};
use crate::{console, format};

pub enum LoginError {
    SecondFactor { message: String },
    Failed { message: String },
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum InstallState {
    Installed,
    Outdated,
    Missing,
}

#[derive(Clone)]
pub struct BuildDto {
    pub name: String,
    pub minecraft: Option<String>,
    pub loader: Option<String>,
    pub size_bytes: u64,
    pub install: InstallState,
    pub has_features: bool,
    pub available: bool,
    pub locked: bool,
    pub lock_reason: Option<String>,
}

pub enum SyncEvent {
    Started,
    Progress(SyncProgress),
    Finished,
}

pub async fn current_endpoint(state: &AppState) -> Option<String> {
    let statuses = state.core.probe_endpoints().await;
    let current = statuses
        .iter()
        .position(|status| status.is_current)
        .unwrap_or(0);
    statuses.into_iter().nth(current).map(|status| status.id)
}

fn login_error(error: &laminara_core::CoreError, code: &str) -> LoginError {
    if let laminara_core::CoreError::App { code: kind, .. } = error
        && kind == laminara_core::error::SECOND_FACTOR_CODE
    {
        return LoginError::SecondFactor {
            message: "Введите код из приложения-аутентификатора".into(),
        };
    }
    if !code.is_empty() && message_contains(error, INVALID_CREDENTIALS) {
        return LoginError::Failed {
            message: "Неверный логин, пароль или код".into(),
        };
    }
    LoginError::Failed {
        message: player_message(error),
    }
}

const INVALID_CREDENTIALS: &str = "invalid credentials";

fn message_contains(error: &laminara_core::CoreError, sentinel: &str) -> bool {
    matches!(
        error,
        laminara_core::CoreError::App { message, .. } if message.to_lowercase().contains(sentinel)
    )
}

const LOGIN_SERVER_DOWN: &str = "Сервер входа сейчас недоступен. Попробуйте позже";
const GAME_RUNNING: &str = "Сначала закройте игру";

pub(crate) async fn refuse_while_playing(state: &AppState) -> Result<(), String> {
    if state.game.lock().await.is_some() {
        return Err(GAME_RUNNING.into());
    }
    Ok(())
}

fn player_message(error: &laminara_core::CoreError) -> String {
    use laminara_core::CoreError;
    match error {
        CoreError::App { message, .. } => {
            for (sentinel, russian) in [
                (INVALID_CREDENTIALS, "Неверный логин или пароль"),
                (
                    "too many attempts",
                    "Слишком много попыток. Подождите несколько минут",
                ),
                ("session expired", "Сессия истекла, войдите заново"),
                ("broken pipe", LOGIN_SERVER_DOWN),
                ("connection reset", LOGIN_SERVER_DOWN),
                ("connection refused", LOGIN_SERVER_DOWN),
                ("i/o timeout", LOGIN_SERVER_DOWN),
            ] {
                if message.to_lowercase().contains(sentinel) {
                    return russian.into();
                }
            }
            if message.trim().is_empty() {
                return "Не удалось выполнить запрос. Попробуйте ещё раз".into();
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
        CoreError::Cancelled => "Загрузка отменена".into(),
    }
}

pub struct Failure {
    pub message: String,
    pub retry: bool,
}

impl Failure {
    fn retryable(message: String) -> Self {
        Failure {
            message,
            retry: true,
        }
    }
}

impl From<String> for Failure {
    fn from(message: String) -> Self {
        Failure {
            message,
            retry: false,
        }
    }
}

impl From<&str> for Failure {
    fn from(message: &str) -> Self {
        Failure::from(message.to_string())
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
const FIRST_RETRY_PAUSE: Duration = Duration::from_secs(5);
const LONGEST_RETRY_PAUSE: Duration = Duration::from_secs(120);

pub(crate) fn spawn_token_keeper(state: Arc<AppState>, generation: u64) {
    tokio::spawn(async move {
        let mut retry_pause = Duration::ZERO;
        let mut failures = 0u32;
        loop {
            let wait = {
                if state.auth.generation() != generation {
                    return;
                }
                let Some(expiry) = state.auth.access_expiry() else {
                    return;
                };
                (expiry - now_unix_nanos() - TOKEN_REFRESH_LEAD).max(1_000_000_000) as u64
            };
            if retry_pause.is_zero() {
                tokio::time::sleep(Duration::from_nanos(wait)).await;
            } else {
                tokio::time::sleep(retry_pause).await;
            }

            let _operation = state.auth.operation.lock().await;
            if state.auth.generation() != generation {
                return;
            }
            let Some((endpoint_id, uuid, _)) = state.auth.identity() else {
                return;
            };
            let Some(refresh) = state.auth.load_refresh(&endpoint_id, &uuid).await else {
                return;
            };
            match state.core.refresh(&refresh).await {
                Ok(tokens) => {
                    state
                        .auth
                        .keep_refresh(&endpoint_id, &uuid, &tokens.refresh)
                        .await;
                    state
                        .auth
                        .update_access(tokens.access, tokens.access_expires_unix_nanos);
                    retry_pause = Duration::ZERO;
                    failures = 0;
                }
                Err(e) if e.server_unreachable() => {
                    failures += 1;
                    retry_pause = laminara_core::backoff::pause(
                        failures,
                        FIRST_RETRY_PAUSE,
                        LONGEST_RETRY_PAUSE,
                    );
                    tracing::warn!(
                        "сервер не ответил на продление, повтор через {retry_pause:?}: {e}"
                    );
                }
                Err(e) => {
                    tracing::warn!("token keeper stopping: {e}");
                    return;
                }
            }
        }
    });
}

pub async fn login(
    state: &Arc<AppState>,
    username: String,
    password: String,
    code: String,
) -> Result<(), LoginError> {
    let _operation = state.auth.operation.lock().await;
    crate::accounts::require_idle(state)
        .await
        .map_err(|message| LoginError::Failed { message })?;
    let policy = crate::accounts::policy(state)
        .await
        .map_err(|message| LoginError::Failed { message })?;
    if !policy.multi_account && state.auth.status().signed_in {
        return Err(LoginError::Failed {
            message: "Сначала выйдите из текущего аккаунта".into(),
        });
    }
    let change = crate::auth::CredentialChange::new(&state.core);
    state.core.sign_out();
    let result = state
        .core
        .login(&username, &password, &code)
        .await
        .map_err(|e| {
            tracing::error!("login failed for {username}: {e:?}");
            login_error(&e, &code)
        })?;
    tracing::info!("signed in as {}", result.account.name);
    let endpoint_id = result.account.endpoint_id.clone();
    let uuid = result.account.uuid.clone();
    let name = result.account.name.clone();

    crate::accounts::check_capacity(&policy, &state.core.saved_accounts(), &uuid)
        .map_err(|message| LoginError::Failed { message })?;

    state
        .auth
        .store_refresh(&endpoint_id, &uuid, &result.tokens.refresh)
        .await
        .map_err(|message| LoginError::Failed {
            message: format!("Не удалось сохранить вход: {message}"),
        })?;
    state
        .auth
        .store_game(
            &endpoint_id,
            &uuid,
            &result.session.access_token,
            &result.session.client_token,
        )
        .await
        .map_err(|message| LoginError::Failed {
            message: format!("Не удалось сохранить вход в игру: {message}"),
        })?;
    state
        .core
        .remember_account(laminara_core::config::SelectedAccount {
            endpoint_id,
            uuid,
            name,
        })
        .map_err(|error| LoginError::Failed {
            message: player_error("remember account", error),
        })?;

    let generation = state.auth.set_session(Session {
        account: result.account,
        access: Zeroizing::new(result.tokens.access),
        access_expires_unix_nanos: result.tokens.access_expires_unix_nanos,
        game: result.session,
    });
    change.commit();
    *state.face.lock().await = None;
    console::lock(&state.game_log).begin("");
    spawn_token_keeper(state.clone(), generation);
    Ok(())
}

pub async fn logout(state: &AppState) -> Result<(), String> {
    let _operation = state.auth.operation.lock().await;
    crate::accounts::require_idle(state).await?;
    if let Some((_, uuid, _)) = state.auth.identity() {
        crate::accounts::forget(state, &uuid).await?;
    }
    state.auth.clear_session();
    state.core.sign_out();
    console::lock(&state.game_log).begin("");
    Ok(())
}

pub async fn restore_session(state: &Arc<AppState>) -> Result<AuthStatus, String> {
    let _operation = state.auth.operation.lock().await;
    if state.auth.status().signed_in {
        return Ok(state.auth.status());
    }
    let Some(account) = state.core.config().selected_account.clone() else {
        return Ok(state.auth.status());
    };
    restore_account(state, account, false).await
}

pub(crate) async fn restore_account(
    state: &Arc<AppState>,
    account: laminara_core::config::SelectedAccount,
    strict: bool,
) -> Result<AuthStatus, String> {
    let change = crate::auth::CredentialChange::new(&state.core);
    state.core.sign_out();
    let Some((access, client)) = state
        .auth
        .load_game(&account.endpoint_id, &account.uuid)
        .await
    else {
        return Err("Сохранённый вход недоступен. Введите логин и пароль заново.".into());
    };
    let Some(base) = state.core.base_url_for(&account.endpoint_id) else {
        return Err("Сервер сохранённого аккаунта больше не настроен".into());
    };

    let mut launcher_access = String::new();
    let mut launcher_access_expires = 0i64;
    if let Some(refresh) = state
        .auth
        .load_refresh(&account.endpoint_id, &account.uuid)
        .await
    {
        match state.core.refresh(&refresh).await {
            Ok(tokens) => {
                state
                    .auth
                    .store_refresh(&account.endpoint_id, &account.uuid, &tokens.refresh)
                    .await
                    .map_err(|error| {
                        tracing::warn!("restored refresh token save failed: {error}");
                        "Не удалось сохранить продлённый вход в хранилище системы".to_string()
                    })?;
                launcher_access = tokens.access;
                launcher_access_expires = tokens.access_expires_unix_nanos;
            }
            Err(e) if !strict && e.server_unreachable() => {
                tracing::warn!("launcher session refresh unavailable: {e}")
            }
            Err(e) => return Err(player_error("launcher session refresh", e)),
        }
    }
    if strict && launcher_access.is_empty() {
        return Err("Сессия истекла. Введите логин и пароль заново.".into());
    }

    if let Err(e) = state.core.report_machine().await {
        if strict || !e.server_unreachable() {
            tracing::warn!("machine check failed during restore: {e:?}");
            return Err(player_error("restore machine", e));
        }
        tracing::warn!("сервер не ответил на проверку машины, вход сохраняю: {e}");
    }

    match state.core.refresh_session(&base, &access, &client).await {
        Ok(session) => {
            if session.uuid != account.uuid {
                return Err("Сервер вернул другой аккаунт. Войдите заново.".into());
            }
            tracing::info!("session restored for {}", session.name);
            state
                .auth
                .store_game(
                    &account.endpoint_id,
                    &session.uuid,
                    &session.access_token,
                    &session.client_token,
                )
                .await
                .map_err(|error| {
                    tracing::warn!("restored game token save failed: {error}");
                    "Не удалось сохранить игровой вход в хранилище системы".to_string()
                })?;
            state
                .core
                .remember_account(laminara_core::config::SelectedAccount {
                    endpoint_id: account.endpoint_id.clone(),
                    uuid: session.uuid.clone(),
                    name: session.name.clone(),
                })
                .map_err(|error| player_error("select account", error))?;
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
            spawn_token_keeper(state.clone(), generation);
        }
        Err(e) if !strict && e.server_unreachable() => {
            tracing::warn!("сервер не отвечает, вход восстановлен из сохранённого: {e}");
            let generation = state.auth.set_session(Session {
                account: laminara_core::Account {
                    uuid: account.uuid.clone(),
                    name: account.name.clone(),
                    endpoint_id: account.endpoint_id.clone(),
                },
                access: Zeroizing::new(launcher_access),
                access_expires_unix_nanos: launcher_access_expires,
                game: laminara_core::account::GameSession {
                    uuid: account.uuid.clone(),
                    name: account.name.clone(),
                    access_token: access,
                    client_token: client,
                },
            });
            spawn_token_keeper(state.clone(), generation);
        }
        Err(e) => return Err(player_error("session restore", e)),
    }
    change.commit();
    *state.face.lock().await = None;
    console::lock(&state.game_log).begin("");
    Ok(state.auth.status())
}

pub struct NewsItemDto {
    pub id: String,
    pub title: String,
    pub body: String,
    pub published_at_unix_nanos: i64,
    pub tag: Option<String>,
    pub link: Option<String>,
    pub banner_data_uri: Option<String>,
}

pub async fn news(state: &AppState) -> Result<Vec<NewsItemDto>, String> {
    let items = match state.core.news().await {
        Ok(items) => items,
        Err(e) => {
            tracing::warn!("news fetch failed: {e}");
            Vec::new()
        }
    };
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

pub async fn open_external(url: String) -> Result<(), String> {
    let lower = url.to_lowercase();
    if !lower.starts_with("http://") && !lower.starts_with("https://") {
        return Err("only http and https links can be opened".into());
    }
    open_in_system(Opened::Link(&url)).map_err(|e| {
        tracing::error!("open external link: {e}");
        "Не удалось открыть ссылку в браузере".to_string()
    })
}

enum Opened<'a> {
    Link(&'a str),
    Folder(&'a Path),
}

fn open_in_system(target: Opened<'_>) -> std::io::Result<()> {
    #[cfg(target_os = "linux")]
    let (program, prefix) = ("xdg-open", None::<&str>);
    #[cfg(target_os = "macos")]
    let (program, prefix) = ("/usr/bin/open", None::<&str>);
    #[cfg(target_os = "windows")]
    let (program, prefix) = match target {
        Opened::Link(_) => ("rundll32.exe", Some("url.dll,FileProtocolHandler")),
        Opened::Folder(_) => ("explorer.exe", None),
    };
    let argument: OsString = match target {
        Opened::Link(url) => url.into(),
        Opened::Folder(path) => path.into(),
    };

    laminara_core::process::async_command(program)
        .args(prefix)
        .arg(argument)
        .spawn()
        .map(|_| ())
}

pub async fn list_builds(state: &AppState) -> Result<Vec<BuildDto>, String> {
    let _operation = state.auth.operation.lock().await;
    let profiles = state
        .core
        .list_profiles()
        .await
        .map_err(|e| player_error("list profiles", e))?;
    let mine = laminara_core::platform::current();
    let build_dirs: Vec<PathBuf> = profiles
        .iter()
        .map(|profile| state.core.profile_dir(&profile.name))
        .collect();
    let installed: Vec<bool> = crate::auth::offload(move || {
        Ok(build_dirs
            .iter()
            .map(|dir| {
                dir.join(LAUNCH_PROFILE_NAME).exists()
                    && !laminara_core::sync::install_unfinished(&dir.join(".laminara"))
            })
            .collect())
    })
    .await?;

    Ok(profiles
        .into_iter()
        .zip(installed)
        .map(|(profile, installed)| BuildDto {
            name: profile.name,
            minecraft: if profile.minecraft_version.trim().is_empty() {
                None
            } else {
                Some(profile.minecraft_version)
            },
            loader: if profile.loader.trim().is_empty() {
                None
            } else {
                Some(profile.loader)
            },
            size_bytes: profile.total_size,
            install: if installed {
                InstallState::Installed
            } else {
                InstallState::Missing
            },
            has_features: profile.has_features,
            available: profile.platforms.is_empty() || profile.platforms.contains(&(mine as i32)),
            locked: profile.locked,
            lock_reason: if profile.lock_reason.trim().is_empty() {
                None
            } else {
                Some(profile.lock_reason)
            },
        })
        .collect())
}

pub async fn sync_profile(
    state: &AppState,
    profile: String,
    on_event: impl Fn(SyncEvent) + Send + Sync + 'static,
) -> Result<(), Failure> {
    let operation = state.auth.operation.lock().await;
    let cancel = CancellationToken::new();
    let _slot = claim_sync(state, &profile, &cancel)?;
    drop(operation);
    tracing::info!("sync started for {profile}");
    on_event(SyncEvent::Started);

    let on_event = Arc::new(on_event);
    match run_sync(state, &profile, on_event.clone(), cancel).await {
        Ok(outcome) => {
            tracing::info!(
                "sync finished for {profile}: downloaded={} linked={} skipped={} pruned={}",
                outcome.downloaded,
                outcome.linked,
                outcome.skipped,
                outcome.pruned
            );
            on_event(SyncEvent::Finished);
            Ok(())
        }
        Err(err) => {
            let failure = match err {
                CoreError::Cancelled => {
                    tracing::info!("sync of {profile} cancelled by the player");
                    Failure::from(player_message(&err))
                }
                err => {
                    tracing::error!("sync failed for {profile}: {err}");
                    sync_failure(&err)
                }
            };
            Err(failure)
        }
    }
}

fn sync_failure(error: &CoreError) -> Failure {
    if error.server_unreachable() {
        Failure::retryable(
            "Загрузка прервалась: сервер не отвечает или пропала связь. Нажмите «Повторить», скачанное не потеряется"
                .into(),
        )
    } else {
        Failure::from(player_message(error))
    }
}

struct SyncSlot<'a> {
    state: &'a AppState,
    id: u64,
}

impl Drop for SyncSlot<'_> {
    fn drop(&mut self) {
        let mut slot = self
            .state
            .sync_job
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if slot.as_ref().is_some_and(|job| job.id == self.id) {
            *slot = None;
        }
    }
}

fn claim_sync<'a>(
    state: &'a AppState,
    build: &str,
    token: &CancellationToken,
) -> Result<SyncSlot<'a>, Failure> {
    let mut slot = state
        .sync_job
        .lock()
        .unwrap_or_else(std::sync::PoisonError::into_inner);
    if let Some(job) = slot.as_ref() {
        return Err(Failure::from(format!(
            "Уже идёт загрузка сборки «{}»",
            job.build
        )));
    }
    let id = state
        .sync_jobs_started
        .fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    *slot = Some(SyncJob {
        id,
        build: build.to_string(),
        token: token.clone(),
    });
    Ok(SyncSlot { state, id })
}

pub(crate) fn refuse_while_syncing(state: &AppState) -> Result<(), String> {
    let slot = state
        .sync_job
        .lock()
        .unwrap_or_else(std::sync::PoisonError::into_inner);
    match slot.as_ref() {
        Some(job) => Err(format!(
            "Идёт загрузка сборки «{}», дождитесь её конца",
            job.build
        )),
        None => Ok(()),
    }
}

async fn run_sync(
    state: &AppState,
    profile: &str,
    on_event: Arc<impl Fn(SyncEvent) + Send + Sync + 'static>,
    cancel: CancellationToken,
) -> Result<laminara_core::sync::SyncOutcome, laminara_core::CoreError> {
    let manifest = tokio::select! {
        _ = cancel.cancelled() => return Err(CoreError::Cancelled),
        manifest = state.core.verified_manifest(profile) => manifest?,
    };
    state
        .core
        .sync_profile(profile, &manifest, cancel, move |progress: SyncProgress| {
            on_event(SyncEvent::Progress(progress))
        })
        .await
}

pub async fn repair_build(state: &AppState, profile: String) -> Result<u64, String> {
    let _operation = state.auth.operation.lock().await;
    refuse_while_syncing(state)?;
    refuse_while_playing(state).await?;
    tracing::info!("checking every file of {profile}");
    let discarded = state
        .core
        .discard_broken(&profile, true)
        .await
        .map_err(|e| player_error(&format!("checking files of {profile}"), e))?;
    tracing::info!("{profile}: discarded {discarded} file(s) that no longer match the manifest");
    Ok(discarded)
}

pub fn cancel_sync(state: &AppState) {
    let slot = state
        .sync_job
        .lock()
        .unwrap_or_else(std::sync::PoisonError::into_inner);
    if let Some(job) = slot.as_ref() {
        job.token.cancel();
    }
}

pub async fn launch(state: &Arc<AppState>, profile: String) -> Result<u64, Failure> {
    let _operation = state.auth.operation.lock().await;
    let mut running = state.game.lock().await;
    if running.is_some() {
        return Err("Игра уже запущена".into());
    }
    let game = state
        .auth
        .game_session()
        .ok_or("Сессия истекла — войдите заново")?;
    let authlib = state.authlib_jar.clone();

    state
        .core
        .report_machine()
        .await
        .map_err(|e| player_error("report machine", e))?;

    let broken = state
        .core
        .verify_installed(&profile)
        .await
        .map_err(|e| player_error(&format!("integrity check failed for {profile}"), e))?;
    if !broken.is_empty() {
        tracing::error!(
            "integrity check failed for {profile}: {} file(s), first: {:?}",
            broken.len(),
            broken.first()
        );
        let discarded = state
            .core
            .discard_broken(&profile, false)
            .await
            .map_err(|e| player_error(&format!("discarding broken files of {profile}"), e))?;
        tracing::info!("discarded {discarded} broken file(s) of {profile}");
        return Err(Failure::retryable(format!(
            "Файлы сборки повреждены ({}) и удалены. Нажмите «Повторить», чтобы скачать их заново",
            broken.len()
        )));
    }
    tracing::info!("launching {profile}");

    let child = state
        .core
        .launch(&profile, &game, &authlib, env!("LAMINARA_VERSION"))
        .await
        .map_err(|e| player_error(&format!("launch failed for {profile}"), e))?;

    let game = console::watch(state.clone(), &profile, child);
    let session = game.session;
    *running = Some(game);
    drop(running);

    if state.core.game_console() {
        crate::ui::open_game_console();
    }
    Ok(session)
}

pub fn game_log_tail(state: &AppState, lines: usize) -> Vec<GameLine> {
    console::lock(&state.game_log).tail(lines)
}

pub async fn player_face(state: &AppState) -> Result<Option<String>, String> {
    let _operation = state.auth.operation.lock().await;
    let Some(game) = state.auth.game_session() else {
        return Ok(None);
    };
    let skin = match state.core.player_skin(&game.uuid).await {
        Ok(Some(url)) => url,
        Ok(None) => return Ok(None),
        Err(error) => {
            tracing::warn!(%error, "профиль игрока не загрузился, в шапке остаётся прежнее лицо");
            return Err(player_message(&error));
        }
    };
    let mut cached = state.face.lock().await;
    if let Some((url, face)) = cached.as_ref()
        && *url == skin
    {
        return Ok(Some(face.clone()));
    }
    match state.core.player_face(&skin).await {
        Ok(face) => {
            *cached = Some((skin, face.clone()));
            Ok(Some(face))
        }
        Err(error) => {
            tracing::warn!(%error, %skin, "лицо скина не собралось, в шапке остаётся прежнее");
            Err(player_message(&error))
        }
    }
}

pub async fn send_launcher_log(state: &AppState) -> Result<String, String> {
    let path = crate::logging::log_file(&state.log_dir);
    let log = crate::logging::tail(&path, crate::setup::REPORT_LOG_BYTES).map_err(|error| {
        tracing::warn!(%error, path = %path.display(), "журнал лаунчера не прочитался");
        "Журнал лаунчера не прочитался".to_string()
    })?;
    state
        .core
        .report_launcher_log(log)
        .await
        .map_err(|e| player_error("launcher log report", e))
}

pub async fn open_launcher_logs(state: &AppState) -> Result<(), String> {
    open_in_system(Opened::Folder(&state.log_dir)).map_err(|error| {
        tracing::error!(%error, "папка журнала лаунчера не открылась");
        "Не удалось открыть папку с журналом".to_string()
    })
}

pub async fn open_game_logs(state: &AppState) -> Result<(), String> {
    let build = console::lock(&state.game_log).build().to_string();
    if build.is_empty() {
        return Err("Игра ещё не запускалась".into());
    }
    let game_dir = state.core.game_dir(&build);
    let logs = game_dir.join("logs");
    let folder = if logs.is_dir() { logs } else { game_dir };
    open_in_system(Opened::Folder(&folder)).map_err(|error| {
        tracing::error!(%error, folder = %folder.display(), "папка журналов не открылась");
        "Не удалось открыть папку с журналами".to_string()
    })
}

pub async fn stop(state: &AppState) -> Result<(), String> {
    if let Some(game) = state.game.lock().await.as_ref() {
        game.token.cancel();
    }
    Ok(())
}

pub struct GeneralSettings {
    pub install_dir: String,
    pub game_console: bool,
}

pub struct BuildSettingsDto {
    pub max_memory_mb: Option<u32>,
    pub default_memory_mb: u32,
    pub allowed_memory_mb: u32,
}

pub fn general_settings(state: &AppState) -> GeneralSettings {
    GeneralSettings {
        install_dir: state.core.install_dir().to_string_lossy().into_owned(),
        game_console: state.core.game_console(),
    }
}

pub async fn collect_garbage(state: &AppState) -> Result<u64, String> {
    refuse_while_syncing(state)?;
    let removed = state
        .core
        .collect_garbage()
        .await
        .map_err(|e| player_error("collect garbage", e))?;
    tracing::info!("cas gc removed {removed} object(s)");
    Ok(removed)
}

pub fn set_game_console(state: &AppState, enabled: bool) -> Result<(), String> {
    state
        .core
        .set_game_console(enabled)
        .map_err(|e| player_error("set game console", e))
}

pub async fn set_install_dir(state: &AppState, path: String) -> Result<(), String> {
    state
        .core
        .set_install_dir(path.into())
        .map_err(|e| player_error("set install dir", e))
}

pub fn build_settings(state: &AppState, profile: String) -> BuildSettingsDto {
    BuildSettingsDto {
        max_memory_mb: state.core.build_settings(&profile).max_memory_mb,
        default_memory_mb: state.core.default_memory_mb(),
        allowed_memory_mb: memory_ceiling(),
    }
}

fn memory_ceiling() -> u32 {
    laminara_core::hostmem::allowed_mb().unwrap_or(16384)
}

pub async fn set_build_memory(
    state: &AppState,
    profile: String,
    max_memory_mb: Option<u32>,
) -> Result<(), String> {
    if let Some(asked) = max_memory_mb {
        let ceiling = memory_ceiling();
        if asked > ceiling {
            return Err(format!(
                "Игре можно отдать не больше {} — это 80% оперативной памяти этого компьютера",
                format::memory(ceiling)
            ));
        }
    }
    state
        .core
        .set_build_memory(&profile, max_memory_mb)
        .map_err(|e| player_error("set build memory", e))
}

pub struct BuildFeatures {
    pub model: Option<FeatureModel>,
    pub selection: FeatureSelection,
}

pub async fn build_features(state: &AppState, profile: String) -> Result<BuildFeatures, String> {
    let _operation = state.auth.operation.lock().await;
    let manifest = state
        .core
        .verified_manifest(&profile)
        .await
        .map_err(|e| player_error("verify manifest", e))?;
    Ok(BuildFeatures {
        model: manifest.manifest.features.clone(),
        selection: state.core.build_settings(&profile).feature_selection,
    })
}

pub async fn set_build_features(
    state: &AppState,
    profile: String,
    selection: FeatureSelection,
) -> Result<(), String> {
    state
        .core
        .set_feature_selection(&profile, selection)
        .map_err(|e| player_error("set feature selection", e))
}

#[derive(Clone, Copy, Default)]
pub struct ServerPlayers {
    pub online: i64,
    pub max: i64,
}

pub struct PlayerCounts {
    pub per_build: HashMap<String, ServerPlayers>,
    pub total: ServerPlayers,
}

pub async fn player_counts(state: &AppState) -> Result<Option<PlayerCounts>, String> {
    let operation = state.auth.operation.lock().await;
    let profiles = state
        .core
        .list_profiles()
        .await
        .map_err(|e| player_error("list profiles", e))?;
    drop(operation);
    let with_address: Vec<(String, String)> = profiles
        .into_iter()
        .filter(|p| !p.server_address.trim().is_empty())
        .map(|p| (p.name, p.server_address.trim().to_string()))
        .collect();

    if with_address.is_empty() {
        return Ok(None);
    }

    let mut addresses: Vec<String> = with_address.iter().map(|(_, addr)| addr.clone()).collect();
    addresses.sort();
    addresses.dedup();

    let pinged = join_all(addresses.into_iter().map(|addr| async move {
        let status = slp::ping(&slp::split_address(&addr).0, slp::split_address(&addr).1).await;
        (addr, status)
    }))
    .await;

    let by_address: HashMap<String, ServerPlayers> = pinged
        .into_iter()
        .filter_map(|(addr, status)| {
            status.map(|s| {
                (
                    addr,
                    ServerPlayers {
                        online: s.online,
                        max: s.max,
                    },
                )
            })
        })
        .collect();

    if by_address.is_empty() {
        return Ok(None);
    }

    let mut per_build = HashMap::new();
    for (name, addr) in &with_address {
        if let Some(&players) = by_address.get(addr) {
            per_build.insert(name.clone(), players);
        }
    }
    let total = by_address
        .values()
        .fold(ServerPlayers::default(), |sum, players| ServerPlayers {
            online: sum.online + players.online,
            max: sum.max + players.max,
        });

    Ok(Some(PlayerCounts { per_build, total }))
}

#[derive(Clone)]
pub struct UpdateDto {
    pub version: String,
    pub mandatory: bool,
    pub size: u64,
    pub can_install: bool,
    pub blocked_reason: Option<String>,
}

const UPDATED_TO: &str = "LAMINARA_UPDATED_TO";

pub async fn check_update(state: &AppState) -> Result<Option<UpdateDto>, String> {
    match state.core.check_update(env!("LAMINARA_VERSION")).await {
        Ok(Some(update)) => {
            let just_tried = std::env::var(UPDATED_TO).as_deref() == Ok(update.version.as_str());
            if just_tried || state.core.stale_update().as_deref() == Some(update.version.as_str()) {
                if just_tried {
                    tracing::warn!(
                        "the update to {} left the launcher on {}, not offering it again",
                        update.version,
                        env!("LAMINARA_VERSION")
                    );
                    let _ = state.core.remember_stale_update(&update.version);
                }
                return Ok(None);
            }
            tracing::info!("launcher update available: {}", update.version);
            Ok(Some(UpdateDto {
                version: update.version,
                mandatory: update.mandatory,
                size: update.size,
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

pub async fn apply_update(
    state: &AppState,
    version: String,
    on_progress: impl Fn(u64, u64) + Send + Sync + 'static,
) -> Result<(), String> {
    refuse_while_playing(state).await?;
    let target = state
        .core
        .apply_update(env!("LAMINARA_VERSION"), on_progress)
        .await
        .map_err(|e| player_error("update failed", e))?;

    tracing::info!("launcher updated, relaunching {}", target.display());
    let _ = laminara_core::process::command(&target)
        .env(UPDATED_TO, &version)
        .spawn();
    crate::ui::quit();
    Ok(())
}

pub async fn report_crash(
    state: &AppState,
    build: String,
    build_version: String,
    loader: String,
    exit_code: i32,
    session: u64,
) -> Result<String, String> {
    let _operation = state.auth.operation.lock().await;
    let log = {
        let log = console::lock(&state.game_log);
        if log.session() != session {
            return Err("Журнал этой игры уже сменился новым запуском".into());
        }
        log.tail_text(crate::setup::REPORT_LOG_BYTES)
    };
    state
        .core
        .report_crash(build, build_version, loader, exit_code, log)
        .await
        .map_err(|err| player_error("crash report", err))
}
