import { TerminalWindow } from "@phosphor-icons/react";
import { useConsole } from "@/consoleStore";
import { describeStatus, type StatusTone } from "@/lib/gameLog";
import { cn } from "@/lib/format";
import { WindowControls } from "@/components/ui/WindowControls";

const toneDot: Record<StatusTone, string> = {
  live: "bg-online shadow-online",
  calm: "bg-mute",
  fault: "bg-danger",
};

export function ConsoleTitleBar() {
  const build = useConsole((state) => state.build);
  const status = useConsole((state) => state.status);
  const { label, tone } = describeStatus(status);

  return (
    <div data-tauri-drag-region className="flex h-9 shrink-0 items-center gap-3 border-b border-border bg-bg-tint pl-4">
      <div className="pointer-events-none flex min-w-0 items-center gap-2.5">
        <TerminalWindow size={17} weight="duotone" className="shrink-0 text-primary" />
        <span data-no-stretch className="shrink-0 text-sm font-semibold">
          Консоль игры
        </span>
        {build && (
          <>
            <span className="h-1 w-1 shrink-0 rounded-full bg-mute" />
            <span className="truncate text-sm text-dim">{build}</span>
          </>
        )}
      </div>
      <span
        data-no-stretch
        className={cn(
          "pointer-events-none flex shrink-0 items-center gap-2 rounded-full border border-border bg-surface px-2.5 py-0.5 text-xs",
          tone === "fault" ? "text-danger" : "text-dim",
        )}
      >
        <span className="relative flex h-2 w-2">
          {tone === "live" && <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-online opacity-60" />}
          <span className={cn("relative inline-flex h-2 w-2 rounded-full", toneDot[tone])} />
        </span>
        {label}
      </span>
      <div className="ml-auto self-stretch">
        <WindowControls maximizable />
      </div>
    </div>
  );
}
