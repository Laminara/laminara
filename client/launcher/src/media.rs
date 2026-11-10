use std::sync::Arc;

use image::imageops::FilterType;
use image::{DynamicImage, GenericImageView, Rgb, RgbImage};
use slint::{Image, Rgb8Pixel, Rgba8Pixel, SharedPixelBuffer};

use crate::motion::HeroMotion;
use crate::setup::decode_data_uri;

const DEFAULT_HERO: &[u8] = include_bytes!("../assets/hero.jpg");
const HERO_MAX_WIDTH: u32 = 1920;
const BLUR_DOWNSCALE: u32 = 4;
const BLUR_SIGMA: f32 = 3.0;
const BLUR_PASSES: usize = 3;

pub struct Hero {
    pub still: HeroFrame,
    pub motion: Option<HeroMotion>,
}

pub struct HeroFrame {
    sharp: SharedPixelBuffer<Rgb8Pixel>,
    blurred: SharedPixelBuffer<Rgb8Pixel>,
}

impl HeroFrame {
    pub(crate) fn from_picture(picture: &DynamicImage) -> Self {
        let fitted;
        let picture = if picture.width() > HERO_MAX_WIDTH {
            let height = picture.height() * HERO_MAX_WIDTH / picture.width();
            fitted = picture.resize_exact(HERO_MAX_WIDTH, height, FilterType::Triangle);
            &fitted
        } else {
            picture
        };
        HeroFrame::from_canvas(rgb_buffer(picture))
    }

    pub(crate) fn from_canvas(sharp: SharedPixelBuffer<Rgb8Pixel>) -> Self {
        if sharp.width() > HERO_MAX_WIDTH {
            let pixels =
                RgbImage::from_raw(sharp.width(), sharp.height(), sharp.as_bytes().to_vec());
            if let Some(pixels) = pixels {
                return HeroFrame::from_picture(&DynamicImage::ImageRgb8(pixels));
            }
        }
        let blurred = soften(shrunk(&sharp));
        HeroFrame {
            blurred: SharedPixelBuffer::clone_from_slice(
                blurred.as_raw(),
                blurred.width(),
                blurred.height(),
            ),
            sharp,
        }
    }

    pub fn sharp(&self) -> Image {
        Image::from_rgb8(self.sharp.clone())
    }

    pub fn blurred(&self) -> Image {
        Image::from_rgb8(self.blurred.clone())
    }
}

pub fn hero(data_uri: &str) -> Hero {
    let custom = custom_hero(data_uri);
    let motion = custom
        .as_ref()
        .and_then(|bytes| HeroMotion::from_bytes(bytes.clone()));
    let picture = custom
        .and_then(|bytes| image::load_from_memory(&bytes).ok())
        .unwrap_or_else(|| image::load_from_memory(DEFAULT_HERO).expect("bundled hero"));
    Hero {
        still: HeroFrame::from_picture(&picture),
        motion,
    }
}

fn shrunk(sharp: &SharedPixelBuffer<Rgb8Pixel>) -> RgbImage {
    let (width, height) = (sharp.width() as usize, sharp.height() as usize);
    let pixels = sharp.as_slice();
    let block = BLUR_DOWNSCALE as usize;
    let small_width = (width / block).max(1);
    let small_height = (height / block).max(1);
    RgbImage::from_fn(small_width as u32, small_height as u32, |x, y| {
        let (x, y) = (x as usize * block, y as usize * block);
        let mut sum = [0u32; 3];
        let mut count = 0;
        for row in y..(y + block).min(height) {
            for pixel in &pixels[row * width + x..row * width + (x + block).min(width)] {
                sum[0] += u32::from(pixel.r);
                sum[1] += u32::from(pixel.g);
                sum[2] += u32::from(pixel.b);
                count += 1;
            }
        }
        let count = count.max(1);
        Rgb([
            (sum[0] / count) as u8,
            (sum[1] / count) as u8,
            (sum[2] / count) as u8,
        ])
    })
}

fn soften(mut image: RgbImage) -> RgbImage {
    let (width, height) = (image.width() as usize, image.height() as usize);
    let mut spare = vec![0u8; width * height * 3];
    for radius in box_radii(BLUR_SIGMA, BLUR_PASSES) {
        box_pass(&image, &mut spare, height, width, 3, width * 3, radius);
        box_pass(&spare, &mut image, width, height, width * 3, 3, radius);
    }
    image
}

fn box_radii(sigma: f32, passes: usize) -> Vec<usize> {
    let variance = 12.0 * sigma * sigma;
    let count = passes as f32;
    let mut lower = (variance / count + 1.0).sqrt().floor() as usize;
    if lower.is_multiple_of(2) {
        lower -= 1;
    }
    let lower_f = lower as f32;
    let narrow = ((variance - count * lower_f * lower_f - 4.0 * count * lower_f - 3.0 * count)
        / (-4.0 * lower_f - 4.0))
        .round() as usize;
    (0..passes)
        .map(|pass| {
            if pass < narrow {
                lower / 2
            } else {
                lower / 2 + 1
            }
        })
        .collect()
}

fn box_pass(
    source: &[u8],
    target: &mut [u8],
    lines: usize,
    length: usize,
    along: usize,
    across: usize,
    radius: usize,
) {
    let span = (2 * radius + 1) as u32;
    for line in 0..lines {
        let base = line * across;
        for channel in 0..3 {
            let at =
                |index: usize| u32::from(source[base + index.min(length - 1) * along + channel]);
            let mut sum = (0..=radius).map(at).sum::<u32>() + radius as u32 * at(0);
            for index in 0..length {
                target[base + index * along + channel] = ((sum + span / 2) / span) as u8;
                sum = sum + at(index + radius + 1) - at(index.saturating_sub(radius));
            }
        }
    }
}

fn custom_hero(data_uri: &str) -> Option<Arc<[u8]>> {
    if data_uri.is_empty() {
        return None;
    }
    if data_uri.starts_with("data:video/") {
        tracing::warn!("видео на фоне не поддерживается — показываю картинку по умолчанию");
        return None;
    }
    let bytes = decode_data_uri(data_uri).filter(|bytes| image::guess_format(bytes).is_ok());
    if bytes.is_none() {
        tracing::warn!("фон проекта не разобрался как картинка — показываю картинку по умолчанию");
    }
    bytes.map(Arc::from)
}

const FROST_DOWNSCALE: u32 = 4;

pub fn frosted(
    snapshot: SharedPixelBuffer<Rgba8Pixel>,
    blur: f32,
    scale: f32,
    tint: Rgb<u8>,
    cover: f32,
) -> Image {
    let Some(frame) = image::RgbaImage::from_raw(
        snapshot.width(),
        snapshot.height(),
        snapshot.as_bytes().to_vec(),
    ) else {
        return Image::default();
    };
    let small = DynamicImage::ImageRgba8(frame).resize_exact(
        (snapshot.width() / FROST_DOWNSCALE).max(1),
        (snapshot.height() / FROST_DOWNSCALE).max(1),
        FilterType::Triangle,
    );
    let mut frosted = small.blur(blur * scale / FROST_DOWNSCALE as f32).to_rgb8();
    for pixel in frosted.pixels_mut() {
        for (channel, toward) in pixel.0.iter_mut().zip(tint.0) {
            *channel = (*channel as f32 * (1.0 - cover) + toward as f32 * cover).round() as u8;
        }
    }
    rgb(&DynamicImage::ImageRgb8(frosted))
}

pub struct Logo {
    pub full: Image,
    pub mark: Option<Image>,
}

pub fn logo(data_uri: &str, mark_size: u32, mark_radius: f32) -> Option<Logo> {
    let bytes = decode_data_uri(data_uri)?;
    if data_uri.starts_with("data:image/svg") {
        let full = Image::load_from_svg_data(&bytes).ok()?;
        return Some(Logo { full, mark: None });
    }
    let decoded = image::load_from_memory(&bytes).ok()?;
    Some(Logo {
        full: rgba(&decoded),
        mark: Some(rounded(&decoded, mark_size, mark_radius)),
    })
}

fn rounded(decoded: &DynamicImage, size: u32, radius: f32) -> Image {
    let fitted = decoded.resize(size, size, FilterType::Lanczos3).to_rgba8();
    let mut canvas = image::RgbaImage::new(size, size);
    image::imageops::overlay(
        &mut canvas,
        &fitted,
        ((size - fitted.width()) / 2) as i64,
        ((size - fitted.height()) / 2) as i64,
    );
    mask(&mut canvas, radius);
    rgba(&DynamicImage::ImageRgba8(canvas))
}

pub fn avatar(data_uri: &str, size: u32) -> Option<Image> {
    let mut face = decode(data_uri)?
        .resize_exact(size, size, FilterType::Nearest)
        .to_rgba8();
    mask(&mut face, size as f32 / 2.0);
    Some(rgba(&DynamicImage::ImageRgba8(face)))
}

fn decode(data_uri: &str) -> Option<DynamicImage> {
    let bytes = decode_data_uri(data_uri)?;
    image::load_from_memory(&bytes).ok()
}

fn mask(canvas: &mut image::RgbaImage, radius: f32) {
    let (width, height) = (canvas.width() as f32, canvas.height() as f32);
    for (x, y, pixel) in canvas.enumerate_pixels_mut() {
        let px = x as f32 + 0.5;
        let py = y as f32 + 0.5;
        let dx = (radius - px).max(px - (width - radius)).max(0.0);
        let dy = (radius - py).max(py - (height - radius)).max(0.0);
        let outside = (dx * dx + dy * dy).sqrt() - radius;
        let coverage = (0.5 - outside).clamp(0.0, 1.0);
        pixel.0[3] = (pixel.0[3] as f32 * coverage).round() as u8;
    }
}

pub fn picture(data_uri: &str) -> Option<Image> {
    let bytes = decode_data_uri(data_uri)?;
    if data_uri.starts_with("data:image/svg") {
        return Image::load_from_svg_data(&bytes).ok();
    }
    let decoded = image::load_from_memory(&bytes).ok()?;
    Some(rgba(&decoded))
}

fn rgb(picture: &DynamicImage) -> Image {
    Image::from_rgb8(rgb_buffer(picture))
}

fn rgb_buffer(picture: &DynamicImage) -> SharedPixelBuffer<Rgb8Pixel> {
    let pixels = picture.to_rgb8();
    SharedPixelBuffer::<Rgb8Pixel>::clone_from_slice(
        pixels.as_raw(),
        pixels.width(),
        pixels.height(),
    )
}

fn rgba(picture: &DynamicImage) -> Image {
    let (width, height) = picture.dimensions();
    let pixels = picture.to_rgba8();
    let buffer = SharedPixelBuffer::<Rgba8Pixel>::clone_from_slice(pixels.as_raw(), width, height);
    Image::from_rgba8(buffer)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_quick_blur_looks_like_a_gaussian_of_the_same_sigma() {
        let picture = image::load_from_memory(DEFAULT_HERO).unwrap();
        let small = shrunk(&rgb_buffer(&picture));
        let reference = image::imageops::blur(&small, BLUR_SIGMA);
        let quick = soften(small);
        let total: u64 = quick
            .as_raw()
            .iter()
            .zip(reference.as_raw())
            .map(|(a, b)| u64::from(a.abs_diff(*b)))
            .sum();
        let mean = total as f64 / quick.as_raw().len() as f64;
        assert!(mean < 0.5, "mean difference {mean}");
    }
}
