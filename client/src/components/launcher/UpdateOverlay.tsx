import { ArrowsClockwise } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { formatBytes } from "@/lib/format";
import { ProgressBar } from "@/components/ui/ProgressBar";
import { BrandMark } from "./BrandMark";

export function UpdateOverlay() {
  const update = useLauncher((state) => state.update);
  const progress = useLauncher((state) => state.updateProgress);
  const total = progress?.total || update?.size || 0;
  const fraction = progress && total > 0 ? progress.done / total : 0;

  return (
    <div data-tauri-drag-region className="relative z-10 flex flex-1 items-center justify-center">
      <div className="w-full max-w-md rounded-lg border border-border bg-bg-tint p-8 shadow-panel">
        <BrandMark />
        <div className="mt-6 flex items-center gap-3">
          <ArrowsClockwise size={20} className="animate-spin text-primary" />
          <div>
            <div className="text-sm font-semibold">Обновление лаунчера</div>
            <div className="text-xs text-dim">Версия {update?.version ?? ""} · запустится автоматически</div>
          </div>
        </div>
        <ProgressBar value={fraction} className="mt-5 h-1.5" />
        <div className="mt-2 text-xs tabular-nums text-dim">
          {formatBytes(progress?.done ?? 0)} / {formatBytes(total)}
        </div>
      </div>
    </div>
  );
}
