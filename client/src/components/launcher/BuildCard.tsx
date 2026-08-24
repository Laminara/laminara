import type { Build } from "@/lib/types";
import { useLauncher } from "@/store";
import { loaderLabels } from "@/config/branding";
import { buildBlock } from "@/lib/buildState";
import { cn, formatCount } from "@/lib/format";
import { ProgressBar } from "@/components/ui/ProgressBar";
import { StatusDot } from "@/components/ui/atoms";

export function BuildCard({ build, selected, onClick }: { build: Build; selected: boolean; onClick: () => void }) {
  const block = buildBlock(build);
  const online = useLauncher((state) => state.players?.perBuild[build.name]);
  const installing = build.install === "syncing";
  return (
    <button
      onClick={onClick}
      className={cn(
        "flex w-full flex-col gap-3 rounded-lg border p-4 text-left transition-colors",
        selected ? "border-primary/50 bg-primary-soft" : "border-border bg-surface hover:bg-surface-2",
        block && "opacity-50",
      )}
    >
      <div className="flex items-center justify-between">
        <span className="text-[10px] font-semibold uppercase tracking-[0.2em] text-mute">{build.loader ? loaderLabels[build.loader] : "Сборка"}</span>
        {block ? (
          <span className="rounded-md bg-surface-2 px-2 py-0.5 text-[9px] font-bold uppercase tracking-wider text-mute">{block.badge}</span>
        ) : selected ? (
          <span className="rounded-md bg-primary px-2 py-0.5 text-[9px] font-bold uppercase tracking-wider text-primary-ink">Сейчас</span>
        ) : (
          <StatusDot state={build.install} />
        )}
      </div>
      <span className="text-[15px] font-bold leading-tight">{build.name}</span>
      <div className="flex items-center gap-3">
        {installing ? (
          <span className="text-xs tabular-nums text-dim">{Math.round((build.progress ?? 0) * 100)}%</span>
        ) : (
          online && (
            <span className="flex items-center gap-1.5 text-xs tabular-nums text-dim">
              <span className="h-1.5 w-1.5 rounded-full bg-online" />
              {formatCount(online.online)}
              <span className="text-mute">/ {formatCount(online.max)}</span>
            </span>
          )
        )}
        <ProgressBar value={build.install === "missing" ? 0 : build.progress ?? 1} className="flex-1" />
      </div>
    </button>
  );
}
