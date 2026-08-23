use crate::proto::api::v1::SignalKind;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Measurement {
    pub kind: SignalKind,
    pub value: String,
    pub confidence: u32,
}

impl Measurement {
    pub fn new(kind: SignalKind, value: impl Into<String>) -> Option<Measurement> {
        let normalized = normalize(&value.into())?;
        Some(Measurement {
            kind,
            value: normalized,
            confidence: 100,
        })
    }
}

const PLACEHOLDERS: &[&str] = &[
    "",
    "0",
    "NONE",
    "NA",
    "N/A",
    "NULL",
    "DEFAULTSTRING",
    "TOBEFILLEDBYOEM",
    "TOBEFILLEDBYOEM.",
    "TOBEFILLED",
    "SYSTEMSERIALNUMBER",
    "SYSTEMMANUFACTURER",
    "SYSTEMPRODUCTNAME",
    "BASEBOARDSERIALNUMBER",
    "CHASSISSERIALNUMBER",
    "SERIALNUMBER",
    "NOTSPECIFIED",
    "NOTAPPLICABLE",
    "UNKNOWN",
    "INVALID",
    "OEM",
    "XXXXXXXX",
    "03000200040005000006000700080009",
    "00000000000000000000000000000000",
    "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
];

pub fn normalize(raw: &str) -> Option<String> {
    let stripped: String = raw
        .trim()
        .chars()
        .filter(|c| c.is_ascii_alphanumeric())
        .map(|c| c.to_ascii_uppercase())
        .collect();
    if stripped.len() < 4 {
        return None;
    }
    if PLACEHOLDERS.contains(&stripped.as_str()) {
        return None;
    }
    if stripped
        .chars()
        .all(|c| c == stripped.as_bytes()[0] as char)
    {
        return None;
    }
    Some(stripped)
}

pub fn cap_for(kind: SignalKind) -> usize {
    match kind {
        SignalKind::DiskSerial | SignalKind::MacAddress => 2,
        _ => 1,
    }
}

pub fn dedupe(measurements: Vec<Measurement>) -> Vec<Measurement> {
    let mut seen: Vec<(SignalKind, String)> = Vec::new();
    let mut counts: Vec<(SignalKind, usize)> = Vec::new();
    let mut out = Vec::new();

    for measurement in measurements {
        if seen
            .iter()
            .any(|(kind, value)| *kind == measurement.kind && *value == measurement.value)
        {
            continue;
        }
        let used = counts
            .iter_mut()
            .find(|(kind, _)| *kind == measurement.kind);
        let count = match used {
            Some((_, count)) => count,
            None => {
                counts.push((measurement.kind, 0));
                &mut counts.last_mut().unwrap().1
            }
        };
        if *count >= cap_for(measurement.kind) {
            continue;
        }
        *count += 1;
        seen.push((measurement.kind, measurement.value.clone()));
        out.push(measurement);
    }
    out.sort_by(|a, b| (a.kind as i32, &a.value).cmp(&(b.kind as i32, &b.value)));
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalize_strips_and_rejects_placeholders() {
        assert_eq!(normalize(" 4c4c-4544 "), Some("4C4C4544".into()));
        assert_eq!(normalize("To Be Filled By O.E.M."), None);
        assert_eq!(normalize("00000000-0000-0000-0000-000000000000"), None);
        assert_eq!(normalize("abc"), None);
        assert_eq!(normalize("AAAAAAAA"), None);
    }

    #[test]
    fn dedupe_caps_repeated_kinds() {
        let measurements = vec![
            Measurement::new(SignalKind::DiskSerial, "disk-one").unwrap(),
            Measurement::new(SignalKind::DiskSerial, "disk-two").unwrap(),
            Measurement::new(SignalKind::DiskSerial, "disk-three").unwrap(),
            Measurement::new(SignalKind::SmbiosUuid, "uuid-one").unwrap(),
            Measurement::new(SignalKind::SmbiosUuid, "uuid-two").unwrap(),
        ];
        let kept = dedupe(measurements);
        assert_eq!(
            kept.iter()
                .filter(|m| m.kind == SignalKind::DiskSerial)
                .count(),
            2
        );
        assert_eq!(
            kept.iter()
                .filter(|m| m.kind == SignalKind::SmbiosUuid)
                .count(),
            1
        );
    }

    #[test]
    fn dedupe_drops_exact_repeats() {
        let measurements = vec![
            Measurement::new(SignalKind::MachineId, "machine-one").unwrap(),
            Measurement::new(SignalKind::MachineId, "MACHINE-ONE").unwrap(),
        ];
        assert_eq!(dedupe(measurements).len(), 1);
    }
}
