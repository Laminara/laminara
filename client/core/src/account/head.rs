use base64::Engine;
use serde::Deserialize;

use crate::error::CoreError;
use crate::transport::Transport;

use super::read_limited;

const PROFILE_BYTES_LIMIT: usize = 256 * 1024;
const SKIN_BYTES_LIMIT: usize = 2 * 1024 * 1024;
const SKIN_UNIT: u32 = 64;
const FACE: u32 = 8;
const SKIN_ERROR: &str = "skin";

fn skin_error(message: String) -> CoreError {
    CoreError::App {
        code: SKIN_ERROR.to_string(),
        message,
    }
}

#[derive(Deserialize)]
struct Profile {
    #[serde(default)]
    properties: Vec<Property>,
}

#[derive(Deserialize)]
struct Property {
    name: String,
    value: String,
}

#[derive(Deserialize)]
struct TexturesPayload {
    #[serde(default)]
    textures: Textures,
}

#[derive(Deserialize, Default)]
struct Textures {
    #[serde(rename = "SKIN")]
    skin: Option<Texture>,
}

#[derive(Deserialize)]
struct Texture {
    url: String,
}

pub async fn skin_url(
    transport: &Transport,
    base_url: &str,
    uuid: &str,
) -> Result<Option<String>, CoreError> {
    let url = format!(
        "{}/yggdrasil/sessionserver/session/minecraft/profile/{}",
        base_url.trim_end_matches('/'),
        uuid.replace('-', "")
    );
    let response = transport
        .client()
        .get(&url)
        .send()
        .await
        .map_err(CoreError::from)?;
    if response.status() == reqwest::StatusCode::NO_CONTENT
        || response.status() == reqwest::StatusCode::NOT_FOUND
    {
        return Ok(None);
    }
    if !response.status().is_success() {
        return Err(skin_error(format!(
            "профиль игрока: http {}",
            response.status()
        )));
    }
    let body = read_limited(response, PROFILE_BYTES_LIMIT, "запрос профиля игрока").await?;
    Ok(skin_url_from_profile(&body))
}

fn skin_url_from_profile(body: &[u8]) -> Option<String> {
    let profile: Profile = serde_json::from_slice(body).ok()?;
    let encoded = profile
        .properties
        .into_iter()
        .find(|property| property.name == "textures")?
        .value;
    let decoded = base64::engine::general_purpose::STANDARD
        .decode(encoded.trim())
        .ok()?;
    let payload: TexturesPayload = serde_json::from_slice(&decoded).ok()?;
    payload
        .textures
        .skin
        .map(|skin| skin.url)
        .filter(|url| !url.is_empty())
}

pub async fn face_from_skin(transport: &Transport, url: &str) -> Result<String, CoreError> {
    let response = transport
        .client()
        .get(url)
        .send()
        .await
        .map_err(CoreError::from)?;
    if !response.status().is_success() {
        return Err(skin_error(format!(
            "картинка скина: http {}",
            response.status()
        )));
    }
    let bytes = read_limited(response, SKIN_BYTES_LIMIT, "запрос скина").await?;
    let face = face_png(&bytes)?;
    Ok(format!(
        "data:image/png;base64,{}",
        base64::engine::general_purpose::STANDARD.encode(face)
    ))
}

struct Skin {
    width: u32,
    height: u32,
    pixels: Vec<u8>,
}

impl Skin {
    fn at(&self, x: u32, y: u32) -> [u8; 4] {
        let offset = ((y * self.width + x) * 4) as usize;
        [
            self.pixels[offset],
            self.pixels[offset + 1],
            self.pixels[offset + 2],
            self.pixels[offset + 3],
        ]
    }
}

fn face_png(bytes: &[u8]) -> Result<Vec<u8>, CoreError> {
    let skin = decode_skin(bytes)?;
    let scale = skin.width / SKIN_UNIT;
    let legacy = skin.height == skin.width / 2;
    let side = FACE * scale;
    let hat = !legacy || has_see_through_pixel(&skin, 32 * scale, 0, 64 * scale, 32 * scale);

    let mut face = Vec::with_capacity((side * side * 4) as usize);
    for y in 0..side {
        for x in 0..side {
            let [red, green, blue, _] = skin.at(FACE * scale + x, FACE * scale + y);
            let base = [red, green, blue, 255];
            let pixel = if hat {
                over(skin.at(40 * scale + x, FACE * scale + y), base)
            } else {
                base
            };
            face.extend_from_slice(&pixel);
        }
    }
    encode_png(side, &face)
}

fn has_see_through_pixel(skin: &Skin, left: u32, top: u32, right: u32, bottom: u32) -> bool {
    (top..bottom).any(|y| (left..right).any(|x| skin.at(x, y)[3] < 128))
}

fn over(top: [u8; 4], bottom: [u8; 4]) -> [u8; 4] {
    let alpha = u32::from(top[3]);
    let blend = |upper: u8, lower: u8| {
        ((u32::from(upper) * alpha + u32::from(lower) * (255 - alpha) + 127) / 255) as u8
    };
    [
        blend(top[0], bottom[0]),
        blend(top[1], bottom[1]),
        blend(top[2], bottom[2]),
        255,
    ]
}

fn decode_skin(bytes: &[u8]) -> Result<Skin, CoreError> {
    let invalid = |reason: String| skin_error(format!("картинка скина: {reason}"));
    let mut decoder = png::Decoder::new(std::io::Cursor::new(bytes));
    decoder.set_transformations(png::Transformations::EXPAND | png::Transformations::ALPHA);
    let mut reader = decoder.read_info().map_err(|e| invalid(e.to_string()))?;
    let size = reader
        .output_buffer_size()
        .ok_or_else(|| invalid("слишком большая".into()))?;
    let mut buffer = vec![0; size];
    let frame = reader
        .next_frame(&mut buffer)
        .map_err(|e| invalid(e.to_string()))?;
    let (width, height) = (frame.width, frame.height);
    if width == 0 || width % SKIN_UNIT != 0 || (height != width && height != width / 2) {
        return Err(invalid(format!("размер {width}×{height} не похож на скин")));
    }
    let samples = &buffer[..frame.buffer_size()];
    let pixels = match (frame.color_type, frame.bit_depth) {
        (png::ColorType::Rgba, png::BitDepth::Eight) => samples.to_vec(),
        (png::ColorType::GrayscaleAlpha, png::BitDepth::Eight) => samples
            .chunks_exact(2)
            .flat_map(|pair| [pair[0], pair[0], pair[0], pair[1]])
            .collect(),
        (png::ColorType::Rgba, png::BitDepth::Sixteen) => {
            samples.chunks_exact(2).map(|pair| pair[0]).collect()
        }
        (png::ColorType::GrayscaleAlpha, png::BitDepth::Sixteen) => samples
            .chunks_exact(4)
            .flat_map(|quad| [quad[0], quad[0], quad[0], quad[2]])
            .collect(),
        (color, depth) => return Err(invalid(format!("формат {color:?} {depth:?}"))),
    };
    Ok(Skin {
        width,
        height,
        pixels,
    })
}

fn encode_png(side: u32, rgba: &[u8]) -> Result<Vec<u8>, CoreError> {
    let failed = |e: png::EncodingError| skin_error(format!("лицо скина: {e}"));
    let mut out = Vec::new();
    let mut encoder = png::Encoder::new(&mut out, side, side);
    encoder.set_color(png::ColorType::Rgba);
    encoder.set_depth(png::BitDepth::Eight);
    let mut writer = encoder.write_header().map_err(failed)?;
    writer.write_image_data(rgba).map_err(failed)?;
    writer.finish().map_err(failed)?;
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    const SKIN: [u8; 4] = [200, 150, 120, 255];
    const EYE: [u8; 4] = [40, 60, 200, 255];
    const HAIR: [u8; 4] = [90, 50, 20, 255];

    fn skin(width: u32, height: u32, paint: impl Fn(u32, u32) -> [u8; 4]) -> Vec<u8> {
        let mut rgba = Vec::new();
        for y in 0..height {
            for x in 0..width {
                rgba.extend_from_slice(&paint(x, y));
            }
        }
        let mut out = Vec::new();
        let mut encoder = png::Encoder::new(&mut out, width, height);
        encoder.set_color(png::ColorType::Rgba);
        encoder.set_depth(png::BitDepth::Eight);
        let mut writer = encoder.write_header().unwrap();
        writer.write_image_data(&rgba).unwrap();
        writer.finish().unwrap();
        out
    }

    fn face_pixels(png_bytes: &[u8]) -> (u32, Vec<[u8; 4]>) {
        let skin = decode_skin_square(png_bytes);
        (
            skin.width,
            skin.pixels
                .chunks_exact(4)
                .map(|p| [p[0], p[1], p[2], p[3]])
                .collect(),
        )
    }

    fn decode_skin_square(bytes: &[u8]) -> Skin {
        let mut decoder = png::Decoder::new(std::io::Cursor::new(bytes));
        decoder.set_transformations(png::Transformations::EXPAND);
        let mut reader = decoder.read_info().unwrap();
        let mut buffer = vec![0; reader.output_buffer_size().unwrap()];
        let frame = reader.next_frame(&mut buffer).unwrap();
        Skin {
            width: frame.width,
            height: frame.height,
            pixels: buffer[..frame.buffer_size()].to_vec(),
        }
    }

    fn classic_face(x: u32, y: u32) -> [u8; 4] {
        let in_face = (8..16).contains(&x) && (8..16).contains(&y);
        let in_hat_front = (40..48).contains(&x) && (8..16).contains(&y);
        if in_face {
            if y == 12 && (x == 10 || x == 13) {
                EYE
            } else {
                SKIN
            }
        } else if in_hat_front && y == 8 {
            HAIR
        } else {
            [0, 0, 0, 0]
        }
    }

    #[test]
    fn the_face_carries_the_hat_layer_on_top() {
        let (side, pixels) = face_pixels(&face_png(&skin(64, 64, classic_face)).unwrap());
        assert_eq!(side, 8);
        assert_eq!(pixels[0], HAIR);
        assert_eq!(pixels[4 * 8 + 2], EYE);
        assert_eq!(pixels[4 * 8 + 5], EYE);
        assert_eq!(pixels[7 * 8], SKIN);
    }

    #[test]
    fn a_half_transparent_hat_pixel_is_blended_into_the_face() {
        let glass = |x: u32, y: u32| {
            if (40..48).contains(&x) && (8..16).contains(&y) {
                [255, 255, 255, 128]
            } else {
                classic_face(x, y)
            }
        };
        let (_, pixels) = face_pixels(&face_png(&skin(64, 64, glass)).unwrap());
        assert_eq!(pixels[7 * 8], [228, 203, 188, 255]);
    }

    #[test]
    fn an_old_skin_with_a_solid_hat_area_shows_the_bare_face() {
        let solid_hat = |x: u32, y: u32| {
            if (32..64).contains(&x) && y < 32 {
                [0, 0, 0, 255]
            } else {
                classic_face(x, y)
            }
        };
        let (_, pixels) = face_pixels(&face_png(&skin(64, 32, solid_hat)).unwrap());
        assert_eq!(pixels[0], SKIN);
        assert_eq!(pixels[4 * 8 + 2], EYE);
    }

    #[test]
    fn an_old_skin_with_a_real_hat_keeps_it() {
        let (_, pixels) = face_pixels(&face_png(&skin(64, 32, classic_face)).unwrap());
        assert_eq!(pixels[0], HAIR);
    }

    #[test]
    fn a_high_resolution_skin_gives_a_sharper_face() {
        let hd = |x: u32, y: u32| classic_face(x / 2, y / 2);
        let (side, pixels) = face_pixels(&face_png(&skin(128, 128, hd)).unwrap());
        assert_eq!(side, 16);
        assert_eq!(pixels[0], HAIR);
        assert_eq!(pixels[8 * 16 + 4], EYE);
    }

    #[test]
    fn a_picture_that_is_not_a_skin_is_refused() {
        assert!(face_png(&skin(100, 60, |_, _| SKIN)).is_err());
        assert!(face_png(b"not a png").is_err());
    }

    #[test]
    fn the_skin_address_is_read_from_the_signed_textures_property() {
        let textures = base64::engine::general_purpose::STANDARD.encode(
            r#"{"timestamp":1,"profileId":"abc","profileName":"Dela1s","textures":{"SKIN":{"url":"https://play.example/yggdrasil/textures/ab12","metadata":{"model":"slim"}}}}"#,
        );
        let profile = format!(
            r#"{{"id":"abc","name":"Dela1s","properties":[{{"name":"textures","value":"{textures}","signature":"sig"}}]}}"#
        );
        assert_eq!(
            skin_url_from_profile(profile.as_bytes()).as_deref(),
            Some("https://play.example/yggdrasil/textures/ab12")
        );
    }

    #[test]
    fn a_profile_without_a_skin_gives_no_address() {
        let textures = base64::engine::general_purpose::STANDARD.encode(r#"{"textures":{}}"#);
        let profile =
            format!(r#"{{"id":"abc","properties":[{{"name":"textures","value":"{textures}"}}]}}"#);
        assert_eq!(skin_url_from_profile(profile.as_bytes()), None);
        assert_eq!(skin_url_from_profile(br#"{"id":"abc","name":"x"}"#), None);
    }
}
