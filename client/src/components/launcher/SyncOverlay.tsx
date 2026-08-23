import { X } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { formatBytes } from "@/lib/format";
import { ProgressBar } from "@/components/ui/ProgressBar";

const stageLabels: Record<string, string> = {
  planning: "Проверка файлов",
  downloading: "Загрузка",
  done: "Установлено",
};

export function SyncOverlay() {
  const sync = useLauncher((state) => state.sync);
  const cancel = useLauncher((state) => state.cancelSync);
  const selected = useLauncher((state) => state.selected);
  const fraction = sync && sync.bytesTotal > 0 ? sync.bytesDone / sync.bytesTotal : 0;

  return (
    <div className="absolute inset-0 z-30 flex items-end justify-center bg-bg/50 p-10" style={{ backdropFilter: "blur(4px)" }}>
      <div className="w-full max-w-2xl rounded-lg border border-border bg-surface p-6 shadow-panel">
        <div className="mb-4 flex items-start justify-between">
          <div>
            <div className="text-[11px] font-semibold uppercase tracking-[0.22em] text-dim">{stageLabels[sync?.stage ?? "planning"] ?? "Синхронизация"}</div>
            <div className="text-lg font-bold">{selected}</div>
          </div>
          <button onClick={() => void cancel()} className="rounded-md p-2 text-dim transition-colors hover:bg-surface-2 hover:text-text">
            <X size={18} />
          </button>
        </div>

        <ProgressBar value={fraction} className="h-1.5" />

        <div className="mt-3 flex items-center justify-between text-sm text-dim">
          <span className="tabular-nums">{sync ? `${formatBytes(sync.bytesDone)} / ${formatBytes(sync.bytesTotal)}` : "Подготовка…"}</span>
          <span className="tabular-nums">{sync ? `${sync.filesDone} / ${sync.filesTotal} файлов` : ""}</span>
        </div>

        {sync?.currentPath && <div className="mt-2 truncate text-xs text-mute">{sync.currentPath}</div>}
      </div>
    </div>
  );
}
