use std::path::Path;

use serde::Deserialize;

use crate::error::CoreError;

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct LaunchProfile {
    pub main_class: String,
    pub java_component: String,
    pub java_major: i32,
    pub os: String,
    pub arch: String,
    pub platform_key: String,
    #[serde(default)]
    pub java_bin: String,
    #[serde(default)]
    pub version_id: String,
    pub asset_index: String,
    pub client_jar: String,
    pub classpath: Vec<String>,
    #[serde(default)]
    pub natives: Vec<String>,
    #[serde(default)]
    pub jvm_args: Vec<String>,
    #[serde(default)]
    pub game_args: Vec<String>,
    pub runtime: String,
}

impl LaunchProfile {
    pub fn load(path: &Path) -> Result<Self, CoreError> {
        let text = std::fs::read_to_string(path)
            .map_err(|e| CoreError::Launch(format!("read profile: {e}")))?;
        serde_json::from_str(&text).map_err(|e| CoreError::Launch(format!("parse profile: {e}")))
    }
}
