use std::collections::HashMap;
use std::path::Path;

use serde::{Deserialize, Serialize};

use crate::error::CoreError;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CacheEntry {
    pub mtime_ns: i128,
    pub size: u64,
    pub algo: i32,
    pub hash_hex: String,
}

#[derive(Debug, Default, Serialize, Deserialize)]
pub struct StatCache {
    #[serde(default)]
    pub dirty: bool,
    #[serde(default)]
    pub entries: HashMap<String, CacheEntry>,
}

impl StatCache {
    pub fn load(path: &Path) -> StatCache {
        std::fs::read_to_string(path)
            .ok()
            .and_then(|text| serde_json::from_str(&text).ok())
            .unwrap_or_default()
    }

    pub fn save(&self, path: &Path) -> Result<(), CoreError> {
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        let text = serde_json::to_string(self).map_err(|e| CoreError::Sync(e.to_string()))?;
        let tmp = path.with_extension("json.tmp");
        std::fs::write(&tmp, text)?;
        std::fs::rename(&tmp, path)?;
        Ok(())
    }

    pub fn get(&self, rel: &str) -> Option<&CacheEntry> {
        self.entries.get(rel)
    }

    pub fn put(&mut self, rel: String, entry: CacheEntry) {
        self.entries.insert(rel, entry);
    }
}

pub fn mtime_ns(metadata: &std::fs::Metadata) -> i128 {
    metadata
        .modified()
        .ok()
        .and_then(|t| {
            t.duration_since(std::time::UNIX_EPOCH)
                .ok()
                .map(|d| d.as_nanos() as i128)
        })
        .unwrap_or(0)
}
