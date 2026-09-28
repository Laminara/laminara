import type { Account, Build, BuildFeatures, EndpointStatus, GameConsoleSnapshot, GameLevel, GameLine, GameStream, LoginFailure, ServerPlayers } from "@/lib/types";

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

export const mockFace = `data:image/svg+xml,${encodeURIComponent(
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 8 8" shape-rendering="crispEdges">' +
    '<rect width="8" height="8" fill="#c8967a"/><rect width="8" height="2" fill="#5a3214"/>' +
    '<rect y="2" width="1" height="2" fill="#5a3214"/><rect x="7" y="2" width="1" height="2" fill="#5a3214"/>' +
    '<rect x="1" y="4" width="2" height="1" fill="#ffffff"/><rect x="2" y="4" width="1" height="1" fill="#283cc8"/>' +
    '<rect x="5" y="4" width="2" height="1" fill="#ffffff"/><rect x="5" y="4" width="1" height="1" fill="#283cc8"/>' +
    '<rect x="3" y="6" width="2" height="1" fill="#8a5a44"/></svg>',
)}`;

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

const mockLogScript: [GameStream, GameLevel, string][] = [
  ["out", "info", "[main/INFO] [cpw.mods.modlauncher.Launcher/MODLAUNCHER]: ModLauncher running: args [--username, Mykyta, --version, 1.20.1]"],
  ["out", "info", "[main/INFO] [net.minecraftforge.fml.loading.ImmediateWindowHandler/]: Loading ImmediateWindowProvider fmlearlywindow"],
  ["out", "debug", "[main/DEBUG] [net.minecraftforge.fml.loading.moddiscovery.ModDiscoverer/SCAN]: Found mod file sodium-0.5.11.jar of type MOD"],
  ["out", "info", "[main/INFO] [mixin/]: SpongePowered MIXIN Subsystem Version=0.8.5 Source=union:/libraries/org/spongepowered/mixin/0.8.5/mixin-0.8.5.jar"],
  ["out", "warn", "[main/WARN] [mixin/]: Reference map 'journeymap.refmap.json' for journeymap.mixins.json could not be read. If this is a development environment you can ignore this message"],
  ["err", "warn", "OpenJDK 64-Bit Server VM warning: Options -Xverify:none and -noverify were deprecated in JDK 13 and will likely be removed in a future release."],
  ["out", "info", "[Render thread/INFO] [minecraft/Minecraft]: Setting user: Mykyta"],
  ["out", "info", "[Render thread/INFO] [minecraft/Minecraft]: Backend library: LWJGL version 3.3.1 build 7"],
  ["out", "info", "[modloading-worker-0/INFO] [net.minecraftforge.common.ForgeMod/FORGEMOD]: Forge mod loading, version 47.3.0, for MC 1.20.1 with MCP 20230612.114412"],
  ["out", "warn", "[modloading-worker-0/WARN] [net.minecraftforge.common.ForgeConfigSpec/CORE]: Configuration file config/create-client.toml is not correct. Correcting"],
  ["out", "error", "[Render thread/ERROR] [minecraft/TextureManager]: Failed to load texture: journeymap:textures/ui/minimap/frame.png"],
  ["out", "error", "java.io.FileNotFoundException: journeymap:textures/ui/minimap/frame.png"],
  ["out", "error", "	at net.minecraft.server.packs.resources.ResourceProvider.lambda$getResourceOrThrow$1(ResourceProvider.java:17) ~[client-1.20.1-20230612.114412-srg.jar%23312!/:?]"],
  ["out", "error", "	at net.minecraft.client.renderer.texture.SimpleTexture$TextureImage.load(SimpleTexture.java:73) ~[client-1.20.1-20230612.114412-srg.jar%23312!/:?]"],
  ["out", "info", "[Render thread/INFO] [minecraft/SoundEngine]: Sound engine started"],
  ["out", "info", "[Render thread/INFO] [minecraft/TextureAtlas]: Created: 1024x1024x4 minecraft:textures/atlas/blocks.png-atlas"],
  ["out", "info", "[Render thread/INFO] [journeymap/]: Журнал JourneyMap: карта мира «Мир Приключений» загружена за 412 мс"],
  ["out", "info", "[Render thread/INFO] [minecraft/ChatComponent]: [CHAT] Добро пожаловать на сервер, Mykyta!"],
];

export function mockGameLines(from: number, count: number): GameLine[] {
  return Array.from({ length: count }, (_, offset) => {
    const seq = from + offset;
    const [stream, level, text] = mockLogScript[seq % mockLogScript.length];
    const seconds = String(Math.floor(seq / 3) % 60).padStart(2, "0");
    const stamped = text.startsWith("[") ? `[19:22:${seconds}] ${text}` : text;
    return { seq, stream, level, text: stamped };
  });
}

export function mockGameConsole(): GameConsoleSnapshot {
  return {
    session: 1,
    build: "Anarchy Universe",
    status: { state: "running" },
    dropped: 0,
    limit: 50_000,
    textLimit: 16 * 1024 * 1024,
    lines: mockGameLines(0, 240),
  };
}
