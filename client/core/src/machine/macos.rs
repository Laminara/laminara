use crate::process::command;

use crate::proto::api::v1::{CollectorFlag, SignalKind};

use super::signals::Measurement;

pub fn collect(out: &mut Vec<Measurement>, flags: &mut Vec<CollectorFlag>) {
    if let Some(text) = ioreg("IOPlatformExpertDevice") {
        if let Some(value) = ioreg_field(&text, "IOPlatformUUID") {
            push(out, SignalKind::PlatformUuid, value);
        }
        if let Some(value) = ioreg_field(&text, "IOPlatformSerialNumber") {
            push(out, SignalKind::PlatformSerial, value);
        }
        if let Some(value) = ioreg_field(&text, "board-id") {
            push(out, SignalKind::BoardSerial, value);
        }
    }

    if let Some(value) = sysctl("hw.model") {
        push(out, SignalKind::Cpu, value);
    }
    if let Some(value) = sysctl("machdep.cpu.brand_string") {
        push(out, SignalKind::Cpu, value);
    }
    if let Some(value) = sysctl("kern.hostname") {
        push(out, SignalKind::Hostname, value);
    }
    if let Some(bytes) = sysctl_u64("hw.memsize") {
        push(
            out,
            SignalKind::MemorySize,
            format!("MEM{}GB", bytes / 1024 / 1024 / 1024),
        );
    }
    if let Some(text) = ioreg("IOEthernetInterface") {
        collect_macs(out, &text);
    }
    if sysctl_u64("kern.hv_vmm_present").unwrap_or(0) == 1 {
        flags.push(CollectorFlag::VirtualMachine);
    }
    if unsafe { libc::geteuid() } == 0 {
        flags.push(CollectorFlag::Elevated);
    }
}

fn push(out: &mut Vec<Measurement>, kind: SignalKind, value: impl Into<String>) {
    if let Some(measurement) = Measurement::new(kind, value) {
        out.push(measurement);
    }
}

fn ioreg(class: &str) -> Option<String> {
    let output = command("/usr/sbin/ioreg")
        .args(["-rd1", "-c", class])
        .output()
        .ok()?;
    if !output.status.success() {
        return None;
    }
    String::from_utf8(output.stdout).ok()
}

fn ioreg_field(text: &str, key: &str) -> Option<String> {
    let needle = format!("\"{key}\"");
    for line in text.lines() {
        let Some(rest) = line.split_once(&needle) else {
            continue;
        };
        let after = rest.1;
        let Some(start) = after.find('"') else {
            continue;
        };
        let value = &after[start + 1..];
        let end = value.find('"')?;
        return Some(value[..end].to_string());
    }
    None
}

fn collect_macs(out: &mut Vec<Measurement>, text: &str) {
    let mut found = Vec::new();
    for line in text.lines() {
        if let Some(rest) = line.split_once("\"IOMACAddress\" = <") {
            if let Some(end) = rest.1.find('>') {
                found.push(rest.1[..end].to_string());
            }
        }
    }
    found.sort();
    for address in found {
        push(out, SignalKind::MacAddress, address);
    }
}

fn sysctl(name: &str) -> Option<String> {
    let output = command("/usr/sbin/sysctl")
        .args(["-n", name])
        .output()
        .ok()?;
    if !output.status.success() {
        return None;
    }
    let value = String::from_utf8(output.stdout).ok()?;
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return None;
    }
    Some(trimmed.to_string())
}

fn sysctl_u64(name: &str) -> Option<u64> {
    sysctl(name)?.parse().ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_ioreg_output() {
        let sample = r#"
  +-o MacBookPro18,3  <class IOPlatformExpertDevice, id 0x100000287>
      "IOPlatformUUID" = "1B2C3D4E-5F60-7182-93A4-B5C6D7E8F901"
      "IOPlatformSerialNumber" = "C02XY1Z2ABCD"
"#;
        assert_eq!(
            ioreg_field(sample, "IOPlatformUUID").as_deref(),
            Some("1B2C3D4E-5F60-7182-93A4-B5C6D7E8F901")
        );
        assert_eq!(
            ioreg_field(sample, "IOPlatformSerialNumber").as_deref(),
            Some("C02XY1Z2ABCD")
        );
        assert_eq!(ioreg_field(sample, "Missing"), None);
    }

    #[test]
    fn parses_mac_addresses() {
        let sample =
            "    \"IOMACAddress\" = <a1b2c3d4e5f6>\n    \"IOMACAddress\" = <0011223344ff>\n";
        let mut out = Vec::new();
        collect_macs(&mut out, sample);
        assert_eq!(out.len(), 2);
    }
}
