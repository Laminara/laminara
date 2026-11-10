use chrono::{DateTime, Datelike, Local};

const GROUP: char = '\u{a0}';
const MONTHS: [&str; 12] = [
    "января",
    "февраля",
    "марта",
    "апреля",
    "мая",
    "июня",
    "июля",
    "августа",
    "сентября",
    "октября",
    "ноября",
    "декабря",
];

pub fn count(value: i64) -> String {
    let digits = value.unsigned_abs().to_string();
    let mut grouped = String::with_capacity(digits.len() + digits.len() / 3);
    for (index, digit) in digits.chars().enumerate() {
        if index > 0 && (digits.len() - index).is_multiple_of(3) {
            grouped.push(GROUP);
        }
        grouped.push(digit);
    }
    if value < 0 {
        format!("-{grouped}")
    } else {
        grouped
    }
}

fn decimal(value: f64) -> String {
    let tenths = (value * 10.0).round() as i64;
    let whole = count(tenths / 10);
    match tenths % 10 {
        0 => whole,
        fraction => format!("{whole},{fraction}"),
    }
}

pub fn bytes(amount: u64) -> String {
    if amount == 0 {
        return "0 Б".into();
    }
    const UNITS: [&str; 5] = ["Б", "КБ", "МБ", "ГБ", "ТБ"];
    let exponent = ((amount as f64).ln() / 1024f64.ln()).floor() as usize;
    let exponent = exponent.min(UNITS.len() - 1);
    let value = amount as f64 / 1024f64.powi(exponent as i32);
    let shown = if value >= 100.0 || exponent == 0 {
        count(value.round() as i64)
    } else {
        decimal(value)
    };
    format!("{shown} {}", UNITS[exponent])
}

pub fn memory(megabytes: u32) -> String {
    bytes(u64::from(megabytes) * 1024 * 1024)
}

pub fn speed(bytes_per_second: f64) -> String {
    format!("{}/с", bytes(bytes_per_second.max(0.0) as u64))
}

pub fn plural<'a>(value: i64, one: &'a str, few: &'a str, many: &'a str) -> &'a str {
    let mod100 = value.abs() % 100;
    let mod10 = mod100 % 10;
    if (11..=14).contains(&mod100) {
        many
    } else if mod10 == 1 {
        one
    } else if (2..=4).contains(&mod10) {
        few
    } else {
        many
    }
}

pub fn time_left(seconds: f64) -> String {
    if seconds < 60.0 {
        return "меньше минуты".into();
    }
    let minutes = (seconds / 60.0).round() as i64;
    if minutes < 60 {
        return format!("{minutes} {}", plural(minutes, "минута", "минуты", "минут"));
    }
    let hours = minutes / 60;
    let rest = minutes % 60;
    format!(
        "{hours} {} {rest} {}",
        plural(hours, "час", "часа", "часов"),
        plural(rest, "минута", "минуты", "минут")
    )
}

pub fn news_date(unix_nanos: i64) -> String {
    if unix_nanos == 0 {
        return String::new();
    }
    let moment = DateTime::from_timestamp_nanos(unix_nanos).with_timezone(&Local);
    format!("{} {}", moment.day(), MONTHS[moment.month0() as usize])
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn counts_are_grouped_like_the_russian_locale() {
        assert_eq!(count(999), "999");
        assert_eq!(count(1284), "1\u{a0}284");
        assert_eq!(count(1_234_567), "1\u{a0}234\u{a0}567");
    }

    #[test]
    fn sizes_keep_one_decimal_below_a_hundred() {
        assert_eq!(bytes(0), "0 Б");
        assert_eq!(bytes(512), "512 Б");
        assert_eq!(bytes(2_460_000_000), "2,3 ГБ");
        assert_eq!(bytes(150 * 1024 * 1024), "150 МБ");
        assert_eq!(bytes(1024 * 1024 * 1024), "1 ГБ");
    }

    #[test]
    fn memory_reads_in_gigabytes_with_a_decimal_comma() {
        assert_eq!(memory(1024), "1 ГБ");
        assert_eq!(memory(7680), "7,5 ГБ");
        assert_eq!(memory(26214), "25,6 ГБ");
    }

    #[test]
    fn plurals_follow_russian_rules() {
        assert_eq!(plural(1, "сборка", "сборки", "сборок"), "сборка");
        assert_eq!(plural(3, "сборка", "сборки", "сборок"), "сборки");
        assert_eq!(plural(11, "сборка", "сборки", "сборок"), "сборок");
        assert_eq!(plural(22, "сборка", "сборки", "сборок"), "сборки");
    }

    #[test]
    fn time_left_reads_naturally() {
        assert_eq!(time_left(30.0), "меньше минуты");
        assert_eq!(time_left(300.0), "5 минут");
        assert_eq!(time_left(3900.0), "1 час 5 минут");
    }
}
