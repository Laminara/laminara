import { Warning } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";

export function CrashModal() {
  const crash = useLauncher((state) => state.crash);
  const dismiss = useLauncher((state) => state.dismissCrash);
  if (!crash) return null;

  return (
    <Modal title="Игра завершилась с ошибкой" subtitle={`Код выхода ${crash.code}`} onClose={dismiss}>
      <div className="flex flex-col gap-4">
        <div className="flex items-start gap-3 rounded-md border border-border bg-surface-2 p-3 text-sm text-dim">
          <Warning size={18} className="mt-0.5 shrink-0 text-primary" />
          <span>Последние строки журнала игры. Полный журнал лаунчера — в папке данных, файл launcher.log.</span>
        </div>

        <pre className="max-h-80 overflow-auto rounded-md border border-border bg-surface p-3 text-[11px] leading-relaxed text-dim">
          {crash.log.length > 0 ? crash.log.join("\n") : "Журнал пуст."}
        </pre>

        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={() => void navigator.clipboard?.writeText(crash.log.join("\n"))}>
            Копировать
          </Button>
          <Button onClick={dismiss} className="px-6">
            Закрыть
          </Button>
        </div>
      </div>
    </Modal>
  );
}
