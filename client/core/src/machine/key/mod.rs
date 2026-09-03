mod software;

#[cfg(target_os = "linux")]
mod tpm;

#[cfg(target_os = "windows")]
mod platform_crypto;

pub const SPKI_ED25519_PREFIX: [u8; 12] = [
    0x30, 0x2A, 0x30, 0x05, 0x06, 0x03, 0x2B, 0x65, 0x70, 0x03, 0x21, 0x00,
];

pub const SPKI_EC_P256_PREFIX: [u8; 27] = [
    0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2A, 0x86, 0x48, 0xCE, 0x3D, 0x02, 0x01, 0x06, 0x08, 0x2A,
    0x86, 0x48, 0xCE, 0x3D, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00, 0x04,
];

pub(super) fn ecdsa_der(r: &[u8], s: &[u8]) -> Vec<u8> {
    fn integer(value: &[u8]) -> Vec<u8> {
        let trimmed = value
            .iter()
            .position(|byte| *byte != 0)
            .map(|index| &value[index..])
            .unwrap_or(&[0]);
        let mut out = vec![0x02];
        if trimmed[0] & 0x80 != 0 {
            out.push(trimmed.len() as u8 + 1);
            out.push(0x00);
        } else {
            out.push(trimmed.len() as u8);
        }
        out.extend_from_slice(trimmed);
        out
    }
    let mut content = integer(r);
    content.extend(integer(s));
    let mut out = vec![0x30, content.len() as u8];
    out.extend(content);
    out
}

pub(super) fn pad_left(target: &mut [u8], value: &[u8]) {
    let width = target.len();
    let source = &value[value.len().saturating_sub(width)..];
    let start = width - source.len();
    target[..start].fill(0);
    target[start..].copy_from_slice(source);
}

pub(super) fn p256_spki(point: &[u8; 64]) -> Vec<u8> {
    let mut der = Vec::with_capacity(SPKI_EC_P256_PREFIX.len() + 64);
    der.extend_from_slice(&SPKI_EC_P256_PREFIX);
    der.extend_from_slice(point);
    der
}

pub trait KeyBackend: Send + Sync {
    fn public_spki(&self) -> Vec<u8>;
    fn sign(&self, message: &[u8]) -> Option<Vec<u8>>;
}

pub struct PlatformKey {
    backend: Box<dyn KeyBackend>,
    hardware_backed: bool,
}

impl PlatformKey {
    pub fn load_or_create() -> PlatformKey {
        if let Some(backend) = hardware() {
            return PlatformKey {
                backend,
                hardware_backed: true,
            };
        }
        PlatformKey::software()
    }

    pub fn software() -> PlatformKey {
        PlatformKey {
            backend: Box::new(software::SoftwareKey::load_or_create()),
            hardware_backed: false,
        }
    }

    pub fn hardware_backed(&self) -> bool {
        self.hardware_backed
    }

    pub fn public_spki(&self) -> Vec<u8> {
        self.backend.public_spki()
    }

    pub fn digest_source(&self) -> String {
        hex::encode(self.backend.public_spki())
    }

    pub fn sign(&self, message: &[u8]) -> Option<Vec<u8>> {
        self.backend.sign(message)
    }
}

fn hardware() -> Option<Box<dyn KeyBackend>> {
    #[cfg(target_os = "linux")]
    {
        if let Some(key) = tpm::open() {
            return Some(Box::new(key));
        }
    }
    #[cfg(target_os = "windows")]
    {
        if let Some(key) = platform_crypto::open() {
            return Some(Box::new(key));
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn der_pads_a_negative_looking_integer() {
        let der = ecdsa_der(&[0xFF; 32], &[0x01; 32]);
        assert_eq!(der[0], 0x30);
        assert_eq!(der[2], 0x02);
        assert_eq!(der[3], 33, "a leading 0x80 bit must be padded");
        assert_eq!(der[4], 0x00);
    }

    #[test]
    fn der_trims_leading_zeros() {
        let mut value = [0u8; 32];
        value[31] = 7;
        assert_eq!(
            ecdsa_der(&value, &value)[3],
            1,
            "leading zeros are not part of the integer"
        );
    }

    #[test]
    fn pad_left_right_aligns() {
        let mut target = [9u8; 32];
        pad_left(&mut target, &[1, 2, 3]);
        assert_eq!(target[..29], [0u8; 29]);
        assert_eq!(&target[29..], &[1, 2, 3]);
    }

    #[test]
    fn a_key_always_exists_and_signs() {
        let key = PlatformKey::load_or_create();
        let spki = key.public_spki();
        assert!(
            spki.len() == 44 || spki.len() == 91,
            "unexpected SPKI length {}",
            spki.len()
        );
        assert!(
            key.sign(b"laminara").is_some(),
            "a key that exists must be able to sign"
        );
        eprintln!(
            "platform key: hardware_backed={} spki={} bytes",
            key.hardware_backed(),
            spki.len()
        );
    }

    #[test]
    fn the_software_key_is_always_available() {
        let key = PlatformKey::software();

        assert!(!key.hardware_backed());
        assert!(!key.public_spki().is_empty());
        assert!(
            key.sign(b"payload").is_some(),
            "the fallback key must sign: it is what a slow hardware key falls back to"
        );
    }
}
