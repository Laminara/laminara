import type { GameLevel, GameLine, GameStatus } from "@/lib/types";

export const CHUNK_LINES = 200;

export type LevelFilter = "all" | "warn" | "error";

export type LevelCounts = Record<GameLevel, number>;

interface LevelFilterView {
  id: LevelFilter;
  label: string;
  hint: string;
  admits: ReadonlySet<GameLevel>;
}

export const levelFilters: LevelFilterView[] = [
  { id: "all", label: "Все", hint: "Весь журнал", admits: new Set<GameLevel>(["debug", "info", "warn", "error"]) },
  { id: "warn", label: "Предупреждения", hint: "Предупреждения и ошибки", admits: new Set<GameLevel>(["warn", "error"]) },
  { id: "error", label: "Ошибки", hint: "Только ошибки", admits: new Set<GameLevel>(["error"]) },
];

const admitted = new Map(levelFilters.map((filter) => [filter.id, filter.admits]));

export function emptyCounts(): LevelCounts {
  return { debug: 0, info: 0, warn: 0, error: 0 };
}

export function countLevels(counts: LevelCounts, lines: GameLine[], sign: 1 | -1): LevelCounts {
  const next = { ...counts };
  for (const line of lines) next[line.level] += sign;
  return next;
}

export function filterCount(filter: LevelFilter, counts: LevelCounts): number {
  let total = 0;
  for (const level of admitted.get(filter) ?? []) total += counts[level];
  return total;
}

function chunkOf(line: GameLine): number {
  return Math.floor(line.seq / CHUNK_LINES);
}

export function chunkKey(chunk: GameLine[]): number {
  return chunkOf(chunk[0]);
}

export function appendLines(chunks: GameLine[][], lines: GameLine[]): GameLine[][] {
  if (lines.length === 0) return chunks;
  const next = chunks.slice();
  let owned: GameLine[] | null = null;
  for (const line of lines) {
    const last: GameLine[] | undefined = owned ?? next[next.length - 1];
    if (last !== undefined && chunkOf(last[0]) === chunkOf(line)) {
      if (owned === null) {
        owned = last.slice();
        next[next.length - 1] = owned;
      }
      owned.push(line);
    } else {
      owned = [line];
      next.push(owned);
    }
  }
  return next;
}

export function overflow(chunks: GameLine[][], lines: number, text: number, limit: number, textLimit: number): number {
  let count = 0;
  for (const chunk of chunks) {
    for (const line of chunk) {
      if (lines - count <= limit && text <= textLimit) return count;
      text -= line.text.length;
      count += 1;
    }
  }
  return count;
}

export function textLength(lines: GameLine[]): number {
  let total = 0;
  for (const line of lines) total += line.text.length;
  return total;
}

export function dropOldest(chunks: GameLine[][], excess: number): { chunks: GameLine[][]; removed: GameLine[] } {
  const next = chunks.slice();
  const removed: GameLine[] = [];
  let left = excess;
  while (left > 0 && next.length > 0) {
    const first = next[0];
    if (first.length <= left) {
      removed.push(...first);
      next.shift();
      left -= first.length;
    } else {
      removed.push(...first.slice(0, left));
      next[0] = first.slice(left);
      left = 0;
    }
  }
  return { chunks: next, removed };
}

const visibleCache = new WeakMap<GameLine[], { key: string; lines: GameLine[] }>();

export function visibleLines(chunk: GameLine[], filter: LevelFilter, needle: string): GameLine[] {
  if (filter === "all" && !needle) return chunk;
  const key = `${filter}\u0000${needle}`;
  const cached = visibleCache.get(chunk);
  if (cached?.key === key) return cached.lines;
  const admits = admitted.get(filter);
  const lines = chunk.filter((line) => admits?.has(line.level) && (!needle || line.text.toLowerCase().includes(needle)));
  visibleCache.set(chunk, { key, lines });
  return lines;
}

export interface TextPart {
  text: string;
  hit: boolean;
}

export function highlightParts(text: string, needle: string): TextPart[] {
  if (!needle) return [{ text, hit: false }];
  const lower = text.toLowerCase();
  if (lower.length !== text.length) return [{ text, hit: false }];
  const parts: TextPart[] = [];
  let from = 0;
  for (let at = lower.indexOf(needle); at !== -1; at = lower.indexOf(needle, at + needle.length)) {
    if (at > from) parts.push({ text: text.slice(from, at), hit: false });
    parts.push({ text: text.slice(at, at + needle.length), hit: true });
    from = at + needle.length;
  }
  if (from < text.length) parts.push({ text: text.slice(from), hit: false });
  return parts;
}

export type StatusTone = "live" | "calm" | "fault";

export function describeStatus(status: GameStatus): { label: string; tone: StatusTone } {
  if (status.state === "running") return { label: "Игра запущена", tone: "live" };
  if (status.state === "idle") return { label: "Игра не запущена", tone: "calm" };
  if (status.stopped) return { label: "Игра остановлена", tone: "calm" };
  if (status.code === 0) return { label: "Игра закрыта", tone: "calm" };
  return { label: `Игра завершилась с ошибкой · код ${status.code}`, tone: "fault" };
}
