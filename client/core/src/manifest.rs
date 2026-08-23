use ed25519_dalek::{Signature, Verifier, VerifyingKey};
use prost::Message;

use crate::error::CoreError;
use crate::proto::core::v1::{HashAlgo, Manifest};

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
    Ok(VerifiedManifest {
        manifest,
        canonical: canonical.to_vec(),
    })
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
}

pub fn algo_name(algo: i32) -> &'static str {
    match HashAlgo::try_from(algo).unwrap_or(HashAlgo::Blake3) {
        HashAlgo::Sha256 => "sha256",
        HashAlgo::Sha1 => "sha1",
        _ => "blake3",
    }
}

pub fn object_key(algo: i32, value: &[u8]) -> String {
    let hex = hex::encode(value);
    format!("{}/{}/{}/{}", algo_name(algo), &hex[0..2], &hex[2..4], hex)
}
