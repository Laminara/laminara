use sysinfo::System;

const SHARE_FOR_GAME: f64 = 0.8;
const STEP_MB: u32 = 512;
const FLOOR_MB: u32 = 2048;

pub fn total_mb() -> Option<u32> {
    let mut system = System::new();
    system.refresh_memory();
    let total = system.total_memory();
    if total == 0 {
        return None;
    }
    Some((total / 1024 / 1024) as u32)
}

pub fn allowed_mb() -> Option<u32> {
    let total = total_mb()?;
    let share = (f64::from(total) * SHARE_FOR_GAME) as u32;
    let rounded = share / STEP_MB * STEP_MB;
    Some(rounded.max(FLOOR_MB))
}
