import { create } from "zustand";
import type { GameConsoleSnapshot, GameExit, GameLine, GameLogBatch, GameStarted, GameStatus } from "@/lib/types";
import { ipc } from "@/lib/ipc";
import { appendLines, countLevels, dropOldest, emptyCounts, overflow, textLength, type LevelCounts, type LevelFilter } from "@/lib/gameLog";

interface ConsoleState {
  ready: boolean;
  session: number;
  build: string;
  status: GameStatus;
  chunks: GameLine[][];
  total: number;
  text: number;
  nextSeq: number;
  dropped: number;
  limit: number;
  textLimit: number;
  counts: LevelCounts;
  query: string;
  filter: LevelFilter;
  trouble: string | null;
  connect: () => Promise<() => void>;
  setQuery: (query: string) => void;
  setFilter: (filter: LevelFilter) => void;
  stopGame: () => Promise<void>;
  openLogs: () => Promise<void>;
  dismissTrouble: () => void;
}

const idle: GameStatus = { state: "idle" };

export const useConsole = create<ConsoleState>((set, get) => {
  const load = (snapshot: GameConsoleSnapshot) =>
    set({
      ready: true,
      session: snapshot.session,
      build: snapshot.build,
      status: snapshot.status,
      chunks: appendLines([], snapshot.lines),
      total: snapshot.lines.length,
      text: textLength(snapshot.lines),
      nextSeq: snapshot.lines.length > 0 ? snapshot.lines[snapshot.lines.length - 1].seq + 1 : 0,
      dropped: snapshot.dropped,
      limit: snapshot.limit,
      textLimit: snapshot.textLimit,
      counts: countLevels(emptyCounts(), snapshot.lines, 1),
    });

  const begin = (started: GameStarted) =>
    set({
      session: started.session,
      build: started.build,
      status: { state: "running" },
      chunks: [],
      total: 0,
      text: 0,
      nextSeq: 0,
      dropped: 0,
      counts: emptyCounts(),
    });

  const append = (batch: GameLogBatch) => {
    const state = get();
    if (batch.session !== state.session) return;
    const fresh = batch.lines.filter((line) => line.seq >= state.nextSeq);
    if (fresh.length === 0) return;
    let chunks = appendLines(state.chunks, fresh);
    let counts = countLevels(state.counts, fresh, 1);
    let total = state.total + fresh.length;
    let text = state.text + textLength(fresh);
    let dropped = state.dropped;
    const excess = overflow(chunks, total, text, state.limit, state.textLimit);
    if (excess > 0) {
      const trimmed = dropOldest(chunks, excess);
      chunks = trimmed.chunks;
      counts = countLevels(counts, trimmed.removed, -1);
      total -= trimmed.removed.length;
      text -= textLength(trimmed.removed);
      dropped += trimmed.removed.length;
    }
    set({ chunks, counts, total, text, dropped, nextSeq: fresh[fresh.length - 1].seq + 1 });
  };

  const finish = (exit: GameExit) => {
    if (exit.session !== get().session) return;
    set({ status: { state: "exited", code: exit.code, stopped: exit.stopped } });
  };

  const attempt = async (action: () => Promise<void>) => {
    try {
      await action();
      set({ trouble: null });
    } catch (err) {
      set({ trouble: String(err) });
    }
  };

  return {
    ready: false,
    session: 0,
    build: "",
    status: idle,
    chunks: [],
    total: 0,
    text: 0,
    nextSeq: 0,
    dropped: 0,
    limit: Number.POSITIVE_INFINITY,
    textLimit: Number.POSITIVE_INFINITY,
    counts: emptyCounts(),
    query: "",
    filter: "all",
    trouble: null,

    connect: async () => {
      let live = false;
      const waiting: (() => void)[] = [];
      const whenLive = (apply: () => void) => (live ? apply() : waiting.push(apply));
      const unlisten = await Promise.all([
        ipc.onGameStarted((started) => whenLive(() => begin(started))),
        ipc.onGameLog((batch) => whenLive(() => append(batch))),
        ipc.onGameExit((exit) => whenLive(() => finish(exit))),
      ]);
      try {
        load(await ipc.gameConsoleSnapshot());
      } catch (err) {
        set({ ready: true, trouble: String(err) });
      }
      live = true;
      for (const apply of waiting) apply();
      return () => {
        for (const off of unlisten) off();
      };
    },

    setQuery: (query) => set({ query }),
    setFilter: (filter) => set({ filter }),
    stopGame: () => attempt(() => ipc.stop()),
    openLogs: () => attempt(() => ipc.openGameLogs()),
    dismissTrouble: () => set({ trouble: null }),
  };
});
