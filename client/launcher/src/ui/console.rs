use std::cell::{Cell, RefCell};
use std::collections::VecDeque;
use std::rc::Rc;
use std::time::Duration;

use laminara_core::gamelog::{GameLine, GameStatus, Level};
use slint::{ComponentHandle, Model, ModelNotify, ModelRc, ModelTracker, SharedString, StyledText};

use crate::console::{SharedLog, lock};
use crate::format;
use crate::{ConsoleLine, ConsoleStore, ConsoleWindow, LogPick};

const COPIED_FOR: Duration = Duration::from_millis(1600);

#[derive(Clone, Copy, PartialEq, Eq)]
enum Filter {
    All,
    Warn,
    Error,
}

impl Filter {
    fn from_index(index: i32) -> Self {
        match index {
            1 => Filter::Warn,
            2 => Filter::Error,
            _ => Filter::All,
        }
    }

    fn admits(self, level: Level) -> bool {
        match self {
            Filter::All => true,
            Filter::Warn => matches!(level, Level::Warn | Level::Error),
            Filter::Error => level == Level::Error,
        }
    }
}

#[derive(Default, Clone, Copy)]
struct Counts {
    debug: i64,
    info: i64,
    warn: i64,
    error: i64,
}

impl Counts {
    fn add(&mut self, level: Level) {
        *self.slot(level) += 1;
    }

    fn remove(&mut self, level: Level) {
        *self.slot(level) -= 1;
    }

    fn slot(&mut self, level: Level) -> &mut i64 {
        match level {
            Level::Debug => &mut self.debug,
            Level::Info => &mut self.info,
            Level::Warn => &mut self.warn,
            Level::Error => &mut self.error,
        }
    }

    fn all(&self) -> i64 {
        self.debug + self.info + self.warn + self.error
    }
}

struct LogRows {
    log: SharedLog,
    session: Cell<u64>,
    first_seq: Cell<u64>,
    next_seq: Cell<u64>,
    filter: Cell<Filter>,
    needle: RefCell<String>,
    matches: RefCell<Option<VecDeque<u64>>>,
    levels: RefCell<VecDeque<Level>>,
    shifted: Cell<usize>,
    counts: Cell<Counts>,
    accent: String,
    notify: ModelNotify,
}

impl LogRows {
    fn new(log: SharedLog, accent: String) -> Self {
        let rows = LogRows {
            log,
            session: Cell::new(0),
            first_seq: Cell::new(0),
            levels: RefCell::new(VecDeque::new()),
            shifted: Cell::new(0),
            next_seq: Cell::new(0),
            filter: Cell::new(Filter::All),
            needle: RefCell::new(String::new()),
            matches: RefCell::new(None),
            counts: Cell::new(Counts::default()),
            accent,
            notify: ModelNotify::default(),
        };
        rows.rebuild();
        rows
    }

    fn narrowed(&self) -> bool {
        self.filter.get() != Filter::All || !self.needle.borrow().is_empty()
    }

    fn admits(&self, line: &GameLine) -> bool {
        let needle = self.needle.borrow();
        self.filter.get().admits(line.level)
            && (needle.is_empty() || line.text.to_lowercase().contains(needle.as_str()))
    }

    fn rebuild(&self) {
        let log = lock(&self.log);
        let lines = log.lines();
        let first = log.next_seq() - lines.len() as u64;
        let mut counts = Counts::default();
        for line in lines {
            counts.add(line.level);
        }
        self.counts.set(counts);
        *self.levels.borrow_mut() = lines.iter().map(|line| line.level).collect();
        self.session.set(log.session());
        self.first_seq.set(first);
        self.next_seq.set(log.next_seq());
        let matches = self.narrowed().then(|| {
            lines
                .iter()
                .filter(|line| self.admits(line))
                .map(|line| line.seq)
                .collect()
        });
        drop(log);
        *self.matches.borrow_mut() = matches;
        self.notify.reset();
    }

    fn catch_up(&self) -> usize {
        let log = lock(&self.log);
        let lines = log.lines();
        let first = log.next_seq() - lines.len() as u64;
        let reset = log.session() != self.session.get()
            || first < self.first_seq.get()
            || first > self.next_seq.get()
            || log.next_seq() < self.next_seq.get();
        if reset {
            drop(log);
            let before = self.row_count();
            self.rebuild();
            return self.row_count().saturating_sub(before);
        }
        let mut counts = self.counts.get();
        let evicted = self.evict_before(first, &mut counts);
        self.shifted.set(self.shifted.get() + evicted);
        let from = (self.next_seq.get() - first) as usize;
        let fresh: Vec<&GameLine> = lines.iter().skip(from).collect();
        if fresh.is_empty() {
            self.counts.set(counts);
            drop(log);
            if evicted > 0 {
                self.notify.row_removed(0, evicted);
            }
            return 0;
        }
        let mut levels = self.levels.borrow_mut();
        for line in &fresh {
            counts.add(line.level);
            levels.push_back(line.level);
        }
        drop(levels);
        self.counts.set(counts);
        self.next_seq.set(log.next_seq());
        let before = self.row_count();
        let added = if self.narrowed() {
            let picked: Vec<u64> = fresh
                .iter()
                .filter(|line| self.admits(line))
                .map(|line| line.seq)
                .collect();
            let count = picked.len();
            if let Some(matches) = self.matches.borrow_mut().as_mut() {
                matches.extend(picked);
            }
            count
        } else {
            fresh.len()
        };
        drop(log);
        if evicted > 0 {
            self.notify.row_removed(0, evicted);
        }
        if added > 0 {
            self.notify.row_added(before, added);
        }
        added
    }

    fn evict_before(&self, first: u64, counts: &mut Counts) -> usize {
        let gone = (first - self.first_seq.get()) as usize;
        if gone == 0 {
            return 0;
        }
        let mut levels = self.levels.borrow_mut();
        let held = gone.min(levels.len());
        for level in levels.drain(..held) {
            counts.remove(level);
        }
        self.first_seq.set(first);
        match self.matches.borrow_mut().as_mut() {
            Some(matches) => {
                let dropped = matches.partition_point(|seq| *seq < first);
                matches.drain(..dropped);
                dropped
            }
            None => gone,
        }
    }

    fn line_at(&self, row: usize) -> Option<GameLine> {
        let log = lock(&self.log);
        let lines = log.lines();
        let first = log.next_seq() - lines.len() as u64;
        let seq = match self.matches.borrow().as_ref() {
            Some(matches) => *matches.get(row)?,
            None => first + row as u64,
        };
        lines.get(seq.checked_sub(first)? as usize).cloned()
    }

    fn visible_text(&self) -> String {
        (0..self.row_count())
            .filter_map(|row| self.line_at(row))
            .map(|line| line.text)
            .collect::<Vec<_>>()
            .join("\n")
    }
}

impl Model for LogRows {
    type Data = ConsoleLine;

    fn row_count(&self) -> usize {
        match self.matches.borrow().as_ref() {
            Some(matches) => matches.len(),
            None => (self.next_seq.get() - self.first_seq.get()) as usize,
        }
    }

    fn row_data(&self, row: usize) -> Option<ConsoleLine> {
        let line = self.line_at(row)?;
        let needle = self.needle.borrow();
        let hit = !needle.is_empty() && line.text.to_lowercase().contains(needle.as_str());
        let rich = if hit {
            highlighted(&line.text, &needle, &self.accent)
        } else {
            StyledText::default()
        };
        Some(ConsoleLine {
            text: SharedString::from(line.text.as_str()),
            level: level_index(line.level),
            hit,
            rich,
        })
    }

    fn model_tracker(&self) -> &dyn ModelTracker {
        &self.notify
    }
}

pub(super) fn level_index(level: Level) -> i32 {
    match level {
        Level::Debug => 0,
        Level::Info => 1,
        Level::Warn => 2,
        Level::Error => 3,
    }
}

fn escape_markdown(text: &str) -> String {
    let mut escaped = String::with_capacity(text.len() * 2);
    for character in text.chars() {
        match character {
            ' ' => escaped.push_str("&#32;"),
            '\t' => escaped.push_str("&#32;&#32;&#32;&#32;"),
            '&' => escaped.push_str("&amp;"),
            '<' => escaped.push_str("&lt;"),
            '>' => escaped.push_str("&gt;"),
            c if c.is_ascii_punctuation() => {
                escaped.push('\\');
                escaped.push(c);
            }
            c => escaped.push(c),
        }
    }
    escaped
}

pub fn picked_rows(anchor: i32, focus: i32) -> impl Iterator<Item = usize> {
    let to = anchor.max(focus);
    usize::try_from(anchor.min(focus))
        .ok()
        .into_iter()
        .flat_map(move |from| from..=to as usize)
}

fn found_spans(text: &str, needle: &str) -> Vec<(usize, usize)> {
    if needle.is_empty() {
        return Vec::new();
    }
    let mut lower = String::with_capacity(text.len());
    let mut origin = Vec::with_capacity(text.len() + 1);
    for (at, character) in text.char_indices() {
        for folded in character.to_lowercase() {
            origin.extend(std::iter::repeat_n(at, folded.len_utf8()));
            lower.push(folded);
        }
    }
    origin.push(text.len());
    let mut spans = Vec::new();
    let mut taken = 0;
    let mut searched = 0;
    while let Some(found) = lower[searched..].find(needle) {
        let start = origin[searched + found];
        let end = origin[searched + found + needle.len()];
        searched += found + needle.len();
        if end > start && start >= taken {
            spans.push((start, end));
            taken = end;
        }
    }
    spans
}

fn highlighted(text: &str, needle: &str, accent: &str) -> StyledText {
    let mut markdown = String::new();
    let mut copied = 0;
    for (start, end) in found_spans(text, needle) {
        markdown.push_str(&escape_markdown(&text[copied..start]));
        markdown.push_str(&format!("<font color=\"{accent}\"><u>"));
        markdown.push_str(&escape_markdown(&text[start..end]));
        markdown.push_str("</u></font>");
        copied = end;
    }
    markdown.push_str(&escape_markdown(&text[copied..]));
    StyledText::from_markdown(&markdown).unwrap_or_default()
}

pub struct ConsoleView {
    window: ConsoleWindow,
    rows: Rc<LogRows>,
    following: Cell<bool>,
    unseen: Cell<usize>,
    copied_timer: slint::Timer,
}

impl ConsoleView {
    pub fn new(log: SharedLog, accent: String) -> Result<Rc<ConsoleView>, slint::PlatformError> {
        let window = ConsoleWindow::new()?;
        let rows = Rc::new(LogRows::new(log, accent));
        window
            .global::<ConsoleStore>()
            .set_lines(ModelRc::from(rows.clone()));
        let view = Rc::new(ConsoleView {
            window,
            rows,
            following: Cell::new(true),
            unseen: Cell::new(0),
            copied_timer: slint::Timer::default(),
        });
        view.bind();
        view.window.global::<ConsoleStore>().set_ready(true);
        view.present();
        Ok(view)
    }

    pub fn show(&self) -> Result<(), slint::PlatformError> {
        self.window.show()
    }

    pub fn raise(&self) {
        let _ = self.window.show();
        self.window.window().set_minimized(false);
        use slint::winit_030::WinitWindowAccessor;
        self.window
            .window()
            .with_winit_window(|window| window.focus_window());
    }

    pub fn close(&self) {
        let _ = self.window.hide();
    }

    fn bind(self: &Rc<Self>) {
        let store = self.window.global::<ConsoleStore>();
        let weak = Rc::downgrade(self);
        store.on_search(move |query| {
            if let Some(view) = weak.upgrade() {
                *view.rows.needle.borrow_mut() = query.trim().to_lowercase();
                view.reframe();
            }
        });
        let weak = Rc::downgrade(self);
        store.on_set_filter(move |index| {
            if let Some(view) = weak.upgrade() {
                view.rows.filter.set(Filter::from_index(index));
                view.window.global::<ConsoleStore>().set_filter(index);
                view.reframe();
            }
        });
        let weak = Rc::downgrade(self);
        store.on_show_everything(move || {
            if let Some(view) = weak.upgrade() {
                view.rows.needle.borrow_mut().clear();
                view.rows.filter.set(Filter::All);
                let store = view.window.global::<ConsoleStore>();
                store.set_query("".into());
                store.set_filter(0);
                view.reframe();
            }
        });
        let weak = Rc::downgrade(self);
        store.on_following(move |following| {
            if let Some(view) = weak.upgrade() {
                view.following.set(following);
                if following {
                    view.unseen.set(0);
                }
                view.present();
            }
        });
        let weak = Rc::downgrade(self);
        store.on_dismiss_trouble(move || {
            if let Some(view) = weak.upgrade() {
                view.window.global::<ConsoleStore>().set_trouble("".into());
            }
        });
    }

    pub fn visible_text(&self) -> String {
        self.rows.visible_text()
    }

    pub fn picked_text(&self) -> String {
        let pick = self.window.global::<LogPick>();
        picked_rows(pick.get_anchor(), pick.get_focus())
            .filter_map(|row| self.rows.line_at(row))
            .map(|line| line.text)
            .collect::<Vec<_>>()
            .join("\n")
    }

    pub fn mark_copied(&self) {
        let store = self.window.global::<ConsoleStore>();
        store.set_copied(true);
        let weak = self.window.as_weak();
        self.copied_timer
            .start(slint::TimerMode::SingleShot, COPIED_FOR, move || {
                if let Some(window) = weak.upgrade() {
                    window.global::<ConsoleStore>().set_copied(false);
                }
            });
    }

    pub fn trouble(&self, message: String) {
        self.window
            .global::<ConsoleStore>()
            .set_trouble(message.into());
    }

    pub fn clear_trouble(&self) {
        self.window.global::<ConsoleStore>().set_trouble("".into());
    }

    fn reframe(&self) {
        self.unseen.set(0);
        self.rows.rebuild();
        self.window.global::<LogPick>().invoke_clear();
        self.present();
    }

    pub fn restarted(&self) {
        self.rows.rebuild();
        self.window.global::<LogPick>().invoke_clear();
        self.unseen.set(0);
        self.window.invoke_restart();
        self.present();
    }

    pub fn grew(&self) {
        let added = self.rows.catch_up();
        let shifted = self.rows.shifted.replace(0);
        if shifted > 0 {
            self.window.global::<LogPick>().invoke_shift(shifted as i32);
        }
        if !self.following.get() {
            self.unseen.set(self.unseen.get() + added);
        }
        self.present();
    }

    pub fn present(&self) {
        let store = self.window.global::<ConsoleStore>();
        let (session, build, status, dropped) = {
            let log = lock(&self.rows.log);
            (
                log.session(),
                log.build().to_string(),
                log.status(),
                log.dropped(),
            )
        };
        let counts = self.rows.counts.get();
        let total = counts.all();
        let shown = self.rows.row_count() as i64;
        let (label, tone) = describe(status);
        let started = session != 0 && !build.is_empty();
        let (label, tone) = if started {
            (label, tone)
        } else {
            describe(GameStatus::Idle)
        };
        store.set_build(build.into());
        store.set_status_label(label.into());
        store.set_status_tone(tone);
        store.set_started(started);
        store.set_running(started && status == GameStatus::Running);
        store.set_total(total as i32);
        store.set_shown(shown as i32);
        store.set_count_all(format::count(total).into());
        store.set_count_warn(format::count(counts.warn + counts.error).into());
        store.set_count_error(format::count(counts.error).into());
        store.set_has_warn(counts.warn + counts.error > 0);
        store.set_has_error(counts.error > 0);
        let mut summary = format!(
            "{} {}",
            format::count(total),
            format::plural(total, "строка", "строки", "строк")
        );
        if self.rows.narrowed() {
            let verb = if self.rows.needle.borrow().is_empty() {
                "показано"
            } else {
                "найдено"
            };
            summary.push_str(&format!(" · {verb} {}", format::count(shown)));
        }
        if dropped > 0 {
            summary.push_str(&format!(
                " · первые {} уже не помещаются, весь журнал — в logs/latest.log",
                format::count(dropped as i64)
            ));
        }
        store.set_summary(summary.into());
        let unseen = self.unseen.get() as i64;
        let pill = if unseen > 0 {
            format!(
                "{} {}",
                format::count(unseen),
                format::plural(unseen, "новая строка", "новые строки", "новых строк")
            )
        } else {
            "К последним строкам".into()
        };
        store.set_pill_label(pill.into());
        if cfg!(target_os = "macos") {
            store.set_shortcut("⌘ F".into());
        }
    }

    pub fn window(&self) -> &ConsoleWindow {
        &self.window
    }
}

fn describe(status: GameStatus) -> (String, i32) {
    match status {
        GameStatus::Running => ("Игра запущена".into(), 0),
        GameStatus::Idle => ("Игра не запущена".into(), 1),
        GameStatus::Exited { stopped: true, .. } => ("Игра остановлена".into(), 1),
        GameStatus::Exited { code: 0, .. } => ("Игра закрыта".into(), 1),
        GameStatus::Exited { code, .. } => (format!("Игра завершилась с ошибкой · код {code}"), 2),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use laminara_core::gamelog::{GameLog, LINE_LIMIT, Stream};
    use std::sync::{Arc, Mutex};

    fn full_log() -> SharedLog {
        let log = Arc::new(Mutex::new(GameLog::default()));
        let mut guard = lock(&log);
        guard.begin("big");
        for index in 0..LINE_LIMIT {
            guard.record(Stream::Out, format!("[main/INFO]: line {index}"));
        }
        drop(guard);
        log
    }

    #[test]
    fn a_full_log_slides_forward_without_starting_over() {
        let log = full_log();
        let rows = LogRows::new(log.clone(), String::new());
        for index in 0..3 {
            lock(&log).record(Stream::Out, format!("[main/ERROR]: late {index}"));
        }
        assert_eq!(rows.catch_up(), 3);
        assert_eq!(rows.row_count(), LINE_LIMIT);
        assert_eq!(rows.first_seq.get(), 3);
        assert_eq!(rows.counts.get().all(), LINE_LIMIT as i64);
        assert_eq!(rows.counts.get().error, 3);
        assert_eq!(
            rows.line_at(LINE_LIMIT - 1).map(|line| line.text),
            Some("[main/ERROR]: late 2".to_string())
        );
    }

    #[test]
    fn a_narrowed_view_drops_only_the_matches_that_left_the_buffer() {
        let log = full_log();
        let rows = LogRows::new(log.clone(), String::new());
        *rows.needle.borrow_mut() = "line 1".into();
        rows.rebuild();
        let before = rows.row_count();
        lock(&log).record(Stream::Out, "[main/INFO]: unrelated".into());
        lock(&log).record(Stream::Out, "[main/INFO]: unrelated".into());
        assert_eq!(rows.catch_up(), 0);
        assert_eq!(rows.row_count(), before - 1);
        assert_eq!(
            rows.line_at(0).map(|line| line.text),
            Some("[main/INFO]: line 10".to_string())
        );
    }

    #[test]
    fn spans_point_at_the_original_text_even_when_lowercase_changes_byte_lengths() {
        let text = "\u{23a}\u{2126} and \u{2126}";
        let spans = found_spans(text, "\u{3c9}");
        assert_eq!(spans.len(), 2);
        for (start, end) in spans {
            assert_eq!(&text[start..end], "\u{2126}");
        }
    }

    #[test]
    fn a_pick_covers_rows_between_its_ends_in_either_direction() {
        assert_eq!(picked_rows(2, 4).collect::<Vec<_>>(), vec![2, 3, 4]);
        assert_eq!(picked_rows(4, 2).collect::<Vec<_>>(), vec![2, 3, 4]);
        assert_eq!(picked_rows(-1, -1).count(), 0);
    }

    #[test]
    fn plain_ascii_matches_ignore_case() {
        assert_eq!(found_spans("Error: ERROR", "error"), vec![(0, 5), (7, 12)]);
        assert!(found_spans("anything", "").is_empty());
    }
}
