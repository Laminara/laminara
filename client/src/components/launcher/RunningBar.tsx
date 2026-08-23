import { Square } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { OnlineDot } from "@/components/ui/OnlineDot";

export function RunningBar() {
  const stop = useLauncher((state) => state.stopGame);
  const selected = useLauncher((state) => state.selected);

  return (
    <div className="absolute inset-x-0 bottom-0 z-30 flex justify-center p-8">
      <div className="flex items-center gap-4 rounded-full border border-border bg-surface px-5 py-3 shadow-panel">
        <span className="flex items-center gap-2 text-sm">
          <OnlineDot />
          Игра запущена — <span className="font-semibold">{selected}</span>
        </span>
        <button
          onClick={() => void stop()}
          className="flex items-center gap-2 rounded-full bg-surface-2 px-4 py-2 text-sm font-semibold transition-colors hover:bg-surface-3"
        >
          <Square size={13} weight="fill" /> Остановить
        </button>
      </div>
    </div>
  );
}
