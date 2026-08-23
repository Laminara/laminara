use std::fs::{File, OpenOptions};
use std::io::{Read, Write};
use std::path::Path;

use super::{ecdsa_der, p256_spki, pad_left, KeyBackend};

const DEVICE: &str = "/dev/tpmrm0";
const DEVICE_FALLBACK: &str = "/dev/tpm0";

const ST_SESSIONS: u16 = 0x8002;
const ST_NO_SESSIONS: u16 = 0x8001;
const ST_HASHCHECK: u16 = 0x8024;

const CC_CREATE_PRIMARY: u32 = 0x0000_0131;
const CC_SIGN: u32 = 0x0000_015D;
const CC_FLUSH_CONTEXT: u32 = 0x0000_0165;

const RH_OWNER: u32 = 0x4000_0001;
const RH_NULL: u32 = 0x4000_0007;
const RS_PW: u32 = 0x4000_0009;

const ALG_SHA256: u16 = 0x000B;
const ALG_NULL: u16 = 0x0010;
const ALG_ECDSA: u16 = 0x0018;
const ALG_ECC: u16 = 0x0023;

const ECC_NIST_P256: u16 = 0x0003;

const OBJECT_ATTRIBUTES: u32 = 0x0004_0072;

const MAX_RESPONSE: usize = 4096;

pub struct TpmKey {
    device: String,
    public: [u8; 64],
}

pub fn open() -> Option<TpmKey> {
    for device in [DEVICE, DEVICE_FALLBACK] {
        if !Path::new(device).exists() {
            continue;
        }
        match derive(device, |_, _, public| Ok(public)) {
            Ok(public) => {
                return Some(TpmKey {
                    device: device.to_string(),
                    public,
                })
            }
            Err(error) => tracing::debug!("tpm {device} unavailable: {error}"),
        }
    }
    None
}

fn derive<T>(
    device: &str,
    work: impl FnOnce(&mut File, u32, [u8; 64]) -> Result<T, String>,
) -> Result<T, String> {
    let mut file = OpenOptions::new()
        .read(true)
        .write(true)
        .open(device)
        .map_err(|e| format!("open {device}: {e}"))?;
    let (handle, public) = create_primary(&mut file)?;
    let outcome = work(&mut file, handle, public);
    let _ = flush(&mut file, handle);
    outcome
}

impl KeyBackend for TpmKey {
    fn public_spki(&self) -> Vec<u8> {
        p256_spki(&self.public)
    }

    fn sign(&self, message: &[u8]) -> Option<Vec<u8>> {
        let digest = sha256(message);
        let expected = self.public;
        derive(&self.device, |file, handle, public| {
            if public != expected {
                return Err("tpm returned a different primary key than before".into());
            }
            sign_digest(file, handle, &digest)
        })
        .ok()
    }
}

fn sha256(message: &[u8]) -> [u8; 32] {
    use sha2::{Digest, Sha256};
    Sha256::digest(message).into()
}

fn transceive(file: &mut File, command: &[u8]) -> Result<Vec<u8>, String> {
    file.write_all(command).map_err(|e| format!("write: {e}"))?;
    let mut buffer = vec![0u8; MAX_RESPONSE];
    let read = file.read(&mut buffer).map_err(|e| format!("read: {e}"))?;
    buffer.truncate(read);
    if buffer.len() < 10 {
        return Err("short response".into());
    }
    let code = u32::from_be_bytes([buffer[6], buffer[7], buffer[8], buffer[9]]);
    if code != 0 {
        return Err(format!("tpm response code 0x{code:08x}"));
    }
    Ok(buffer)
}

struct Writer(Vec<u8>);

impl Writer {
    fn new() -> Writer {
        Writer(Vec::new())
    }
    fn u8(&mut self, value: u8) -> &mut Self {
        self.0.push(value);
        self
    }
    fn u16(&mut self, value: u16) -> &mut Self {
        self.0.extend_from_slice(&value.to_be_bytes());
        self
    }
    fn u32(&mut self, value: u32) -> &mut Self {
        self.0.extend_from_slice(&value.to_be_bytes());
        self
    }
    fn bytes(&mut self, value: &[u8]) -> &mut Self {
        self.0.extend_from_slice(value);
        self
    }
    fn sized(&mut self, value: &[u8]) -> &mut Self {
        self.u16(value.len() as u16).bytes(value)
    }
}

fn framed(tag: u16, code: u32, body: &[u8]) -> Vec<u8> {
    let mut out = Writer::new();
    out.u16(tag)
        .u32(10 + body.len() as u32)
        .u32(code)
        .bytes(body);
    out.0
}

fn empty_password_session() -> Vec<u8> {
    let mut out = Writer::new();
    out.u32(RS_PW).u16(0).u8(0).u16(0);
    out.0
}

fn ecc_template() -> Vec<u8> {
    let mut public = Writer::new();
    public
        .u16(ALG_ECC)
        .u16(ALG_SHA256)
        .u32(OBJECT_ATTRIBUTES)
        .u16(0)
        .u16(ALG_NULL)
        .u16(ALG_ECDSA)
        .u16(ALG_SHA256)
        .u16(ECC_NIST_P256)
        .u16(ALG_NULL)
        .u16(0)
        .u16(0);
    public.0
}

fn create_primary(file: &mut File) -> Result<(u32, [u8; 64]), String> {
    let session = empty_password_session();
    let template = ecc_template();

    let mut body = Writer::new();
    body.u32(RH_OWNER)
        .u32(session.len() as u32)
        .bytes(&session)
        .u16(4)
        .u16(0)
        .u16(0)
        .sized(&template)
        .u16(0)
        .u32(0);

    let response = transceive(file, &framed(ST_SESSIONS, CC_CREATE_PRIMARY, &body.0))?;
    let mut cursor = 10;
    let handle = read_u32(&response, &mut cursor)?;
    let _parameter_size = read_u32(&response, &mut cursor)?;
    let public_size = read_u16(&response, &mut cursor)? as usize;
    if public_size < 24 {
        return Err("public area too small".into());
    }
    cursor += 20;
    let x = read_sized(&response, &mut cursor)?;
    let y = read_sized(&response, &mut cursor)?;

    let mut point = [0u8; 64];
    pad_left(&mut point[..32], x);
    pad_left(&mut point[32..], y);
    Ok((handle, point))
}

fn sign_digest(file: &mut File, handle: u32, digest: &[u8; 32]) -> Result<Vec<u8>, String> {
    let session = empty_password_session();

    let mut body = Writer::new();
    body.u32(handle)
        .u32(session.len() as u32)
        .bytes(&session)
        .sized(digest)
        .u16(ALG_NULL)
        .u16(ST_HASHCHECK)
        .u32(RH_NULL)
        .u16(0);

    let response = transceive(file, &framed(ST_SESSIONS, CC_SIGN, &body.0))?;
    let mut cursor = 10;
    let _parameter_size = read_u32(&response, &mut cursor)?;
    let algorithm = read_u16(&response, &mut cursor)?;
    if algorithm != ALG_ECDSA {
        return Err(format!("unexpected signature algorithm 0x{algorithm:04x}"));
    }
    let _hash = read_u16(&response, &mut cursor)?;
    let r = read_sized(&response, &mut cursor)?;
    let s = read_sized(&response, &mut cursor)?;
    Ok(ecdsa_der(r, s))
}

fn flush(file: &mut File, handle: u32) -> Result<(), String> {
    let mut body = Writer::new();
    body.u32(handle);
    transceive(file, &framed(ST_NO_SESSIONS, CC_FLUSH_CONTEXT, &body.0)).map(|_| ())
}

fn read_u16(buffer: &[u8], cursor: &mut usize) -> Result<u16, String> {
    if *cursor + 2 > buffer.len() {
        return Err("truncated response".into());
    }
    let value = u16::from_be_bytes([buffer[*cursor], buffer[*cursor + 1]]);
    *cursor += 2;
    Ok(value)
}

fn read_u32(buffer: &[u8], cursor: &mut usize) -> Result<u32, String> {
    if *cursor + 4 > buffer.len() {
        return Err("truncated response".into());
    }
    let value = u32::from_be_bytes([
        buffer[*cursor],
        buffer[*cursor + 1],
        buffer[*cursor + 2],
        buffer[*cursor + 3],
    ]);
    *cursor += 4;
    Ok(value)
}

fn read_sized<'a>(buffer: &'a [u8], cursor: &mut usize) -> Result<&'a [u8], String> {
    let size = read_u16(buffer, cursor)? as usize;
    if *cursor + size > buffer.len() {
        return Err("truncated response".into());
    }
    let value = &buffer[*cursor..*cursor + size];
    *cursor += size;
    Ok(value)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tpm_key_is_stable_and_signs() {
        let Some(key) = open() else {
            eprintln!("no usable TPM on this host, skipping");
            return;
        };
        let again = open().expect("a TPM that opened once must open again");
        assert_eq!(
            key.public, again.public,
            "the primary key must be derived identically every time"
        );

        let spki = key.public_spki();
        assert_eq!(spki.len(), 91);

        let signature = key.sign(b"laminara.machine.v1").expect("sign");
        assert_eq!(signature[0], 0x30, "signature must be ASN.1 DER");
        eprintln!(
            "tpm: key {} bytes, signature {} bytes",
            spki.len(),
            signature.len()
        );
    }
}
