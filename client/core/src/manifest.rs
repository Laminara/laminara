use std::collections::{HashMap, HashSet};

use ed25519_dalek::{Signature, Verifier, VerifyingKey};
use prost::Message;

use crate::error::CoreError;
use crate::proto::core::v1::{FilePolicy, HashAlgo, Manifest};

pub struct VerifiedManifest {
    pub manifest: Manifest,
    pub canonical: Vec<u8>,
}

pub fn verify_signed<M: Message + Default>(
    keys: &[VerifyingKey],
    canonical: &[u8],
    signature: &[u8],
) -> Result<M, CoreError> {
    let sig_bytes: [u8; 64] = signature.try_into().map_err(|_| CoreError::Untrusted)?;
    let signature = Signature::from_bytes(&sig_bytes);
    if !keys
        .iter()
        .any(|key| key.verify(canonical, &signature).is_ok())
    {
        return Err(CoreError::Untrusted);
    }
    M::decode(canonical).map_err(|e| CoreError::Sync(format!("decode signed document: {e}")))
}

pub fn verify_and_decode(
    keys: &[VerifyingKey],
    canonical: &[u8],
    signature: &[u8],
) -> Result<VerifiedManifest, CoreError> {
    let manifest: Manifest = verify_signed(keys, canonical, signature)?;
    validate(&manifest)?;
    Ok(VerifiedManifest {
        manifest,
        canonical: canonical.to_vec(),
    })
}

pub fn algo_name(algo: i32) -> Result<&'static str, CoreError> {
    match HashAlgo::try_from(algo) {
        Ok(HashAlgo::Blake3) => Ok("blake3"),
        Ok(HashAlgo::Sha256) => Ok("sha256"),
        Ok(HashAlgo::Sha1) => Ok("sha1"),
        _ => Err(CoreError::Sync(format!(
            "неподдерживаемый алгоритм хеша: {algo}"
        ))),
    }
}

fn validate_hash(algo: i32, value: &[u8]) -> Result<(), CoreError> {
    let expected = match algo_name(algo)? {
        "sha1" => 20,
        _ => 32,
    };
    if value.len() != expected {
        return Err(CoreError::Sync(format!(
            "неверная длина хеша: {} вместо {expected}",
            value.len()
        )));
    }
    Ok(())
}

pub fn object_key(algo: i32, value: &[u8]) -> Result<String, CoreError> {
    validate_hash(algo, value)?;
    let hex = hex::encode(value);
    Ok(format!(
        "{}/{}/{}/{}",
        algo_name(algo)?,
        &hex[0..2],
        &hex[2..4],
        hex
    ))
}

pub(crate) fn validate_manifest_path(path: &str) -> Result<(), CoreError> {
    if path.is_empty()
        || path.starts_with('/')
        || path.contains(['\\', ':'])
        || path
            .chars()
            .any(|c| c.is_control() || "<>\"|?*".contains(c))
    {
        return Err(CoreError::Sync(format!("unsafe manifest path: {path}")));
    }
    for segment in path.split('/') {
        let device = segment
            .split('.')
            .next()
            .unwrap_or("")
            .trim_end()
            .to_uppercase();
        let reserved = matches!(
            device.as_str(),
            "CON" | "PRN" | "AUX" | "NUL" | "CONIN$" | "CONOUT$"
        ) || ["COM", "LPT"].iter().any(|prefix| {
            device.strip_prefix(prefix).is_some_and(|n| {
                matches!(
                    n,
                    "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9" | "¹" | "²" | "³"
                )
            })
        });
        if segment.is_empty()
            || segment == "."
            || segment == ".."
            || segment.ends_with(['.', ' '])
            || reserved
        {
            return Err(CoreError::Sync(format!(
                "unsafe segment in manifest path: {path}"
            )));
        }
    }
    let lower = path.to_uppercase();
    if lower == ".LAMINARA" || lower.starts_with(".LAMINARA/") {
        return Err(CoreError::Sync(format!(
            "manifest path targets client state: {path}"
        )));
    }
    Ok(())
}

pub(crate) fn validate(manifest: &Manifest) -> Result<(), CoreError> {
    let mut paths = HashSet::new();
    let mut objects = HashMap::new();
    let mut total = 0u64;
    for file in &manifest.files {
        validate_manifest_path(&file.path)?;
        FilePolicy::try_from(file.policy).map_err(|_| {
            CoreError::Sync(format!(
                "неподдерживаемая политика файла {}: {}",
                file.path, file.policy
            ))
        })?;
        let object = file
            .object
            .as_ref()
            .ok_or_else(|| CoreError::Sync(format!("у файла {} нет объекта", file.path)))?;
        let hash = object
            .hash
            .as_ref()
            .ok_or_else(|| CoreError::Sync(format!("у объекта {} нет хеша", file.path)))?;
        validate_hash(hash.algo, &hash.value)?;
        if let Some(previous) = objects.insert((hash.algo, hash.value.as_slice()), object.size) {
            if previous != object.size {
                return Err(CoreError::Sync(format!(
                    "разные размеры одного объекта: {}",
                    file.path
                )));
            }
        }
        total = total
            .checked_add(object.size)
            .ok_or_else(|| CoreError::Sync("суммарный размер файлов слишком велик".into()))?;

        if !paths.insert(file.path.to_uppercase()) {
            return Err(CoreError::Sync(format!(
                "повторяющийся путь: {}",
                file.path
            )));
        }
    }
    for path in &paths {
        for (index, _) in path.match_indices('/') {
            if paths.contains(&path[..index]) {
                return Err(CoreError::Sync(format!(
                    "файл используется как каталог: {path}"
                )));
            }
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use ed25519_dalek::{Signer, SigningKey};

    fn signed(key: &SigningKey, canonical: &[u8]) -> Vec<u8> {
        key.sign(canonical).to_bytes().to_vec()
    }

    #[test]
    fn any_trusted_key_verifies_and_others_do_not() {
        let retiring = SigningKey::from_bytes(&[1u8; 32]);
        let incoming = SigningKey::from_bytes(&[2u8; 32]);
        let stranger = SigningKey::from_bytes(&[3u8; 32]);
        let ring = vec![incoming.verifying_key(), retiring.verifying_key()];

        let manifest = Manifest {
            modpack: "survival".into(),
            ..Default::default()
        };
        let canonical = manifest.encode_to_vec();

        for key in [&retiring, &incoming] {
            let verified = verify_and_decode(&ring, &canonical, &signed(key, &canonical))
                .expect("a key in the ring must verify");
            assert_eq!(verified.manifest.modpack, "survival");
        }
        assert!(
            matches!(
                verify_and_decode(&ring, &canonical, &signed(&stranger, &canonical)),
                Err(CoreError::Untrusted)
            ),
            "a key outside the ring must never verify"
        );
    }
    #[test]
    fn object_keys_reject_malformed_hashes_without_panicking() {
        for (algo, len) in [
            (HashAlgo::Blake3, 32),
            (HashAlgo::Sha256, 32),
            (HashAlgo::Sha1, 20),
        ] {
            assert!(object_key(algo as i32, &vec![0; len]).is_ok());
            for bad_len in [0, 1, len - 1, len + 1] {
                assert!(object_key(algo as i32, &vec![0; bad_len]).is_err());
            }
        }
        for algo in [0, -1, 99] {
            assert!(object_key(algo, &[0; 32]).is_err());
        }
    }

    #[test]
    fn signatures_are_checked_before_accepting_manifest_metadata() {
        let key = SigningKey::from_bytes(&[1; 32]);
        let ring = [key.verifying_key()];
        let manifest = Manifest {
            files: vec![crate::proto::core::v1::ManifestFile {
                path: "mods/a.jar".into(),
                ..Default::default()
            }],
            ..Default::default()
        };
        let bytes = manifest.encode_to_vec();
        assert!(matches!(
            verify_and_decode(&ring, &bytes, &signed(&key, &bytes)),
            Err(CoreError::Sync(_))
        ));
        assert!(matches!(
            verify_and_decode(&ring, &bytes, &[]),
            Err(CoreError::Untrusted)
        ));
        let original = Manifest::default().encode_to_vec();
        assert!(matches!(
            verify_and_decode(&ring, &bytes, &signed(&key, &original)),
            Err(CoreError::Untrusted)
        ));
    }
}
