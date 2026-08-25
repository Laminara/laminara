import { PaperPlaneTilt, Warning } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";

export function CrashModal() {
  const crash = useLauncher((state) => state.crash);
  const dismiss = useLauncher((state) => state.dismissCrash);
  const send = useLauncher((state) => state.sendCrash);
  const sending = useLauncher((state) => state.crashSending);
  const sent = useLauncher((state) => state.crashSent);
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

        {sent && <div className="rounded-md border border-border bg-surface-2 p-3 text-sm text-dim">{sent}</div>}

        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={() => void navigator.clipboard?.writeText(crash.log.join("\n"))}>
            Копировать
          </Button>
          <Button variant="ghost" size="sm" onClick={() => void send()} disabled={sending || sent !== null}>
            <PaperPlaneTilt size={16} />
            {sending ? "Отправляю" : "Отправить разработчикам"}
          </Button>
          <Button onClick={dismiss} className="px-6">
            Закрыть
          </Button>
        </div>
      </div>
    </Modal>
  );
}
