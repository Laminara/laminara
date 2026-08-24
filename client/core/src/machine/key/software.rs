use std::fs;
use std::path::PathBuf;

use ed25519_dalek::{Signer, SigningKey};
use keyring::Entry;
use rand::RngCore;

use super::{KeyBackend, SPKI_ED25519_PREFIX};
use crate::KEYRING_SERVICE as SERVICE;

const ENTRY: &str = "machine-key";

pub struct SoftwareKey {
    signing: SigningKey,
}

impl SoftwareKey {
    pub fn load_or_create() -> SoftwareKey {
        let from_keyring = read_keyring();
        let from_file = read_file();
        let bytes = match (from_keyring, from_file) {
            (Some(bytes), _) => bytes,
            (None, Some(bytes)) => bytes,
            (None, None) => {
                let mut fresh = [0u8; 32];
                rand::thread_rng().fill_bytes(&mut fresh);
                fresh
            }
        };
        write_keyring(&bytes);
        write_file(&bytes);
        SoftwareKey {
            signing: SigningKey::from_bytes(&bytes),
        }
    }
}

impl KeyBackend for SoftwareKey {
    fn public_spki(&self) -> Vec<u8> {
        let mut der = Vec::with_capacity(SPKI_ED25519_PREFIX.len() + 32);
        der.extend_from_slice(&SPKI_ED25519_PREFIX);
        der.extend_from_slice(self.signing.verifying_key().as_bytes());
        der
    }

    fn sign(&self, message: &[u8]) -> Option<Vec<u8>> {
        Some(self.signing.sign(message).to_bytes().to_vec())
    }
}

fn entry() -> Option<Entry> {
    Entry::new(SERVICE, ENTRY).ok()
}

fn read_keyring() -> Option<[u8; 32]> {
    let stored = crate::secrets::blocking(|| entry()?.get_password().ok())?;
    decode(&stored)
}

fn write_keyring(bytes: &[u8; 32]) {
    let encoded = hex::encode(bytes);
    crate::secrets::blocking(|| {
        if let Some(entry) = entry() {
            let _ = entry.set_password(&encoded);
        }
    });
}

fn key_file() -> Option<PathBuf> {
    dirs::home_dir().map(|home| home.join(".laminara-machine"))
}

fn read_file() -> Option<[u8; 32]> {
    let stored = fs::read_to_string(key_file()?).ok()?;
    decode(stored.trim())
}

fn write_file(bytes: &[u8; 32]) {
    let Some(path) = key_file() else { return };
    if fs::write(&path, hex::encode(bytes)).is_err() {
        return;
    }
    hide(&path);
}

#[cfg(unix)]
fn hide(path: &PathBuf) {
    use std::os::unix::fs::PermissionsExt;
    let _ = fs::set_permissions(path, fs::Permissions::from_mode(0o600));
}

#[cfg(windows)]
fn hide(path: &PathBuf) {
    use crate::process::command;
    let system_root = std::env::var("SystemRoot").unwrap_or_else(|_| "C:\\Windows".into());
    let attrib = format!("{system_root}\\System32\\attrib.exe");
    let _ = command(attrib)
        .args(["+h", &path.to_string_lossy()])
        .status();
}

fn decode(value: &str) -> Option<[u8; 32]> {
    let raw = hex::decode(value.trim()).ok()?;
    raw.try_into().ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn spki_wraps_the_raw_key() {
        let key = SoftwareKey {
            signing: SigningKey::from_bytes(&[7u8; 32]),
        };
        let der = key.public_spki();
        assert_eq!(der.len(), 44);
        assert_eq!(&der[..12], &SPKI_ED25519_PREFIX);
        assert_eq!(&der[12..], key.signing.verifying_key().as_bytes());
    }

    #[test]
    fn signature_verifies_against_its_own_key() {
        use ed25519_dalek::{Signature, Verifier};
        let key = SoftwareKey {
            signing: SigningKey::from_bytes(&[9u8; 32]),
        };
        let message = b"laminara.machine.v1";
        let signature = Signature::from_slice(&key.sign(message).unwrap()).unwrap();
        assert!(key
            .signing
            .verifying_key()
            .verify(message, &signature)
            .is_ok());
    }
}
