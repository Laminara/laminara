use std::time::Duration;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;

pub struct Status {
    pub online: i64,
    pub max: i64,
}

fn write_varint(buf: &mut Vec<u8>, mut value: u32) {
    loop {
        let mut byte = (value & 0x7f) as u8;
        value >>= 7;
        if value != 0 {
            byte |= 0x80;
        }
        buf.push(byte);
        if value == 0 {
            break;
        }
    }
}

fn write_string(buf: &mut Vec<u8>, text: &str) {
    write_varint(buf, text.len() as u32);
    buf.extend_from_slice(text.as_bytes());
}

async fn read_varint(stream: &mut TcpStream) -> std::io::Result<u32> {
    let mut result = 0u32;
    let mut shift = 0u32;
    loop {
        let mut byte = [0u8; 1];
        stream.read_exact(&mut byte).await?;
        result |= ((byte[0] & 0x7f) as u32) << shift;
        if byte[0] & 0x80 == 0 {
            break;
        }
        shift += 7;
        if shift >= 32 {
            return Err(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                "varint too long",
            ));
        }
    }
    Ok(result)
}

pub fn split_address(address: &str) -> (String, u16) {
    let address = address.trim();
    match address.rsplit_once(':') {
        Some((host, port)) if port.parse::<u16>().is_ok() => {
            (host.to_string(), port.parse().unwrap())
        }
        _ => (address.to_string(), 25565),
    }
}

pub async fn ping(host: &str, port: u16) -> Option<Status> {
    let query = async {
        let mut stream = TcpStream::connect((host, port)).await.ok()?;

        let mut handshake = Vec::new();
        write_varint(&mut handshake, 0x00);
        write_varint(&mut handshake, 767);
        write_string(&mut handshake, host);
        handshake.extend_from_slice(&port.to_be_bytes());
        write_varint(&mut handshake, 1);
        let mut framed = Vec::new();
        write_varint(&mut framed, handshake.len() as u32);
        framed.extend_from_slice(&handshake);
        stream.write_all(&framed).await.ok()?;

        stream.write_all(&[0x01, 0x00]).await.ok()?;

        let _packet_len = read_varint(&mut stream).await.ok()?;
        let _packet_id = read_varint(&mut stream).await.ok()?;
        let json_len = read_varint(&mut stream).await.ok()? as usize;
        let mut json = vec![0u8; json_len];
        stream.read_exact(&mut json).await.ok()?;

        let value: serde_json::Value = serde_json::from_slice(&json).ok()?;
        let players = value.get("players")?;
        let online = players.get("online")?.as_i64()?;
        let max = players.get("max").and_then(|m| m.as_i64()).unwrap_or(0);
        Some(Status { online, max })
    };
    tokio::time::timeout(Duration::from_secs(3), query)
        .await
        .ok()
        .flatten()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn splits_address() {
        assert_eq!(
            split_address("mc.example.net"),
            ("mc.example.net".to_string(), 25565)
        );
        assert_eq!(
            split_address("mc.example.net:25570"),
            ("mc.example.net".to_string(), 25570)
        );
    }

    #[tokio::test]
    async fn live_ping() {
        if std::env::var("LAMINARA_SLP_E2E").is_err() {
            return;
        }
        let address =
            std::env::var("LAMINARA_SLP_ADDR").unwrap_or_else(|_| "mc.hypixel.net".into());
        let (host, port) = split_address(&address);
        let status = ping(&host, port).await;
        eprintln!(
            "ping {address} -> {:?}",
            status.as_ref().map(|s| (s.online, s.max))
        );
        assert!(status.is_some(), "expected a live status from {address}");
    }
}
