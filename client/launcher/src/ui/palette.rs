use slint::Color;

use crate::setup::Branding;

const WHITE: Color = Color::from_rgb_u8(255, 255, 255);
const DARK_INK: Color = Color::from_rgb_u8(0x1b, 0x14, 0x10);
const LOGIN_TINT: f32 = 0.06;
const STRONG_TINT: f32 = 0.18;

pub struct Palette {
    pub bg: Option<Color>,
    pub primary: Option<Color>,
    pub primary_strong: Option<Color>,
    pub primary_ink: Option<Color>,
}

pub fn from_branding(branding: &Branding) -> Palette {
    let primary = parse(&branding.primary_color);
    let ink = parse(&branding.primary_ink).or_else(|| primary.map(readable_ink));
    Palette {
        bg: parse(&branding.background_color),
        primary,
        primary_strong: primary.map(|colour| mix(colour, WHITE, STRONG_TINT)),
        primary_ink: ink,
    }
}

pub fn login_surface(bg: Color) -> Color {
    mix(bg, WHITE, LOGIN_TINT)
}

pub fn hex(colour: Color) -> String {
    format!(
        "#{:02x}{:02x}{:02x}",
        colour.red(),
        colour.green(),
        colour.blue()
    )
}

fn mix(base: Color, other: Color, amount: f32) -> Color {
    let blend = |a: u8, b: u8| (a as f32 * (1.0 - amount) + b as f32 * amount).round() as u8;
    Color::from_argb_u8(
        base.alpha(),
        blend(base.red(), other.red()),
        blend(base.green(), other.green()),
        blend(base.blue(), other.blue()),
    )
}

fn readable_ink(colour: Color) -> Color {
    let luminance = (0.299 * colour.red() as f32
        + 0.587 * colour.green() as f32
        + 0.114 * colour.blue() as f32)
        / 255.0;
    if luminance > 0.6 { DARK_INK } else { WHITE }
}

fn parse(raw: &str) -> Option<Color> {
    let [red, green, blue, alpha] = csscolorparser::parse(raw.trim()).ok()?.to_rgba8();
    Some(Color::from_argb_u8(alpha, red, green, blue))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hex_colours_of_every_length_parse() {
        assert_eq!(parse("#fff"), Some(Color::from_rgb_u8(255, 255, 255)));
        assert_eq!(parse("#ecc275"), Some(Color::from_rgb_u8(0xec, 0xc2, 0x75)));
        assert_eq!(parse("#00000080"), Some(Color::from_argb_u8(0x80, 0, 0, 0)));
        assert_eq!(parse(""), None);
        assert_eq!(parse("не цвет"), None);
    }

    #[test]
    fn any_css_colour_the_old_launcher_accepted_parses() {
        assert_eq!(parse("red"), Some(Color::from_rgb_u8(255, 0, 0)));
        assert_eq!(
            parse("rgb(236 194 117)"),
            Some(Color::from_rgb_u8(0xec, 0xc2, 0x75))
        );
        assert_eq!(
            parse("rgba(13, 10, 9, 0.5)"),
            Some(Color::from_argb_u8(128, 13, 10, 9))
        );
        assert_eq!(
            parse("hsl(0 100% 50%)"),
            Some(Color::from_rgb_u8(255, 0, 0))
        );
    }

    #[test]
    fn ink_follows_how_light_the_primary_is() {
        assert_eq!(readable_ink(Color::from_rgb_u8(0xec, 0xc2, 0x75)), DARK_INK);
        assert_eq!(readable_ink(Color::from_rgb_u8(0x20, 0x30, 0x80)), WHITE);
    }
}
