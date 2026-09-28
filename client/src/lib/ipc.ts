import type {
  Account,
  AppView,
  AuthStatus,
  Build,
  BuildFeatures,
  BuildSettings,
  EndpointStatus,
  FeatureSelection,
  GameConsoleSnapshot,
  GameExit,
  GameLine,
  GameLogBatch,
  GameStarted,
  GeneralSettings,
  LauncherUpdate,
  LoginFailure,
  NewsItem,
  PlayerCounts,
  SyncEvent,
} from "@/lib/types";
import { mockAccount, mockBuilds, mockEndpoint, mockFace, mockFeatures, mockGameConsole, mockGameLines, mockLoginFailures, mockPlayerCounts } from "@/lib/mock";

export const isTauri =
  !import.meta.env.DEV || (typeof window !== "undefined" && "__TAURI_INTERNALS__" in window);

const CONSOLE_WINDOW = "console";
const MOCK_LOG_EVERY_MS = 700;

async function core<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  const { invoke } = await import("@tauri-apps/api/core");
  return invoke<T>(command, args);
}

async function subscribe<T>(event: string, handler: (payload: T) => void): Promise<() => void> {
  const { listen } = await import("@tauri-apps/api/event");
  return listen<T>(event, (message) => handler(message.payload));
}

function mockLogStream(handler: (batch: GameLogBatch) => void): () => void {
  let next = mockGameConsole().lines.length;
  const timer = setInterval(() => {
    const count = 1 + Math.floor(Math.random() * 3);
    handler({ session: 1, lines: mockGameLines(next, count) });
    next += count;
  }, MOCK_LOG_EVERY_MS);
  return () => clearInterval(timer);
}

async function mockSync(onEvent: (event: SyncEvent) => void): Promise<void> {
  const filesTotal = 1200;
  const bytesTotal = 2_400_000_000;
  onEvent({ event: "started" });
  for (let step = 1; step <= 12; step += 1) {
    await new Promise((resolve) => setTimeout(resolve, 110));
    onEvent({
      event: "progress",
      data: {
        stage: "downloading",
        filesDone: Math.round((filesTotal / 12) * step),
        filesTotal,
        bytesDone: Math.round((bytesTotal / 12) * step),
        bytesTotal,
        currentPath: `mods/module-${step}.jar`,
      },
    });
  }
  onEvent({ event: "finished", data: { downloaded: filesTotal, skipped: 0, pruned: 0 } });
}

export const ipc = {
  probeEndpoints: (): Promise<EndpointStatus[]> => (isTauri ? core("probe_endpoints") : Promise.resolve([mockEndpoint])),


  restoreSession: (): Promise<AuthStatus> =>
    isTauri ? core("restore_session") : Promise.resolve({ signedIn: true, username: mockAccount.name, uuid: mockAccount.uuid }),

  login: (username: string, password: string, code: string): Promise<Account> => {
    if (isTauri) return core("login", { username, password, code });
    const failure = (kind: LoginFailure["kind"]): LoginFailure => ({ kind, message: mockLoginFailures[kind] });
    if (password === "wrong") return Promise.reject(failure("failed"));
    if (password === "2fa" && code.length !== 6) return Promise.reject(failure("secondFactor"));
    return Promise.resolve(mockAccount);
  },

  logout: (): Promise<void> => (isTauri ? core("logout") : Promise.resolve()),

  playerFace: (): Promise<string | null> => (isTauri ? core("player_face") : Promise.resolve(mockFace)),

  listBuilds: (): Promise<Build[]> => (isTauri ? core("list_builds") : Promise.resolve(mockBuilds)),

  syncProfile: async (profile: string, onEvent: (event: SyncEvent) => void): Promise<void> => {
    if (!isTauri) return mockSync(onEvent);
    const { Channel, invoke } = await import("@tauri-apps/api/core");
    const channel = new Channel<SyncEvent>();
    channel.onmessage = onEvent;
    await invoke("sync_profile", { profile, onEvent: channel });
  },

  cancelSync: (): Promise<void> => (isTauri ? core("cancel_sync") : Promise.resolve()),

  repairBuild: (profile: string): Promise<number> => (isTauri ? core("repair_build", { profile }) : Promise.resolve(0)),

  reportCrash: (report: {
    build: string;
    buildVersion: string;
    loader: string;
    exitCode: number;
    session: number;
  }): Promise<string> => (isTauri ? core("report_crash", report) : Promise.resolve("Отчёт отправлен, спасибо")),

  launch: (profile: string): Promise<number> => (isTauri ? core("launch", { profile }) : Promise.resolve(1)),

  stop: (): Promise<void> => (isTauri ? core("stop") : Promise.resolve()),

  playerCounts: (): Promise<PlayerCounts | null> =>
    isTauri
      ? core("player_counts")
      : Promise.resolve({
          perBuild: mockPlayerCounts,
          total: Object.values(mockPlayerCounts).reduce(
            (sum, players) => ({ online: sum.online + players.online, max: sum.max + players.max }),
            { online: 0, max: 0 },
          ),
        }),

  generalSettings: (): Promise<GeneralSettings> =>
    isTauri
      ? core("general_settings")
      : Promise.resolve({
          installDir: "~/.local/share/laminara/games",
          defaultMemoryMb: 4096,
          endpoints: [{ id: "local", baseUrl: "http://127.0.0.1:8099" }],
          version: "0.1.0",
          gameConsole: false,
        }),


  setInstallDir: (path: string): Promise<void> => (isTauri ? core("set_install_dir", { path }) : Promise.resolve()),

  setGameConsole: (enabled: boolean): Promise<void> => (isTauri ? core("set_game_console", { enabled }) : Promise.resolve()),

  pickFolder: async (defaultPath?: string): Promise<string | null> => {
    if (!isTauri) return null;
    const { open } = await import("@tauri-apps/plugin-dialog");
    const selected = await open({ directory: true, multiple: false, defaultPath });
    return typeof selected === "string" ? selected : null;
  },

  buildSettings: (profile: string): Promise<BuildSettings> =>
    isTauri ? core("build_settings", { profile }) : Promise.resolve({ maxMemoryMb: null, defaultMemoryMb: 4096, allowedMemoryMb: 12288 }),

  setBuildMemory: (profile: string, maxMemoryMb: number | null): Promise<void> =>
    isTauri ? core("set_build_memory", { profile, maxMemoryMb }) : Promise.resolve(),

  branding: (): Promise<Record<string, string> | null> => (isTauri ? core("branding") : Promise.resolve(null)),

  news: (): Promise<NewsItem[]> => (isTauri ? core("news") : Promise.resolve([])),

  openExternal: (url: string): Promise<void> => (isTauri ? core("open_external", { url }) : Promise.resolve()),

  collectGarbage: (): Promise<number> => (isTauri ? core("collect_garbage") : Promise.resolve(0)),

  checkUpdate: (): Promise<LauncherUpdate | null> => (isTauri ? core("check_update") : Promise.resolve(null)),

  applyUpdate: async (version: string, onProgress: (done: number, total: number) => void): Promise<void> => {
    if (!isTauri) return;
    const { Channel, invoke } = await import("@tauri-apps/api/core");
    const channel = new Channel<{ bytesDone: number; bytesTotal: number }>();
    channel.onmessage = (message) => onProgress(message.bytesDone, message.bytesTotal);
    await invoke("apply_update", { version, onEvent: channel });
  },

  view: async (): Promise<AppView> => {
    if (!isTauri) return new URLSearchParams(window.location.search).get("view") === CONSOLE_WINDOW ? "console" : "main";
    const { getCurrentWindow } = await import("@tauri-apps/api/window");
    return getCurrentWindow().label === CONSOLE_WINDOW ? "console" : "main";
  },

  onGameStarted: (handler: (started: GameStarted) => void): Promise<() => void> =>
    isTauri ? subscribe("game:started", handler) : Promise.resolve(() => {}),

  onGameLog: (handler: (batch: GameLogBatch) => void): Promise<() => void> =>
    isTauri ? subscribe("game:log", handler) : Promise.resolve(mockLogStream(handler)),

  onGameExit: (handler: (exit: GameExit) => void): Promise<() => void> =>
    isTauri ? subscribe("game:exit", handler) : Promise.resolve(() => {}),

  gameConsoleSnapshot: (): Promise<GameConsoleSnapshot> =>
    isTauri ? core("game_console_snapshot") : Promise.resolve(mockGameConsole()),

  gameLogTail: (lines: number): Promise<GameLine[]> =>
    isTauri ? core("game_log_tail", { lines }) : Promise.resolve(mockGameConsole().lines.slice(-lines)),

  openGameConsole: async (): Promise<void> => {
    if (isTauri) return core("open_game_console");
    window.open(`${window.location.pathname}?view=${CONSOLE_WINDOW}`, CONSOLE_WINDOW, "width=1080,height=640");
  },

  openGameLogs: (): Promise<void> => (isTauri ? core("open_game_logs") : Promise.resolve()),

  sendLauncherLog: (): Promise<string> =>
    isTauri ? core("send_launcher_log") : new Promise((resolve) => setTimeout(() => resolve("Журнал отправлен, спасибо"), 600)),

  openLauncherLogs: (): Promise<void> => (isTauri ? core("open_launcher_logs") : Promise.resolve()),

  revealWindow: async (): Promise<void> => {
    if (!isTauri) return;
    const { getCurrentWindow } = await import("@tauri-apps/api/window");
    const current = getCurrentWindow();
    await current.show();
    await current.setFocus();
  },

  buildFeatures: (profile: string): Promise<BuildFeatures> =>
    isTauri ? core("build_features", { profile }) : Promise.resolve(mockFeatures()),

  setBuildFeatures: (profile: string, selection: FeatureSelection): Promise<void> =>
    isTauri ? core("set_build_features", { profile, selection }) : Promise.resolve(),
};
