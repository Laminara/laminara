use crate::proto::core::v1::Platform;

pub fn key(platform: Platform) -> Option<String> {
    if platform == Platform::Unspecified {
        return None;
    }
    Some(
        platform
            .as_str_name()
            .trim_start_matches("PLATFORM_")
            .to_ascii_lowercase()
            .replace('_', "-"),
    )
}

pub fn parse(value: &str) -> Option<Platform> {
    let wanted = value.trim().to_ascii_lowercase();
    ALL.iter()
        .copied()
        .find(|platform| key(*platform).is_some_and(|k| k == wanted))
}

pub const ALL: [Platform; 8] = [
    Platform::WindowsX64,
    Platform::WindowsX86,
    Platform::WindowsArm64,
    Platform::Linux,
    Platform::LinuxI386,
    Platform::MacOs,
    Platform::MacOsArm64,
    Platform::LinuxArm64,
];

pub fn current() -> Platform {
    match (std::env::consts::OS, std::env::consts::ARCH) {
        ("windows", "x86") => Platform::WindowsX86,
        ("windows", "aarch64") => Platform::WindowsArm64,
        ("windows", _) => Platform::WindowsX64,
        ("macos", "aarch64") => Platform::MacOsArm64,
        ("macos", _) => Platform::MacOs,
        (_, "x86") => Platform::LinuxI386,
        (_, "aarch64") => Platform::LinuxArm64,
        _ => Platform::Linux,
    }
}

pub fn current_key() -> String {
    key(current()).unwrap_or_else(|| "linux".into())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keys_match_the_mojang_vocabulary() {
        assert_eq!(key(Platform::WindowsX64).unwrap(), "windows-x64");
        assert_eq!(key(Platform::MacOsArm64).unwrap(), "mac-os-arm64");
        assert_eq!(key(Platform::Linux).unwrap(), "linux");
        assert_eq!(key(Platform::LinuxI386).unwrap(), "linux-i386");
        assert!(key(Platform::Unspecified).is_none());
    }

    #[test]
    fn parse_round_trips_every_platform() {
        for platform in ALL {
            let k = key(platform).unwrap();
            assert_eq!(parse(&k), Some(platform), "round trip failed for {k}");
        }
        assert_eq!(parse("nope"), None);
    }

    #[test]
    fn current_is_a_real_platform() {
        assert_ne!(current(), Platform::Unspecified);
        assert!(!current_key().is_empty());
    }
}
