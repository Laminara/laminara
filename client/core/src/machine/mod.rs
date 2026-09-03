mod key;
mod signals;

#[cfg(target_os = "linux")]
mod linux;
#[cfg(target_os = "macos")]
mod macos;
#[cfg(target_os = "windows")]
mod windows;

use std::time::{Duration, SystemTime, UNIX_EPOCH};

use crate::proto::api::v1::{CollectorFlag, MachineReport, Signal, SignalKind};

pub use key::PlatformKey;
use signals::Measurement;

pub const REPORT_SCHEMA_VERSION: u32 = 1;

const COLLECT_BUDGET: Duration = Duration::from_secs(3);

const KEY_BUDGET: Duration = Duration::from_secs(10);

const DIGEST_BYTES: usize = 16;

const CANONICAL_DOMAIN: &[u8] = b"laminara.machine.v1\x00";

fn digest(salt: &[u8; 32], kind: SignalKind, value: &str) -> Vec<u8> {
    let mut hasher = blake3::Hasher::new_keyed(salt);
    hasher.update(&(kind as i32).to_le_bytes());
    hasher.update(&[0]);
    hasher.update(value.as_bytes());
    hasher.finalize().as_bytes()[..DIGEST_BYTES].to_vec()
}

fn collect_measurements() -> (Vec<Measurement>, Vec<CollectorFlag>) {
    let mut measurements = Vec::new();
    let mut flags = Vec::new();

    #[cfg(target_os = "linux")]
    linux::collect(&mut measurements, &mut flags);
    #[cfg(target_os = "macos")]
    macos::collect(&mut measurements, &mut flags);
    #[cfg(target_os = "windows")]
    windows::collect(&mut measurements, &mut flags);

    (signals::dedupe(measurements), flags)
}

pub struct MachineFacts {
    signals: Vec<Signal>,
    flags: Vec<i32>,
    key: Option<PlatformKey>,
    os_version: String,
}

impl MachineFacts {
    pub async fn collect(salt: &[u8; 32]) -> MachineFacts {
        let collected = tokio::time::timeout(
            COLLECT_BUDGET,
            tokio::task::spawn_blocking(collect_measurements),
        )
        .await
        .ok()
        .and_then(|joined| joined.ok());

        let (measurements, mut flags) = match collected {
            Some(pair) => pair,
            None => (Vec::new(), vec![CollectorFlag::Weak]),
        };

        let key = match tokio::time::timeout(
            KEY_BUDGET,
            tokio::task::spawn_blocking(PlatformKey::load_or_create),
        )
        .await
        .ok()
        .and_then(|joined| joined.ok())
        {
            Some(key) => Some(key),
            None => tokio::task::spawn_blocking(PlatformKey::software)
                .await
                .ok(),
        };

        let mut signals: Vec<Signal> = measurements
            .iter()
            .map(|measurement| Signal {
                kind: measurement.kind as i32,
                digest: digest(salt, measurement.kind, &measurement.value),
                confidence: measurement.confidence,
            })
            .collect();

        if let Some(key) = &key {
            if !key.hardware_backed() {
                flags.push(CollectorFlag::PlatformKeyFallback);
            }
            signals.push(Signal {
                kind: SignalKind::PlatformKey as i32,
                digest: digest(salt, SignalKind::PlatformKey, &key.digest_source()),
                confidence: 100,
            });
        }
        signals.sort_by(|a, b| (a.kind, &a.digest).cmp(&(b.kind, &b.digest)));

        if signals.len() < 3 && !flags.contains(&CollectorFlag::Weak) {
            flags.push(CollectorFlag::Weak);
        }

        MachineFacts {
            signals,
            flags: flags.iter().map(|flag| *flag as i32).collect(),
            key,
            os_version: os_version(),
        }
    }

    pub fn report(&self, nonce: Vec<u8>, launcher_version: &str) -> MachineReport {
        let mut report = MachineReport {
            schema_version: REPORT_SCHEMA_VERSION,
            signals: self.signals.clone(),
            flags: self.flags.clone(),
            platform: crate::platform::current() as i32,
            os_version: self.os_version.clone(),
            launcher_version: launcher_version.to_string(),
            nonce,
            platform_key_public: self
                .key
                .as_ref()
                .map(|key| key.public_spki())
                .unwrap_or_default(),
            platform_key_signature: Vec::new(),
            collected_at_unix_nanos: SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .map(|elapsed| elapsed.as_nanos() as i64)
                .unwrap_or_default(),
        };
        if let Some(key) = &self.key {
            match key.sign(&canonical(&report)) {
                Some(signature) => report.platform_key_signature = signature,
                None => report.platform_key_public.clear(),
            }
        }
        report
    }
}

pub fn parse_salt(hex_value: &str) -> Option<[u8; 32]> {
    let raw = hex::decode(hex_value.trim()).ok()?;
    raw.try_into().ok()
}

pub fn canonical(report: &MachineReport) -> Vec<u8> {
    let mut out = Vec::new();
    out.extend_from_slice(CANONICAL_DOMAIN);
    out.extend_from_slice(&report.schema_version.to_le_bytes());

    out.extend_from_slice(&(report.signals.len() as u32).to_le_bytes());
    for signal in &report.signals {
        out.extend_from_slice(&signal.kind.to_le_bytes());
        out.extend_from_slice(&(signal.digest.len() as u32).to_le_bytes());
        out.extend_from_slice(&signal.digest);
    }

    out.extend_from_slice(&(report.flags.len() as u32).to_le_bytes());
    for flag in &report.flags {
        out.extend_from_slice(&flag.to_le_bytes());
    }

    out.extend_from_slice(&report.platform.to_le_bytes());
    out.extend_from_slice(&report.collected_at_unix_nanos.to_le_bytes());
    out.extend_from_slice(&(report.nonce.len() as u32).to_le_bytes());
    out.extend_from_slice(&report.nonce);
    out
}

pub(crate) fn os_version() -> String {
    #[cfg(target_os = "linux")]
    {
        if let Ok(text) = std::fs::read_to_string("/etc/os-release") {
            for line in text.lines() {
                if let Some(value) = line.strip_prefix("PRETTY_NAME=") {
                    return value.trim_matches('"').to_string();
                }
            }
        }
        "linux".into()
    }
    #[cfg(not(target_os = "linux"))]
    {
        std::env::consts::OS.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn digest_is_salted_and_kind_scoped() {
        let salt = [1u8; 32];
        let other = [2u8; 32];
        let a = digest(&salt, SignalKind::DiskSerial, "SERIAL123");
        assert_eq!(a.len(), DIGEST_BYTES);
        assert_ne!(
            a,
            digest(&other, SignalKind::DiskSerial, "SERIAL123"),
            "a different salt must not correlate"
        );
        assert_ne!(
            a,
            digest(&salt, SignalKind::BoardSerial, "SERIAL123"),
            "the same value under two kinds is two facts"
        );
        assert_eq!(a, digest(&salt, SignalKind::DiskSerial, "SERIAL123"));
    }

    #[test]
    fn canonical_covers_every_signed_field() {
        let base = MachineReport {
            schema_version: 1,
            signals: vec![Signal {
                kind: SignalKind::MachineId as i32,
                digest: vec![1, 2, 3],
                confidence: 100,
            }],
            flags: vec![CollectorFlag::VirtualMachine as i32],
            platform: 3,
            os_version: "irrelevant".into(),
            launcher_version: "irrelevant".into(),
            nonce: vec![9, 9],
            platform_key_public: vec![4, 4],
            platform_key_signature: vec![5, 5],
            collected_at_unix_nanos: 42,
        };
        let signed = canonical(&base);

        let mut cosmetic = base.clone();
        cosmetic.os_version = "something else".into();
        cosmetic.platform_key_signature = vec![7, 7];
        assert_eq!(
            signed,
            canonical(&cosmetic),
            "unsigned fields must not change the message"
        );

        for mutate in [
            |r: &mut MachineReport| r.nonce = vec![8, 8],
            |r: &mut MachineReport| r.signals[0].digest = vec![9],
            |r: &mut MachineReport| r.flags.clear(),
            |r: &mut MachineReport| r.platform = 4,
            |r: &mut MachineReport| r.collected_at_unix_nanos = 43,
        ] {
            let mut changed = base.clone();
            mutate(&mut changed);
            assert_ne!(
                signed,
                canonical(&changed),
                "a signed field changed without changing the message"
            );
        }
    }

    #[test]
    fn canonical_matches_the_server() {
        use sha2::{Digest, Sha256};

        let report = MachineReport {
            schema_version: 1,
            signals: vec![
                Signal {
                    kind: SignalKind::MachineId as i32,
                    digest: (0u8..16).collect(),
                    confidence: 100,
                },
                Signal {
                    kind: SignalKind::DiskSerial as i32,
                    digest: vec![0xAB; 16],
                    confidence: 50,
                },
            ],
            flags: vec![
                CollectorFlag::VirtualMachine as i32,
                CollectorFlag::PlatformKeyFallback as i32,
            ],
            platform: 3,
            os_version: "not signed".into(),
            launcher_version: "not signed".into(),
            nonce: vec![0xDE, 0xAD, 0xBE, 0xEF],
            platform_key_public: vec![1, 2, 3],
            platform_key_signature: vec![4, 5, 6],
            collected_at_unix_nanos: 1_735_689_600_000_000_000,
        };
        let sum = Sha256::digest(canonical(&report));
        assert_eq!(
            hex::encode(sum),
            "30ac67a2e51a688728889ee17f40d60240876ab7d5ffc0e184dcd7f745af06de"
        );
    }

    #[tokio::test]
    async fn report_is_signed_and_within_budget() {
        use ed25519_dalek::{Signature, Verifier, VerifyingKey};

        let salt = [3u8; 32];
        let started = std::time::Instant::now();
        let report = MachineFacts::collect(&salt)
            .await
            .report(vec![1, 2, 3, 4], "0.1.0-test");
        let ceiling = COLLECT_BUDGET + KEY_BUDGET + Duration::from_secs(2);
        assert!(
            started.elapsed() < ceiling,
            "collecting machine facts is bounded by {ceiling:?}, took {:?}",
            started.elapsed()
        );

        assert_eq!(report.schema_version, REPORT_SCHEMA_VERSION);
        assert!(!report.signals.is_empty(), "no signals collected");
        for signal in &report.signals {
            assert_eq!(signal.digest.len(), DIGEST_BYTES);
        }
        assert!(
            report
                .signals
                .iter()
                .any(|s| s.kind == SignalKind::PlatformKey as i32),
            "the platform key must always be reported"
        );

        match report.platform_key_public.len() {
            44 => {
                let raw: [u8; 32] = report.platform_key_public[12..].try_into().unwrap();
                let verifying = VerifyingKey::from_bytes(&raw).unwrap();
                let signature = Signature::from_slice(&report.platform_key_signature).unwrap();
                assert!(
                    verifying.verify(&canonical(&report), &signature).is_ok(),
                    "ed25519 report signature does not verify"
                );
            }
            91 => {
                assert_eq!(
                    report.platform_key_signature[0], 0x30,
                    "an ecdsa signature must be ASN.1 DER"
                );
                assert!(
                    report
                        .flags
                        .iter()
                        .all(|flag| *flag != CollectorFlag::PlatformKeyFallback as i32),
                    "a hardware-backed key must not be reported as a fallback"
                );
            }
            other => panic!("unexpected platform key length {other}"),
        }
    }
}
