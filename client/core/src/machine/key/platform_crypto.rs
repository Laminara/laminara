use crate::process::command;

use super::{ecdsa_der, p256_spki, pad_left, KeyBackend};

const PROVIDER: &str = "Microsoft Platform Crypto Provider";

pub struct PlatformCryptoKey {
    public: [u8; 64],
}

pub fn open() -> Option<PlatformCryptoKey> {
    let public = run(&script(&format!(
        r#"
$key = Get-LaminaraKey
$ecdsa = [System.Security.Cryptography.ECDsaCng]::new($key)
$p = $ecdsa.ExportParameters($false)
[Convert]::ToBase64String($p.Q.X) + ":" + [Convert]::ToBase64String($p.Q.Y)
"#
    )))?;
    let (x, y) = public.trim().split_once(':')?;
    let x = decode_base64(x)?;
    let y = decode_base64(y)?;

    let mut point = [0u8; 64];
    pad_left(&mut point[..32], &x);
    pad_left(&mut point[32..], &y);
    Some(PlatformCryptoKey { public: point })
}

impl KeyBackend for PlatformCryptoKey {
    fn public_spki(&self) -> Vec<u8> {
        p256_spki(&self.public)
    }

    fn sign(&self, message: &[u8]) -> Option<Vec<u8>> {
        use base64::Engine;
        let payload = base64::engine::general_purpose::STANDARD.encode(message);
        let signature = run(&script(&format!(
            r#"
$key = Get-LaminaraKey
$ecdsa = [System.Security.Cryptography.ECDsaCng]::new($key)
$data = [Convert]::FromBase64String("{payload}")
[Convert]::ToBase64String($ecdsa.SignData($data, [System.Security.Cryptography.HashAlgorithmName]::SHA256))
"#
        )))?;
        let raw = decode_base64(signature.trim())?;
        if raw.len() != 64 {
            return None;
        }
        Some(ecdsa_der(&raw[..32], &raw[32..]))
    }
}

fn script(body: &str) -> String {
    let key_name = format!("{}.machine", crate::KEYRING_SERVICE);
    format!(
        r#"
$ErrorActionPreference='Stop'
function Get-LaminaraKey {{
  $name = "{key_name}"
  $provider = [System.Security.Cryptography.CngProvider]::new("{PROVIDER}")
  if ([System.Security.Cryptography.CngKey]::Exists($name, $provider)) {{
    return [System.Security.Cryptography.CngKey]::Open($name, $provider)
  }}
  $params = [System.Security.Cryptography.CngKeyCreationParameters]::new()
  $params.Provider = $provider
  $params.KeyCreationOptions = [System.Security.Cryptography.CngKeyCreationOptions]::None
  $params.ExportPolicy = [System.Security.Cryptography.CngExportPolicies]::None
  $params.KeyUsage = [System.Security.Cryptography.CngKeyUsages]::Signing
  return [System.Security.Cryptography.CngKey]::Create([System.Security.Cryptography.CngAlgorithm]::ECDsaP256, $name, $params)
}}
{body}
"#
    )
}

fn run(body: &str) -> Option<String> {
    let system_root = std::env::var("SystemRoot").unwrap_or_else(|_| "C:\\Windows".into());
    let powershell = format!("{system_root}\\System32\\WindowsPowerShell\\v1.0\\powershell.exe");
    let output = command(powershell)
        .args([
            "-NoProfile",
            "-NonInteractive",
            "-EncodedCommand",
            &encode_command(body),
        ])
        .output()
        .ok()?;
    if !output.status.success() {
        return None;
    }
    let text = String::from_utf8(output.stdout).ok()?;
    let trimmed = text.trim();
    if trimmed.is_empty() {
        return None;
    }
    Some(trimmed.to_string())
}

fn encode_command(script: &str) -> String {
    use base64::Engine;
    let utf16: Vec<u8> = script
        .encode_utf16()
        .flat_map(|unit| unit.to_le_bytes())
        .collect();
    base64::engine::general_purpose::STANDARD.encode(utf16)
}

fn decode_base64(value: &str) -> Option<Vec<u8>> {
    use base64::Engine;
    base64::engine::general_purpose::STANDARD.decode(value).ok()
}
