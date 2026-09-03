use std::fs;
use std::io::Write;
use std::path::Path;

pub fn write(path: &Path, bytes: &[u8]) -> std::io::Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }
    let _ = fs::remove_file(path);
    let mut file = create(path)?;
    file.write_all(bytes)?;
    file.sync_all()
}

#[cfg(unix)]
fn create(path: &Path) -> std::io::Result<fs::File> {
    use std::os::unix::fs::OpenOptionsExt;
    fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(path)
}

#[cfg(not(unix))]
fn create(path: &Path) -> std::io::Result<fs::File> {
    fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(path)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn secrets_are_not_world_readable() {
        let dir = std::env::temp_dir().join("laminara-privatefile");
        let path = dir.join("token");
        write(&path, b"secret").unwrap();

        assert_eq!(fs::read(&path).unwrap(), b"secret");

        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = fs::metadata(&path).unwrap().permissions().mode() & 0o777;
            assert_eq!(mode, 0o600, "секрет читается кем угодно: mode {mode:o}");
        }

        write(&path, b"replaced").unwrap();
        assert_eq!(fs::read(&path).unwrap(), b"replaced");
    }
}
