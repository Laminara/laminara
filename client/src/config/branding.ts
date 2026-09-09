import type { Loader } from "@/lib/types";

export interface Branding {
  name: string;
  windowTitle: string;
  tagline: string;
  primaryColor: string;
  primaryInk: string;
  backgroundColor: string;
  logoDataUri: string;
  heroMediaDataUri: string;
  siteUrl: string;
}

const fallback: Branding = {
  name: "LAMINARA",
  windowTitle: "Laminara",
  tagline: "",
  primaryColor: "",
  primaryInk: "",
  backgroundColor: "",
  logoDataUri: "",
  heroMediaDataUri: "",
  siteUrl: "",
};

let current: Branding = fallback;

export function brand(): Branding {
  return current;
}

function readableInk(colour: string | undefined): string | null {
  const hex = (colour ?? "").trim().replace("#", "");
  if (hex.length !== 3 && hex.length !== 6) return null;
  const full = hex.length === 3 ? hex.split("").map((part) => part + part).join("") : hex;
  const value = Number.parseInt(full, 16);
  if (Number.isNaN(value)) return null;
  const red = (value >> 16) & 0xff;
  const green = (value >> 8) & 0xff;
  const blue = value & 0xff;
  const luminance = (0.299 * red + 0.587 * green + 0.114 * blue) / 255;
  return luminance > 0.6 ? "#1b1410" : "#ffffff";
}

export function applyBranding(raw: Partial<Branding> | null | undefined) {
  current = { ...fallback, ...(raw ?? {}) };
  const root = document.documentElement;
  if (current.primaryColor) {
    root.style.setProperty("--lm-primary", current.primaryColor);
    root.style.setProperty("--lm-primary-strong", `color-mix(in srgb, ${current.primaryColor} 82%, white)`);
    root.style.setProperty("--lm-primary-soft", `color-mix(in srgb, ${current.primaryColor} 16%, transparent)`);
  }
  const ink = current.primaryInk || readableInk(current.primaryColor);
  if (ink) root.style.setProperty("--lm-primary-ink", ink);
  if (current.backgroundColor) root.style.setProperty("--lm-bg", current.backgroundColor);
  if (current.windowTitle) document.title = current.windowTitle;
}

const HERO_FALLBACK = "/hero.jpg";

function isVideo(source: string) {
  if (source.startsWith("data:")) return source.startsWith("data:video/");
  return /\.(mp4|webm|mov|m4v)(\?|#|$)/i.test(source);
}

export const branding = {
  get name() {
    return current.name;
  },
  get heroMedia() {
    return current.heroMediaDataUri || HERO_FALLBACK;
  },
  get heroIsVideo() {
    return isVideo(current.heroMediaDataUri || HERO_FALLBACK);
  },
};

export const loaderLabels: Record<Loader, string> = {
  vanilla: "Vanilla",
  fabric: "Fabric",
  quilt: "Quilt",
  forge: "Forge",
  neoforge: "NeoForge",
};

export const labels = {
  selectedBuild: "Выбранная сборка",
  changeBuild: "Сменить сборку",
  play: "Играть",
  players: "играют",
  update: "Обновить",
  install: "Установить",
} as const;
