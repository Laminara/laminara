import { WarningCircle, X } from "@phosphor-icons/react";
import { useLauncher } from "@/store";

export function ErrorToast() {
  const error = useLauncher((state) => state.error);
  const phase = useLauncher((state) => state.phase);
  const dismiss = useLauncher((state) => state.dismissError);

  if (!error || phase === "login") return null;

  return (
    <div className="absolute inset-x-0 bottom-0 z-40 flex justify-center p-8">
      <div className="flex max-w-2xl items-start gap-3 rounded-lg border border-border bg-bg-tint px-5 py-3.5 shadow-panel">
        <WarningCircle size={20} className="mt-0.5 shrink-0 text-primary" />
        <span className="text-sm text-dim">{error}</span>
        <button onClick={dismiss} className="-mr-1 rounded-md p-1 text-mute transition-colors hover:bg-surface-2 hover:text-text" aria-label="Закрыть">
          <X size={16} />
        </button>
      </div>
    </div>
  );
}
