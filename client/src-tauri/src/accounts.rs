use laminara_core::config::SelectedAccount;
use laminara_core::proto::api::v1::AccountPolicy;
use serde::Serialize;
use tauri::{AppHandle, State};

use crate::auth::AuthStatus;
use crate::AppState;

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AccountOptions {
    multi_account: bool,
    max_accounts: u32,
    accounts: Vec<SelectedAccount>,
}

pub(crate) fn check_capacity(
    policy: &AccountPolicy,
    accounts: &[SelectedAccount],
    uuid: &str,
) -> Result<(), String> {
    if policy.multi_account
        && accounts.len() >= policy.max_accounts as usize
        && !accounts.iter().any(|account| account.uuid == uuid)
    {
        return Err(format!(
            "Можно сохранить не больше {} аккаунтов. Удалите один из списка.",
            policy.max_accounts
        ));
    }
    Ok(())
}

pub(crate) async fn policy(state: &AppState) -> Result<AccountPolicy, String> {
    let mut policy = state.core.account_policy().await.map_err(|_| {
        "Не удалось получить настройки аккаунтов с сервера. Повторите позже.".to_string()
    })?;
    policy.max_accounts = if policy.multi_account {
        policy.max_accounts.clamp(1, 20)
    } else {
        1
    };
    Ok(policy)
}

pub(crate) async fn require_idle(state: &AppState) -> Result<(), String> {
    if state.sync_job.lock().unwrap().is_some() {
        return Err("Дождитесь завершения загрузки или отмените её".into());
    }
    if state.game.lock().await.is_some() {
        return Err("Сначала закройте игру".into());
    }
    Ok(())
}

pub(crate) async fn forget(state: &AppState, uuid: &str) -> Result<(), String> {
    for endpoint in &state.core.config().endpoints {
        state
            .auth
            .clear_account(&endpoint.id, uuid)
            .await
            .map_err(|error| {
                tracing::warn!("account secret removal failed: {error}");
                "Не удалось удалить сохранённый вход из хранилища системы. Повторите позже."
                    .to_string()
            })?;
    }
    state
        .core
        .forget_account(uuid)
        .map_err(|_| "Не удалось сохранить список аккаунтов".to_string())
}

#[tauri::command]
pub async fn account_options(state: State<'_, AppState>) -> Result<AccountOptions, String> {
    let _operation = state.auth.operation.lock().await;
    let policy = policy(&state).await?;
    Ok(AccountOptions {
        multi_account: policy.multi_account,
        max_accounts: policy.max_accounts,
        accounts: state.core.saved_accounts(),
    })
}

#[tauri::command]
pub async fn switch_account(
    app: AppHandle,
    state: State<'_, AppState>,
    uuid: String,
) -> Result<AuthStatus, String> {
    let _operation = state.auth.operation.lock().await;
    require_idle(&state).await?;
    if !policy(&state).await?.multi_account {
        return Err("Сохранение нескольких аккаунтов отключено на сервере".into());
    }
    if state.auth.status().uuid.as_deref() == Some(&uuid) {
        return Ok(state.auth.status());
    }
    let account = state
        .core
        .saved_accounts()
        .into_iter()
        .find(|account| account.uuid == uuid)
        .ok_or("Аккаунт больше не сохранён")?;
    crate::commands::restore_account(app, &state, account, true).await
}

#[tauri::command]
pub async fn remove_account(
    state: State<'_, AppState>,
    uuid: String,
) -> Result<AuthStatus, String> {
    let _operation = state.auth.operation.lock().await;
    require_idle(&state).await?;
    let _account = state
        .core
        .saved_accounts()
        .into_iter()
        .find(|account| account.uuid == uuid)
        .ok_or("Аккаунт больше не сохранён")?;
    forget(&state, &uuid).await?;
    if state.auth.status().uuid.as_deref() == Some(&uuid) {
        state.auth.clear_session();
        state.core.sign_out();
        *state.face.lock().await = None;
        crate::console::lock(&state.game_log).begin("");
    }
    Ok(state.auth.status())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn lowering_limit_blocks_new_accounts_but_allows_reauthentication() {
        let accounts = ["alice", "bob"].map(|uuid| SelectedAccount {
            endpoint_id: "main".into(),
            uuid: uuid.into(),
            name: uuid.into(),
        });
        for limit in [1, 2] {
            let policy = AccountPolicy {
                multi_account: true,
                max_accounts: limit,
            };
            assert!(check_capacity(&policy, &accounts, "alice").is_ok());
            assert!(check_capacity(&policy, &accounts, "bob").is_ok());
            assert!(check_capacity(&policy, &accounts, "eve").is_err());
        }
        assert!(check_capacity(
            &AccountPolicy {
                multi_account: true,
                max_accounts: 3
            },
            &accounts,
            "eve"
        )
        .is_ok());
    }
}
