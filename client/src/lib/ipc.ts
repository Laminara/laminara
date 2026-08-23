import type {
  Account,
  AuthStatus,
  Build,
  BuildFeatures,
  BuildSettings,
  EndpointStatus,
  FeatureSelection,
  GeneralSettings,
  LauncherUpdate,
  NewsItem,
  PlayerCounts,
  SyncEvent,
} from "@/lib/types";
import { mockAccount, mockBuilds, mockEndpoint, mockFeatures, mockPlayerCounts } from "@/lib/mock";

const isTauri = typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;

async function core<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  const { invoke } = await import("@tauri-apps/api/core");
  return invoke<T>(command, args);
}

async function mockSync(onEvent: (event: SyncEvent) => void): Promise<void> {
  const filesTotal = 1200;
  const bytesTotal = 2_400_000_000;
  onEvent({ event: "started", data: { filesTotal, bytesTotal } });
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

  authStatus: (): Promise<AuthStatus> =>
    isTauri ? core("auth_status") : Promise.resolve({ signedIn: true, username: mockAccount.name, uuid: mockAccount.uuid }),

  restoreSession: (): Promise<AuthStatus> =>
    isTauri ? core("restore_session") : Promise.resolve({ signedIn: true, username: mockAccount.name, uuid: mockAccount.uuid }),

  login: (username: string, password: string): Promise<Account> =>
    isTauri ? core("login", { username, password }) : Promise.resolve(mockAccount),

  logout: (): Promise<void> => (isTauri ? core("logout") : Promise.resolve()),

  listBuilds: (): Promise<Build[]> => (isTauri ? core("list_builds") : Promise.resolve(mockBuilds)),

  syncProfile: async (profile: string, onEvent: (event: SyncEvent) => void): Promise<void> => {
    if (!isTauri) return mockSync(onEvent);
    const { Channel, invoke } = await import("@tauri-apps/api/core");
    const channel = new Channel<SyncEvent>();
    channel.onmessage = onEvent;
    await invoke("sync_profile", { profile, onEvent: channel });
  },

  cancelJob: (job: string): Promise<void> => (isTauri ? core("cancel_job", { job }) : Promise.resolve()),

  launch: (profile: string): Promise<void> => (isTauri ? core("launch", { profile }) : Promise.resolve()),

  stop: (): Promise<void> => (isTauri ? core("stop") : Promise.resolve()),

  playerCounts: (): Promise<PlayerCounts> =>
    isTauri
      ? core("player_counts")
      : Promise.resolve({ perBuild: mockPlayerCounts, total: Object.values(mockPlayerCounts).reduce((a, b) => a + b, 0) }),

  generalSettings: (): Promise<GeneralSettings> =>
    isTauri
      ? core("general_settings")
      : Promise.resolve({
          installDir: "~/.local/share/laminara/games",
          defaultMemoryMb: 4096,
          endpoints: [{ id: "local", baseUrl: "http://127.0.0.1:8099" }],
          version: "0.1.0",
        }),

  setDefaultMemory: (mb: number): Promise<void> => (isTauri ? core("set_default_memory", { mb }) : Promise.resolve()),

  setInstallDir: (path: string): Promise<void> => (isTauri ? core("set_install_dir", { path }) : Promise.resolve()),

  pickFolder: async (defaultPath?: string): Promise<string | null> => {
    if (!isTauri) return null;
    const { open } = await import("@tauri-apps/plugin-dialog");
    const selected = await open({ directory: true, multiple: false, defaultPath });
    return typeof selected === "string" ? selected : null;
  },

  buildSettings: (profile: string): Promise<BuildSettings> =>
    isTauri ? core("build_settings", { profile }) : Promise.resolve({ maxMemoryMb: null, defaultMemoryMb: 4096 }),

  setBuildMemory: (profile: string, maxMemoryMb: number | null): Promise<void> =>
    isTauri ? core("set_build_memory", { profile, maxMemoryMb }) : Promise.resolve(),

  branding: (): Promise<Record<string, string> | null> => (isTauri ? core("branding") : Promise.resolve(null)),

  news: (): Promise<NewsItem[]> => (isTauri ? core("news") : Promise.resolve([])),

  openExternal: (url: string): Promise<void> => (isTauri ? core("open_external", { url }) : Promise.resolve()),

  collectGarbage: (): Promise<number> => (isTauri ? core("collect_garbage") : Promise.resolve(0)),

  checkUpdate: (): Promise<LauncherUpdate | null> => (isTauri ? core("check_update") : Promise.resolve(null)),

  applyUpdate: async (onProgress: (done: number, total: number) => void): Promise<void> => {
    if (!isTauri) return;
    const { Channel, invoke } = await import("@tauri-apps/api/core");
    const channel = new Channel<{ bytesDone: number; bytesTotal: number }>();
    channel.onmessage = (message) => onProgress(message.bytesDone, message.bytesTotal);
    await invoke("apply_update", { onEvent: channel });
  },

  onGameExit: async (handler: (code: number) => void): Promise<() => void> => {
    if (!isTauri) return () => {};
    const { listen } = await import("@tauri-apps/api/event");
    return listen<number>("game:exit", (event) => handler(event.payload));
  },

  onGameLog: async (handler: (line: string) => void): Promise<() => void> => {
    if (!isTauri) return () => {};
    const { listen } = await import("@tauri-apps/api/event");
    return listen<string>("game:log", (event) => handler(event.payload));
  },

  buildFeatures: (profile: string): Promise<BuildFeatures> =>
    isTauri ? core("build_features", { profile }) : Promise.resolve(mockFeatures()),

  setBuildFeatures: (profile: string, selection: FeatureSelection): Promise<void> =>
    isTauri ? core("set_build_features", { profile, selection }) : Promise.resolve(),
};
