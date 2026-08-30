use std::path::Path;

use base64::Engine;
use serde::{Deserialize, Serialize};

use crate::error::CoreError;
use crate::transport::Transport;

pub const MACHINE_TICKET_HEADER: &str = "Laminara-Machine-Ticket";

#[derive(Debug, Clone)]
pub struct GameSession {
    pub uuid: String,
    pub name: String,
    pub access_token: String,
    pub client_token: String,
}

#[derive(Serialize)]
struct Agent {
    name: &'static str,
    version: u32,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct AuthenticateRequest {
    agent: Agent,
    username: String,
    password: String,
    client_token: String,
    request_user: bool,
    #[serde(rename = "twoFactorCode", skip_serializing_if = "String::is_empty")]
    two_factor_code: String,
}

#[derive(Deserialize)]
struct SelectedProfile {
    id: String,
    name: String,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct AuthenticateResponse {
    access_token: String,
    client_token: String,
    selected_profile: SelectedProfile,
}

#[derive(Deserialize)]
struct YggError {
    #[serde(default)]
    error: Option<String>,
    #[serde(rename = "errorMessage")]
    error_message: Option<String>,
}

const SECOND_FACTOR_EXCEPTION: &str = "SecondFactorRequiredException";

async fn failure(response: reqwest::Response, stage: &str) -> CoreError {
    let status = response.status();
    let parsed = response
        .text()
        .await
        .ok()
        .and_then(|body| serde_json::from_str::<YggError>(&body).ok());
    let message = parsed
        .as_ref()
        .and_then(|err| err.error_message.clone())
        .unwrap_or_else(|| format!("{stage}: http {status}"));
    let code = match parsed.and_then(|err| err.error) {
        Some(kind) if kind == SECOND_FACTOR_EXCEPTION => {
            crate::error::SECOND_FACTOR_CODE.to_string()
        }
        _ => "yggdrasil".to_string(),
    };
    CoreError::App { code, message }
}

pub async fn authenticate(
    transport: &Transport,
    base_url: &str,
    username: &str,
    password: &str,
    two_factor_code: &str,
    client_token: &str,
) -> Result<GameSession, CoreError> {
    let url = format!(
        "{}/yggdrasil/authserver/authenticate",
        base_url.trim_end_matches('/')
    );
    let request = AuthenticateRequest {
        agent: Agent {
            name: "Minecraft",
            version: 1,
        },
        username: username.to_string(),
        password: password.to_string(),
        client_token: client_token.to_string(),
        request_user: false,
        two_factor_code: two_factor_code.to_string(),
    };
    let mut builder = transport.client().post(&url).json(&request);
    if let Some(ticket) = transport.machine_ticket() {
        builder = builder.header(MACHINE_TICKET_HEADER, ticket);
    }
    let response = builder
        .send()
        .await
        .map_err(|e| CoreError::Transport(e.to_string()))?;
    if !response.status().is_success() {
        return Err(failure(response, "authenticate").await);
    }
    let auth: AuthenticateResponse = response
        .json()
        .await
        .map_err(|e| CoreError::Transport(e.to_string()))?;
    Ok(GameSession {
        uuid: auth.selected_profile.id,
        name: auth.selected_profile.name,
        access_token: auth.access_token,
        client_token: auth.client_token,
    })
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct RefreshRequest {
    access_token: String,
    client_token: String,
    request_user: bool,
}

pub async fn refresh(
    transport: &Transport,
    base_url: &str,
    access_token: &str,
    client_token: &str,
) -> Result<GameSession, CoreError> {
    let url = format!(
        "{}/yggdrasil/authserver/refresh",
        base_url.trim_end_matches('/')
    );
    let request = RefreshRequest {
        access_token: access_token.to_string(),
        client_token: client_token.to_string(),
        request_user: false,
    };
    let response = transport
        .client()
        .post(&url)
        .json(&request)
        .send()
        .await
        .map_err(|e| CoreError::Transport(e.to_string()))?;
    if !response.status().is_success() {
        return Err(failure(response, "refresh").await);
    }
    let auth: AuthenticateResponse = response
        .json()
        .await
        .map_err(|e| CoreError::Transport(e.to_string()))?;
    Ok(GameSession {
        uuid: auth.selected_profile.id,
        name: auth.selected_profile.name,
        access_token: auth.access_token,
        client_token: auth.client_token,
    })
}

pub async fn prefetch(transport: &Transport, base_url: &str) -> Result<String, CoreError> {
    let url = format!("{}/yggdrasil/", base_url.trim_end_matches('/'));
    let response = transport
        .client()
        .get(&url)
        .send()
        .await
        .map_err(|e| CoreError::Transport(e.to_string()))?;
    let bytes = response
        .bytes()
        .await
        .map_err(|e| CoreError::Transport(e.to_string()))?;
    Ok(base64::engine::general_purpose::STANDARD.encode(&bytes))
}

pub fn ensure_client_token(path: &Path) -> Result<String, CoreError> {
    if let Ok(existing) = std::fs::read_to_string(path) {
        let trimmed = existing.trim();
        if !trimmed.is_empty() {
            return Ok(trimmed.to_string());
        }
    }
    let mut bytes = [0u8; 16];
    rand::RngCore::fill_bytes(&mut rand::thread_rng(), &mut bytes);
    let token = hex::encode(bytes);
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    std::fs::write(path, &token)?;
    Ok(token)
}
