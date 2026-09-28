import { ArrowClockwise, PaperPlaneTilt, WarningCircle, X } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { Button } from "@/components/ui/Button";

const reportLabels = {
  idle: "Отправить журнал",
  sending: "Отправляю",
  sent: "Журнал отправлен",
  failed: "Отправить журнал",
} as const;

export function ErrorToast() {
  const error = useLauncher((state) => state.error);
  const phase = useLauncher((state) => state.phase);
  const dismiss = useLauncher((state) => state.dismissError);
  const play = useLauncher((state) => state.play);
  const logReport = useLauncher((state) => state.logReport);
  const sendLauncherLog = useLauncher((state) => state.sendLauncherLog);

  if (!error || phase === "login") return null;

  return (
    <div role="alert" className="pointer-events-none absolute inset-x-0 bottom-[196px] z-40 flex justify-center px-8">
      <div className="pointer-events-auto flex max-w-3xl items-center gap-3 rounded-lg border border-border bg-panel-2 py-2.5 pl-5 pr-3 shadow-panel backdrop-blur-xl">
        <WarningCircle size={20} className="shrink-0 text-primary" />
        <div className="min-w-0 flex-1">
          <p className="text-sm text-dim [overflow-wrap:anywhere]">{error.message}</p>
          {logReport.state === "failed" && (
            <p className="mt-0.5 text-xs text-danger [overflow-wrap:anywhere]">Журнал не ушёл: {logReport.message}</p>
          )}
        </div>
        {error.retry && (
          <Button
            variant="secondary"
            size="sm"
            icon={<ArrowClockwise size={16} />}
            onClick={() => {
              dismiss();
              void play();
            }}
            className="shrink-0"
          >
            <span data-no-stretch>Повторить</span>
          </Button>
        )}
        <Button
          variant="secondary"
          size="sm"
          icon={<PaperPlaneTilt size={16} />}
          onClick={() => void sendLauncherLog()}
          disabled={logReport.state === "sending" || logReport.state === "sent"}
          className="shrink-0"
        >
          <span data-no-stretch>{reportLabels[logReport.state]}</span>
        </Button>
        <button
          onClick={dismiss}
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded-sm text-mute transition-colors hover:bg-surface-2 hover:text-text"
          aria-label="Закрыть"
        >
          <X size={16} />
        </button>
      </div>
    </div>
  );
}
