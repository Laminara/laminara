import type { Account, Build, BuildFeatures, EndpointStatus, LoginFailure, ServerPlayers } from "@/lib/types";

export const mockEndpoint: EndpointStatus = {
  id: "eu-1",
  baseUrl: "https://eu-1.laminara.net",
  healthy: true,
  latencyMs: 18,
  isCurrent: true,
};

export const mockAccount: Account = {
  uuid: "0af1c2d3e4f5",
  name: "Mykyta",
  endpointId: "eu-1",
};

export const mockLoginFailures: Record<LoginFailure["kind"], string> = {
  secondFactor: "Введите код из приложения-аутентификатора",
  failed: "Неверный логин или пароль",
};

export const mockPlayerCounts: Record<string, ServerPlayers> = {
  "Anarchy Universe": { online: 1284, max: 2000 },
  "Automation Factory": { online: 512, max: 800 },
  "RPG Ascendancy": { online: 964, max: 1500 },
  "Industrial Galaxy": { online: 341, max: 500 },
  "Pixelmon Odyssey": { online: 1607, max: 2500 },
};

export const mockBuilds: Build[] = [
  { name: "Anarchy Universe", minecraft: "1.21.1", loader: "neoforge", sizeBytes: 2_460_000_000, install: "installed", hasFeatures: true, progress: 1 },
  { name: "Automation Factory", minecraft: "1.21.1", loader: "forge", sizeBytes: 3_120_000_000, install: "outdated", progress: 0.62 },
  { name: "RPG Ascendancy", minecraft: "1.20.1", loader: "fabric", sizeBytes: 1_780_000_000, install: "missing", progress: 0 },
  { name: "Industrial Galaxy", minecraft: "1.21.1", loader: "neoforge", sizeBytes: 2_980_000_000, install: "missing", progress: 0 },
  { name: "Pixelmon Odyssey", minecraft: "1.20.4", loader: "forge", sizeBytes: 4_010_000_000, install: "installed", progress: 1 },
];

export function mockFeatures(): BuildFeatures {
  return {
    model: [
      {
        id: "graphics",
        title: "Графика",
        description: "Движок рендера",
        selection: "single",
        required: false,
        options: [
          {
            id: "sodium",
            title: "Sodium",
            description: "Быстрый рендер",
            defaultEnabled: true,
            files: ["mods/sodium.jar"],
            meta: { icon: "", badge: "Рекомендуется", addedSize: 3_100_000, requires: [], incompatibleWith: [] },
            groups: [
              {
                id: "extras",
                title: "Дополнения Sodium",
                description: "",
                selection: "multi",
                required: false,
                options: [
                  { id: "extra", title: "Sodium Extra", description: "", defaultEnabled: false, files: ["mods/sodium-extra.jar"], meta: { icon: "", badge: "", addedSize: 420_000, requires: [], incompatibleWith: [] }, groups: [] },
                  { id: "reeses", title: "Reese's Shadows", description: "", defaultEnabled: false, files: ["mods/reeses.jar"], meta: { icon: "", badge: "", addedSize: 180_000, requires: [], incompatibleWith: [] }, groups: [] },
                ],
              },
            ],
          },
          { id: "embeddium", title: "Embeddium", description: "Альтернатива", defaultEnabled: false, files: ["mods/embeddium.jar"], meta: { icon: "", badge: "", addedSize: 2_800_000, requires: [], incompatibleWith: [] }, groups: [] },
        ],
      },
      {
        id: "extras",
        title: "Дополнительно",
        description: "",
        selection: "multi",
        required: false,
        options: [
          { id: "jei", title: "JEI", description: "Просмотр рецептов", defaultEnabled: true, files: ["mods/jei.jar"], meta: { icon: "", badge: "", addedSize: 1_200_000, requires: [], incompatibleWith: [] }, groups: [] },
          { id: "minimap", title: "JourneyMap", description: "Мини-карта", defaultEnabled: false, files: ["mods/journeymap.jar"], meta: { icon: "", badge: "", addedSize: 5_400_000, requires: [], incompatibleWith: [] }, groups: [] },
        ],
      },
    ],
    selection: { selected: {} },
    active: [],
  };
}
