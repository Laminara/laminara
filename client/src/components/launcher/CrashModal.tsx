import { PaperPlaneTilt, TerminalWindow, Warning } from "@phosphor-icons/react";
import { useLauncher, type Crash } from "@/store";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { LogLine, logTextClass } from "@/components/console/LogLine";
import { cn } from "@/lib/format";

export function CrashModal({ crash }: { crash: Crash }) {
  const dismiss = useLauncher((state) => state.dismissCrash);
  const send = useLauncher((state) => state.sendCrash);
  const sending = useLauncher((state) => state.crashSending);
  const sent = useLauncher((state) => state.crashSent);
  const trouble = useLauncher((state) => state.crashError);
  const openConsole = useLauncher((state) => state.openConsole);

  return (
    <Modal title="Игра завершилась с ошибкой" subtitle={`Код выхода ${crash.code}`} onClose={dismiss}>
      <div className="flex h-full flex-col gap-4">
        <div className="flex shrink-0 items-start gap-3 rounded-md border border-border bg-surface-2 p-3 text-sm text-dim">
          <Warning size={18} className="mt-0.5 shrink-0 text-primary" />
          <span>Последние строки журнала игры. Весь журнал с поиском по ошибкам — в консоли.</span>
        </div>

        <div className={cn("selectable min-h-0 flex-1 overflow-auto rounded-md border border-border bg-surface py-3", logTextClass)}>
          {crash.log.length > 0 ? crash.log.map((line) => <LogLine key={line.seq} line={line} />) : <p className="px-4 text-dim">Журнал пуст.</p>}
        </div>

        {sent && <div className="shrink-0 rounded-md border border-border bg-surface-2 p-3 text-sm text-dim">{sent}</div>}
        {trouble && <div className="shrink-0 rounded-md bg-danger/15 p-3 text-sm text-danger">Отчёт не ушёл: {trouble}</div>}

        <div className="flex shrink-0 justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={() => void openConsole()}>
            <TerminalWindow size={16} />
            Весь журнал
          </Button>
          <Button variant="ghost" size="sm" onClick={() => void navigator.clipboard?.writeText(crash.log.map((line) => line.text).join("\n"))}>
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
