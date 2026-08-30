import { create } from "zustand";
import type { Account, ActiveModal, Build, EndpointStatus, LauncherUpdate, LoginFailure, NewsItem, Phase, PlayerCounts, SyncEvent, SyncState } from "@/lib/types";
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
  twoFactor: boolean;
  modal: ActiveModal;
  menuOpen: boolean;
  gameLog: string[];
  crash: { code: number; log: string[]; build: string; loader: string; version: string; full: string[] } | null;
  crashSending: boolean;
  crashSent: string | null;
  stoppedByUser: boolean;
  update: LauncherUpdate | null;
  updateProgress: { done: number; total: number } | null;
  updateDismissed: boolean;
  listeners: (() => void)[];
  binding: Promise<void> | null;
  startup: Promise<void> | null;
  unbinding: boolean;
  openModal: (modal: ActiveModal) => void;
  closeModal: () => void;
  toggleMenu: () => void;
  closeMenu: () => void;
  markOutdated: (name: string) => void;
  dismissCrash: () => void;
  sendCrash: () => Promise<void>;
  dismissError: () => void;
  refreshNews: () => Promise<void>;
  checkUpdate: () => Promise<void>;
  installUpdate: () => Promise<void>;
  continueStartup: () => Promise<void>;
  dismissUpdate: () => void;
  bindListeners: () => Promise<void>;
  unbindListeners: () => void;
  init: () => Promise<void>;
  login: (username: string, password: string, code?: string) => Promise<void>;
  logout: () => Promise<void>;
  select: (name: string) => void;
  play: () => Promise<void>;
  cancelSync: () => Promise<void>;
  stopGame: () => Promise<void>;
  refreshPlayers: () => Promise<void>;
}

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
  twoFactor: false,
  modal: null,
  menuOpen: false,
  gameLog: [],
  crash: null,
  crashSending: false,
  crashSent: null,
  stoppedByUser: false,
  update: null,
  updateProgress: null,
  updateDismissed: false,
  listeners: [],
  binding: null,
  startup: null,
  unbinding: false,

  dismissCrash: () => set({ crash: null, crashSent: null }),

  sendCrash: async () => {
    const crash = get().crash;
    if (!crash || get().crashSending) return;
    set({ crashSending: true });
    try {
      const message = await ipc.reportCrash({
        build: crash.build,
        buildVersion: crash.version,
        loader: crash.loader,
        exitCode: crash.code,
        log: crash.full.join("\n"),
      });
      set({ crashSent: message });
    } catch (err) {
      set({ crashSent: String(err) });
    } finally {
      set({ crashSending: false });
    }
  },
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
    const update = get().update;
    if (!update) return;
    set({ updateProgress: { done: 0, total: 0 } });
    try {
      await ipc.applyUpdate(update.version, (done, total) => set({ updateProgress: { done, total } }));
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

  bindListeners: () => {
    const pending = get().binding;
    if (pending) return pending;
    if (get().listeners.length) return Promise.resolve();
    const attached: (() => void)[] = [];
    const binding = (async () => {
      try {
        attached.push(
          await ipc.onGameLog((line) =>
            set((state) => ({
              gameLog: state.gameLog.length >= LOG_LIMIT ? [...state.gameLog.slice(-(LOG_LIMIT - 1)), line] : [...state.gameLog, line],
            })),
          ),
        );
        attached.push(
          await ipc.onGameExit((code) => {
            const stoppedByUser = get().stoppedByUser;
            set({ stoppedByUser: false });
            if (get().phase === "running") set({ phase: "home" });
            if (code === 0 || stoppedByUser) return;
            setTimeout(() => {
              const state = get();
              const build = state.builds.find((item) => item.name === state.selected);
              set({
                crash: {
                  code,
                  log: state.gameLog.slice(-CRASH_LOG_LINES),
                  full: state.gameLog,
                  build: state.selected ?? "",
                  loader: build?.loader ?? "",
                  version: build?.minecraft ?? "",
                },
                crashSent: null,
              });
            }, CRASH_LOG_SETTLE_MS);
          }),
        );
        if (get().unbinding) {
          for (const off of attached) off();
          return;
        }
        set({ listeners: [...get().listeners, ...attached] });
      } catch (err) {
        for (const off of attached) off();
        console.error("listener bind failed", err);
        throw err;
      }
    })();
    set({ binding });
    return binding.finally(() => {
      if (get().binding === binding) set({ binding: null });
    });
  },

  unbindListeners: () => {
    set({ unbinding: true });
    for (const off of get().listeners) off();
    set({ listeners: [], unbinding: false });
  },

  init: async () => {
    await get().bindListeners().catch(() => undefined);
    const started = get().startup;
    if (started) return started;
    const startup = (async () => {
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
    })();
    set({ startup });
    return startup;
  },

  login: async (username, password, code = "") => {
    set({ error: null });
    try {
      const account = await ipc.login(username, password, code);
      const builds = await ipc.listBuilds();
      set({ account, builds, selected: pickBuild(builds), phase: "home", twoFactor: false });
      void get().refreshPlayers();
      void get().refreshNews();
    } catch (err) {
      const failure = asLoginFailure(err);
      set({ error: failure.message, twoFactor: failure.kind === "secondFactor" });
    }
  },

  logout: async () => {
    await ipc.logout();
    set({ account: null, phase: "login", error: null, twoFactor: false });
  },

  play: async () => {
    const name = get().selected;
    if (!name) return;
    const block = buildBlock(get().builds.find((item) => item.name === name));
    if (block) {
      set({ error: block.reason });
      return;
    }
    set({ phase: "syncing", sync: null, error: null, crash: null, crashSent: null, gameLog: [], stoppedByUser: false });
    try {
      const rate = newRateMeter();
      await ipc.syncProfile(name, (event: SyncEvent) => {
        if (event.event === "started") {
          set({ sync: { stage: "planning", filesDone: 0, filesTotal: event.data.filesTotal, bytesDone: 0, bytesTotal: event.data.bytesTotal } });
        } else if (event.event === "progress") {
          set({ sync: { ...event.data, ...rate(event.data.bytesDone, event.data.bytesTotal) } });
        }
      });
      set({ builds: await ipc.listBuilds() });
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

function asLoginFailure(err: unknown): LoginFailure {
  if (err && typeof err === "object" && "kind" in err) {
    const failure = err as { kind: string; message?: string };
    if (failure.kind === "secondFactor" || failure.kind === "failed") {
      return { kind: failure.kind, message: failure.message || "Не удалось войти. Попробуйте ещё раз" };
    }
  }
  return { kind: "failed", message: String(err) };
}

function pickBuild(builds: Build[]): string | null {
  return (builds.find(isPlayable) ?? builds[0])?.name ?? null;
}

export function useSelectedBuild(): Build | null {
  return useLauncher((state) => state.builds.find((build) => build.name === state.selected) ?? null);
}

function newRateMeter() {
  let lastBytes = 0;
  let lastAt = Date.now();
  let speed = 0;
  return (bytesDone: number, bytesTotal: number) => {
    const now = Date.now();
    const seconds = (now - lastAt) / 1000;
    const gained = bytesDone - lastBytes;
    if (seconds >= 0.35 && gained >= 0) {
      const sample = gained / seconds;
      speed = speed === 0 ? sample : speed * 0.7 + sample * 0.3;
      lastBytes = bytesDone;
      lastAt = now;
    }
    if (speed <= 0) return {};
    const left = Math.max(0, bytesTotal - bytesDone);
    return { bytesPerSecond: speed, secondsLeft: left / speed };
  };
}
