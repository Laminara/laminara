use std::fs::File;
use std::io::{Read, Seek, SeekFrom};
use std::path::Path;

const MAGIC: &[u8; 16] = b"LAMINARA_CONFIG1";
const TRAILER_LEN: u64 = 24;
const BUNDLE_CONFIG: &str = "laminara.client.json";

pub fn read() -> Option<String> {
    read_for(&std::env::current_exe().ok()?)
}

fn read_for(exe: &Path) -> Option<String> {
    read_from(exe).or_else(|| read_beside(exe))
}

fn read_beside(exe: &Path) -> Option<String> {
    let resources = exe.parent()?.parent()?.join("Resources").join(BUNDLE_CONFIG);
    let payload = std::fs::read_to_string(resources).ok()?;
    if payload.trim().is_empty() {
        return None;
    }
    Some(payload)
}

fn read_from(path: &Path) -> Option<String> {
    let mut file = File::open(path).ok()?;
    let size = file.metadata().ok()?.len();
    if size <= TRAILER_LEN {
        return None;
    }

    file.seek(SeekFrom::End(-(TRAILER_LEN as i64))).ok()?;
    let mut trailer = [0u8; TRAILER_LEN as usize];
    file.read_exact(&mut trailer).ok()?;
    if &trailer[8..] != MAGIC {
        return None;
    }

    let length = u64::from_le_bytes(trailer[..8].try_into().ok()?);
    if length == 0 || length > size - TRAILER_LEN {
        return None;
    }

    file.seek(SeekFrom::End(-((TRAILER_LEN + length) as i64)))
        .ok()?;
    let mut payload = vec![0u8; length as usize];
    file.read_exact(&mut payload).ok()?;
    String::from_utf8(payload).ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    fn write_candidate(dir: &Path, name: &str, body: &[u8]) -> std::path::PathBuf {
        let path = dir.join(name);
        let mut file = File::create(&path).unwrap();
        file.write_all(body).unwrap();
        path
    }

    fn baked(config: &str) -> Vec<u8> {
        let mut body = b"pretend this is a launcher binary".to_vec();
        body.extend_from_slice(config.as_bytes());
        body.extend_from_slice(&(config.len() as u64).to_le_bytes());
        body.extend_from_slice(MAGIC);
        body
    }

    #[test]
    fn reads_the_config_appended_to_the_file() {
        let dir = std::env::temp_dir().join("laminara-baked-read");
        std::fs::create_dir_all(&dir).unwrap();
        let config = r#"{"endpoints":[{"id":"main","baseUrl":"https://example"}]}"#;
        let path = write_candidate(&dir, "with-config", &baked(config));

        assert_eq!(read_from(&path).as_deref(), Some(config));
    }

    #[test]
    fn ignores_a_binary_without_the_trailer() {
        let dir = std::env::temp_dir().join("laminara-baked-plain");
        std::fs::create_dir_all(&dir).unwrap();
        let path = write_candidate(&dir, "plain", b"just a launcher binary");

        assert_eq!(read_from(&path), None);
    }

    fn bundle(name: &str, executable: &[u8], config: Option<&str>) -> std::path::PathBuf {
        let root = std::env::temp_dir().join(name).join("Laminara.app");
        let _ = std::fs::remove_dir_all(&root);
        std::fs::create_dir_all(root.join("Contents/MacOS")).unwrap();
        std::fs::create_dir_all(root.join("Contents/Resources")).unwrap();
        let exe = root.join("Contents/MacOS/laminara");
        std::fs::write(&exe, executable).unwrap();
        if let Some(payload) = config {
            std::fs::write(root.join("Contents/Resources").join(BUNDLE_CONFIG), payload).unwrap();
        }
        exe
    }

    #[test]
    fn reads_the_config_that_lies_next_to_the_app_bundle() {
        let config = r#"{"endpoints":[{"id":"main","baseUrl":"https://bundle.example"}]}"#;
        let exe = bundle("laminara-bundle-read", b"signed mach-o", Some(config));

        assert_eq!(read_for(&exe).as_deref(), Some(config));
    }

    #[test]
    fn a_bundle_without_the_file_stays_unconfigured() {
        let exe = bundle("laminara-bundle-empty", b"signed mach-o", None);

        assert_eq!(read_for(&exe), None);
    }

    #[test]
    fn the_trailer_wins_over_the_file_in_the_bundle() {
        let config = r#"{"endpoints":[{"id":"main","baseUrl":"https://trailer.example"}]}"#;
        let exe = bundle("laminara-bundle-both", &baked(config), Some("{\"endpoints\":[]}"));

        assert_eq!(read_for(&exe).as_deref(), Some(config));
    }

    #[test]
    fn ignores_a_trailer_that_claims_more_than_the_file_holds() {
        let dir = std::env::temp_dir().join("laminara-baked-broken");
        std::fs::create_dir_all(&dir).unwrap();
        let mut body = b"short".to_vec();
        body.extend_from_slice(&u64::MAX.to_le_bytes());
        body.extend_from_slice(MAGIC);
        let path = write_candidate(&dir, "broken", &body);

        assert_eq!(read_from(&path), None);
    }
}
