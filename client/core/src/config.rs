use std::path::{Path, PathBuf};

use ed25519_dalek::VerifyingKey;
use serde::{Deserialize, Serialize};

use crate::error::CoreError;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct EndpointConfig {
    pub id: String,
    pub base_url: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct SelectedAccount {
    pub endpoint_id: String,
    pub uuid: String,
    pub name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct BuildSettings {
    #[serde(default)]
    pub max_memory_mb: Option<u32>,
    #[serde(default)]
    pub feature_selection: crate::features::FeatureSelection,
}

fn default_memory_mb() -> u32 {
    4096
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ClientConfig {
    pub schema_version: u32,
    pub endpoints: Vec<EndpointConfig>,
    pub server_public_key_hex: String,
    #[serde(default, skip_serializing)]
    pub trusted_public_keys_hex: Vec<String>,
    #[serde(default, skip_serializing)]
    pub hwid_salt_hex: String,
    pub install_dir: PathBuf,
    #[serde(default)]
    pub game_dir: Option<PathBuf>,
    #[serde(default)]
    pub selected_account: Option<SelectedAccount>,
    #[serde(default)]
    pub saved_accounts: Vec<SelectedAccount>,
    #[serde(default)]
    pub selected_profile: Option<String>,
    #[serde(default)]
    pub jvm_tuning: Vec<String>,
    #[serde(default = "default_memory_mb")]
    pub default_memory_mb: u32,
    #[serde(default)]
    pub build_settings: std::collections::HashMap<String, BuildSettings>,
    #[serde(default)]
    pub stale_update: Option<String>,
    #[serde(default)]
    pub game_console: bool,
}

impl ClientConfig {
    pub fn accounts(&self) -> Vec<SelectedAccount> {
        let mut accounts = Vec::new();
        for account in self
            .selected_account
            .iter()
            .chain(self.saved_accounts.iter())
        {
            if self
                .endpoints
                .iter()
                .any(|endpoint| endpoint.id == account.endpoint_id)
                && !accounts
                    .iter()
                    .any(|saved: &SelectedAccount| saved.uuid == account.uuid)
            {
                accounts.push(account.clone());
            }
        }
        accounts
    }

    pub fn load(path: &Path) -> Result<Self, CoreError> {
        let text = std::fs::read_to_string(path)
            .map_err(|e| CoreError::Config(format!("read {}: {e}", path.display())))?;
        serde_json::from_str(&text).map_err(|e| CoreError::Config(format!("parse: {e}")))
    }

    pub fn save(&self, path: &Path) -> Result<(), CoreError> {
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent).map_err(|e| CoreError::Config(e.to_string()))?;
        }
        let text =
            serde_json::to_string_pretty(self).map_err(|e| CoreError::Config(e.to_string()))?;
        let tmp = path.with_extension(format!("json.{}.tmp", std::process::id()));
        crate::privatefile::write(&tmp, text.as_bytes())
            .map_err(|e| CoreError::Config(e.to_string()))?;
        std::fs::rename(&tmp, path).map_err(|e| CoreError::Config(e.to_string()))?;
        Ok(())
    }

    fn parse_key(value: &str) -> Result<VerifyingKey, CoreError> {
        let raw = hex::decode(value.trim())
            .map_err(|e| CoreError::Config(format!("public key hex: {e}")))?;
        let bytes: [u8; 32] = raw
            .as_slice()
            .try_into()
            .map_err(|_| CoreError::Config("public key must be 32 bytes".into()))?;
        VerifyingKey::from_bytes(&bytes).map_err(|e| CoreError::Config(format!("public key: {e}")))
    }

    pub fn verifying_keys(&self) -> Result<Vec<VerifyingKey>, CoreError> {
        if self.trusted_public_keys_hex.is_empty() {
            return Ok(vec![Self::parse_key(&self.server_public_key_hex)?]);
        }
        self.trusted_public_keys_hex
            .iter()
            .map(|value| Self::parse_key(value))
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn old_config_keeps_selected_account_without_storing_credentials() {
        let cfg: ClientConfig = serde_json::from_value(serde_json::json!({
            "schemaVersion": 1, "endpoints": [{"id": "main", "baseUrl": "https://example.com"}],
            "serverPublicKeyHex": "", "installDir": "game",
            "selectedAccount": {"endpointId": "main", "uuid": "alice", "name": "Alice"}
        }))
        .unwrap();
        assert!(cfg.saved_accounts.is_empty());
        let accounts = cfg.accounts();
        assert_eq!(accounts.len(), 1);
        let value = serde_json::to_value(&accounts[0]).unwrap();
        assert_eq!(value.as_object().unwrap().len(), 3);
        assert_eq!(value["name"], "Alice");
    }

    #[test]
    fn mirrors_do_not_duplicate_accounts_and_unknown_endpoints_are_ignored() {
        let cfg: ClientConfig = serde_json::from_value(serde_json::json!({
            "schemaVersion": 1, "endpoints": [
                {"id": "main", "baseUrl": "https://example.com"},
                {"id": "mirror", "baseUrl": "https://mirror.example.com"}
            ], "serverPublicKeyHex": "", "installDir": "game",
            "selectedAccount": {"endpointId": "mirror", "uuid": "alice", "name": "NewName"},
            "savedAccounts": [
                {"endpointId": "main", "uuid": "alice", "name": "OldName"},
                {"endpointId": "main", "uuid": "bob", "name": "Bob"},
                {"endpointId": "removed", "uuid": "eve", "name": "Eve"}
            ]
        }))
        .unwrap();
        assert_eq!(cfg.accounts().len(), 2);
        assert_eq!(cfg.accounts()[0].name, "NewName");
    }
}
