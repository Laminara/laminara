use crate::process::command;

use crate::proto::api::v1::{CollectorFlag, SignalKind};

use super::signals::Measurement;

pub fn collect(out: &mut Vec<Measurement>, flags: &mut Vec<CollectorFlag>) {
    let Some(text) = run_probe() else {
        flags.push(CollectorFlag::Weak);
        return;
    };

    let mut smbios_seen = false;
    for line in text.lines() {
        let Some((key, value)) = line.split_once('=') else {
            continue;
        };
        let value = value.trim();
        if value.is_empty() {
            continue;
        }
        match key.trim() {
            "UUID" => {
                smbios_seen = true;
                push(out, SignalKind::SmbiosUuid, value);
            }
            "BOARD" => {
                smbios_seen = true;
                push(out, SignalKind::BoardSerial, value);
            }
            "BIOS" => push(out, SignalKind::PlatformSerial, value),
            "MACHINEGUID" => push(out, SignalKind::MachineId, value),
            "INSTALL" => push(out, SignalKind::OsInstallId, value),
            "DISK" => push(out, SignalKind::DiskSerial, value),
            "VOLUME" => push(out, SignalKind::VolumeId, value),
            "MAC" => push(out, SignalKind::MacAddress, value),
            "GPU" => push(out, SignalKind::Gpu, value),
            "CPU" => push(out, SignalKind::Cpu, value),
            "HOST" => push(out, SignalKind::Hostname, value),
            "MEMBYTES" => {
                if let Ok(bytes) = value.parse::<u64>() {
                    push(
                        out,
                        SignalKind::MemorySize,
                        format!("MEM{}GB", bytes / 1024 / 1024 / 1024),
                    );
                }
            }
            "VENDOR" | "MODEL" => {
                if is_virtual_hint(value) {
                    if !flags.contains(&CollectorFlag::VirtualMachine) {
                        flags.push(CollectorFlag::VirtualMachine);
                    }
                }
            }
            _ => {}
        }
    }
    if !smbios_seen {
        flags.push(CollectorFlag::SmbiosUnreadable);
    }
}

fn push(out: &mut Vec<Measurement>, kind: SignalKind, value: impl Into<String>) {
    if let Some(measurement) = Measurement::new(kind, value) {
        out.push(measurement);
    }
}

fn is_virtual_hint(value: &str) -> bool {
    const HINTS: &[&str] = &[
        "VMWARE",
        "VIRTUALBOX",
        "INNOTEK",
        "QEMU",
        "KVM",
        "XEN",
        "PARALLELS",
        "BOCHS",
        "VIRTUAL MACHINE",
    ];
    let upper = value.to_uppercase();
    HINTS.iter().any(|hint| upper.contains(hint))
}

const PROBE: &str = r#"
$ErrorActionPreference='SilentlyContinue'
$p = Get-CimInstance Win32_ComputerSystemProduct
if ($p) { "UUID=" + $p.UUID; "MODEL=" + $p.Name; "VENDOR=" + $p.Vendor }
$b = Get-CimInstance Win32_BaseBoard
if ($b) { "BOARD=" + $b.SerialNumber }
$i = Get-CimInstance Win32_BIOS
if ($i) { "BIOS=" + $i.SerialNumber }
$c = Get-CimInstance Win32_ComputerSystem
if ($c) { "HOST=" + $c.Name; "MEMBYTES=" + $c.TotalPhysicalMemory; "VENDOR=" + $c.Manufacturer; "MODEL=" + $c.Model }
Get-CimInstance Win32_DiskDrive | Where-Object { $_.SerialNumber -and $_.MediaType -notlike '*Removable*' } | ForEach-Object { "DISK=" + $_.SerialNumber.Trim() }
$v = Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='C:'"
if ($v) { "VOLUME=" + $v.VolumeSerialNumber }
Get-CimInstance Win32_NetworkAdapter | Where-Object { $_.MACAddress -and $_.PNPDeviceID -like 'PCI*' } | ForEach-Object { "MAC=" + $_.MACAddress }
$g = Get-CimInstance Win32_VideoController | Select-Object -First 1
if ($g) { "GPU=" + $g.PNPDeviceID }
$u = Get-CimInstance Win32_Processor | Select-Object -First 1
if ($u) { "CPU=" + $u.Name + ":" + $u.NumberOfCores }
$k = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Cryptography'
if ($k) { "MACHINEGUID=" + $k.MachineGuid }
$w = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion'
if ($w) { "INSTALL=" + $w.InstallDate + $w.InstallTime + $w.ProductId }
"#;

fn run_probe() -> Option<String> {
    let system_root = std::env::var("SystemRoot").unwrap_or_else(|_| "C:\\Windows".into());
    let powershell = format!("{system_root}\\System32\\WindowsPowerShell\\v1.0\\powershell.exe");
    let output = command(powershell)
        .args([
            "-NoProfile",
            "-NonInteractive",
            "-EncodedCommand",
            &encode_command(PROBE),
        ])
        .output()
        .ok()?;
    String::from_utf8(output.stdout).ok()
}

fn encode_command(script: &str) -> String {
    use base64::Engine;
    let utf16: Vec<u8> = script
        .encode_utf16()
        .flat_map(|unit| unit.to_le_bytes())
        .collect();
    base64::engine::general_purpose::STANDARD.encode(utf16)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn encodes_as_utf16le_base64() {
        assert_eq!(encode_command("hi"), "aABpAA==");
    }

    #[test]
    fn spots_virtual_vendors() {
        assert!(is_virtual_hint("VMware, Inc."));
        assert!(is_virtual_hint("Virtual Machine"));
        assert!(!is_virtual_hint("ASUSTeK COMPUTER INC."));
    }
}
