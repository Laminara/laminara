import { Square, TerminalWindow } from "@phosphor-icons/react";
import { useLauncher } from "@/store";

export function RunningBar() {
  const stop = useLauncher((state) => state.stopGame);
  const openConsole = useLauncher((state) => state.openConsole);
  const selected = useLauncher((state) => state.selected);

  return (
    <div className="z-40 flex justify-center px-10 pb-3">
      <div className="flex items-center gap-4 rounded-full border border-primary/40 bg-panel-2 py-2 pl-5 pr-2 shadow-panel backdrop-blur-xl">
        <span className="flex items-center gap-2.5 text-sm">
          <span className="relative flex h-2.5 w-2.5">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-online opacity-70" />
            <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-online shadow-online" />
          </span>
          Игра запущена — <span className="font-semibold">{selected}</span>
        </span>
        <div className="flex items-center gap-2">
          <button
            onClick={() => void openConsole()}
            className="flex h-9 items-center gap-2 rounded-full border border-border bg-surface-2 px-4 text-sm font-semibold text-text transition-colors hover:bg-surface-3"
          >
            <TerminalWindow size={15} /> Консоль
          </button>
          <button
            onClick={() => void stop()}
            className="flex h-9 items-center gap-2 rounded-full bg-primary px-4 text-sm font-semibold text-primary-ink transition-opacity hover:opacity-90"
          >
            <Square size={13} weight="fill" /> Остановить
          </button>
        </div>
      </div>
    </div>
  );
}
