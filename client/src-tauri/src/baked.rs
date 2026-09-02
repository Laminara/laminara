use std::fs::File;
use std::io::{Read, Seek, SeekFrom};
use std::path::Path;

const MAGIC: &[u8; 16] = b"LAMINARA_CONFIG1";
const TRAILER_LEN: u64 = 24;

pub fn read() -> Option<String> {
    read_from(&std::env::current_exe().ok()?)
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
