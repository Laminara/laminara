import { X } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { formatBytes, formatLeft, formatSpeed, plural } from "@/lib/format";
import { ProgressBar } from "@/components/ui/ProgressBar";

const stageLabels: Record<string, string> = {
  planning: "Проверка файлов",
  downloading: "Загрузка",
  done: "Установлено",
  launching: "Проверяю файлы и запускаю игру",
};

export function SyncOverlay() {
  const sync = useLauncher((state) => state.sync);
  const cancel = useLauncher((state) => state.cancelSync);
  const cancelling = useLauncher((state) => state.cancelRequested);
  const selected = useLauncher((state) => state.selected);
  const fraction = sync && sync.bytesTotal > 0 ? sync.bytesDone / sync.bytesTotal : 0;
  const stage = cancelling ? "Отменяю загрузку" : (stageLabels[sync?.stage ?? "planning"] ?? "Синхронизация");

  let amount = "Подготовка…";
  if (sync && sync.bytesTotal > 0) amount = `${formatBytes(sync.bytesDone)} / ${formatBytes(sync.bytesTotal)}`;
  else if (sync?.stage === "downloading") amount = "Всё уже скачано";

  return (
    <div className="absolute inset-0 z-30 flex items-end justify-center bg-bg/50 p-10" style={{ backdropFilter: "blur(4px)" }}>
      <div className="w-full max-w-2xl rounded-lg border border-border bg-surface p-6 shadow-panel">
        <div className="mb-4 flex items-start justify-between">
          <div>
            <div className="text-[11px] font-semibold uppercase tracking-[0.22em] text-dim">{stage}</div>
            <div className="text-lg font-bold">{selected}</div>
          </div>
          {sync?.stage !== "launching" && (
            <button
              onClick={() => void cancel()}
              disabled={cancelling}
              aria-label="Отменить загрузку"
              className="rounded-md p-2 text-dim transition-colors hover:bg-surface-2 hover:text-text disabled:opacity-40"
            >
              <X size={18} />
            </button>
          )}
        </div>

        <ProgressBar value={fraction} className="h-1.5" />

        <div className="mt-3 flex items-center justify-between text-sm text-dim">
          <span className="tabular-nums">{amount}</span>
          <span className="tabular-nums">
            {sync && sync.filesTotal > 0
              ? `${sync.filesDone} / ${sync.filesTotal} ${plural(sync.filesTotal, "файл", "файла", "файлов")}`
              : ""}
          </span>
        </div>

        {sync && (sync.bytesPerSecond || sync.secondsLeft) && (
          <div className="mt-1 flex items-center gap-2 text-xs text-mute">
            {sync.bytesPerSecond ? <span className="tabular-nums">{formatSpeed(sync.bytesPerSecond)}</span> : null}
            {sync.bytesPerSecond && sync.secondsLeft ? <span>·</span> : null}
            {sync.secondsLeft ? <span>осталось {formatLeft(sync.secondsLeft)}</span> : null}
          </div>
        )}

        {sync?.currentPath && <div className="mt-2 truncate text-xs text-mute">{sync.currentPath}</div>}
      </div>
    </div>
  );
}
