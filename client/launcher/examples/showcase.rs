use std::rc::Rc;

use image::ImageEncoder;
use slint::{ComponentHandle, Image, ModelRc, SharedString, VecModel};

use laminara::scene;
use laminara::{
    AccountItem, BuildItem, ConsoleLine, ConsoleStore, ConsoleWindow, FeatureRow, Install,
    LibraryRow, LogRow, MainWindow, Media, ModalKind, NewsItem, Phase, Store,
};

const FACE: &str = r##"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 8 8" shape-rendering="crispEdges"><rect width="8" height="8" fill="#c8967a"/><rect width="8" height="2" fill="#5a3214"/><rect y="2" width="1" height="2" fill="#5a3214"/><rect x="7" y="2" width="1" height="2" fill="#5a3214"/><rect x="1" y="4" width="2" height="1" fill="#ffffff"/><rect x="2" y="4" width="1" height="1" fill="#283cc8"/><rect x="5" y="4" width="2" height="1" fill="#ffffff"/><rect x="5" y="4" width="1" height="1" fill="#283cc8"/><rect x="3" y="6" width="2" height="1" fill="#8a5a44"/></svg>"##;

#[allow(clippy::too_many_arguments)]
fn build(
    name: &str,
    kind: &str,
    minecraft: &str,
    size: &str,
    install: Install,
    online: &str,
    max: &str,
    features: bool,
) -> BuildItem {
    BuildItem {
        name: name.into(),
        kind: kind.into(),
        minecraft: minecraft.into(),
        size: size.into(),
        install,
        has_features: features,
        has_players: true,
        online: online.into(),
        max: max.into(),
        ..BuildItem::default()
    }
}

fn builds() -> Vec<BuildItem> {
    vec![
        build(
            "Anarchy Universe",
            "NeoForge",
            "1.21.1",
            "2,3 ГБ",
            Install::Installed,
            "1\u{a0}284",
            "2\u{a0}000",
            true,
        ),
        build(
            "Automation Factory",
            "Forge",
            "1.21.1",
            "2,9 ГБ",
            Install::Outdated,
            "512",
            "800",
            false,
        ),
        build(
            "RPG Ascendancy",
            "Fabric",
            "1.20.1",
            "1,7 ГБ",
            Install::Missing,
            "964",
            "1\u{a0}500",
            false,
        ),
        build(
            "Industrial Galaxy",
            "NeoForge",
            "1.21.1",
            "2,8 ГБ",
            Install::Missing,
            "341",
            "500",
            false,
        ),
        build(
            "Pixelmon Odyssey",
            "Forge",
            "1.20.4",
            "3,7 ГБ",
            Install::Installed,
            "1\u{a0}607",
            "2\u{a0}500",
            false,
        ),
    ]
}

fn news() -> Vec<NewsItem> {
    vec![
        NewsItem {
            title: "Открыли новый сезон Anarchy Universe".into(),
            body: "Сброс карты в субботу в 18:00 по Москве. Сохраните ценное в эндер-сундуки — их перенесём.".into(),
            date: "27 октября".into(),
            tag: "Сезон".into(),
            caption: "Сезон".into(),
            link: "https://example.com".into(),
            ..NewsItem::default()
        },
        NewsItem {
            title: "Технические работы 2 ноября".into(),
            body: "С 03:00 до 05:00 сервер будет недоступен.".into(),
            date: "2 ноября".into(),
            caption: "2 ноября".into(),
            ..NewsItem::default()
        },
        NewsItem {
            title: "Новая сборка Pixelmon Odyssey".into(),
            body: "Добавили покемонов девятого поколения.".into(),
            date: "6 ноября".into(),
            tag: "Сборки".into(),
            caption: "Сборки".into(),
            ..NewsItem::default()
        },
    ]
}

fn option(
    depth: i32,
    gap: f32,
    title: &str,
    description: &str,
    badge: &str,
    active: bool,
    single: bool,
) -> FeatureRow {
    FeatureRow {
        depth,
        gap,
        title: title.into(),
        description: description.into(),
        badge: badge.into(),
        active,
        single,
        ..FeatureRow::default()
    }
}

fn header(depth: i32, gap: f32, title: &str, description: &str) -> FeatureRow {
    FeatureRow {
        header: true,
        depth,
        gap,
        title: title.into(),
        description: description.into(),
        opens: depth > 0,
        ..FeatureRow::default()
    }
}

fn feature_rows() -> Vec<FeatureRow> {
    vec![
        header(0, 0.0, "Графика", "Движок рендера"),
        FeatureRow {
            none: true,
            ..option(0, 8.0, "Нет", "", "", false, true)
        },
        option(
            0,
            6.0,
            "Sodium",
            "Быстрый рендер",
            "Рекомендуется",
            true,
            true,
        ),
        header(1, 12.0, "Дополнения Sodium", ""),
        option(1, 8.0, "Sodium Extra", "", "", false, false),
        option(1, 6.0, "Reese\u{2019}s Shadows", "", "", false, false),
        option(0, 6.0, "Embeddium", "Альтернатива", "", false, true),
        header(0, 24.0, "Дополнительно", ""),
        option(0, 8.0, "JEI", "Просмотр рецептов", "", true, false),
        option(0, 6.0, "JourneyMap", "Мини-карта", "", false, false),
    ]
}

const SCRIPT: [(i32, &str); 18] = [
    (
        1,
        "[main/INFO] [cpw.mods.modlauncher.Launcher/MODLAUNCHER]: ModLauncher running: args [--username, Mykyta, --version, 1.20.1]",
    ),
    (
        1,
        "[main/INFO] [net.minecraftforge.fml.loading.ImmediateWindowHandler/]: Loading ImmediateWindowProvider fmlearlywindow",
    ),
    (
        0,
        "[main/DEBUG] [net.minecraftforge.fml.loading.moddiscovery.ModDiscoverer/SCAN]: Found mod file sodium-0.5.11.jar of type MOD",
    ),
    (
        1,
        "[main/INFO] [mixin/]: SpongePowered MIXIN Subsystem Version=0.8.5 Source=union:/libraries/org/spongepowered/mixin/0.8.5/mixin-0.8.5.jar",
    ),
    (
        2,
        "[main/WARN] [mixin/]: Reference map \u{27}journeymap.refmap.json\u{27} for journeymap.mixins.json could not be read. If this is a development environment you can ignore this message",
    ),
    (
        2,
        "OpenJDK 64-Bit Server VM warning: Options -Xverify:none and -noverify were deprecated in JDK 13 and will likely be removed in a future release.",
    ),
    (
        1,
        "[Render thread/INFO] [minecraft/Minecraft]: Setting user: Mykyta",
    ),
    (
        1,
        "[Render thread/INFO] [minecraft/Minecraft]: Backend library: LWJGL version 3.3.1 build 7",
    ),
    (
        1,
        "[modloading-worker-0/INFO] [net.minecraftforge.common.ForgeMod/FORGEMOD]: Forge mod loading, version 47.3.0, for MC 1.20.1 with MCP 20230612.114412",
    ),
    (
        2,
        "[modloading-worker-0/WARN] [net.minecraftforge.common.ForgeConfigSpec/CORE]: Configuration file config/create-client.toml is not correct. Correcting",
    ),
    (
        3,
        "[Render thread/ERROR] [minecraft/TextureManager]: Failed to load texture: journeymap:textures/ui/minimap/frame.png",
    ),
    (
        3,
        "java.io.FileNotFoundException: journeymap:textures/ui/minimap/frame.png",
    ),
    (
        3,
        "\tat net.minecraft.server.packs.resources.ResourceProvider.lambda$getResourceOrThrow$1(ResourceProvider.java:17) ~[client-1.20.1-20230612.114412-srg.jar%23312!/:?]",
    ),
    (
        3,
        "\tat net.minecraft.client.renderer.texture.SimpleTexture$TextureImage.load(SimpleTexture.java:73) ~[client-1.20.1-20230612.114412-srg.jar%23312!/:?]",
    ),
    (
        1,
        "[Render thread/INFO] [minecraft/SoundEngine]: Sound engine started",
    ),
    (
        1,
        "[Render thread/INFO] [minecraft/TextureAtlas]: Created: 1024x1024x4 minecraft:textures/atlas/blocks.png-atlas",
    ),
    (
        1,
        "[Render thread/INFO] [journeymap/]: Журнал JourneyMap: карта мира «Мир Приключений» загружена за 412 мс",
    ),
    (
        1,
        "[Render thread/INFO] [minecraft/ChatComponent]: [CHAT] Добро пожаловать на сервер, Mykyta!",
    ),
];

fn log_lines() -> Vec<ConsoleLine> {
    (0..240usize)
        .map(|seq| {
            let (level, text) = SCRIPT[seq % SCRIPT.len()];
            let seconds = (seq / 3) % 60;
            let stamped = if text.starts_with('[') {
                format!("[19:22:{seconds:02}] {text}")
            } else {
                text.to_string()
            };
            ConsoleLine {
                text: SharedString::from(stamped),
                level,
                hit: false,
                rich: Default::default(),
            }
        })
        .collect()
}

fn face_uri() -> String {
    use base64::Engine;
    let face = Image::load_from_svg_data(FACE.as_bytes())
        .unwrap()
        .to_rgba8()
        .unwrap();
    let mut png = Vec::new();
    image::codecs::png::PngEncoder::new(&mut png)
        .write_image(
            face.as_bytes(),
            face.width(),
            face.height(),
            image::ExtendedColorType::Rgba8,
        )
        .unwrap();
    format!(
        "data:image/png;base64,{}",
        base64::engine::general_purpose::STANDARD.encode(png)
    )
}

fn home(window: &MainWindow) {
    let store = window.global::<Store>();
    let items = builds();
    store.set_phase(Phase::Home);
    store.set_has_total(true);
    store.set_total_online("4\u{a0}708".into());
    store.set_total_max("7\u{a0}300".into());
    store.set_account_name("Mykyta".into());
    store.set_account_initial("M".into());
    store.set_has_face(true);
    store.set_face(laminara::media::avatar(&face_uri(), scene::FACE_PIXELS).unwrap());
    store.set_multi_account(true);
    store.set_max_accounts(5);
    store.set_selected(items[0].name.clone());
    store.set_has_build(true);
    store.set_build(items[0].clone());
    store.set_action_label("ИГРАТЬ".into());
    store.set_build_count(items.len() as i32);
    store.set_library_caption("5 сборок".into());
    let grid = Rc::new(VecModel::<LibraryRow>::default());
    scene::fill_library(&grid, &items);
    store.set_library(ModelRc::from(grid));
    store.set_builds(ModelRc::new(VecModel::from(items)));
    store.set_running_build("Anarchy Universe".into());
    store.set_version("1.16.0".into());
    store.set_accounts(ModelRc::new(VecModel::from(vec![
        AccountItem {
            uuid: "a".into(),
            name: "Mykyta".into(),
            initial: "M".into(),
            current: true,
        },
        AccountItem {
            uuid: "b".into(),
            name: "Builder".into(),
            initial: "B".into(),
            current: false,
        },
    ])));
}

fn with_news(window: &MainWindow) {
    let store = window.global::<Store>();
    let items = news();
    store.set_news_caption("3 записи".into());
    store.set_news_pairs(ModelRc::new(VecModel::from(scene::news_pairs(&items))));
    store.set_news_rest(ModelRc::new(VecModel::from(scene::news_rest(&items))));
    store.set_news(ModelRc::new(VecModel::from(items)));
    store.set_unread(2);
}

fn open(window: &MainWindow, kind: ModalKind) {
    let store = window.global::<Store>();
    store.set_modal(kind);
    store.set_modal_open(true);
    store.set_modal_profile("Anarchy Universe".into());
}

fn capture_later<T: ComponentHandle + 'static>(weak: slint::Weak<T>, output: Option<String>) {
    let Some(output) = output else {
        return;
    };
    let hovered = weak.clone();
    slint::Timer::single_shot(std::time::Duration::from_millis(150), move || {
        if let Some(component) = hovered.upgrade() {
            hover(component.window());
        }
    });
    slint::Timer::single_shot(std::time::Duration::from_millis(1000), move || {
        if let Some(component) = weak.upgrade() {
            let shot = component.window().take_snapshot().unwrap();
            image::save_buffer(
                &output,
                shot.as_bytes(),
                shot.width(),
                shot.height(),
                image::ColorType::Rgba8,
            )
            .unwrap();
            let _ = slint::quit_event_loop();
        }
    });
}

fn hover(window: &slint::Window) {
    let Some(spot) = std::env::args().nth(3) else {
        return;
    };
    let points: Vec<slint::LogicalPosition> = spot
        .split('>')
        .filter_map(|point| {
            let (x, y) = point.split_once(',')?;
            Some(slint::LogicalPosition::new(
                x.parse().ok()?,
                y.parse().ok()?,
            ))
        })
        .collect();
    use slint::platform::{PointerEventButton, WindowEvent};
    match points.as_slice() {
        [position] => window.dispatch_event(WindowEvent::PointerMoved {
            position: *position,
        }),
        [from, to] => {
            let button = PointerEventButton::Left;
            window.dispatch_event(WindowEvent::PointerMoved { position: *from });
            window.dispatch_event(WindowEvent::PointerPressed {
                position: *from,
                button,
            });
            window.dispatch_event(WindowEvent::PointerMoved { position: *to });
            window.dispatch_event(WindowEvent::PointerReleased {
                position: *to,
                button,
            });
        }
        _ => {}
    }
}

fn console(output: Option<String>) -> Result<(), slint::PlatformError> {
    let window = ConsoleWindow::new()?;
    let store = window.global::<ConsoleStore>();
    store.set_ready(true);
    store.set_build("Anarchy Universe".into());
    store.set_status_label("Игра запущена".into());
    store.set_status_tone(0);
    store.set_started(true);
    store.set_running(true);
    store.set_total(240);
    store.set_shown(240);
    store.set_count_all("240".into());
    store.set_count_warn("67".into());
    store.set_count_error("53".into());
    store.set_has_warn(true);
    store.set_has_error(true);
    store.set_summary("240 строк".into());
    store.set_lines(ModelRc::new(VecModel::from(log_lines())));
    window.show()?;
    let weak = window.as_weak();
    slint::Timer::single_shot(std::time::Duration::from_millis(300), move || {
        if let Some(window) = weak.upgrade() {
            window.invoke_pin();
        }
    });
    capture_later(window.as_weak(), output);
    slint::run_event_loop()
}

fn main() -> Result<(), slint::PlatformError> {
    let scene = std::env::args().nth(1).unwrap_or_else(|| "home".into());
    let output = std::env::args().nth(2);
    if scene == "console" {
        return console(output);
    }
    let window = MainWindow::new()?;
    scene::bind_formatters(&window);
    let hero = laminara::media::hero("");
    let media = window.global::<Media>();
    media.set_hero(hero.still.sharp());
    media.set_hero_blurred(hero.still.blurred());
    window.global::<Store>().on_only_digits(|text| text);
    home(&window);
    window.show()?;
    let weak = window.as_weak();
    slint::Timer::single_shot(std::time::Duration::from_millis(400), move || {
        if let Some(window) = weak.upgrade() {
            stage(&window, &scene);
            capture_later(window.as_weak(), output.clone());
        }
    });
    slint::run_event_loop()
}

fn stage(window: &MainWindow, scene: &str) {
    let store = window.global::<Store>();
    match scene {
        "accounts" | "general" | "build" | "features" | "library" | "news" | "crash" => {
            scene::freeze_backdrop(window, scene::MODAL_FROST)
        }
        "sync" => scene::freeze_backdrop(window, scene::SYNC_FROST),
        _ => {}
    }
    match scene {
        "login" => store.set_phase(Phase::Login),
        "menu" => store.set_menu_open(true),
        "accounts" => open(window, ModalKind::Accounts),
        "general" => {
            store.set_general_ready(true);
            store.set_install_dir("~/.local/share/laminara/games".into());
            open(window, ModalKind::General);
        }
        "build" => {
            store.set_build_ready(true);
            store.set_memory(4096);
            store.set_memory_ceiling(12288);
            open(window, ModalKind::Build);
        }
        "features" => {
            store.set_features_ready(true);
            store.set_feature_rows(ModelRc::new(VecModel::from(feature_rows())));
            open(window, ModalKind::Features);
        }
        "library" => open(window, ModalKind::Library),
        "home-news" => with_news(window),
        "news" => {
            with_news(window);
            open(window, ModalKind::News);
        }
        "error" => {
            store.set_error_shown(true);
            store.set_error_message(
                "Загрузка прервалась: сервер не отвечает или пропала связь. Нажмите «Повторить», скачанное не потеряется".into(),
            );
            store.set_error_retry(true);
        }
        "sync" => {
            store.set_phase(Phase::Syncing);
            store.set_sync_stage("Загрузка".into());
            store.set_sync_build("Anarchy Universe".into());
            store.set_sync_fraction(0.5);
            store.set_sync_amount("1,1 ГБ / 2,2 ГБ".into());
            store.set_sync_files("600 / 1200 файлов".into());
            store.set_sync_speed("11,9 МБ/с".into());
            store.set_sync_left("2 минуты".into());
            store.set_sync_path("mods/module-6.jar".into());
            store.set_sync_cancellable(true);
        }
        "running" => store.set_phase(Phase::Running),
        "crash" => {
            let rows: Vec<LogRow> = (0..12usize)
                .map(|index| LogRow {
                    text: format!("[19:22:{index:02}] [Render thread/INFO] [minecraft/Minecraft]: Line number {index} of the crash log").into(),
                    level: [1, 2, 3, 0][index % 4],
                })
                .collect();
            store.set_crash_log(ModelRc::new(VecModel::from(rows)));
            store.set_crash_code("1".into());
            store.set_crash_open(true);
        }
        "update-banner" => {
            store.set_update_banner(true);
            store.set_update_version("1.17.0".into());
            store.set_update_mandatory(true);
            store.set_update_detail("17,2 МБ · скачайте свежий лаунчер с сайта проекта".into());
        }
        "updating" => {
            store.set_phase(Phase::Updating);
            store.set_update_version("1.17.0".into());
            store.set_update_fraction(1.0 / 3.0);
            store.set_update_progress("5,7 МБ / 17,2 МБ".into());
        }
        _ => {}
    }
}
