use slint::{ComponentHandle, Model, ModelRc, VecModel};

use crate::{BuildItem, LibraryRow, MainWindow, Media, NewsItem, NewsPair, Store, Theme};
use crate::{format, media};

pub const FACE_PIXELS: u32 = 64;
const LIBRARY_COLUMNS: usize = 3;
const NEWS_REST: usize = 2;

#[derive(Clone, Copy)]
pub struct Frost {
    blur: f32,
    cover: f32,
    over_background: bool,
}

pub const MODAL_FROST: Frost = Frost {
    blur: 6.0,
    cover: 0.55,
    over_background: false,
};

pub const SYNC_FROST: Frost = Frost {
    blur: 4.0,
    cover: 0.5,
    over_background: true,
};

pub fn bind_formatters(window: &MainWindow) {
    window
        .global::<Store>()
        .on_memory_label(|megabytes| format::memory(megabytes.max(0) as u32).into());
}

pub fn freeze_backdrop(window: &MainWindow, frost: Frost) {
    let tint = if frost.over_background {
        let bg = window.global::<Theme>().get_bg();
        image::Rgb([bg.red(), bg.green(), bg.blue()])
    } else {
        image::Rgb([0, 0, 0])
    };
    let surface = window.window();
    match surface.take_snapshot() {
        Ok(snapshot) => window.global::<Media>().set_backdrop(media::frosted(
            snapshot,
            frost.blur,
            surface.scale_factor(),
            tint,
            frost.cover,
        )),
        Err(error) => tracing::warn!(%error, "снимок окна под затемнение не получился"),
    }
}

pub fn replace_rows<T: Clone + PartialEq + 'static>(model: &VecModel<T>, rows: Vec<T>) {
    let shared = rows.len().min(model.row_count());
    for (index, row) in rows.iter().enumerate().take(shared) {
        if model.row_data(index).as_ref() != Some(row) {
            model.set_row_data(index, row.clone());
        }
    }
    if model.row_count() > rows.len() {
        for index in (rows.len()..model.row_count()).rev() {
            model.remove(index);
        }
    } else {
        for row in rows.into_iter().skip(shared) {
            model.push(row);
        }
    }
}

pub fn fill_library(grid: &VecModel<LibraryRow>, items: &[BuildItem]) {
    let chunks: Vec<&[BuildItem]> = items.chunks(LIBRARY_COLUMNS).collect();
    while grid.row_count() > chunks.len() {
        grid.remove(grid.row_count() - 1);
    }
    for (index, chunk) in chunks.into_iter().enumerate() {
        let existing = grid.row_data(index).and_then(|row| {
            row.items
                .as_any()
                .downcast_ref::<VecModel<BuildItem>>()
                .map(|cells| replace_rows(cells, chunk.to_vec()))
        });
        if existing.is_none() {
            let row = LibraryRow {
                items: ModelRc::new(VecModel::from(chunk.to_vec())),
            };
            if index < grid.row_count() {
                grid.set_row_data(index, row);
            } else {
                grid.push(row);
            }
        }
    }
}

pub fn news_pairs(news: &[NewsItem]) -> Vec<NewsPair> {
    news.chunks(2)
        .map(|pair| NewsPair {
            left: pair[0].clone(),
            has_right: pair.len() > 1,
            right: pair.get(1).cloned().unwrap_or_default(),
        })
        .collect()
}

pub fn news_rest(news: &[NewsItem]) -> Vec<NewsItem> {
    news.iter().skip(1).take(NEWS_REST).cloned().collect()
}
