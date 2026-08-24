import type { InstallState, Loader } from "@/lib/types";

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

export function applyBranding(raw: Partial<Branding> | null | undefined) {
  current = { ...fallback, ...(raw ?? {}) };
  const root = document.documentElement;
  if (current.primaryColor) {
    root.style.setProperty("--lm-primary", current.primaryColor);
    root.style.setProperty("--lm-primary-strong", current.primaryColor);
    root.style.setProperty("--lm-primary-soft", `color-mix(in srgb, ${current.primaryColor} 16%, transparent)`);
  }
  if (current.primaryInk) root.style.setProperty("--lm-primary-ink", current.primaryInk);
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

export const installLabels: Record<InstallState, string> = {
  installed: "Установлено",
  outdated: "Обновить",
  missing: "Не установлено",
  syncing: "Синхронизация",
};

export const labels = {
  selectedBuild: "Выбранная сборка",
  changeBuild: "Сменить сборку",
  play: "Играть",
  players: "играют",
  update: "Обновить",
  install: "Установить",
} as const;
