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

#[derive(Debug, Clone, Serialize, Deserialize)]
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
    pub selected_profile: Option<String>,
    #[serde(default)]
    pub jvm_tuning: Vec<String>,
    #[serde(default = "default_memory_mb")]
    pub default_memory_mb: u32,
    #[serde(default)]
    pub build_settings: std::collections::HashMap<String, BuildSettings>,
}

impl ClientConfig {
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
        let tmp = path.with_extension("json.tmp");
        std::fs::write(&tmp, text).map_err(|e| CoreError::Config(e.to_string()))?;
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
