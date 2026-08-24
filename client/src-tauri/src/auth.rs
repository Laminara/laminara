use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Mutex;

use keyring::Entry;
use laminara_core::account::GameSession;
use laminara_core::state::Account;
use laminara_core::secrets::blocking;
use laminara_core::KEYRING_SERVICE as SERVICE;
use serde::Serialize;
use zeroize::Zeroizing;

pub struct Session {
    pub account: Account,
    pub access: Zeroizing<String>,
    pub access_expires_unix_nanos: i64,
    pub game: GameSession,
}

#[derive(Default)]
pub struct AuthManager {
    session: Mutex<Option<Session>>,
    generation: AtomicU64,
}

#[derive(Serialize, Clone)]
#[serde(rename_all = "camelCase")]
pub struct AuthStatus {
    pub signed_in: bool,
    pub username: Option<String>,
    pub uuid: Option<String>,
}

impl AuthManager {
    fn entry(endpoint_id: &str, uuid: &str) -> Result<Entry, String> {
        Entry::new(SERVICE, &format!("{endpoint_id}:{uuid}")).map_err(|e| e.to_string())
    }

    pub fn store_refresh(
        &self,
        endpoint_id: &str,
        uuid: &str,
        refresh: &str,
    ) -> Result<(), String> {
        let entry = Self::entry(endpoint_id, uuid)?;
        blocking(|| entry.set_password(refresh).map_err(|e| e.to_string()))
    }

    pub fn load_refresh(&self, endpoint_id: &str, uuid: &str) -> Option<String> {
        let entry = Self::entry(endpoint_id, uuid).ok()?;
        blocking(|| entry.get_password().ok())
    }

    pub fn clear_refresh(&self, endpoint_id: &str, uuid: &str) {
        if let Ok(entry) = Self::entry(endpoint_id, uuid) {
            blocking(|| {
                let _ = entry.delete_credential();
            });
        }
    }

    fn game_entry(endpoint_id: &str, uuid: &str) -> Result<Entry, String> {
        Entry::new(SERVICE, &format!("{endpoint_id}:{uuid}:yggdrasil")).map_err(|e| e.to_string())
    }

    pub fn store_game(
        &self,
        endpoint_id: &str,
        uuid: &str,
        access: &str,
        client: &str,
    ) -> Result<(), String> {
        let entry = Self::game_entry(endpoint_id, uuid)?;
        let secret = format!("{access}\n{client}");
        blocking(|| entry.set_password(&secret).map_err(|e| e.to_string()))
    }

    pub fn load_game(&self, endpoint_id: &str, uuid: &str) -> Option<(String, String)> {
        let entry = Self::game_entry(endpoint_id, uuid).ok()?;
        let raw = blocking(|| entry.get_password().ok())?;
        let (access, client) = raw.split_once('\n')?;
        Some((access.to_string(), client.to_string()))
    }

    pub fn clear_game(&self, endpoint_id: &str, uuid: &str) {
        if let Ok(entry) = Self::game_entry(endpoint_id, uuid) {
            blocking(|| {
                let _ = entry.delete_credential();
            });
        }
    }

    pub fn set_session(&self, session: Session) -> u64 {
        *self.session.lock().unwrap() = Some(session);
        self.generation.fetch_add(1, Ordering::SeqCst) + 1
    }

    pub fn clear_session(&self) {
        *self.session.lock().unwrap() = None;
        self.generation.fetch_add(1, Ordering::SeqCst);
    }

    pub fn generation(&self) -> u64 {
        self.generation.load(Ordering::SeqCst)
    }

    pub fn access_expiry(&self) -> Option<i64> {
        self.session
            .lock()
            .unwrap()
            .as_ref()
            .map(|s| s.access_expires_unix_nanos)
    }

    pub fn update_access(&self, token: String, expires_unix_nanos: i64) {
        if let Some(session) = self.session.lock().unwrap().as_mut() {
            session.access = Zeroizing::new(token);
            session.access_expires_unix_nanos = expires_unix_nanos;
        }
    }

    pub fn status(&self) -> AuthStatus {
        match self.session.lock().unwrap().as_ref() {
            Some(session) => AuthStatus {
                signed_in: true,
                username: Some(session.account.name.clone()),
                uuid: Some(session.account.uuid.clone()),
            },
            None => AuthStatus {
                signed_in: false,
                username: None,
                uuid: None,
            },
        }
    }

    pub fn game_session(&self) -> Option<GameSession> {
        self.session
            .lock()
            .unwrap()
            .as_ref()
            .map(|s| s.game.clone())
    }

    pub fn identity(&self) -> Option<(String, String, String)> {
        self.session.lock().unwrap().as_ref().map(|s| {
            (
                s.account.endpoint_id.clone(),
                s.account.uuid.clone(),
                s.account.name.clone(),
            )
        })
    }
}
