import { Square } from "@phosphor-icons/react";
import { useLauncher } from "@/store";

export function RunningBar() {
  const stop = useLauncher((state) => state.stopGame);
  const selected = useLauncher((state) => state.selected);

  return (
    <div className="z-40 flex justify-center px-10 pb-3">
      <div className="flex items-center gap-4 rounded-full border border-primary/40 bg-panel-2 px-5 py-3 shadow-panel backdrop-blur-xl">
        <span className="flex items-center gap-2.5 text-sm">
          <span className="relative flex h-2.5 w-2.5">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-online opacity-70" />
            <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-online shadow-online" />
          </span>
          Игра запущена — <span className="font-semibold">{selected}</span>
        </span>
        <button
          onClick={() => void stop()}
          className="flex items-center gap-2 rounded-full bg-primary px-4 py-2 text-sm font-semibold text-primary-ink transition-opacity hover:opacity-90"
        >
          <Square size={13} weight="fill" /> Остановить
        </button>
      </div>
    </div>
  );
}
