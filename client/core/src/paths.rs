use std::path::{Path, PathBuf};

#[derive(Debug, Clone)]
pub struct LaminaraPaths {
    pub config_dir: PathBuf,
    pub data_dir: PathBuf,
}

impl LaminaraPaths {
    pub fn config_file(&self) -> PathBuf {
        self.config_dir.join("config.json")
    }

    pub fn state_dir(&self, profile: &str) -> PathBuf {
        self.data_dir
            .join("profiles")
            .join(profile)
            .join(".laminara")
    }
}

pub fn profile_root(install_dir: &Path, profile: &str) -> PathBuf {
    install_dir.join(profile)
}
