import { create } from "zustand";
import type { Account, ActiveModal, Build, EndpointStatus, LauncherUpdate, NewsItem, Phase, PlayerCounts, SyncEvent, SyncState } from "@/lib/types";
import { ipc } from "@/lib/ipc";
import { buildBlock, isPlayable } from "@/lib/buildState";

const LOG_LIMIT = 400;
const NEWS_SEEN_KEY = "laminara.news.seen";

const quietly = async (task: () => Promise<void>) => {
  try {
    await task();
  } catch {
    return;
  }
};
const CRASH_LOG_LINES = 40;
const CRASH_LOG_SETTLE_MS = 400;

interface LauncherState {
  phase: Phase;
  endpoint: EndpointStatus | null;
  account: Account | null;
  builds: Build[];
  selected: string | null;
  sync: SyncState | null;
  players: PlayerCounts | null;
  news: NewsItem[];
  unreadNews: number;
  error: string | null;
  modal: ActiveModal;
  menuOpen: boolean;
  gameLog: string[];
  crash: { code: number; log: string[] } | null;
  stoppedByUser: boolean;
  update: LauncherUpdate | null;
  updateProgress: { done: number; total: number } | null;
  updateDismissed: boolean;
  openModal: (modal: ActiveModal) => void;
  closeModal: () => void;
  toggleMenu: () => void;
  closeMenu: () => void;
  markOutdated: (name: string) => void;
  dismissCrash: () => void;
  dismissError: () => void;
  refreshNews: () => Promise<void>;
  checkUpdate: () => Promise<void>;
  installUpdate: () => Promise<void>;
  continueStartup: () => Promise<void>;
  dismissUpdate: () => void;
  init: () => Promise<void>;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  select: (name: string) => void;
  play: () => Promise<void>;
  cancelSync: () => Promise<void>;
  stopGame: () => Promise<void>;
  refreshPlayers: () => Promise<void>;
}

let listenersBound = false;

export const useLauncher = create<LauncherState>((set, get) => ({
  phase: "connecting",
  endpoint: null,
  account: null,
  builds: [],
  selected: null,
  sync: null,
  players: null,
  news: [],
  unreadNews: 0,
  error: null,
  modal: null,
  menuOpen: false,
  gameLog: [],
  crash: null,
  stoppedByUser: false,
  update: null,
  updateProgress: null,
  updateDismissed: false,

  dismissCrash: () => set({ crash: null }),
  dismissError: () => set({ error: null }),
  dismissUpdate: () => set({ updateDismissed: true }),
  openModal: (modal) => {
    if (modal?.kind === "news") {
      const seen = get().news.map((item) => item.id);
      localStorage.setItem(NEWS_SEEN_KEY, JSON.stringify(seen));
      set({ unreadNews: 0 });
    }
    set({ modal, menuOpen: false });
  },
  closeModal: () => set({ modal: null }),
  toggleMenu: () => set((state) => ({ menuOpen: !state.menuOpen })),
  closeMenu: () => set({ menuOpen: false }),
  select: (name) => set({ selected: name }),
  markOutdated: (name) =>
    set((state) => ({
      builds: state.builds.map((build) => (build.name === name && build.install === "installed" ? { ...build, install: "outdated" } : build)),
    })),

  refreshPlayers: () => quietly(async () => set({ players: await ipc.playerCounts() })),

  refreshNews: () =>
    quietly(async () => {
      const news = await ipc.news();
      const seen: string[] = JSON.parse(localStorage.getItem(NEWS_SEEN_KEY) ?? "[]");
      set({ news, unreadNews: news.filter((item) => !seen.includes(item.id)).length });
    }),

  checkUpdate: () => quietly(async () => set({ update: await ipc.checkUpdate() })),

  installUpdate: async () => {
    set({ updateProgress: { done: 0, total: 0 } });
    try {
      await ipc.applyUpdate((done, total) => set({ updateProgress: { done, total } }));
    } catch (err) {
      set({ updateProgress: null, update: null, error: String(err) });
      await get().continueStartup();
    }
  },

  continueStartup: async () => {
    try {
      const status = await ipc.restoreSession();
      if (!status.signedIn) {
        set({ phase: "login" });
        return;
      }
      const builds = await ipc.listBuilds();
      set({
        account: status.username ? { uuid: status.uuid ?? "", name: status.username, endpointId: get().endpoint?.id ?? "" } : null,
        builds,
        selected: pickBuild(builds),
        phase: "home",
      });
      void get().refreshPlayers();
      void get().refreshNews();
    } catch (err) {
      set({ phase: "login", error: String(err) });
    }
  },

  init: async () => {
    if (!listenersBound) {
      listenersBound = true;
      void ipc.onGameLog((line) =>
        set((state) => ({
          gameLog: state.gameLog.length >= LOG_LIMIT ? [...state.gameLog.slice(-(LOG_LIMIT - 1)), line] : [...state.gameLog, line],
        })),
      );
      void ipc.onGameExit((code) => {
        const stoppedByUser = get().stoppedByUser;
        set({ stoppedByUser: false });
        if (get().phase === "running") set({ phase: "home" });
        if (code === 0 || stoppedByUser) return;
        setTimeout(() => set({ crash: { code, log: get().gameLog.slice(-CRASH_LOG_LINES) } }), CRASH_LOG_SETTLE_MS);
      });
    }

    try {
      const endpoints = await ipc.probeEndpoints();
      set({ endpoint: endpoints.find((item) => item.isCurrent) ?? endpoints[0] ?? null });
    } catch (err) {
      set({ phase: "login", error: String(err) });
      return;
    }

    await get().checkUpdate();
    if (get().update?.canInstall) {
      set({ phase: "updating" });
      await get().installUpdate();
      return;
    }

    await get().continueStartup();
  },

  login: async (username, password) => {
    set({ error: null });
    try {
      const account = await ipc.login(username, password);
      const builds = await ipc.listBuilds();
      set({ account, builds, selected: pickBuild(builds), phase: "home" });
      void get().refreshPlayers();
      void get().refreshNews();
    } catch (err) {
      set({ error: String(err) });
    }
  },

  logout: async () => {
    await ipc.logout();
    set({ account: null, phase: "login" });
  },

  play: async () => {
    const name = get().selected;
    if (!name) return;
    const block = buildBlock(get().builds.find((item) => item.name === name));
    if (block) {
      set({ error: block.reason });
      return;
    }
    set({ phase: "syncing", sync: null, error: null, crash: null, gameLog: [], stoppedByUser: false });
    try {
      await ipc.syncProfile(name, (event: SyncEvent) => {
        if (event.event === "started") {
          set({ sync: { stage: "planning", filesDone: 0, filesTotal: event.data.filesTotal, bytesDone: 0, bytesTotal: event.data.bytesTotal } });
        } else if (event.event === "progress") {
          set({ sync: { ...event.data } });
        }
      });
      await ipc.launch(name);
      if (get().phase === "syncing") set({ phase: "running" });
    } catch (err) {
      set({ phase: "home", error: String(err) });
    }
  },

  cancelSync: async () => {
    const name = get().selected;
    if (name) await ipc.cancelJob(name);
    set({ phase: "home", sync: null });
  },

  stopGame: async () => {
    set({ stoppedByUser: true });
    await ipc.stop();
  },
}));

function pickBuild(builds: Build[]): string | null {
  return (builds.find(isPlayable) ?? builds[0])?.name ?? null;
}

export function useSelectedBuild(): Build | null {
  return useLauncher((state) => state.builds.find((build) => build.name === state.selected) ?? null);
}
