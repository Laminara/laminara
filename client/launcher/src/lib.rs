mod accounts;
mod auth;
mod baked;
mod commands;
mod console;
mod format;
mod logging;
pub mod media;
pub mod motion;
mod runtime;
mod setup;
mod ui;

pub use ui::scene;

use std::sync::Arc;

slint::include_modules!();

pub fn run() {
    let name = setup::storage_name();
    let data_dir = setup::adopt_legacy(dirs::data_dir(), &name);
    setup::adopt_legacy(dirs::config_dir(), &name);
    let log_dir = data_dir.map(|dir| logging::log_dir(&dir));
    let _log_guard = log_dir.as_ref().and_then(|dir| logging::init(dir));
    tracing::info!(
        version = env!("LAMINARA_VERSION"),
        "Laminara launcher starting"
    );

    let state = {
        let _context = runtime::get().enter();
        setup::init_state()
    };
    let state = match state {
        Ok(state) => Arc::new(state),
        Err(error) => {
            tracing::error!("init failed: {error}");
            rfd::MessageDialog::new()
                .set_level(rfd::MessageLevel::Error)
                .set_title("Лаунчер не запустился")
                .set_description(format!("Не удалось подготовить лаунчер: {error}"))
                .show();
            std::process::exit(1);
        }
    };
    laminara_core::update::swap::cleanup_stale(&laminara_core::update::detect());
    tracing::info!("launcher initialised");

    if let Err(error) = ui::run(state) {
        tracing::error!(%error, "окно лаунчера не открылось");
        std::process::exit(1);
    }
}
