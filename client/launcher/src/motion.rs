use std::io::Cursor;
use std::sync::{Arc, Condvar, Mutex, PoisonError};
use std::time::{Duration, Instant};

use image::codecs::png::PngDecoder;
use image::codecs::webp::WebPDecoder;
use image::{AnimationDecoder, DynamicImage, Frames, ImageFormat};
use slint::{Rgb8Pixel, SharedPixelBuffer};

use crate::media::HeroFrame;

const MIN_FRAME_MS: u64 = 10;
const SLOW_FRAME_MS: u64 = 100;

#[derive(Clone)]
pub struct HeroMotion {
    bytes: Arc<[u8]>,
    format: ImageFormat,
}

impl HeroMotion {
    pub(crate) fn from_bytes(bytes: Arc<[u8]>) -> Option<Self> {
        let format = image::guess_format(&bytes).ok()?;
        let motion = HeroMotion { bytes, format };
        let mut reel = motion.reel()?;
        reel.next()?;
        reel.next()?;
        Some(motion)
    }

    fn reel(&self) -> Option<Reel> {
        let reader = Cursor::new(self.bytes.clone());
        let frames = match self.format {
            ImageFormat::Gif => return GifReel::open(reader).map(Reel::Gif),
            ImageFormat::WebP => WebPDecoder::new(reader).ok()?.into_frames(),
            ImageFormat::Png => {
                let decoder = PngDecoder::new(reader).ok()?;
                if !decoder.is_apng().ok()? {
                    return None;
                }
                decoder.apng().ok()?.into_frames()
            }
            _ => return None,
        };
        Some(Reel::Frames(frames))
    }

    pub fn play(self, show: impl Fn(HeroFrame) + Send + 'static) -> HeroPlayer {
        let gate = Arc::new((Mutex::new(true), Condvar::new()));
        let held = gate.clone();
        let spawned = std::thread::Builder::new()
            .name("hero-motion".into())
            .spawn(move || self.run(&held, show));
        if let Err(error) = spawned {
            tracing::warn!(%error, "анимация фона не запустилась");
        }
        HeroPlayer { gate }
    }

    fn run(&self, gate: &(Mutex<bool>, Condvar), show: impl Fn(HeroFrame)) {
        loop {
            let Some(mut reel) = self.reel() else {
                return;
            };
            let mut shown = 0;
            loop {
                wait_until_moving(gate);
                let started = Instant::now();
                let Some((frame, delay)) = reel.next() else {
                    break;
                };
                show(frame);
                shown += 1;
                std::thread::sleep(delay.saturating_sub(started.elapsed()));
            }
            if shown < 2 {
                return;
            }
        }
    }
}

pub struct HeroPlayer {
    gate: Arc<(Mutex<bool>, Condvar)>,
}

impl HeroPlayer {
    pub fn set_moving(&self, moving: bool) {
        let (flag, wake) = &*self.gate;
        let mut current = flag.lock().unwrap_or_else(PoisonError::into_inner);
        if *current != moving {
            *current = moving;
            wake.notify_all();
        }
    }
}

fn wait_until_moving(gate: &(Mutex<bool>, Condvar)) {
    let (flag, wake) = gate;
    let mut moving = flag.lock().unwrap_or_else(PoisonError::into_inner);
    while !*moving {
        moving = wake.wait(moving).unwrap_or_else(PoisonError::into_inner);
    }
}

fn paced(millis: u64) -> Duration {
    Duration::from_millis(if millis <= MIN_FRAME_MS {
        SLOW_FRAME_MS
    } else {
        millis
    })
}

enum Reel {
    Gif(Box<GifReel>),
    Frames(Frames<'static>),
}

impl Reel {
    fn next(&mut self) -> Option<(HeroFrame, Duration)> {
        match self {
            Reel::Gif(reel) => reel.next(),
            Reel::Frames(frames) => {
                let frame = frames.next()?.ok()?;
                let (numerator, denominator) = frame.delay().numer_denom_ms();
                let millis = u64::from(numerator) / u64::from(denominator.max(1));
                let picture = DynamicImage::ImageRgba8(frame.into_buffer());
                Some((HeroFrame::from_picture(&picture), paced(millis)))
            }
        }
    }
}

#[derive(Clone, Copy)]
struct Area {
    left: usize,
    top: usize,
    width: usize,
    height: usize,
}

enum Undo {
    Clear(Area),
    Restore(Area, Vec<Rgb8Pixel>),
}

struct GifReel {
    decoder: gif::Decoder<Cursor<Arc<[u8]>>>,
    global: Vec<u8>,
    width: u32,
    height: u32,
    canvas: Vec<Rgb8Pixel>,
    undo: Option<Undo>,
}

impl GifReel {
    fn open(reader: Cursor<Arc<[u8]>>) -> Option<Box<Self>> {
        let mut options = gif::DecodeOptions::new();
        options.set_color_output(gif::ColorOutput::Indexed);
        let decoder = options.read_info(reader).ok()?;
        let global = decoder
            .global_palette()
            .map(<[u8]>::to_vec)
            .unwrap_or_default();
        let width = u32::from(decoder.width()).max(1);
        let height = u32::from(decoder.height()).max(1);
        Some(Box::new(GifReel {
            decoder,
            global,
            width,
            height,
            canvas: vec![Rgb8Pixel::default(); width as usize * height as usize],
            undo: None,
        }))
    }

    fn next(&mut self) -> Option<(HeroFrame, Duration)> {
        let GifReel {
            decoder,
            global,
            width,
            height,
            canvas,
            undo,
        } = self;
        let stride = *width as usize;
        let rows = *height as usize;
        match undo.take() {
            Some(Undo::Clear(area)) => {
                paint(canvas, stride, area, |_, _| Some(Rgb8Pixel::default()))
            }
            Some(Undo::Restore(area, saved)) => paint(canvas, stride, area, |row, column| {
                Some(saved[row * area.width + column])
            }),
            None => {}
        }
        let frame = decoder.read_next_frame().ok()??;
        let left = usize::from(frame.left).min(stride);
        let top = usize::from(frame.top).min(rows);
        let area = Area {
            left,
            top,
            width: usize::from(frame.width).min(stride - left),
            height: usize::from(frame.height).min(rows - top),
        };
        *undo = match frame.dispose {
            gif::DisposalMethod::Background => Some(Undo::Clear(area)),
            gif::DisposalMethod::Previous => {
                Some(Undo::Restore(area, snapshot(canvas, stride, area)))
            }
            _ => None,
        };
        let palette = frame.palette.as_deref().unwrap_or(global);
        let source_width = usize::from(frame.width);
        paint(canvas, stride, area, |row, column| {
            let index = *frame.buffer.get(row * source_width + column)?;
            if frame.transparent == Some(index) {
                return None;
            }
            let at = usize::from(index) * 3;
            let colour = palette.get(at..at + 3)?;
            Some(Rgb8Pixel::new(colour[0], colour[1], colour[2]))
        });
        let delay = paced(u64::from(frame.delay) * 10);
        let mut shown = SharedPixelBuffer::<Rgb8Pixel>::new(*width, *height);
        shown.make_mut_slice().copy_from_slice(canvas);
        Some((HeroFrame::from_canvas(shown), delay))
    }
}

fn paint(
    pixels: &mut [Rgb8Pixel],
    stride: usize,
    area: Area,
    mut colour: impl FnMut(usize, usize) -> Option<Rgb8Pixel>,
) {
    for row in 0..area.height {
        let line = (area.top + row) * stride + area.left;
        for column in 0..area.width {
            if let Some(value) = colour(row, column) {
                pixels[line + column] = value;
            }
        }
    }
}

fn snapshot(pixels: &[Rgb8Pixel], stride: usize, area: Area) -> Vec<Rgb8Pixel> {
    (0..area.height)
        .flat_map(|row| {
            let line = (area.top + row) * stride + area.left;
            pixels[line..line + area.width].iter().copied()
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tricky_gif() -> Vec<u8> {
        let palette = [0u8, 0, 0, 255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0];
        let mut bytes = Vec::new();
        {
            let mut encoder = gif::Encoder::new(&mut bytes, 8, 6, &palette).unwrap();
            let mut frame =
                |left, top, width, height, fill: &dyn Fn(u16, u16) -> u8, dispose, transparent| {
                    let buffer = (0..height)
                        .flat_map(|y| (0..width).map(move |x| (x, y)))
                        .map(|(x, y)| fill(x, y))
                        .collect();
                    let frame = gif::Frame {
                        left,
                        top,
                        width,
                        height,
                        dispose,
                        transparent,
                        delay: 4,
                        buffer: std::borrow::Cow::Owned(buffer),
                        ..gif::Frame::default()
                    };
                    encoder.write_frame(&frame).unwrap();
                };
            frame(
                0,
                0,
                8,
                6,
                &|x, y| ((x + y) % 3) as u8,
                gif::DisposalMethod::Keep,
                None,
            );
            frame(
                2,
                1,
                4,
                3,
                &|x, _| if x % 2 == 0 { 4 } else { 3 },
                gif::DisposalMethod::Background,
                Some(3),
            );
            frame(
                1,
                2,
                5,
                3,
                &|_, y| y as u8 + 1,
                gif::DisposalMethod::Previous,
                None,
            );
            frame(
                3,
                0,
                5,
                2,
                &|x, _| x as u8 % 4,
                gif::DisposalMethod::Keep,
                Some(0),
            );
            frame(0, 3, 8, 3, &|_, _| 2, gif::DisposalMethod::Keep, None);
        }
        bytes
    }

    #[test]
    fn gif_frames_match_the_reference_decoder_pixel_for_pixel() {
        let bytes: Arc<[u8]> = tricky_gif().into();
        let reference: Vec<_> = image::codecs::gif::GifDecoder::new(Cursor::new(bytes.clone()))
            .unwrap()
            .into_frames()
            .collect::<Result<_, _>>()
            .unwrap();
        let mut reel = GifReel::open(Cursor::new(bytes)).unwrap();
        for (index, expected) in reference.iter().enumerate() {
            let (_, delay) = reel.next().unwrap();
            assert_eq!(delay, Duration::from_millis(40));
            let ours: Vec<[u8; 3]> = reel
                .canvas
                .as_slice()
                .iter()
                .map(|pixel| [pixel.r, pixel.g, pixel.b])
                .collect();
            let theirs: Vec<[u8; 3]> = expected
                .buffer()
                .pixels()
                .map(|pixel| [pixel[0], pixel[1], pixel[2]])
                .collect();
            assert_eq!(ours, theirs, "frame {index}");
        }
        assert!(reel.next().is_none());
    }

    #[test]
    fn a_still_gif_is_not_motion() {
        let palette = [0u8, 0, 0, 255, 255, 255];
        let mut bytes = Vec::new();
        {
            let mut encoder = gif::Encoder::new(&mut bytes, 2, 2, &palette).unwrap();
            encoder
                .write_frame(&gif::Frame::from_indexed_pixels(
                    2,
                    2,
                    vec![0, 1, 1, 0],
                    None,
                ))
                .unwrap();
        }
        assert!(HeroMotion::from_bytes(bytes.into()).is_none());
        assert!(HeroMotion::from_bytes(tricky_gif().into()).is_some());
    }
}
