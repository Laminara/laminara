use std::fs;
use std::path::Path;

use crate::proto::api::v1::{CollectorFlag, SignalKind};

use super::signals::Measurement;

pub fn collect(out: &mut Vec<Measurement>, flags: &mut Vec<CollectorFlag>) {
    let mut smbios_seen = false;

    if let Some(value) =
        read_trimmed("/etc/machine-id").or_else(|| read_trimmed("/var/lib/dbus/machine-id"))
    {
        push(out, SignalKind::MachineId, value);
    }

    for (path, kind) in [
        ("/sys/class/dmi/id/product_uuid", SignalKind::SmbiosUuid),
        ("/sys/class/dmi/id/board_serial", SignalKind::BoardSerial),
        (
            "/sys/class/dmi/id/product_serial",
            SignalKind::PlatformSerial,
        ),
    ] {
        if let Some(value) = read_trimmed(path) {
            smbios_seen = true;
            push(out, kind, value);
        }
    }

    let board: Vec<String> = ["sys_vendor", "product_name", "board_name"]
        .iter()
        .filter_map(|name| read_trimmed(&format!("/sys/class/dmi/id/{name}")))
        .collect();
    if !board.is_empty() {
        push(out, SignalKind::Hostname, board.join(":"));
    }

    if !smbios_seen {
        flags.push(CollectorFlag::SmbiosUnreadable);
    }

    collect_disks(out);
    collect_macs(out);

    if let Some(value) = read_trimmed("/proc/sys/kernel/hostname") {
        push(out, SignalKind::Hostname, value);
    }
    if let Some(model) = cpu_model() {
        push(out, SignalKind::Cpu, model);
    }
    if let Some(total) = memory_bucket() {
        push(out, SignalKind::MemorySize, total);
    }
    if let Some(gpu) = gpu_identity() {
        push(out, SignalKind::Gpu, gpu);
    }
    if is_virtual() {
        flags.push(CollectorFlag::VirtualMachine);
    }
    if is_container() {
        flags.push(CollectorFlag::Container);
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

fn read_trimmed(path: impl AsRef<Path>) -> Option<String> {
    let raw = fs::read_to_string(path).ok()?;
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return None;
    }
    Some(trimmed.to_string())
}

fn collect_disks(out: &mut Vec<Measurement>) {
    let Ok(entries) = fs::read_dir("/dev/disk/by-id") else {
        return;
    };
    let mut names: Vec<String> = entries
        .filter_map(|entry| entry.ok())
        .filter_map(|entry| entry.file_name().into_string().ok())
        .filter(|name| !name.contains("-part"))
        .filter(|name| {
            name.starts_with("ata-")
                || name.starts_with("nvme-")
                || name.starts_with("scsi-")
                || name.starts_with("usb-")
        })
        .filter(|name| !name.starts_with("nvme-eui.") && !name.starts_with("scsi-0"))
        .collect();
    names.sort();
    for name in names {
        if let Some((_, serial)) = name.rsplit_once('_') {
            push(out, SignalKind::DiskSerial, serial);
        } else {
            push(out, SignalKind::DiskSerial, name);
        }
    }
}

fn collect_macs(out: &mut Vec<Measurement>) {
    let Ok(entries) = fs::read_dir("/sys/class/net") else {
        return;
    };
    let mut names: Vec<String> = entries
        .filter_map(|entry| entry.ok()?.file_name().into_string().ok())
        .collect();
    names.sort();
    for name in names {
        if name == "lo" {
            continue;
        }
        let base = format!("/sys/class/net/{name}");
        if read_trimmed(format!("{base}/address_assign_type")).as_deref() != Some("0") {
            continue;
        }
        if let Some(address) = read_trimmed(format!("{base}/address")) {
            push(out, SignalKind::MacAddress, address);
        }
    }
}

fn cpu_model() -> Option<String> {
    let text = fs::read_to_string("/proc/cpuinfo").ok()?;
    for line in text.lines() {
        if let Some((key, value)) = line.split_once(':') {
            let key = key.trim();
            if key == "model name" || key == "Model" || key == "Processor" {
                return Some(value.trim().to_string());
            }
        }
    }
    None
}

fn memory_bucket() -> Option<String> {
    let text = fs::read_to_string("/proc/meminfo").ok()?;
    for line in text.lines() {
        if let Some(rest) = line.strip_prefix("MemTotal:") {
            let kb: u64 = rest.trim().trim_end_matches(" kB").trim().parse().ok()?;
            return Some(format!("MEM{}GB", kb / 1024 / 1024));
        }
    }
    None
}

fn gpu_identity() -> Option<String> {
    let entries = fs::read_dir("/sys/bus/pci/devices").ok()?;
    let mut found: Vec<String> = Vec::new();
    for entry in entries.filter_map(|entry| entry.ok()) {
        let path = entry.path();
        let class = read_trimmed(path.join("class")).unwrap_or_default();
        if !class.starts_with("0x03") {
            continue;
        }
        let vendor = read_trimmed(path.join("vendor")).unwrap_or_default();
        let device = read_trimmed(path.join("device")).unwrap_or_default();
        if !vendor.is_empty() && !device.is_empty() {
            found.push(format!("{vendor}:{device}"));
        }
    }
    found.sort();
    if found.is_empty() {
        return None;
    }
    Some(found.join(","))
}

fn is_virtual() -> bool {
    let hints = [
        "QEMU",
        "KVM",
        "VMWARE",
        "VIRTUALBOX",
        "INNOTEK",
        "XEN",
        "MICROSOFT CORPORATION",
        "PARALLELS",
        "BOCHS",
        "HYPER-V",
    ];
    for name in ["sys_vendor", "product_name", "board_vendor"] {
        if let Some(value) = read_trimmed(format!("/sys/class/dmi/id/{name}")) {
            let upper = value.to_uppercase();
            if hints.iter().any(|hint| upper.contains(hint)) {
                return true;
            }
        }
    }
    fs::read_to_string("/proc/cpuinfo")
        .map(|text| text.contains("hypervisor"))
        .unwrap_or(false)
}

fn is_container() -> bool {
    Path::new("/.dockerenv").exists()
        || fs::read_to_string("/proc/1/cgroup")
            .map(|text| {
                text.contains("docker") || text.contains("lxc") || text.contains("kubepods")
            })
            .unwrap_or(false)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn collects_something_on_this_machine() {
        let mut out = Vec::new();
        let mut flags = Vec::new();
        collect(&mut out, &mut flags);
        assert!(
            !out.is_empty(),
            "a Linux host must yield at least one signal"
        );
        assert!(
            out.iter()
                .any(|m| m.kind == SignalKind::MachineId || m.kind == SignalKind::DiskSerial),
            "expected a strong signal, got {:?}",
            out.iter().map(|m| m.kind).collect::<Vec<_>>()
        );
    }

    #[test]
    fn memory_bucket_is_whole_gigabytes() {
        if let Some(value) = memory_bucket() {
            assert!(value.starts_with("MEM") && value.ends_with("GB"), "{value}");
        }
    }
}
