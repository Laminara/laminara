import { ArrowsClockwise, X } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { formatBytes } from "@/lib/format";
import { ProgressBar } from "@/components/ui/ProgressBar";

export function UpdateBanner() {
  const update = useLauncher((state) => state.update);
  const progress = useLauncher((state) => state.updateProgress);
  const dismissed = useLauncher((state) => state.updateDismissed);
  const dismiss = useLauncher((state) => state.dismissUpdate);

  if (!update || update.canInstall || (dismissed && !update.mandatory)) return null;

  const installing = progress !== null;
  const fraction = progress && progress.total > 0 ? progress.done / progress.total : 0;

  return (
    <div className="absolute inset-x-0 top-0 z-40 flex justify-center px-9 pt-20">
      <div className="flex w-full max-w-2xl items-center gap-4 rounded-lg border border-border bg-bg-tint px-5 py-3.5 shadow-panel">
        <ArrowsClockwise size={20} className="shrink-0 text-primary" />
        <div className="flex-1">
          <div className="text-sm font-semibold">
            Доступно обновление лаунчера {update.version}
            {update.mandatory && <span className="ml-2 rounded-full bg-primary-soft px-2 py-0.5 text-[10px] uppercase tracking-wide text-primary">Обязательное</span>}
          </div>
          {installing ? (
            <div className="mt-2">
              <ProgressBar value={fraction} className="h-1.5" />
              <div className="mt-1 text-xs tabular-nums text-dim">
                {formatBytes(progress.done)} / {formatBytes(progress.total || update.size)}
              </div>
            </div>
          ) : (
            <div className="mt-0.5 text-xs text-dim">
              {update.canInstall ? update.notes || `${formatBytes(update.size)} · перезапустится автоматически` : update.blockedReason}
            </div>
          )}
        </div>

        {!installing && !update.mandatory && (
          <button onClick={dismiss} className="rounded-md p-1.5 text-dim transition-colors hover:bg-surface-2 hover:text-text" aria-label="Позже">
            <X size={16} />
          </button>
        )}
      </div>
    </div>
  );
}
