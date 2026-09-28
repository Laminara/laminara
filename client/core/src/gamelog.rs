use std::borrow::Cow;
use std::collections::VecDeque;
use std::iter::Peekable;
use std::str::Chars;
use std::sync::Mutex;

use encoding_rs::Encoding;
use serde::Serialize;
use tokio::io::{AsyncBufReadExt, AsyncRead, AsyncReadExt, BufReader};

pub const LINE_LIMIT: usize = 50_000;
pub const TEXT_LIMIT_BYTES: usize = 16 * 1024 * 1024;
pub const LINE_BYTES_LIMIT: u64 = 64 * 1024;
const SHOWN_CHARS_LIMIT: usize = 8 * 1024;
const LEVEL_SEARCH_BYTES: usize = 200;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Stream {
    Out,
    Err,
}

impl Stream {
    fn slot(self) -> usize {
        match self {
            Stream::Out => 0,
            Stream::Err => 1,
        }
    }

    fn unmarked_level(self) -> Level {
        match self {
            Stream::Out => Level::Info,
            Stream::Err => Level::Warn,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Level {
    Debug,
    Info,
    Warn,
    Error,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GameLine {
    pub seq: u64,
    pub stream: Stream,
    pub level: Level,
    pub text: String,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize)]
#[serde(tag = "state", rename_all = "camelCase")]
pub enum GameStatus {
    #[default]
    Idle,
    Running,
    Exited {
        code: i32,
        stopped: bool,
    },
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Snapshot {
    pub session: u64,
    pub build: String,
    pub status: GameStatus,
    pub dropped: u64,
    pub limit: usize,
    pub text_limit: usize,
    pub lines: Vec<GameLine>,
}

#[derive(Debug, Default)]
pub struct GameLog {
    session: u64,
    build: String,
    status: GameStatus,
    lines: VecDeque<GameLine>,
    next_seq: u64,
    sent_seq: u64,
    dropped: u64,
    text_bytes: usize,
    carried: [Option<Level>; 2],
}

impl GameLog {
    pub fn begin(&mut self, build: &str) -> u64 {
        *self = GameLog {
            session: self.session + 1,
            build: build.to_string(),
            status: GameStatus::Running,
            ..GameLog::default()
        };
        self.session
    }

    pub fn session(&self) -> u64 {
        self.session
    }

    pub fn build(&self) -> &str {
        &self.build
    }

    pub fn record(&mut self, stream: Stream, text: String) {
        let slot = stream.slot();
        let level = level_of(&text)
            .or_else(|| opening_level(&text))
            .or(self.carried[slot])
            .unwrap_or_else(|| stream.unmarked_level());
        self.carried[slot] = Some(level);
        self.text_bytes += text.len();
        self.lines.push_back(GameLine {
            seq: self.next_seq,
            stream,
            level,
            text,
        });
        self.next_seq += 1;
        while self.lines.len() > LINE_LIMIT || self.text_bytes > TEXT_LIMIT_BYTES {
            let Some(oldest) = self.lines.pop_front() else {
                break;
            };
            self.text_bytes -= oldest.text.len();
            self.dropped += 1;
        }
    }

    pub fn unsent(&mut self) -> Vec<GameLine> {
        let first = self.next_seq - self.lines.len() as u64;
        let from = self.sent_seq.max(first);
        self.sent_seq = self.next_seq;
        self.lines
            .iter()
            .skip((from - first) as usize)
            .cloned()
            .collect()
    }

    pub fn finish(&mut self, session: u64, code: i32, stopped: bool) -> bool {
        if session != self.session {
            return false;
        }
        self.status = GameStatus::Exited { code, stopped };
        true
    }

    pub fn snapshot(&self) -> Snapshot {
        Snapshot {
            session: self.session,
            build: self.build.clone(),
            status: self.status,
            dropped: self.dropped,
            limit: LINE_LIMIT,
            text_limit: TEXT_LIMIT_BYTES,
            lines: self.lines.iter().cloned().collect(),
        }
    }

    pub fn tail(&self, count: usize) -> Vec<GameLine> {
        let skip = self.lines.len().saturating_sub(count);
        self.lines.iter().skip(skip).cloned().collect()
    }

    pub fn tail_text(&self, byte_limit: usize) -> String {
        let mut picked = Vec::new();
        let mut total = 0;
        for line in self.lines.iter().rev() {
            let cost = line.text.len() + 1;
            if total + cost > byte_limit {
                break;
            }
            total += cost;
            picked.push(line.text.as_str());
        }
        picked.reverse();
        picked.join("\n")
    }
}

pub async fn pump<R: AsyncRead + Unpin>(
    pipe: R,
    stream: Stream,
    log: &Mutex<GameLog>,
    session: u64,
) {
    let mut reader = BufReader::new(pipe);
    let mut raw = Vec::new();
    loop {
        raw.clear();
        match (&mut reader)
            .take(LINE_BYTES_LIMIT)
            .read_until(b'\n', &mut raw)
            .await
        {
            Ok(0) | Err(_) => break,
            Ok(_) => {
                let text = decode(&raw);
                let mut log = log
                    .lock()
                    .unwrap_or_else(std::sync::PoisonError::into_inner);
                if log.session() == session {
                    log.record(stream, text);
                }
            }
        }
    }
}

pub fn decode(raw: &[u8]) -> String {
    let raw = raw.strip_suffix(b"\n").unwrap_or(raw);
    let raw = raw.strip_suffix(b"\r").unwrap_or(raw);
    let text = match std::str::from_utf8(raw) {
        Ok(text) => Cow::Borrowed(text),
        Err(error) if error.error_len().is_none() => String::from_utf8_lossy(raw),
        Err(_) => fallback_encoding().decode_without_bom_handling(raw).0,
    };
    readable(&text)
}

#[cfg(windows)]
fn fallback_encoding() -> &'static Encoding {
    static ENCODING: std::sync::OnceLock<&'static Encoding> = std::sync::OnceLock::new();
    ENCODING.get_or_init(|| encoding_for_code_page(ansi_code_page()))
}

#[cfg(not(windows))]
fn fallback_encoding() -> &'static Encoding {
    encoding_rs::UTF_8
}

#[cfg(windows)]
fn ansi_code_page() -> u32 {
    #[link(name = "kernel32")]
    extern "system" {
        fn GetACP() -> u32;
    }
    unsafe { GetACP() }
}

#[cfg(any(windows, test))]
fn encoding_for_code_page(page: u32) -> &'static Encoding {
    let label = match page {
        65001 => return encoding_rs::UTF_8,
        866 => "ibm866".to_string(),
        932 => "shift_jis".to_string(),
        936 => "gbk".to_string(),
        949 => "euc-kr".to_string(),
        950 => "big5".to_string(),
        20866 => "koi8-r".to_string(),
        21866 => "koi8-u".to_string(),
        other => format!("windows-{other}"),
    };
    Encoding::for_label(label.as_bytes()).unwrap_or(encoding_rs::WINDOWS_1252)
}

fn readable(text: &str) -> String {
    let mut shown = String::with_capacity(text.len());
    let mut count = 0;
    let mut chars = text.chars().peekable();
    while let Some(c) = chars.next() {
        match c {
            '\u{1b}' => skip_escape(&mut chars),
            '§' if chars.next_if(char::is_ascii_alphanumeric).is_some() => {}
            '\t' => {
                shown.push(c);
                count += 1;
            }
            c if c.is_control() => {}
            c => {
                shown.push(c);
                count += 1;
            }
        }
        if count >= SHOWN_CHARS_LIMIT && chars.peek().is_some() {
            shown.push('…');
            break;
        }
    }
    shown
}

fn skip_escape(chars: &mut Peekable<Chars>) {
    match chars.next() {
        Some('[') => {
            for c in chars.by_ref() {
                if ('\u{40}'..='\u{7e}').contains(&c) {
                    break;
                }
            }
        }
        Some(']') => {
            while let Some(c) = chars.next() {
                if c == '\u{7}' {
                    break;
                }
                if c == '\u{1b}' {
                    chars.next_if_eq(&'\\');
                    break;
                }
            }
        }
        _ => {}
    }
}

const LEVEL_MARKERS: [(&str, Level); 17] = [
    ("/TRACE]", Level::Debug),
    ("/DEBUG]", Level::Debug),
    ("/INFO]", Level::Info),
    ("/WARN]", Level::Warn),
    ("/ERROR]", Level::Error),
    ("/FATAL]", Level::Error),
    ("[TRACE]", Level::Debug),
    ("[DEBUG]", Level::Debug),
    ("[FINE]", Level::Debug),
    ("[FINER]", Level::Debug),
    ("[INFO]", Level::Info),
    ("[WARN]", Level::Warn),
    ("[WARNING]", Level::Warn),
    ("[ERROR]", Level::Error),
    ("[SEVERE]", Level::Error),
    ("[FATAL]", Level::Error),
    ("[STDERR]", Level::Warn),
];

const ERROR_OPENINGS: [&str; 5] = [
    "Exception in thread ",
    "Error: ",
    "Error occurred during initialization",
    "---- Minecraft Crash Report ----",
    "#@!@# Game crashed!",
];

fn level_of(text: &str) -> Option<Level> {
    let mut end = text.len().min(LEVEL_SEARCH_BYTES);
    while !text.is_char_boundary(end) {
        end -= 1;
    }
    let head = &text[..end];
    LEVEL_MARKERS
        .iter()
        .filter_map(|(marker, level)| head.find(marker).map(|at| (at, *level)))
        .min_by_key(|(at, _)| *at)
        .map(|(_, level)| level)
}

fn opening_level(text: &str) -> Option<Level> {
    ERROR_OPENINGS
        .iter()
        .any(|opening| text.starts_with(opening))
        .then_some(Level::Error)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn recorded(lines: &[(Stream, &str)]) -> Vec<Level> {
        let mut log = GameLog::default();
        log.begin("test");
        for (stream, text) in lines {
            log.record(*stream, text.to_string());
        }
        log.snapshot().lines.iter().map(|line| line.level).collect()
    }

    #[test]
    fn levels_are_read_from_every_loader_log_format() {
        let levels = recorded(&[
            (Stream::Out, "[19:22:31] [Client thread/INFO]: Setting user: Dela1s"),
            (Stream::Out, "[19Sep2026 19:22:31.123] [main/WARN] [net.minecraftforge.fml.loading.moddiscovery.ModFile/LOADING]: bad jar"),
            (Stream::Out, "[19:22:31] [main/ERROR] (FabricLoader) Incompatible mods found"),
            (Stream::Out, "[19:22:31] [Client thread/INFO] [FML]: Forge Mod Loader version 10.13.4"),
            (Stream::Out, "2026-09-27 19:22:31 [SEVERE] [ForgeModLoader] Fatal errors were detected"),
            (Stream::Out, "[19:22:31] [Render thread/DEBUG]: Reloading ResourceManager"),
            (Stream::Out, "[19:22:31] [main/FATAL]: Unreported exception thrown!"),
        ]);
        assert_eq!(
            levels,
            [
                Level::Info,
                Level::Warn,
                Level::Error,
                Level::Info,
                Level::Error,
                Level::Debug,
                Level::Error
            ]
        );
    }

    #[test]
    fn the_first_marker_wins_over_one_quoted_in_the_message() {
        let levels = recorded(&[(
            Stream::Out,
            "[19:22:31] [main/INFO]: handler [ERROR] registered",
        )]);
        assert_eq!(levels, [Level::Info]);
    }

    #[test]
    fn a_stack_trace_keeps_the_level_of_the_line_that_opened_it() {
        let levels = recorded(&[
            (Stream::Out, "[19:22:33] [Render thread/ERROR]: Failed to load texture"),
            (Stream::Out, "java.io.FileNotFoundException: minecraft:textures/missing.png"),
            (Stream::Out, "\tat net.minecraft.client.renderer.texture.SimpleTexture.load(SimpleTexture.java:42)"),
            (Stream::Out, "[19:22:34] [Render thread/INFO]: Loaded 12 textures"),
            (Stream::Out, "  continued info"),
        ]);
        assert_eq!(
            levels,
            [
                Level::Error,
                Level::Error,
                Level::Error,
                Level::Info,
                Level::Info
            ]
        );
    }

    #[test]
    fn unmarked_stderr_is_a_warning_and_a_thread_crash_is_an_error() {
        let levels = recorded(&[
            (Stream::Out, "plain stdout line"),
            (Stream::Err, "OpenJDK 64-Bit Server VM warning: Options -Xverify:none are deprecated"),
            (Stream::Err, "Exception in thread \"main\" java.lang.NoClassDefFoundError: org/lwjgl/LWJGLException"),
            (Stream::Err, "\tat java.base/java.lang.Class.forName0(Native Method)"),
            (Stream::Out, "still plain stdout"),
        ]);
        assert_eq!(
            levels,
            [
                Level::Info,
                Level::Warn,
                Level::Error,
                Level::Error,
                Level::Info
            ]
        );
    }

    #[test]
    fn colour_codes_and_terminal_escapes_are_removed() {
        assert_eq!(
            decode(b"\x1b[32m[19:22:31] [main/INFO]\x1b[0m: \xc2\xa7aGreen \xc2\xa7lbold\r\n"),
            "[19:22:31] [main/INFO]: Green bold"
        );
        assert_eq!(decode(b"\x1b]0;title\x07after"), "after");
        assert_eq!(decode(b"tab\tstays\x07"), "tab\tstays");
        assert_eq!(decode("цена 5 §".as_bytes()), "цена 5 §");
    }

    #[test]
    fn a_windows_code_page_line_is_decoded_instead_of_lost() {
        let cp1251 = encoding_rs::WINDOWS_1251.encode("Игрок Вася вошёл").0;
        let decoded = encoding_for_code_page(1251)
            .decode_without_bom_handling(&cp1251)
            .0;
        assert_eq!(decoded, "Игрок Вася вошёл");
        assert_eq!(encoding_for_code_page(866), encoding_rs::IBM866);
        assert_eq!(encoding_for_code_page(65001), encoding_rs::UTF_8);
        assert_eq!(encoding_for_code_page(1), encoding_rs::WINDOWS_1252);
    }

    #[test]
    fn invalid_utf8_never_ends_the_line() {
        let decoded = decode(b"caf\xe9 au lait\n");
        assert!(decoded.starts_with("caf"));
        assert!(decoded.ends_with(" au lait"));
    }

    #[test]
    fn an_endless_line_is_cut_for_display() {
        let long = "x".repeat(SHOWN_CHARS_LIMIT + 50);
        let decoded = decode(long.as_bytes());
        assert_eq!(decoded.chars().count(), SHOWN_CHARS_LIMIT + 1);
        assert!(decoded.ends_with('…'));
        assert_eq!(
            decode("y".repeat(SHOWN_CHARS_LIMIT).as_bytes())
                .chars()
                .count(),
            SHOWN_CHARS_LIMIT
        );
    }

    #[test]
    fn the_buffer_keeps_the_newest_lines_and_counts_what_it_dropped() {
        let mut log = GameLog::default();
        log.begin("big");
        for index in 0..LINE_LIMIT + 5 {
            log.record(Stream::Out, format!("line {index}"));
        }
        let snapshot = log.snapshot();
        assert_eq!(snapshot.lines.len(), LINE_LIMIT);
        assert_eq!(snapshot.dropped, 5);
        assert_eq!(snapshot.lines[0].text, "line 5");
        assert_eq!(snapshot.lines[0].seq, 5);
    }

    #[test]
    fn a_flood_of_long_lines_is_bounded_by_bytes_too() {
        let mut log = GameLog::default();
        log.begin("flood");
        let line = "x".repeat(SHOWN_CHARS_LIMIT);
        let fits = TEXT_LIMIT_BYTES / line.len();
        for _ in 0..fits + 10 {
            log.record(Stream::Out, line.clone());
        }
        let snapshot = log.snapshot();
        assert_eq!(snapshot.lines.len(), fits);
        assert_eq!(snapshot.dropped, 10);
        assert_eq!(snapshot.text_limit, TEXT_LIMIT_BYTES);
    }

    #[test]
    fn a_line_cut_inside_a_utf8_character_stays_utf8() {
        let whole = "Журнал JourneyMap: карта мира".as_bytes();
        let cut = &whole[..whole.len() - 1];
        let decoded = decode(cut);
        assert!(decoded.starts_with("Журнал JourneyMap: карта ми"));
    }

    #[test]
    fn unsent_hands_out_each_line_once() {
        let mut log = GameLog::default();
        log.begin("batch");
        log.record(Stream::Out, "a".into());
        log.record(Stream::Err, "b".into());
        let first: Vec<_> = log.unsent().into_iter().map(|line| line.text).collect();
        assert_eq!(first, ["a", "b"]);
        assert!(log.unsent().is_empty());
        log.record(Stream::Out, "c".into());
        let second: Vec<_> = log.unsent().into_iter().map(|line| line.seq).collect();
        assert_eq!(second, [2]);
    }

    #[test]
    fn a_new_game_starts_a_clean_session() {
        let mut log = GameLog::default();
        let first = log.begin("one");
        log.record(Stream::Out, "old".into());
        assert!(log.finish(first, 1, false));
        let second = log.begin("two");
        assert_eq!(second, first + 1);
        assert!(!log.finish(first, 0, false));
        let snapshot = log.snapshot();
        assert_eq!(snapshot.build, "two");
        assert_eq!(snapshot.status, GameStatus::Running);
        assert!(snapshot.lines.is_empty());
    }

    #[test]
    fn a_crash_report_takes_the_newest_lines_that_fit() {
        let mut log = GameLog::default();
        log.begin("tail");
        for text in ["first", "second", "third"] {
            log.record(Stream::Out, text.into());
        }
        assert_eq!(log.tail_text(13), "second\nthird");
        assert_eq!(log.tail_text(1024), "first\nsecond\nthird");
        let tail: Vec<_> = log.tail(2).into_iter().map(|line| line.text).collect();
        assert_eq!(tail, ["second", "third"]);
    }

    #[test]
    fn status_serialises_the_way_the_console_reads_it() {
        let exited = serde_json::to_value(GameStatus::Exited {
            code: 1,
            stopped: false,
        })
        .unwrap();
        assert_eq!(
            exited,
            serde_json::json!({ "state": "exited", "code": 1, "stopped": false })
        );
        let idle = serde_json::to_value(GameStatus::Idle).unwrap();
        assert_eq!(idle, serde_json::json!({ "state": "idle" }));
    }

    #[tokio::test]
    async fn the_pump_reads_every_line_of_a_stream_into_its_session() {
        let log = Mutex::new(GameLog::default());
        let session = log.lock().unwrap().begin("pump");
        let stream: &[u8] = b"[main/INFO]: one\r\n\xcf\xf0\xe8\xe2\xe5\xf2\nlast without newline";
        pump(stream, Stream::Out, &log, session).await;
        let texts: Vec<_> = log
            .lock()
            .unwrap()
            .snapshot()
            .lines
            .into_iter()
            .map(|line| line.text)
            .collect();
        assert_eq!(texts.len(), 3);
        assert_eq!(texts[0], "[main/INFO]: one");
        assert_eq!(texts[2], "last without newline");
    }

    #[tokio::test]
    async fn a_pump_from_an_older_game_does_not_write_into_the_new_one() {
        let log = Mutex::new(GameLog::default());
        let old = log.lock().unwrap().begin("old");
        log.lock().unwrap().begin("new");
        pump(&b"stray\n"[..], Stream::Out, &log, old).await;
        assert!(log.lock().unwrap().snapshot().lines.is_empty());
    }
}
