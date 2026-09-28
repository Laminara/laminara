import { WarningCircle, X } from "@phosphor-icons/react";
import { useConsole } from "@/consoleStore";
import { formatCount, plural } from "@/lib/format";

const searchShortcut = /Mac|iPhone|iPad/.test(navigator.userAgent) ? "⌘ F" : "Ctrl + F";

export function ConsoleFooter({ shown, needle }: { shown: number; needle: string }) {
  const total = useConsole((state) => state.total);
  const dropped = useConsole((state) => state.dropped);
  const filter = useConsole((state) => state.filter);
  const trouble = useConsole((state) => state.trouble);
  const dismissTrouble = useConsole((state) => state.dismissTrouble);
  const narrowed = filter !== "all" || needle !== "";

  if (trouble) {
    return (
      <div role="alert" className="flex h-8 shrink-0 items-center gap-2 border-t border-border bg-danger/10 px-4 text-xs text-danger">
        <WarningCircle size={14} className="shrink-0" />
        <span className="truncate">{trouble}</span>
        <button onClick={dismissTrouble} className="ml-auto flex h-6 w-6 items-center justify-center rounded-sm transition-colors hover:bg-danger/15" aria-label="Скрыть">
          <X size={12} weight="bold" />
        </button>
      </div>
    );
  }

  return (
    <div className="flex h-8 shrink-0 items-center gap-4 border-t border-border px-4 text-xs text-dim">
      <span className="min-w-0 truncate tabular-nums">
        {formatCount(total)} {plural(total, "строка", "строки", "строк")}
        {narrowed && ` · ${needle ? "найдено" : "показано"} ${formatCount(shown)}`}
        {dropped > 0 && ` · первые ${formatCount(dropped)} уже не помещаются, весь журнал — в logs/latest.log`}
      </span>
      <span data-no-stretch className="ml-auto shrink-0">
        {searchShortcut} — поиск
      </span>
    </div>
  );
}
