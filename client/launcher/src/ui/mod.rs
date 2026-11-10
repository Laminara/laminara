mod console;
mod features;
mod palette;
pub mod scene;

use std::cell::RefCell;
use std::collections::HashSet;
use std::future::Future;
use std::path::PathBuf;
use std::rc::Rc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, PoisonError};
use std::time::{Duration, Instant};

use laminara_core::config::SelectedAccount;
use laminara_core::gamelog::GameLine;
use laminara_core::sync::{SyncProgress, SyncStage};
use slint::{ComponentHandle, ModelRc, SharedString, VecModel};

use crate::accounts::{self, AccountOptions};
use crate::commands::{
    self, BuildDto, Failure, InstallState, LoginError, NewsItemDto, PlayerCounts, SyncEvent,
    UpdateDto,
};
use crate::console::GameExit;
use crate::runtime::on_core;
use crate::setup::{AppState, embedded_branding};
use crate::{
    AccountItem, BuildItem, ConsoleStore, Install, LibraryRow, LogPick, LogRow, MainWindow, Media,
    ModalKind, NewsItem, NewsPair, Phase, Report, Store, Theme, format, media,
};

use self::console::{ConsoleView, level_index};
use self::features::FeatureEditor;
use self::scene::{FACE_PIXELS, MODAL_FROST, SYNC_FROST, replace_rows};

const NEWS_SEEN_FILE: &str = "news-seen.json";
const FACE_RECHECK: Duration = Duration::from_secs(120);
const PLAYERS_EVERY: Duration = Duration::from_secs(30);
const BUILDS_EVERY: Duration = Duration::from_secs(20);
const CRASH_LOG_LINES: usize = 40;
const RATE_WINDOW: f64 = 0.35;
const LOGO_PIXELS: u32 = 72;
const LOGO_RADIUS: f32 = 28.0;

thread_local! {
    static APP: RefCell<Option<Rc<App>>> = const { RefCell::new(None) };
}

fn with_app(work: impl FnOnce(&Rc<App>)) {
    let app = APP.with(|slot| slot.borrow().clone());
    if let Some(app) = app {
        work(&app);
    }
}

fn on_ui(work: impl FnOnce(&Rc<App>) + Send + 'static) {
    let _ = slint::invoke_from_event_loop(move || with_app(work));
}

fn latest_on_ui<T: Send + 'static>(
    apply: impl Fn(&Rc<App>, T) + Send + Sync + 'static,
) -> impl Fn(T) + Send + Sync + 'static {
    let latest: Arc<Mutex<Option<T>>> = Arc::new(Mutex::new(None));
    let pending = Arc::new(AtomicBool::new(false));
    let apply = Arc::new(apply);
    move |value: T| {
        *latest.lock().unwrap_or_else(PoisonError::into_inner) = Some(value);
        if !pending.swap(true, Ordering::AcqRel) {
            let latest = latest.clone();
            let pending = pending.clone();
            let apply = apply.clone();
            on_ui(move |app| {
                pending.store(false, Ordering::Release);
                let value = latest.lock().unwrap_or_else(PoisonError::into_inner).take();
                if let Some(value) = value {
                    apply(app, value);
                }
            });
        }
    }
}

pub fn game_started(_session: u64, _build: String) {
    on_ui(|app| {
        if let Some(view) = app.console.borrow().as_ref() {
            view.restarted();
        }
    });
}

pub fn game_log_grew() {
    on_ui(|app| {
        if let Some(view) = app.console.borrow().as_ref() {
            view.grew();
        }
    });
}

pub fn game_exited(exit: GameExit) {
    on_ui(move |app| {
        if let Some(view) = app.console.borrow().as_ref() {
            view.grew();
        }
        app.game_exited(exit);
    });
}

pub fn open_game_console() {
    on_ui(|app| app.open_console());
}

pub fn quit() {
    let _ = slint::invoke_from_event_loop(|| {
        let _ = slint::quit_event_loop();
    });
}

#[derive(Clone)]
struct Problem {
    message: String,
    retry: bool,
}

impl Problem {
    fn plain(message: impl Into<String>) -> Self {
        Problem {
            message: message.into(),
            retry: false,
        }
    }
}

impl From<Failure> for Problem {
    fn from(failure: Failure) -> Self {
        Problem {
            message: failure.message,
            retry: failure.retry,
        }
    }
}

enum LogReport {
    Idle,
    Sending,
    Sent(String),
    Failed(String),
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum SyncShown {
    Planning,
    Downloading,
    Done,
    Launching,
}

struct SyncView {
    stage: SyncShown,
    files_done: u64,
    files_total: u64,
    bytes_done: u64,
    bytes_total: u64,
    current_path: Option<String>,
    bytes_per_second: Option<f64>,
    seconds_left: Option<f64>,
}

struct RateMeter {
    last_bytes: Option<u64>,
    last_at: Instant,
    speed: f64,
}

impl RateMeter {
    fn new() -> Self {
        RateMeter {
            last_bytes: None,
            last_at: Instant::now(),
            speed: 0.0,
        }
    }

    fn sample(&mut self, bytes_done: u64, bytes_total: u64) -> (Option<f64>, Option<f64>) {
        let now = Instant::now();
        if self.last_bytes.is_none_or(|last| bytes_done < last) {
            self.last_bytes = Some(bytes_done);
            self.last_at = now;
        }
        let seconds = now.duration_since(self.last_at).as_secs_f64();
        let gained = bytes_done.saturating_sub(self.last_bytes.unwrap_or(bytes_done)) as f64;
        if seconds >= RATE_WINDOW {
            let sample = gained / seconds;
            self.speed = if self.speed == 0.0 {
                sample
            } else {
                self.speed * 0.7 + sample * 0.3
            };
            self.last_bytes = Some(bytes_done);
            self.last_at = now;
        }
        if self.speed <= 0.0 {
            return (None, None);
        }
        let left = bytes_total.saturating_sub(bytes_done) as f64;
        (Some(self.speed), Some(left / self.speed))
    }
}

struct Crash {
    session: u64,
    code: i32,
    log: Vec<GameLine>,
    build: String,
    loader: String,
    version: String,
}

#[derive(Clone)]
struct Account {
    uuid: String,
    name: String,
}

struct ModelState {
    phase: Phase,
    epoch: u64,
    busy: bool,
    accounts: AccountOptions,
    accounts_request: u64,
    accounts_loading: bool,
    accounts_error: Option<String>,
    account: Option<Account>,
    face: Option<String>,
    face_image: Option<slint::Image>,
    face_checked: Option<Instant>,
    builds: Vec<BuildDto>,
    selected: Option<String>,
    sync: Option<SyncView>,
    players: Option<PlayerCounts>,
    news: Vec<NewsItemDto>,
    news_banners: Vec<Option<slint::Image>>,
    unread: usize,
    error: Option<Problem>,
    report: LogReport,
    two_factor: bool,
    login_error: Option<String>,
    modal: ModalKind,
    modal_open: bool,
    modal_profile: String,
    modal_ticket: u64,
    menu_open: bool,
    crash: Option<Crash>,
    crash_open: bool,
    crash_sending: bool,
    crash_sent: Option<String>,
    crash_error: Option<String>,
    cancel_requested: bool,
    sync_run: u64,
    running_session: Option<u64>,
    last_exit: Option<GameExit>,
    stale: Vec<String>,
    update: Option<UpdateDto>,
    update_progress: Option<(u64, u64)>,
    update_dismissed: bool,
    general_loaded: bool,
    cleaned: Option<u64>,
    dialog_trouble: Option<String>,
    build_loaded: bool,
    memory_ceiling: u32,
    loaded_memory: Option<u32>,
    features: Option<FeatureEditor>,
    features_loaded: bool,
    library_query: String,
    meter: RateMeter,
}

impl Default for ModelState {
    fn default() -> Self {
        ModelState {
            phase: Phase::Connecting,
            epoch: 0,
            busy: false,
            accounts: AccountOptions {
                multi_account: false,
                max_accounts: 1,
                accounts: Vec::new(),
            },
            accounts_request: 0,
            accounts_loading: false,
            accounts_error: None,
            account: None,
            face: None,
            face_image: None,
            face_checked: None,
            builds: Vec::new(),
            selected: None,
            sync: None,
            players: None,
            news: Vec::new(),
            news_banners: Vec::new(),
            unread: 0,
            error: None,
            report: LogReport::Idle,
            two_factor: false,
            login_error: None,
            modal: ModalKind::None,
            modal_open: false,
            modal_profile: String::new(),
            modal_ticket: 0,
            menu_open: false,
            crash: None,
            crash_open: false,
            crash_sending: false,
            crash_sent: None,
            crash_error: None,
            cancel_requested: false,
            sync_run: 0,
            running_session: None,
            last_exit: None,
            stale: Vec::new(),
            update: None,
            update_progress: None,
            update_dismissed: false,
            general_loaded: false,
            cleaned: None,
            dialog_trouble: None,
            build_loaded: false,
            memory_ceiling: 16384,
            loaded_memory: None,
            features: None,
            features_loaded: false,
            library_query: String::new(),
            meter: RateMeter::new(),
        }
    }
}

struct Lists {
    builds: Rc<VecModel<BuildItem>>,
    news: Rc<VecModel<NewsItem>>,
    news_rest: Rc<VecModel<NewsItem>>,
    news_pairs: Rc<VecModel<NewsPair>>,
    accounts: Rc<VecModel<AccountItem>>,
    feature_rows: Rc<VecModel<crate::FeatureRow>>,
    library: Rc<VecModel<LibraryRow>>,
    crash_log: Rc<VecModel<LogRow>>,
}

impl Lists {
    fn new() -> Self {
        Lists {
            builds: Rc::new(VecModel::default()),
            news: Rc::new(VecModel::default()),
            news_rest: Rc::new(VecModel::default()),
            news_pairs: Rc::new(VecModel::default()),
            accounts: Rc::new(VecModel::default()),
            feature_rows: Rc::new(VecModel::default()),
            library: Rc::new(VecModel::default()),
            crash_log: Rc::new(VecModel::default()),
        }
    }
}

fn webview_launcher_was_here() -> bool {
    let service = laminara_core::KEYRING_SERVICE;
    let profiles = [
        dirs::data_local_dir().map(|dir| dir.join(service)),
        dirs::home_dir().map(|home| home.join("Library").join("WebKit").join(service)),
    ];
    profiles
        .into_iter()
        .flatten()
        .any(|profile| profile.is_dir())
}

pub struct App {
    window: MainWindow,
    state: Arc<AppState>,
    model: RefCell<ModelState>,
    lists: Lists,
    console: RefCell<Option<Rc<ConsoleView>>>,
    clipboard: RefCell<Option<arboard::Clipboard>>,
    hero_player: RefCell<Option<crate::motion::HeroPlayer>>,
    timers: [slint::Timer; 2],
    seen_file: PathBuf,
}

pub fn run(state: Arc<AppState>) -> Result<(), slint::PlatformError> {
    let window = MainWindow::new()?;
    let app = Rc::new(App {
        seen_file: state.data_dir.join(NEWS_SEEN_FILE),
        window,
        state,
        model: RefCell::new(ModelState::default()),
        lists: Lists::new(),
        console: RefCell::new(None),
        clipboard: RefCell::new(None),
        hero_player: RefCell::new(None),
        timers: [slint::Timer::default(), slint::Timer::default()],
    });
    APP.with(|slot| *slot.borrow_mut() = Some(app.clone()));
    app.wear_branding();
    app.attach_lists();
    app.bind();
    app.watch_focus();
    app.render();
    app.window.show()?;
    app.spawn(|app| async move { app.init().await });
    slint::run_event_loop_until_quit()?;
    if let Some(view) = app.console.borrow_mut().take() {
        view.close();
    }
    let _ = app.window.hide();
    APP.with(|slot| slot.borrow_mut().take());
    Ok(())
}

impl App {
    fn spawn<F, Fut>(self: &Rc<Self>, task: F)
    where
        F: FnOnce(Rc<App>) -> Fut,
        Fut: Future<Output = ()> + 'static,
    {
        let future = task(self.clone());
        if let Err(error) = slint::spawn_local(future) {
            tracing::error!(%error, "задача интерфейса не запустилась");
        }
    }

    fn store(&self) -> Store<'_> {
        self.window.global::<Store>()
    }

    fn show_hero(&self, frame: &media::HeroFrame) {
        let media = self.window.global::<Media>();
        media.set_hero(frame.sharp());
        media.set_hero_blurred(frame.blurred());
    }

    fn pace_hero(&self) {
        let player = self.hero_player.borrow();
        let Some(player) = player.as_ref() else {
            return;
        };
        let covered = {
            let model = self.model.borrow();
            model.modal_open
                || model.crash_open
                || matches!(model.phase, Phase::Syncing | Phase::Running)
        };
        player.set_moving(!covered && !self.window.window().is_minimized());
    }

    fn wear_branding(&self) {
        let branding = embedded_branding();
        let palette = palette::from_branding(branding);
        let theme = self.window.global::<Theme>();
        if let Some(bg) = palette.bg {
            theme.set_bg(bg);
        }
        theme.set_login_surface(palette::login_surface(theme.get_bg()));
        if let Some(primary) = palette.primary {
            theme.set_primary(primary);
        }
        if let Some(strong) = palette.primary_strong {
            theme.set_primary_strong(strong);
        }
        if let Some(ink) = palette.primary_ink {
            theme.set_primary_ink(ink);
        }
        let hero = media::hero(&branding.hero_media_data_uri.take());
        let logo = (!branding.logo_data_uri.is_empty())
            .then(|| media::logo(&branding.logo_data_uri, LOGO_PIXELS, LOGO_RADIUS))
            .flatten();
        if !branding.logo_data_uri.is_empty() && logo.is_none() {
            tracing::warn!(
                "логотип проекта не разобрался как картинка — остаётся знак по умолчанию"
            );
        }
        let media = self.window.global::<Media>();
        self.show_hero(&hero.still);
        if let Some(motion) = hero.motion {
            let player = motion.play(|frame| {
                let _ =
                    slint::invoke_from_event_loop(move || with_app(|app| app.show_hero(&frame)));
            });
            *self.hero_player.borrow_mut() = Some(player);
        }
        media.set_has_logo(logo.is_some());
        if let Some(logo) = logo {
            media.set_logo(logo.full);
            if let Some(mark) = logo.mark {
                media.set_logo_mark(mark);
            }
        }
        if !branding.name.trim().is_empty() {
            media.set_name(branding.name.trim().into());
        }
        media.set_tagline(branding.tagline.trim().into());
        if !branding.window_title.trim().is_empty() {
            media.set_title(branding.window_title.trim().into());
        }
    }

    fn dress_console(&self, view: &ConsoleView) {
        let main_theme = self.window.global::<Theme>();
        let theme = view.window().global::<Theme>();
        theme.set_bg(main_theme.get_bg());
        theme.set_primary(main_theme.get_primary());
        theme.set_primary_strong(main_theme.get_primary_strong());
        theme.set_primary_ink(main_theme.get_primary_ink());
        let main_media = self.window.global::<Media>();
        let media = view.window().global::<Media>();
        media.set_title(main_media.get_title());
        media.set_has_logo(main_media.get_has_logo());
        media.set_logo(main_media.get_logo());
    }

    fn attach_lists(&self) {
        let store = self.store();
        store.set_builds(ModelRc::from(self.lists.builds.clone()));
        store.set_news(ModelRc::from(self.lists.news.clone()));
        store.set_news_rest(ModelRc::from(self.lists.news_rest.clone()));
        store.set_news_pairs(ModelRc::from(self.lists.news_pairs.clone()));
        store.set_accounts(ModelRc::from(self.lists.accounts.clone()));
        store.set_feature_rows(ModelRc::from(self.lists.feature_rows.clone()));
        store.set_library(ModelRc::from(self.lists.library.clone()));
        store.set_crash_log(ModelRc::from(self.lists.crash_log.clone()));
        store.set_version(env!("LAMINARA_VERSION").into());
    }

    fn settle_window(&self) -> bool {
        use slint::winit_030::{WinitWindowAccessor, winit};
        let settled = self
            .window
            .window()
            .with_winit_window(|window: &winit::window::Window| {
                window.set_enabled_buttons(
                    winit::window::WindowButtons::CLOSE | winit::window::WindowButtons::MINIMIZE,
                );
                let Some(monitor) = window
                    .current_monitor()
                    .or_else(|| window.primary_monitor())
                else {
                    return;
                };
                let (area, origin, outer) =
                    (monitor.size(), monitor.position(), window.outer_size());
                let centred = |start: i32, room: u32, size: u32| {
                    start + (room.saturating_sub(size) / 2) as i32
                };
                window.set_outer_position(winit::dpi::PhysicalPosition::new(
                    centred(origin.x, area.width, outer.width),
                    centred(origin.y, area.height, outer.height),
                ));
            });
        settled.is_some()
    }

    fn watch_focus(self: &Rc<Self>) {
        use slint::winit_030::{EventResult, WinitWindowAccessor, winit};
        let weak = Rc::downgrade(self);
        let settled = std::cell::Cell::new(false);
        self.window.window().on_winit_window_event(move |_, event| {
            if !settled.get()
                && let Some(app) = weak.upgrade()
            {
                settled.set(app.settle_window());
            }
            if matches!(
                event,
                winit::event::WindowEvent::Resized(_) | winit::event::WindowEvent::Occluded(_)
            ) && let Some(app) = weak.upgrade()
            {
                app.pace_hero();
            }
            if let winit::event::WindowEvent::Focused(true) = event
                && let Some(app) = weak.upgrade()
                && app.is_home()
                && !app.window.window().is_minimized()
            {
                app.spawn(|app| async move { app.refresh_builds().await });
                app.spawn(|app| async move { app.refresh_face(false).await });
            }
            EventResult::Propagate
        });
        let weak = Rc::downgrade(self);
        self.window.window().on_close_requested(move || {
            if weak.upgrade().is_some() {
                let _ = slint::quit_event_loop();
            }
            slint::CloseRequestResponse::HideWindow
        });
    }

    fn is_home(&self) -> bool {
        !matches!(
            self.model.borrow().phase,
            Phase::Connecting | Phase::Login | Phase::Updating
        )
    }

    fn bind(self: &Rc<Self>) {
        scene::bind_formatters(&self.window);
        let store = self.store();
        macro_rules! on {
            ($setter:ident, |$app:ident $(, $arg:ident)*| $body:expr) => {{
                let weak = Rc::downgrade(self);
                store.$setter(move |$($arg),*| {
                    if let Some($app) = weak.upgrade() {
                        $body;
                    }
                });
            }};
        }
        on!(on_login, |app, user, pass, code| app.spawn(
            move |app| async move {
                app.login(user.to_string(), pass.to_string(), code.to_string())
                    .await
            }
        ));
        on!(on_login_edited, |app| app.dismiss_error());
        on!(on_logout, |app| app
            .spawn(|app| async move { app.logout().await }));
        on!(on_toggle_menu, |app| app.toggle_menu());
        on!(on_close_menu, |app| app.close_menu());
        on!(on_open_modal, |app, kind| app.open_modal(kind));
        on!(on_close_modal, |app| app.close_modal());
        on!(on_select, |app, name| app.select(name.to_string()));
        on!(on_play, |app| app
            .spawn(|app| async move { app.play().await }));
        on!(on_cancel_sync, |app| app
            .spawn(|app| async move { app.cancel_sync().await }));
        on!(on_stop_game, |app| app
            .spawn(|app| async move { app.stop_game().await }));
        on!(on_open_console, |app| app.open_console());
        on!(on_send_launcher_log, |app| app
            .spawn(|app| async move { app.send_launcher_log().await }));
        on!(on_open_launcher_logs, |app| app
            .spawn(|app| async move { app.open_launcher_logs().await }));
        on!(on_dismiss_error, |app| app.dismiss_error());
        on!(on_retry, |app| {
            app.dismiss_error();
            app.spawn(|app| async move { app.play().await });
        });
        on!(on_dismiss_update, |app| {
            app.model.borrow_mut().update_dismissed = true;
            app.render();
        });
        on!(on_dismiss_crash, |app| app.dismiss_crash());
        on!(on_send_crash, |app| app
            .spawn(|app| async move { app.send_crash().await }));
        on!(on_copy_crash, |app| app.copy_crash());
        let weak = Rc::downgrade(self);
        self.window.global::<LogPick>().on_copy(move || {
            if let Some(app) = weak.upgrade() {
                app.copy_crash_picked();
            }
        });
        on!(on_open_link, |app, link| app.open_link(link.to_string()));
        on!(on_reload_accounts, |app| app
            .spawn(|app| async move { app.load_accounts().await }));
        on!(on_switch_account, |app, uuid| app.spawn(
            move |app| async move { app.switch_account(uuid.to_string()).await }
        ));
        on!(on_remove_account, |app, uuid| app.spawn(
            move |app| async move { app.remove_account(uuid.to_string()).await }
        ));
        on!(on_pick_folder, |app| app
            .spawn(|app| async move { app.pick_folder().await }));
        on!(on_collect_garbage, |app| app
            .spawn(|app| async move { app.collect_garbage().await }));
        on!(on_save_general, |app| app
            .spawn(|app| async move { app.save_general().await }));
        on!(on_save_build, |app| app
            .spawn(|app| async move { app.save_build().await }));
        on!(on_repair_build, |app| app.repair_from_dialog());
        on!(on_feature_pick, |app, group, option, active, single| {
            if let Some(editor) = app.model.borrow_mut().features.as_mut() {
                editor.pick(&group, &option, active, single);
            }
            app.render();
        });
        on!(on_feature_none, |app, group| {
            if let Some(editor) = app.model.borrow_mut().features.as_mut() {
                editor.clear(&group);
            }
            app.render();
        });
        on!(on_features_reset, |app| {
            if let Some(editor) = app.model.borrow_mut().features.as_mut() {
                editor.reset();
            }
            app.render();
        });
        on!(on_features_save, |app| app
            .spawn(|app| async move { app.save_features().await }));
        on!(on_library_search, |app, query| {
            app.model.borrow_mut().library_query = query.trim().to_lowercase();
            app.render();
        });
        on!(on_quit, |_app| {
            let _ = slint::quit_event_loop();
        });
        store.on_only_digits(|text| {
            text.chars()
                .filter(char::is_ascii_digit)
                .take(6)
                .collect::<String>()
                .into()
        });
    }

    fn start_timers(self: &Rc<Self>) {
        let weak = Rc::downgrade(self);
        self.timers[0].start(slint::TimerMode::Repeated, PLAYERS_EVERY, move || {
            if let Some(app) = weak.upgrade()
                && app.is_home()
                && !app.window.window().is_minimized()
            {
                app.spawn(|app| async move { app.refresh_players().await });
            }
        });
        let weak = Rc::downgrade(self);
        self.timers[1].start(slint::TimerMode::Repeated, BUILDS_EVERY, move || {
            if let Some(app) = weak.upgrade()
                && app.is_home()
                && !app.window.window().is_minimized()
            {
                app.spawn(|app| async move { app.refresh_builds().await });
            }
        });
    }

    async fn init(self: Rc<Self>) {
        let state = self.state.clone();
        let _ = on_core(async move { commands::current_endpoint(&state).await }).await;
        self.check_update().await;
        let can_install = self
            .model
            .borrow()
            .update
            .as_ref()
            .is_some_and(|update| update.can_install);
        if can_install {
            self.model.borrow_mut().phase = Phase::Updating;
            self.render();
            self.install_update().await;
            return;
        }
        self.start_timers();
        self.continue_startup().await;
    }

    async fn check_update(self: &Rc<Self>) {
        let state = self.state.clone();
        if let Ok(update) = on_core(async move { commands::check_update(&state).await }).await {
            self.model.borrow_mut().update = update;
            self.render();
        }
    }

    async fn install_update(self: &Rc<Self>) {
        let Some(version) = self
            .model
            .borrow()
            .update
            .as_ref()
            .map(|update| update.version.clone())
        else {
            return;
        };
        self.model.borrow_mut().update_progress = Some((0, 0));
        self.render();
        let state = self.state.clone();
        let result = on_core(async move {
            let on_progress = latest_on_ui(|app, progress: (u64, u64)| {
                app.model.borrow_mut().update_progress = Some(progress);
                app.render();
            });
            commands::apply_update(&state, version, move |done, total| {
                on_progress((done, total))
            })
            .await
        })
        .await;
        if let Err(message) = result {
            {
                let mut model = self.model.borrow_mut();
                model.update_progress = None;
                model.update = None;
                model.error = Some(Problem::plain(message));
            }
            self.render();
            self.start_timers();
            self.continue_startup().await;
        }
    }

    async fn continue_startup(self: &Rc<Self>) {
        let epoch = self.model.borrow().epoch;
        self.spawn(|app| async move {
            app.refresh_accounts().await.ok();
        });
        let state = self.state.clone();
        let status = on_core(async move { commands::restore_session(&state).await }).await;
        if epoch != self.model.borrow().epoch {
            return;
        }
        match status {
            Ok(status) if status.signed_in => {
                let state = self.state.clone();
                let builds = on_core(async move { commands::list_builds(&state).await }).await;
                if epoch != self.model.borrow().epoch {
                    return;
                }
                {
                    let mut model = self.model.borrow_mut();
                    model.account = status.username.map(|name| Account {
                        uuid: status.uuid.unwrap_or_default(),
                        name,
                    });
                    let (builds, trouble) = match builds {
                        Ok(builds) => (builds, None),
                        Err(message) => (Vec::new(), Some(Problem::plain(message))),
                    };
                    model.selected = pick_build(&builds);
                    model.builds = builds;
                    model.phase = Phase::Home;
                    model.error = trouble;
                }
                self.render();
                self.spawn(|app| async move { app.refresh_players().await });
                self.spawn(|app| async move { app.refresh_news().await });
                self.spawn(|app| async move { app.refresh_face(true).await });
            }
            Ok(_) => {
                self.model.borrow_mut().phase = Phase::Login;
                self.render();
            }
            Err(message) => {
                let mut model = self.model.borrow_mut();
                model.phase = Phase::Login;
                model.error = Some(Problem::plain(message));
                drop(model);
                self.render();
            }
        }
    }

    async fn refresh_accounts(self: &Rc<Self>) -> Result<(), String> {
        let request = {
            let mut model = self.model.borrow_mut();
            model.accounts_request += 1;
            model.accounts_request
        };
        let state = self.state.clone();
        let options = on_core(async move { accounts::account_options(&state).await }).await?;
        let mut model = self.model.borrow_mut();
        if request != model.accounts_request {
            return Ok(());
        }
        if !options.multi_account && model.modal == ModalKind::Accounts {
            model.modal_open = false;
        }
        model.accounts = options;
        drop(model);
        self.render();
        Ok(())
    }

    async fn load_accounts(self: &Rc<Self>) {
        {
            let mut model = self.model.borrow_mut();
            model.accounts_loading = true;
            model.accounts_error = None;
        }
        self.render();
        let result = self.refresh_accounts().await;
        {
            let mut model = self.model.borrow_mut();
            model.accounts_loading = false;
            model.accounts_error = result.err();
        }
        self.render();
    }

    fn reset_account_view(&self) {
        let mut model = self.model.borrow_mut();
        model.epoch += 1;
        model.account = None;
        model.builds.clear();
        model.selected = None;
        model.face = None;
        model.face_image = None;
        model.face_checked = None;
        model.players = None;
        model.news.clear();
        model.news_banners.clear();
        model.unread = 0;
        model.error = None;
        model.two_factor = false;
        model.login_error = None;
        model.crash = None;
        model.crash_open = false;
        model.crash_sent = None;
        model.crash_error = None;
        model.menu_open = false;
        model.stale.clear();
        model.running_session = None;
        model.last_exit = None;
    }

    fn begin_account_operation(&self) -> bool {
        let mut model = self.model.borrow_mut();
        if model.busy {
            return false;
        }
        model.busy = true;
        model.error = None;
        model.login_error = None;
        true
    }

    fn end_account_operation(&self) {
        self.model.borrow_mut().busy = false;
        self.render();
    }

    async fn switch_account(self: &Rc<Self>, uuid: String) {
        if !self.begin_account_operation() {
            return;
        }
        self.render();
        let state = self.state.clone();
        match on_core(async move { accounts::switch_account(&state, uuid).await }).await {
            Ok(_) => {
                self.reset_account_view();
                self.model.borrow_mut().modal_open = false;
                self.render();
                self.continue_startup().await;
                let _ = self.refresh_accounts().await;
            }
            Err(message) => self.model.borrow_mut().error = Some(Problem::plain(message)),
        }
        self.end_account_operation();
    }

    async fn remove_account(self: &Rc<Self>, uuid: String) {
        if !self.begin_account_operation() {
            return;
        }
        self.render();
        let state = self.state.clone();
        match on_core(async move { accounts::remove_account(&state, uuid).await }).await {
            Ok(status) => {
                if !status.signed_in {
                    self.reset_account_view();
                    self.model.borrow_mut().phase = Phase::Login;
                }
                self.store().set_accounts_confirm("".into());
                let _ = self.refresh_accounts().await;
            }
            Err(message) => self.model.borrow_mut().error = Some(Problem::plain(message)),
        }
        self.end_account_operation();
    }

    async fn login(self: &Rc<Self>, username: String, password: String, code: String) {
        if !self.begin_account_operation() {
            return;
        }
        self.render();
        let state = self.state.clone();
        match on_core(async move { commands::login(&state, username, password, code).await }).await
        {
            Ok(()) => {
                self.reset_account_view();
                self.model.borrow_mut().modal_open = false;
                self.store().set_accounts_adding(false);
                self.render();
                self.window.invoke_refocus();
                self.continue_startup().await;
            }
            Err(failure) => {
                let mut model = self.model.borrow_mut();
                let (message, second_factor) = match failure {
                    LoginError::SecondFactor { message } => (message, true),
                    LoginError::Failed { message } => (message, false),
                };
                model.login_error = Some(message.clone());
                model.error = Some(Problem::plain(message));
                model.two_factor = second_factor;
            }
        }
        self.end_account_operation();
    }

    async fn logout(self: &Rc<Self>) {
        if !self.begin_account_operation() {
            return;
        }
        self.render();
        let state = self.state.clone();
        match on_core(async move { commands::logout(&state).await }).await {
            Ok(()) => {
                self.reset_account_view();
                {
                    let mut model = self.model.borrow_mut();
                    model.phase = Phase::Login;
                    model.modal_open = false;
                }
                self.render();
                self.window.invoke_refocus();
                let _ = self.refresh_accounts().await;
            }
            Err(message) => self.model.borrow_mut().error = Some(Problem::plain(message)),
        }
        self.end_account_operation();
    }

    fn dismiss_error(&self) {
        {
            let mut model = self.model.borrow_mut();
            model.error = None;
            model.login_error = None;
            model.two_factor = false;
            model.report = LogReport::Idle;
        }
        self.render();
    }

    fn toggle_menu(self: &Rc<Self>) {
        let opening = {
            let mut model = self.model.borrow_mut();
            model.menu_open = !model.menu_open;
            model.menu_open
        };
        self.render();
        if opening {
            self.spawn(|app| async move {
                app.refresh_accounts().await.ok();
            });
        }
    }

    fn close_menu(&self) {
        self.model.borrow_mut().menu_open = false;
        self.render();
    }

    fn open_modal(self: &Rc<Self>, kind: ModalKind) {
        if kind == ModalKind::Accounts && !self.model.borrow().accounts.multi_account {
            return;
        }
        if !self.model.borrow().modal_open {
            self.model.borrow_mut().menu_open = false;
            self.render();
            scene::freeze_backdrop(&self.window, MODAL_FROST);
        }
        let ticket = {
            let mut model = self.model.borrow_mut();
            if kind == ModalKind::News {
                self.remember_seen(model.news.iter().map(|item| item.id.clone()).collect());
                model.unread = 0;
            }
            model.modal = kind;
            model.modal_open = true;
            model.menu_open = false;
            model.modal_ticket += 1;
            model.dialog_trouble = None;
            model.modal_profile = model.selected.clone().unwrap_or_default();
            model.loaded_memory = None;
            model.modal_ticket
        };
        let store = self.store();
        match kind {
            ModalKind::Accounts => {
                store.set_accounts_adding(false);
                store.set_accounts_confirm("".into());
                self.spawn(|app| async move { app.load_accounts().await });
            }
            ModalKind::General => {
                let settings = commands::general_settings(&self.state);
                let mut model = self.model.borrow_mut();
                model.general_loaded = true;
                model.cleaned = None;
                let profile = model.modal_profile.clone();
                drop(model);
                store.set_install_dir(settings.install_dir.into());
                store.set_game_console(settings.game_console);
                if !profile.is_empty() {
                    self.load_build_memory(profile);
                }
            }
            ModalKind::Build => {
                let profile = self.model.borrow().modal_profile.clone();
                self.load_build_memory(profile);
            }
            ModalKind::Features => {
                {
                    let mut model = self.model.borrow_mut();
                    model.features = None;
                    model.features_loaded = false;
                }
                self.spawn(move |app| async move { app.load_features(ticket).await });
            }
            ModalKind::Library => {
                self.model.borrow_mut().library_query.clear();
            }
            ModalKind::News | ModalKind::None => {}
        }
        self.render();
    }

    fn load_build_memory(&self, profile: String) {
        let settings = commands::build_settings(&self.state, profile);
        let memory = settings
            .max_memory_mb
            .unwrap_or(settings.default_memory_mb)
            .min(settings.allowed_memory_mb);
        let mut model = self.model.borrow_mut();
        model.build_loaded = true;
        model.memory_ceiling = settings.allowed_memory_mb;
        model.loaded_memory = Some(memory);
        drop(model);
        self.store().set_memory(memory as i32);
    }

    fn close_modal(&self) {
        {
            let mut model = self.model.borrow_mut();
            if model.modal == ModalKind::Accounts {
                if model.busy {
                    return;
                }
                model.error = None;
                model.login_error = None;
                model.two_factor = false;
            }
            model.modal_open = false;
        }
        self.render();
        self.window.invoke_refocus();
    }

    async fn load_features(self: &Rc<Self>, ticket: u64) {
        let profile = self.model.borrow().modal_profile.clone();
        let state = self.state.clone();
        let wanted = profile.clone();
        let result = on_core(async move { commands::build_features(&state, wanted).await }).await;
        let mut model = self.model.borrow_mut();
        if model.modal_ticket != ticket {
            return;
        }
        match result {
            Ok(loaded) => {
                model.features = Some(FeatureEditor::new(profile, loaded.model, loaded.selection));
                model.features_loaded = true;
            }
            Err(message) => model.dialog_trouble = Some(message),
        }
        drop(model);
        self.render();
    }

    async fn save_features(self: &Rc<Self>) {
        let Some((profile, selection)) = self
            .model
            .borrow()
            .features
            .as_ref()
            .map(|editor| (editor.profile.clone(), editor.selection.clone()))
        else {
            return;
        };
        let state = self.state.clone();
        let target = profile.clone();
        match on_core(async move { commands::set_build_features(&state, target, selection).await })
            .await
        {
            Ok(()) => {
                self.mark_outdated(&profile);
                self.close_modal();
            }
            Err(message) => {
                self.model.borrow_mut().dialog_trouble = Some(message);
                self.render();
            }
        }
    }

    fn mark_outdated(&self, name: &str) {
        let mut model = self.model.borrow_mut();
        if !model.stale.iter().any(|stale| stale == name) {
            model.stale.push(name.to_string());
        }
        for build in model.builds.iter_mut() {
            if build.name == name && build.install == InstallState::Installed {
                build.install = InstallState::Outdated;
            }
        }
    }

    async fn pick_folder(self: &Rc<Self>) {
        let current = self.store().get_install_dir().to_string();
        let dialog = rfd::AsyncFileDialog::new()
            .set_directory(&current)
            .set_parent(&self.window.window().window_handle());
        if let Some(folder) = dialog.pick_folder().await {
            self.store()
                .set_install_dir(folder.path().to_string_lossy().into_owned().into());
        }
    }

    async fn collect_garbage(self: &Rc<Self>) {
        let state = self.state.clone();
        match on_core(async move { commands::collect_garbage(&state).await }).await {
            Ok(removed) => self.model.borrow_mut().cleaned = Some(removed),
            Err(message) => self.model.borrow_mut().dialog_trouble = Some(message),
        }
        self.render();
    }

    async fn save_general(self: &Rc<Self>) {
        let store = self.store();
        let install_dir = store.get_install_dir().to_string();
        let game_console = store.get_game_console();
        let current = commands::general_settings(&self.state);
        let changed_memory = {
            let model = self.model.borrow();
            let memory = store.get_memory().max(0) as u32;
            model
                .loaded_memory
                .filter(|loaded| model.build_loaded && *loaded != memory)
                .map(|_| (model.modal_profile.clone(), memory))
        };
        let state = self.state.clone();
        let result = on_core(async move {
            if !install_dir.is_empty() && install_dir != current.install_dir {
                commands::set_install_dir(&state, install_dir).await?;
            }
            if game_console != current.game_console {
                commands::set_game_console(&state, game_console)?;
            }
            if let Some((profile, memory)) = changed_memory {
                commands::set_build_memory(&state, profile, Some(memory)).await?;
            }
            Ok::<(), String>(())
        })
        .await;
        match result {
            Ok(()) => self.close_modal(),
            Err(message) => {
                self.model.borrow_mut().dialog_trouble = Some(message);
                self.render();
            }
        }
    }

    async fn save_build(self: &Rc<Self>) {
        let profile = self.model.borrow().modal_profile.clone();
        let memory = self.store().get_memory().max(0) as u32;
        let state = self.state.clone();
        match on_core(
            async move { commands::set_build_memory(&state, profile, Some(memory)).await },
        )
        .await
        {
            Ok(()) => self.close_modal(),
            Err(message) => {
                self.model.borrow_mut().dialog_trouble = Some(message);
                self.render();
            }
        }
    }

    fn repair_from_dialog(self: &Rc<Self>) {
        let profile = self.model.borrow().modal_profile.clone();
        self.close_modal();
        let weak = Rc::downgrade(self);
        let exit =
            Duration::from_millis(self.window.global::<Theme>().get_modal_exit().max(0) as u64);
        slint::Timer::single_shot(exit, move || {
            if let Some(app) = weak.upgrade() {
                app.spawn(move |app| async move { app.repair_build(profile).await });
            }
        });
    }

    fn select(&self, name: String) {
        self.model.borrow_mut().selected = Some(name);
        self.render();
    }

    async fn refresh_players(self: &Rc<Self>) {
        let epoch = self.model.borrow().epoch;
        let state = self.state.clone();
        if let Ok(players) = on_core(async move { commands::player_counts(&state).await }).await {
            let mut model = self.model.borrow_mut();
            if epoch == model.epoch {
                model.players = players;
                drop(model);
                self.render();
            }
        }
    }

    async fn refresh_builds(self: &Rc<Self>) {
        if self.model.borrow().phase != Phase::Home {
            return;
        }
        let epoch = self.model.borrow().epoch;
        let state = self.state.clone();
        let Ok(fresh) = on_core(async move { commands::list_builds(&state).await }).await else {
            return;
        };
        let mut model = self.model.borrow_mut();
        if epoch != model.epoch || model.busy {
            return;
        }
        let builds: Vec<BuildDto> = fresh
            .into_iter()
            .map(|mut build| {
                if model.stale.contains(&build.name) && build.install == InstallState::Installed {
                    build.install = InstallState::Outdated;
                }
                build
            })
            .collect();
        let keep = model
            .selected
            .as_ref()
            .is_some_and(|selected| builds.iter().any(|build| &build.name == selected));
        if !keep {
            model.selected = pick_build(&builds);
        }
        model.builds = builds;
        drop(model);
        self.render();
    }

    fn remember_seen(&self, seen: Vec<String>) {
        if let Ok(json) = serde_json::to_string(&seen)
            && let Err(error) = std::fs::write(&self.seen_file, json)
        {
            tracing::warn!(%error, "не вышло запомнить прочитанные новости");
        }
    }

    async fn refresh_news(self: &Rc<Self>) {
        let epoch = self.model.borrow().epoch;
        let state = self.state.clone();
        let Ok(news) = on_core(async move { commands::news(&state).await }).await else {
            return;
        };
        let seen: HashSet<String> = match std::fs::read_to_string(&self.seen_file) {
            Ok(text) => serde_json::from_str::<Vec<String>>(&text).unwrap_or_default(),
            Err(_) if webview_launcher_was_here() => {
                let all: Vec<String> = news.iter().map(|item| item.id.clone()).collect();
                self.remember_seen(all.clone());
                all
            }
            Err(_) => Vec::new(),
        }
        .into_iter()
        .collect();
        let banners = news
            .iter()
            .map(|item| item.banner_data_uri.as_deref().and_then(media::picture))
            .collect();
        let mut model = self.model.borrow_mut();
        if epoch != model.epoch {
            return;
        }
        model.unread = news.iter().filter(|item| !seen.contains(&item.id)).count();
        model.news = news;
        model.news_banners = banners;
        drop(model);
        self.render();
    }

    async fn refresh_face(self: &Rc<Self>, force: bool) {
        let epoch = {
            let mut model = self.model.borrow_mut();
            if model.account.is_none() {
                return;
            }
            if !force
                && model
                    .face_checked
                    .is_some_and(|at| at.elapsed() < FACE_RECHECK)
            {
                return;
            }
            model.face_checked = Some(Instant::now());
            model.epoch
        };
        let state = self.state.clone();
        let Ok(face) = on_core(async move { commands::player_face(&state).await }).await else {
            return;
        };
        let mut model = self.model.borrow_mut();
        if epoch != model.epoch {
            return;
        }
        if model.face != face {
            model.face_image = face
                .as_deref()
                .and_then(|uri| media::avatar(uri, FACE_PIXELS));
            model.face = face;
        }
        drop(model);
        self.render();
    }

    async fn play(self: &Rc<Self>) {
        let (name, run) = {
            let mut model = self.model.borrow_mut();
            if model.phase != Phase::Home || model.busy {
                return;
            }
            let Some(name) = model.selected.clone() else {
                return;
            };
            if let Some(block) = model
                .builds
                .iter()
                .find(|build| build.name == name)
                .and_then(block_of)
            {
                model.error = Some(Problem::plain(block.reason));
                drop(model);
                self.render();
                return;
            }
            model.error = None;
            model.crash_open = false;
            drop(model);
            self.render();
            scene::freeze_backdrop(&self.window, SYNC_FROST);
            let mut model = self.model.borrow_mut();
            model.sync_run += 1;
            model.meter = RateMeter::new();
            model.phase = Phase::Syncing;
            model.sync = None;
            model.crash = None;
            model.crash_open = false;
            model.crash_sent = None;
            model.crash_error = None;
            model.cancel_requested = false;
            (name, model.sync_run)
        };
        self.render();
        let current = |app: &App| app.model.borrow().sync_run == run;
        let synced = self.sync(name.clone(), run, SyncShown::Launching).await;
        if !current(self) {
            return;
        }
        if let Err(failure) = synced {
            self.sync_failed(failure);
            return;
        }
        self.model.borrow_mut().stale.retain(|stale| stale != &name);
        self.spawn(|app| async move {
            let state = app.state.clone();
            if let Ok(builds) = on_core(async move { commands::list_builds(&state).await }).await {
                app.model.borrow_mut().builds = builds;
                app.render();
            }
        });
        if self.model.borrow().cancel_requested {
            self.back_home();
            return;
        }
        let state = self.state.clone();
        let target = name.clone();
        let launched = on_core(async move { commands::launch(&state, target).await }).await;
        if !current(self) {
            return;
        }
        match launched {
            Ok(session) => {
                let early = self
                    .model
                    .borrow()
                    .last_exit
                    .as_ref()
                    .filter(|exit| exit.session == session)
                    .cloned();
                if let Some(exit) = early {
                    self.back_home();
                    self.settle_exit(exit).await;
                    return;
                }
                {
                    let mut model = self.model.borrow_mut();
                    model.phase = Phase::Running;
                    model.running_session = Some(session);
                }
                self.render();
            }
            Err(failure) => self.sync_failed(failure),
        }
    }

    fn back_home(&self) {
        let mut model = self.model.borrow_mut();
        model.phase = Phase::Home;
        model.sync = None;
        drop(model);
        self.render();
    }

    fn sync_failed(&self, failure: Failure) {
        let mut model = self.model.borrow_mut();
        model.phase = Phase::Home;
        model.sync = None;
        if !model.cancel_requested {
            model.error = Some(failure.into());
        }
        drop(model);
        self.render();
    }

    async fn sync(
        self: &Rc<Self>,
        name: String,
        run: u64,
        final_stage: SyncShown,
    ) -> Result<(), Failure> {
        let on_event = latest_on_ui(move |app, event: SyncEvent| {
            app.apply_sync_event(event, run, final_stage)
        });
        let state = self.state.clone();
        on_core(async move { commands::sync_profile(&state, name, on_event).await }).await
    }

    fn apply_sync_event(&self, event: SyncEvent, run: u64, final_stage: SyncShown) {
        let mut model = self.model.borrow_mut();
        if model.sync_run != run {
            return;
        }
        match event {
            SyncEvent::Started => {
                model.meter = RateMeter::new();
                model.sync = Some(SyncView {
                    stage: SyncShown::Planning,
                    files_done: 0,
                    files_total: 0,
                    bytes_done: 0,
                    bytes_total: 0,
                    current_path: None,
                    bytes_per_second: None,
                    seconds_left: None,
                });
            }
            SyncEvent::Progress(progress) => {
                let SyncProgress {
                    stage,
                    files_done,
                    files_total,
                    bytes_done,
                    bytes_total,
                    current_path,
                } = progress;
                let (bytes_per_second, seconds_left) = model.meter.sample(bytes_done, bytes_total);
                model.sync = Some(SyncView {
                    stage: match stage {
                        SyncStage::Planning => SyncShown::Planning,
                        SyncStage::Downloading => SyncShown::Downloading,
                        SyncStage::Done => SyncShown::Done,
                    },
                    files_done,
                    files_total,
                    bytes_done,
                    bytes_total,
                    current_path,
                    bytes_per_second,
                    seconds_left,
                });
            }
            SyncEvent::Finished => {
                if let Some(sync) = model.sync.as_mut() {
                    sync.stage = final_stage;
                }
            }
        }
        drop(model);
        self.render();
    }

    async fn repair_build(self: &Rc<Self>, name: String) {
        let run = {
            let mut model = self.model.borrow_mut();
            if model.phase != Phase::Home {
                return;
            }
            model.error = None;
            drop(model);
            self.render();
            scene::freeze_backdrop(&self.window, SYNC_FROST);
            let mut model = self.model.borrow_mut();
            model.sync_run += 1;
            model.meter = RateMeter::new();
            model.phase = Phase::Syncing;
            model.sync = None;
            model.cancel_requested = false;
            model.selected.get_or_insert_with(|| name.clone());
            model.sync_run
        };
        self.render();
        let current = |app: &App| app.model.borrow().sync_run == run;
        let state = self.state.clone();
        let target = name.clone();
        let discarded = on_core(async move { commands::repair_build(&state, target).await }).await;
        if !current(self) {
            return;
        }
        if let Err(message) = discarded {
            self.sync_failed(Failure {
                message,
                retry: false,
            });
            return;
        }
        if self.model.borrow().cancel_requested {
            self.back_home();
            return;
        }
        let synced = self.sync(name, run, SyncShown::Done).await;
        if !current(self) {
            return;
        }
        if let Err(failure) = synced {
            self.sync_failed(failure);
            return;
        }
        let state = self.state.clone();
        match on_core(async move { commands::list_builds(&state).await }).await {
            Ok(builds) => {
                {
                    let mut model = self.model.borrow_mut();
                    model.builds = builds;
                    model.phase = Phase::Home;
                    model.sync = None;
                }
                self.render();
            }
            Err(message) => self.sync_failed(Failure {
                message,
                retry: false,
            }),
        }
    }

    async fn cancel_sync(self: &Rc<Self>) {
        {
            let mut model = self.model.borrow_mut();
            if model.cancel_requested {
                return;
            }
            model.cancel_requested = true;
        }
        self.render();
        commands::cancel_sync(&self.state);
    }

    async fn stop_game(self: &Rc<Self>) {
        let state = self.state.clone();
        if let Err(message) = on_core(async move { commands::stop(&state).await }).await {
            self.model.borrow_mut().error = Some(Problem::plain(message));
            self.render();
        }
    }

    fn open_console(self: &Rc<Self>) {
        if let Some(view) = self.console.borrow().as_ref() {
            view.raise();
            return;
        }
        let accent = palette::hex(self.window.global::<Theme>().get_primary());
        let view = match ConsoleView::new(self.state.game_log.clone(), accent) {
            Ok(view) => view,
            Err(error) => {
                tracing::error!(%error, "окно консоли не открылось");
                self.model.borrow_mut().error =
                    Some(Problem::plain("Не удалось открыть консоль игры"));
                self.render();
                return;
            }
        };
        self.dress_console(&view);
        self.bind_console(&view);
        if let Err(error) = view.show() {
            tracing::error!(%error, "окно консоли не показалось");
            return;
        }
        *self.console.borrow_mut() = Some(view);
    }

    fn bind_console(self: &Rc<Self>, view: &Rc<ConsoleView>) {
        let store = view.window().global::<ConsoleStore>();
        let weak = Rc::downgrade(self);
        store.on_close(move || {
            if let Some(app) = weak.upgrade()
                && let Some(view) = app.console.borrow_mut().take()
            {
                view.close();
            }
        });
        let weak = Rc::downgrade(self);
        store.on_copy(move || {
            if let Some(app) = weak.upgrade() {
                let text = app
                    .console
                    .borrow()
                    .as_ref()
                    .map(|view| view.visible_text());
                if let Some(text) = text
                    && app.copy_text(text)
                    && let Some(view) = app.console.borrow().as_ref()
                {
                    view.mark_copied();
                }
            }
        });
        let weak = Rc::downgrade(self);
        view.window().global::<LogPick>().on_copy(move || {
            if let Some(app) = weak.upgrade() {
                let text = app.console.borrow().as_ref().map(|view| view.picked_text());
                if let Some(text) = text {
                    app.copy_text(text);
                }
            }
        });
        let weak = Rc::downgrade(self);
        store.on_open_logs(move || {
            if let Some(app) = weak.upgrade() {
                app.spawn(|app| async move {
                    let state = app.state.clone();
                    let result =
                        on_core(async move { commands::open_game_logs(&state).await }).await;
                    app.console_outcome(result);
                });
            }
        });
        let weak = Rc::downgrade(self);
        store.on_stop(move || {
            if let Some(app) = weak.upgrade() {
                app.spawn(|app| async move {
                    let state = app.state.clone();
                    let result = on_core(async move { commands::stop(&state).await }).await;
                    app.console_outcome(result);
                });
            }
        });
        let weak = Rc::downgrade(view);
        view.window().window().on_close_requested(move || {
            if weak.upgrade().is_some() {
                with_app(|app| {
                    app.console.borrow_mut().take();
                });
            }
            slint::CloseRequestResponse::HideWindow
        });
    }

    fn console_outcome(&self, result: Result<(), String>) {
        if let Some(view) = self.console.borrow().as_ref() {
            match result {
                Ok(()) => view.clear_trouble(),
                Err(message) => view.trouble(message),
            }
        }
    }

    fn copy_text(&self, text: String) -> bool {
        let mut clipboard = self.clipboard.borrow_mut();
        if clipboard.is_none() {
            match arboard::Clipboard::new() {
                Ok(opened) => *clipboard = Some(opened),
                Err(error) => {
                    tracing::warn!(%error, "буфер обмена недоступен");
                    return false;
                }
            }
        }
        let Some(clipboard) = clipboard.as_mut() else {
            return false;
        };
        match clipboard.set_text(text) {
            Ok(()) => true,
            Err(error) => {
                tracing::warn!(%error, "текст не попал в буфер обмена");
                false
            }
        }
    }

    async fn send_launcher_log(self: &Rc<Self>) {
        {
            let mut model = self.model.borrow_mut();
            if matches!(model.report, LogReport::Sending) {
                return;
            }
            model.report = LogReport::Sending;
        }
        self.render();
        let state = self.state.clone();
        let result = on_core(async move { commands::send_launcher_log(&state).await }).await;
        self.model.borrow_mut().report = match result {
            Ok(message) => LogReport::Sent(message),
            Err(message) => LogReport::Failed(message),
        };
        self.render();
    }

    async fn open_launcher_logs(self: &Rc<Self>) {
        let state = self.state.clone();
        if let Err(message) =
            on_core(async move { commands::open_launcher_logs(&state).await }).await
        {
            self.model.borrow_mut().error = Some(Problem::plain(message));
            self.render();
        }
    }

    fn open_link(&self, link: String) {
        let runtime = crate::runtime::get();
        runtime.spawn(async move {
            if let Err(message) = commands::open_external(link).await {
                tracing::warn!("{message}");
            }
        });
    }

    fn game_exited(self: &Rc<Self>, exit: GameExit) {
        let running = self.model.borrow().running_session;
        self.model.borrow_mut().last_exit = Some(exit.clone());
        if running == Some(exit.session) {
            self.spawn(move |app| async move { app.settle_exit(exit).await });
        }
    }

    async fn settle_exit(self: &Rc<Self>, exit: GameExit) {
        {
            let mut model = self.model.borrow_mut();
            model.running_session = None;
            if model.phase == Phase::Running {
                model.phase = Phase::Home;
            }
        }
        self.render();
        if exit.code == 0 || exit.stopped {
            return;
        }
        let tail = commands::game_log_tail(&self.state, CRASH_LOG_LINES);
        if self.model.borrow().phase != Phase::Home || self.model.borrow().busy {
            return;
        }
        scene::freeze_backdrop(&self.window, MODAL_FROST);
        let mut model = self.model.borrow_mut();
        let build = model.builds.iter().find(|build| build.name == exit.build);
        self.window.global::<LogPick>().invoke_clear();
        model.crash = Some(Crash {
            session: exit.session,
            code: exit.code,
            log: tail,
            loader: build
                .and_then(|build| build.loader.clone())
                .unwrap_or_default(),
            version: build
                .and_then(|build| build.minecraft.clone())
                .unwrap_or_default(),
            build: exit.build,
        });
        model.crash_open = true;
        model.crash_sent = None;
        model.crash_error = None;
        drop(model);
        self.render();
    }

    fn dismiss_crash(&self) {
        {
            let mut model = self.model.borrow_mut();
            model.crash_open = false;
            model.crash_sent = None;
        }
        self.render();
        self.window.invoke_refocus();
    }

    async fn send_crash(self: &Rc<Self>) {
        let report = {
            let mut model = self.model.borrow_mut();
            if model.crash_sending {
                return;
            }
            let Some(crash) = model.crash.as_ref() else {
                return;
            };
            let report = (
                crash.build.clone(),
                crash.version.clone(),
                crash.loader.clone(),
                crash.code,
                crash.session,
            );
            model.crash_sending = true;
            report
        };
        self.render();
        let state = self.state.clone();
        let (build, version, loader, code, session) = report;
        let result = on_core(async move {
            commands::report_crash(&state, build, version, loader, code, session).await
        })
        .await;
        {
            let mut model = self.model.borrow_mut();
            model.crash_sending = false;
            match result {
                Ok(message) => {
                    model.crash_sent = Some(message);
                    model.crash_error = None;
                }
                Err(message) => model.crash_error = Some(message),
            }
        }
        self.render();
    }

    fn copy_crash_picked(&self) {
        let pick = self.window.global::<LogPick>();
        let text = self.model.borrow().crash.as_ref().map(|crash| {
            console::picked_rows(pick.get_anchor(), pick.get_focus())
                .filter_map(|row| crash.log.get(row))
                .map(|line| line.text.as_str())
                .collect::<Vec<_>>()
                .join("\n")
        });
        if let Some(text) = text {
            self.copy_text(text);
        }
    }

    fn copy_crash(&self) {
        let text = self.model.borrow().crash.as_ref().map(|crash| {
            crash
                .log
                .iter()
                .map(|line| line.text.as_str())
                .collect::<Vec<_>>()
                .join("\n")
        });
        if let Some(text) = text {
            self.copy_text(text);
        }
    }

    fn render(&self) {
        let model = self.model.borrow();
        let store = self.store();
        store.set_phase(model.phase);
        store.set_busy(model.busy);

        let total = model
            .players
            .as_ref()
            .map(|players| players.total)
            .filter(|total| total.max > 0);
        store.set_has_total(total.is_some());
        if let Some(total) = total {
            store.set_total_online(format::count(total.online).into());
            store.set_total_max(format::count(total.max).into());
        }
        let name = model
            .account
            .as_ref()
            .map(|account| account.name.clone())
            .unwrap_or_else(|| "Гость".into());
        store.set_account_initial(
            initial_of(model.account.as_ref().map_or("", |account| &account.name)).into(),
        );
        store.set_account_name(name.into());
        store.set_has_face(model.face_image.is_some());
        if let Some(face) = &model.face_image {
            store.set_face(face.clone());
        }
        store.set_menu_open(model.menu_open);
        store.set_multi_account(model.accounts.multi_account);
        store.set_max_accounts(model.accounts.max_accounts as i32);

        let items: Vec<BuildItem> = model
            .builds
            .iter()
            .map(|build| build_item(build, model.players.as_ref()))
            .collect();
        let selected = model.selected.clone().unwrap_or_default();
        let current = items
            .iter()
            .find(|item| item.name.as_str() == selected)
            .cloned();
        store.set_selected(selected.into());
        store.set_has_build(current.is_some());
        let current_dto = model
            .builds
            .iter()
            .find(|build| Some(&build.name) == model.selected.as_ref());
        store.set_action_label(
            match current_dto.map(|build| build.install) {
                Some(InstallState::Missing) => "УСТАНОВИТЬ",
                Some(InstallState::Outdated) => "ОБНОВИТЬ",
                _ => "ИГРАТЬ",
            }
            .into(),
        );
        if let Some(current) = current {
            store.set_build(current);
        }
        store.set_build_count(items.len() as i32);
        store.set_library_caption(
            format!(
                "{} {}",
                items.len(),
                format::plural(items.len() as i64, "сборка", "сборки", "сборок")
            )
            .into(),
        );
        let needle = model.library_query.clone();
        let filtered: Vec<BuildItem> = model
            .builds
            .iter()
            .zip(items.iter())
            .filter(|(build, _)| {
                build.name.to_lowercase().contains(&needle)
                    || build
                        .loader
                        .clone()
                        .unwrap_or_default()
                        .to_lowercase()
                        .contains(&needle)
            })
            .map(|(_, item)| item.clone())
            .collect();
        store.set_library_empty(filtered.is_empty());
        scene::fill_library(&self.lists.library, &filtered);
        replace_rows(&self.lists.builds, items);

        let news: Vec<NewsItem> = model
            .news
            .iter()
            .zip(model.news_banners.iter().chain(std::iter::repeat(&None)))
            .map(|(item, banner)| news_item(item, banner.clone()))
            .collect();
        store.set_news_caption(
            format!(
                "{} {}",
                news.len(),
                format::plural(news.len() as i64, "запись", "записи", "записей")
            )
            .into(),
        );
        replace_rows(&self.lists.news_pairs, scene::news_pairs(&news));
        replace_rows(&self.lists.news_rest, scene::news_rest(&news));
        replace_rows(&self.lists.news, news);
        store.set_unread(model.unread as i32);

        store.set_running_build(model.selected.clone().unwrap_or_default().into());

        render_sync(&store, &model);

        let update = model.update.as_ref();
        store.set_update_banner(update.is_some_and(|update| {
            !update.can_install && (!model.update_dismissed || update.mandatory)
        }));
        if let Some(update) = update {
            store.set_update_version(update.version.clone().into());
            store.set_update_mandatory(update.mandatory);
            store.set_update_detail(
                update
                    .blocked_reason
                    .clone()
                    .unwrap_or_else(|| {
                        format!(
                            "{} · скачайте свежий лаунчер с сайта проекта",
                            format::bytes(update.size)
                        )
                    })
                    .into(),
            );
            let total = model
                .update_progress
                .map(|(_, total)| total)
                .filter(|total| *total > 0)
                .unwrap_or(update.size);
            let done = model.update_progress.map(|(done, _)| done).unwrap_or(0);
            store.set_update_fraction(if total > 0 && model.update_progress.is_some() {
                done as f32 / total as f32
            } else {
                0.0
            });
            store.set_update_progress(if total > 0 {
                format!("{} / {}", format::bytes(done), format::bytes(total)).into()
            } else {
                SharedString::new()
            });
        }

        store.set_error_shown(model.error.is_some());
        if let Some(error) = &model.error {
            store.set_error_message(error.message.clone().into());
            store.set_error_retry(error.retry);
        }
        let (report, report_message) = match &model.report {
            LogReport::Idle => (Report::Idle, String::new()),
            LogReport::Sending => (Report::Sending, String::new()),
            LogReport::Sent(message) => (Report::Sent, message.clone()),
            LogReport::Failed(message) => (Report::Failed, message.clone()),
        };
        store.set_report(report);
        store.set_report_message(report_message.into());
        store.set_two_factor(model.two_factor);
        let form_error = model
            .login_error
            .clone()
            .or_else(|| model.error.as_ref().map(|error| error.message.clone()));
        store.set_login_error(form_error.unwrap_or_default().into());

        store.set_modal_open(model.modal_open);
        store.set_modal(model.modal);
        store.set_modal_profile(model.modal_profile.clone().into());

        let current_uuid = model.account.as_ref().map(|account| account.uuid.clone());
        replace_rows(
            &self.lists.accounts,
            model
                .accounts
                .accounts
                .iter()
                .map(|account| account_item(account, current_uuid.as_deref()))
                .collect(),
        );
        store.set_accounts_loading(model.accounts_loading);
        store.set_accounts_error(model.accounts_error.clone().unwrap_or_default().into());

        store.set_general_ready(model.general_loaded);
        store.set_cleaned(
            model
                .cleaned
                .map(|count| count.to_string())
                .unwrap_or_default()
                .into(),
        );
        store.set_dialog_trouble(model.dialog_trouble.clone().unwrap_or_default().into());
        store.set_build_ready(model.build_loaded);
        store.set_memory_ceiling(model.memory_ceiling as i32);
        store.set_features_ready(model.features_loaded);
        store.set_features_empty(model.features.as_ref().is_none_or(FeatureEditor::is_empty));
        replace_rows(
            &self.lists.feature_rows,
            model
                .features
                .as_ref()
                .map(FeatureEditor::rows)
                .unwrap_or_default(),
        );

        store.set_crash_open(model.crash_open);
        self.pace_hero();
        if let Some(crash) = &model.crash {
            store.set_crash_code(crash.code.to_string().into());
            replace_rows(
                &self.lists.crash_log,
                crash
                    .log
                    .iter()
                    .map(|line| LogRow {
                        text: line.text.clone().into(),
                        level: level_index(line.level),
                    })
                    .collect(),
            );
        }
        store.set_crash_sending(model.crash_sending);
        store.set_crash_sent(model.crash_sent.clone().unwrap_or_default().into());
        store.set_crash_trouble(model.crash_error.clone().unwrap_or_default().into());
    }
}

fn render_sync(store: &Store<'_>, model: &ModelState) {
    let sync = model.sync.as_ref();
    let stage = sync.map(|sync| sync.stage).unwrap_or(SyncShown::Planning);
    store.set_sync_stage(
        match stage {
            SyncShown::Planning => "Проверка файлов",
            SyncShown::Downloading => "Загрузка",
            SyncShown::Done => "Установлено",
            SyncShown::Launching => "Проверяю файлы и запускаю игру",
        }
        .into(),
    );
    store.set_cancelling(model.cancel_requested);
    store.set_sync_cancellable(stage != SyncShown::Launching);
    store.set_sync_build(model.selected.clone().unwrap_or_default().into());
    let fraction = sync
        .filter(|sync| sync.bytes_total > 0)
        .map(|sync| sync.bytes_done as f32 / sync.bytes_total as f32)
        .unwrap_or(0.0);
    store.set_sync_fraction(fraction);
    let amount = match sync {
        Some(sync) if sync.bytes_total > 0 => {
            format!(
                "{} / {}",
                format::bytes(sync.bytes_done),
                format::bytes(sync.bytes_total)
            )
        }
        Some(sync) if sync.stage == SyncShown::Downloading => "Всё уже скачано".into(),
        _ => "Подготовка…".into(),
    };
    store.set_sync_amount(amount.into());
    store.set_sync_files(
        sync.filter(|sync| sync.files_total > 0)
            .map(|sync| {
                format!(
                    "{} / {} {}",
                    sync.files_done,
                    sync.files_total,
                    format::plural(sync.files_total as i64, "файл", "файла", "файлов")
                )
            })
            .unwrap_or_default()
            .into(),
    );
    store.set_sync_speed(
        sync.and_then(|sync| sync.bytes_per_second)
            .filter(|speed| *speed > 0.0)
            .map(format::speed)
            .unwrap_or_default()
            .into(),
    );
    store.set_sync_left(
        sync.and_then(|sync| sync.seconds_left)
            .filter(|left| *left > 0.0)
            .map(format::time_left)
            .unwrap_or_default()
            .into(),
    );
    store.set_sync_path(
        sync.and_then(|sync| sync.current_path.clone())
            .unwrap_or_default()
            .into(),
    );
}

struct Block {
    badge: &'static str,
    reason: String,
}

const LOCKED_FALLBACK: &str = "Доступ к этой сборке выдаётся вручную";

fn block_of(build: &BuildDto) -> Option<Block> {
    if build.locked {
        let reason = build
            .lock_reason
            .as_deref()
            .map(str::trim)
            .filter(|reason| !reason.is_empty())
            .unwrap_or(LOCKED_FALLBACK);
        return Some(Block {
            badge: "Нет доступа",
            reason: reason.to_string(),
        });
    }
    if !build.available {
        return Some(Block {
            badge: "Нет для вашей ОС",
            reason: "Эта сборка не собрана для вашей системы".into(),
        });
    }
    None
}

fn pick_build(builds: &[BuildDto]) -> Option<String> {
    builds
        .iter()
        .find(|build| block_of(build).is_none())
        .or_else(|| builds.first())
        .map(|build| build.name.clone())
}

fn loader_label(loader: &str) -> String {
    match loader {
        "vanilla" => "Vanilla".into(),
        "fabric" => "Fabric".into(),
        "quilt" => "Quilt".into(),
        "forge" => "Forge".into(),
        "neoforge" => "NeoForge".into(),
        "cleanroom" => "Cleanroom".into(),
        other => {
            let mut letters = other.chars();
            letters
                .next()
                .map(|first| first.to_uppercase().chain(letters).collect())
                .unwrap_or_default()
        }
    }
}

fn build_item(build: &BuildDto, players: Option<&PlayerCounts>) -> BuildItem {
    let block = block_of(build);
    let online = players
        .and_then(|players| players.per_build.get(&build.name))
        .filter(|players| players.max > 0);
    BuildItem {
        name: build.name.clone().into(),
        kind: build
            .loader
            .as_deref()
            .map(loader_label)
            .unwrap_or_default()
            .into(),
        minecraft: build.minecraft.clone().unwrap_or_default().into(),
        size: format::bytes(build.size_bytes).into(),
        install: match build.install {
            InstallState::Installed => Install::Installed,
            InstallState::Outdated => Install::Outdated,
            InstallState::Missing => Install::Missing,
        },
        has_features: build.has_features,
        blocked: block.is_some(),
        badge: block
            .as_ref()
            .map(|block| block.badge)
            .unwrap_or_default()
            .into(),
        reason: block.map(|block| block.reason).unwrap_or_default().into(),
        has_players: online.is_some(),
        online: online
            .map(|players| format::count(players.online))
            .unwrap_or_default()
            .into(),
        max: online
            .map(|players| format::count(players.max))
            .unwrap_or_default()
            .into(),
    }
}

fn news_item(item: &NewsItemDto, banner: Option<slint::Image>) -> NewsItem {
    let date = format::news_date(item.published_at_unix_nanos);
    NewsItem {
        title: item.title.clone().into(),
        body: item.body.clone().into(),
        caption: item.tag.clone().unwrap_or_else(|| date.clone()).into(),
        date: date.into(),
        tag: item.tag.clone().unwrap_or_default().into(),
        link: item.link.clone().unwrap_or_default().into(),
        has_banner: banner.is_some(),
        banner: banner.unwrap_or_default(),
    }
}

fn initial_of(name: &str) -> String {
    name.chars()
        .next()
        .map_or_else(|| "?".into(), |first| first.to_uppercase().collect())
}

fn account_item(account: &SelectedAccount, current: Option<&str>) -> AccountItem {
    AccountItem {
        uuid: account.uuid.clone().into(),
        name: account.name.clone().into(),
        initial: initial_of(&account.name).into(),
        current: current == Some(account.uuid.as_str()),
    }
}
