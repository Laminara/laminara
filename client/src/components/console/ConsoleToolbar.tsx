import { useState, type ReactNode, type RefObject } from "react";
import { Check, Copy, FolderOpen, MagnifyingGlass, Stop, X } from "@phosphor-icons/react";
import { useConsole } from "@/consoleStore";
import { filterCount, levelFilters, type LevelFilter } from "@/lib/gameLog";
import type { GameLine } from "@/lib/types";
import { cn, formatCount } from "@/lib/format";
import { Button } from "@/components/ui/Button";

const COPIED_FOR_MS = 1600;

const countTone: Record<LevelFilter, string> = {
  all: "text-dim",
  warn: "text-warn",
  error: "text-danger",
};

interface ActionProps {
  label: string;
  icon: ReactNode;
  variant?: "secondary" | "danger";
  disabled?: boolean;
  hint: string;
  onClick: () => void;
}

function Action({ label, icon, variant = "secondary", disabled, hint, onClick }: ActionProps) {
  return (
    <Button variant={variant} size="sm" icon={icon} onClick={onClick} disabled={disabled} title={hint} aria-label={label}>
      <span data-no-stretch className="max-[1023px]:hidden">
        {label}
      </span>
    </Button>
  );
}

interface ToolbarProps {
  searchRef: RefObject<HTMLInputElement | null>;
  visible: GameLine[][];
  shown: number;
}

export function ConsoleToolbar({ searchRef, visible, shown }: ToolbarProps) {
  const query = useConsole((state) => state.query);
  const setQuery = useConsole((state) => state.setQuery);
  const filter = useConsole((state) => state.filter);
  const setFilter = useConsole((state) => state.setFilter);
  const counts = useConsole((state) => state.counts);
  const session = useConsole((state) => state.session);
  const running = useConsole((state) => state.status.state === "running");
  const stopGame = useConsole((state) => state.stopGame);
  const openLogs = useConsole((state) => state.openLogs);
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    const text = visible.flatMap((lines) => lines.map((line) => line.text)).join("\n");
    await navigator.clipboard?.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), COPIED_FOR_MS);
  };

  return (
    <div className="flex shrink-0 items-center gap-3 border-b border-border px-4 py-3">
      <label className="flex h-9 min-w-40 max-w-[340px] flex-1 cursor-text items-center rounded-md bg-surface-2 ring-1 ring-border ring-inset transition-shadow focus-within:ring-border-strong">
        <MagnifyingGlass size={16} className="ml-3 shrink-0 text-mute" />
        <input
          ref={searchRef}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== "Escape") return;
            if (query) setQuery("");
            else event.currentTarget.blur();
          }}
          placeholder="Поиск"
          spellCheck={false}
          aria-label="Поиск по журналу"
          className="h-9 min-w-0 flex-1 bg-transparent px-2 text-sm text-text outline-none placeholder:text-mute"
        />
        {query && (
          <button
            onClick={() => {
              setQuery("");
              searchRef.current?.focus();
            }}
            className="mr-1.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-sm text-dim transition-colors hover:bg-surface-3 hover:text-text"
            aria-label="Очистить поиск"
          >
            <X size={14} weight="bold" />
          </button>
        )}
      </label>

      <div
        role="radiogroup"
        aria-label="Какие строки показывать"
        data-no-stretch
        className="flex h-9 shrink-0 items-center rounded-md bg-surface-2 p-1 ring-1 ring-border ring-inset"
      >
        {levelFilters.map((item) => {
          const active = filter === item.id;
          const count = filterCount(item.id, counts);
          return (
            <button
              key={item.id}
              role="radio"
              aria-checked={active}
              title={item.hint}
              data-no-scale
              onClick={() => setFilter(item.id)}
              className={cn(
                "flex h-full items-center gap-1.5 rounded-sm px-3 text-[13px] font-medium transition-colors",
                active ? "bg-surface-3 text-text" : "text-dim hover:text-text",
              )}
            >
              {item.label}
              <span className={cn("text-xs tabular-nums", count > 0 ? countTone[item.id] : "text-dim")}>{formatCount(count)}</span>
            </button>
          );
        })}
      </div>

      <div className="ml-auto flex shrink-0 items-center gap-2">
        <Action
          label="Копировать"
          icon={copied ? <Check size={16} weight="bold" className="text-online" /> : <Copy size={16} />}
          onClick={() => void copy()}
          disabled={shown === 0}
          hint="Скопировать строки, которые сейчас видны"
        />
        <Action
          label="Журналы"
          icon={<FolderOpen size={16} />}
          onClick={() => void openLogs()}
          disabled={session === 0}
          hint="Открыть папку logs этой сборки: там latest.log и debug.log"
        />
        {running && (
          <Action label="Остановить" variant="danger" icon={<Stop size={14} weight="fill" />} onClick={() => void stopGame()} hint="Остановить игру" />
        )}
      </div>
    </div>
  );
}
