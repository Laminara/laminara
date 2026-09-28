import { create } from "zustand";
import type { Account, ActiveModal, Build, EndpointStatus, GameExit, GameLine, LauncherUpdate, LogReport, LoginFailure, NewsItem, Phase, PlayerCounts, Problem, SyncEvent, SyncState } from "@/lib/types";
import { ipc } from "@/lib/ipc";
import { buildBlock, isPlayable } from "@/lib/buildState";

const NEWS_SEEN_KEY = "laminara.news.seen";
const FACE_RECHECK_MS = 120_000;

const quietly = async (task: () => Promise<void>) => {
  try {
    await task();
  } catch {
    return;
  }
};
const CRASH_LOG_LINES = 40;

export interface Crash {
  session: number;
  code: number;
  log: GameLine[];
  build: string;
  loader: string;
  version: string;
}

interface LauncherState {
  phase: Phase;
  endpoint: EndpointStatus | null;
  account: Account | null;
  face: string | null;
  faceCheckedAt: number;
  builds: Build[];
  selected: string | null;
  sync: SyncState | null;
  players: PlayerCounts | null;
  news: NewsItem[];
  unreadNews: number;
  error: Problem | null;
  logReport: LogReport;
  twoFactor: boolean;
  modal: ActiveModal;
  menuOpen: boolean;
  crash: Crash | null;
  crashSending: boolean;
  crashSent: string | null;
  crashError: string | null;
  cancelRequested: boolean;
  syncRun: number;
  runningSession: number | null;
  lastExit: GameExit | null;
  staleBuilds: string[];
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
  refreshFace: (force?: boolean) => Promise<void>;
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
  openConsole: () => Promise<void>;
  sendLauncherLog: () => Promise<void>;
  openLauncherLogs: () => Promise<void>;
  settleExit: (exit: GameExit) => Promise<void>;
  refreshPlayers: () => Promise<void>;
  refreshBuilds: () => Promise<void>;
  repairBuild: (name: string) => Promise<number>;
}

export const useLauncher = create<LauncherState>((set, get) => ({
  phase: "connecting",
  cancelRequested: false,
  syncRun: 0,
  runningSession: null,
  lastExit: null,
  staleBuilds: [],
  endpoint: null,
  account: null,
  face: null,
  faceCheckedAt: 0,
  builds: [],
  selected: null,
  sync: null,
  players: null,
  news: [],
  unreadNews: 0,
  error: null,
  logReport: { state: "idle" },
  twoFactor: false,
  modal: null,
  menuOpen: false,
  crash: null,
  crashSending: false,
  crashError: null,
  crashSent: null,
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
        session: crash.session,
      });
      set({ crashSent: message, crashError: null });
    } catch (err) {
      set({ crashError: String(err) });
    } finally {
      set({ crashSending: false });
    }
  },
  dismissError: () => set({ error: null, logReport: { state: "idle" } }),
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
      staleBuilds: state.staleBuilds.includes(name) ? state.staleBuilds : [...state.staleBuilds, name],
      builds: state.builds.map((build) => (build.name === name && build.install === "installed" ? { ...build, install: "outdated" } : build)),
    })),

  refreshPlayers: () => quietly(async () => set({ players: await ipc.playerCounts() })),

  refreshBuilds: () =>
    quietly(async () => {
      const phase = get().phase;
      if (phase !== "home") return;
      const fresh = await ipc.listBuilds();
      const stale = get().staleBuilds;
      const builds = fresh.map((build) =>
        stale.includes(build.name) && build.install === "installed" ? { ...build, install: "outdated" as const } : build,
      );
      const selected = get().selected;
      set({ builds, selected: selected && builds.some((build) => build.name === selected) ? selected : pickBuild(builds) });
    }),

  refreshNews: () =>
    quietly(async () => {
      const news = await ipc.news();
      const seen: string[] = JSON.parse(localStorage.getItem(NEWS_SEEN_KEY) ?? "[]");
      set({ news, unreadNews: news.filter((item) => !seen.includes(item.id)).length });
    }),

  refreshFace: (force = false) => {
    if (!get().account) return Promise.resolve();
    if (!force && Date.now() - get().faceCheckedAt < FACE_RECHECK_MS) return Promise.resolve();
    set({ faceCheckedAt: Date.now() });
    return quietly(async () => set({ face: await ipc.playerFace() }));
  },

  checkUpdate: () => quietly(async () => set({ update: await ipc.checkUpdate() })),

  installUpdate: async () => {
    const update = get().update;
    if (!update) return;
    set({ updateProgress: { done: 0, total: 0 } });
    try {
      await ipc.applyUpdate(update.version, (done, total) => set({ updateProgress: { done, total } }));
    } catch (err) {
      set({ updateProgress: null, update: null, error: problem(err) });
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
      const account = status.username ? { uuid: status.uuid ?? "", name: status.username, endpointId: get().endpoint?.id ?? "" } : null;
      let builds: Build[] = [];
      let trouble: string | null = null;
      try {
        builds = await ipc.listBuilds();
      } catch (err) {
        trouble = String(err);
      }
      set({ account, builds, selected: pickBuild(builds), phase: "home", error: trouble === null ? null : problem(trouble) });
      void get().refreshPlayers();
      void get().refreshNews();
      void get().refreshFace(true);
    } catch (err) {
      set({ phase: "login", error: problem(err) });
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
          await ipc.onGameExit((exit) => {
            set({ lastExit: exit });
            if (get().runningSession === exit.session) void get().settleExit(exit);
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
        set({ phase: "login", error: problem(err) });
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
      void get().refreshFace(true);
    } catch (err) {
      const failure = asLoginFailure(err);
      set({ error: problem(failure.message), twoFactor: failure.kind === "secondFactor" });
    }
  },

  logout: async () => {
    await ipc.logout();
    set({ account: null, face: null, faceCheckedAt: 0, phase: "login", error: null, twoFactor: false });
  },

  play: async () => {
    if (get().phase !== "home") return;
    const name = get().selected;
    if (!name) return;
    const block = buildBlock(get().builds.find((item) => item.name === name));
    if (block) {
      set({ error: problem(block.reason) });
      return;
    }
    const run = get().syncRun + 1;
    const current = () => get().syncRun === run;
    set({ phase: "syncing", syncRun: run, sync: null, error: null, crash: null, crashSent: null, crashError: null, cancelRequested: false });
    try {
      await ipc.syncProfile(name, syncTracker(set, get, run, "launching"));
      if (!current()) return;
      set({ staleBuilds: get().staleBuilds.filter((item) => item !== name) });
      ipc.listBuilds().then((builds) => set({ builds })).catch(() => undefined);
      if (get().cancelRequested) {
        set({ phase: "home", sync: null });
        return;
      }
      const session = await ipc.launch(name);
      const early = get().lastExit;
      if (early?.session === session) {
        set({ phase: "home", sync: null });
        await get().settleExit(early);
        return;
      }
      set({ phase: "running", runningSession: session });
    } catch (err) {
      if (!current()) return;
      set({ phase: "home", sync: null });
      if (!get().cancelRequested) set({ error: problem(err) });
    }
  },

  repairBuild: async (name) => {
    if (get().phase !== "home") return 0;
    const run = get().syncRun + 1;
    const current = () => get().syncRun === run;
    set({ phase: "syncing", syncRun: run, sync: null, error: null, cancelRequested: false });
    try {
      const discarded = await ipc.repairBuild(name);
      if (!current()) return discarded;
      if (get().cancelRequested) {
        set({ phase: "home", sync: null });
        return discarded;
      }
      await ipc.syncProfile(name, syncTracker(set, get, run, "done"));
      if (!current()) return discarded;
      set({ builds: await ipc.listBuilds(), phase: "home", sync: null });
      return discarded;
    } catch (err) {
      if (!current()) return 0;
      set({ phase: "home", sync: null });
      if (!get().cancelRequested) set({ error: problem(err) });
      return 0;
    }
  },

  cancelSync: async () => {
    if (get().cancelRequested) return;
    set({ cancelRequested: true });
    await ipc.cancelSync().catch(() => undefined);
  },

  stopGame: async () => {
    try {
      await ipc.stop();
    } catch (err) {
      set({ error: problem(err) });
    }
  },

  openConsole: async () => {
    try {
      await ipc.openGameConsole();
    } catch (err) {
      set({ error: problem(err) });
    }
  },

  sendLauncherLog: async () => {
    if (get().logReport.state === "sending") return;
    set({ logReport: { state: "sending" } });
    try {
      set({ logReport: { state: "sent", message: await ipc.sendLauncherLog() } });
    } catch (err) {
      set({ logReport: { state: "failed", message: problem(err).message } });
    }
  },

  openLauncherLogs: async () => {
    try {
      await ipc.openLauncherLogs();
    } catch (err) {
      set({ error: problem(err) });
    }
  },

  settleExit: async (exit) => {
    set({ runningSession: null });
    if (get().phase === "running") set({ phase: "home" });
    if (exit.code === 0 || exit.stopped) return;
    const tail = await ipc.gameLogTail(CRASH_LOG_LINES).catch(() => []);
    if (get().phase !== "home") return;
    const build = get().builds.find((item) => item.name === exit.build);
    set({
      crash: {
        session: exit.session,
        code: exit.code,
        log: tail,
        build: exit.build,
        loader: build?.loader ?? "",
        version: build?.minecraft ?? "",
      },
      crashSent: null,
      crashError: null,
    });
  },
}));

function problem(err: unknown): Problem {
  if (err && typeof err === "object" && "message" in err) {
    const failure = err as { message: unknown; retry?: unknown };
    return { message: String(failure.message), retry: failure.retry === true };
  }
  return { message: String(err), retry: false };
}

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

type SetLauncher = (partial: Partial<LauncherState>) => void;

function syncTracker(set: SetLauncher, get: () => LauncherState, run: number, finalStage: string) {
  const rate = newRateMeter();
  return (event: SyncEvent) => {
    if (get().syncRun !== run) return;
    if (event.event === "started") {
      set({ sync: { stage: "planning", filesDone: 0, filesTotal: 0, bytesDone: 0, bytesTotal: 0 } });
    } else if (event.event === "progress") {
      set({ sync: { ...event.data, ...rate(event.data.bytesDone, event.data.bytesTotal) } });
    } else if (event.event === "finished") {
      const sync = get().sync;
      if (sync) set({ sync: { ...sync, stage: finalStage } });
    }
  };
}

function newRateMeter() {
  let lastBytes: number | null = null;
  let lastAt = Date.now();
  let speed = 0;
  return (bytesDone: number, bytesTotal: number) => {
    const now = Date.now();
    if (lastBytes === null || bytesDone < lastBytes) {
      lastBytes = bytesDone;
      lastAt = now;
    }
    const seconds = (now - lastAt) / 1000;
    const gained = bytesDone - lastBytes;
    if (seconds >= 0.35) {
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
