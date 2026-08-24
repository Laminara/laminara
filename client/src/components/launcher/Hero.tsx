import { Gear, Play, SlidersHorizontal } from "@phosphor-icons/react";
import { useLauncher, useSelectedBuild } from "@/store";
import { labels, loaderLabels } from "@/config/branding";
import { buildBlock } from "@/lib/buildState";
import { formatBytes, formatCount } from "@/lib/format";
import { Button } from "@/components/ui/Button";
import { Dot } from "@/components/ui/atoms";
import { OnlineDot } from "@/components/ui/OnlineDot";

export function Hero() {
  const build = useSelectedBuild();
  const play = useLauncher((state) => state.play);
  const players = useLauncher((state) => state.players);
  const openModal = useLauncher((state) => state.openModal);
  if (!build) return null;
  const online = players?.perBuild[build.name];
  const block = buildBlock(build);
  const actionLabel = build.install === "missing" ? labels.install : build.install === "outdated" ? labels.update : labels.play;

  return (
    <section className="flex max-w-3xl flex-col gap-6 px-10 pb-4">
      <div className="flex items-center gap-2.5 text-[11px] font-semibold uppercase tracking-[0.24em] text-dim">
        <span>{labels.selectedBuild}</span>
        <Dot />
        <span>{build.loader ? loaderLabels[build.loader] : "Minecraft"}</span>
        <Dot />
        <span>{formatBytes(build.sizeBytes)}</span>
      </div>

      <h1 className="max-w-2xl text-[84px] font-extrabold leading-[0.9] tracking-tight">{build.name}</h1>

      {online && online.max > 0 && (
        <div className="flex items-center gap-2.5 text-sm">
          <OnlineDot />
          <span className="font-semibold tabular-nums text-text">
            {formatCount(online.online)}
            <span className="text-dim"> / {formatCount(online.max)}</span>
          </span>
          <span className="text-dim">{labels.players}</span>
        </div>
      )}

      <div className="mt-1 flex items-center gap-3">
        <Button
          variant="primary"
          icon={<Play size={16} weight="fill" />}
          onClick={() => void play()}
          disabled={block !== null}
          className="px-7 py-4 text-[15px]"
        >
          {block ? block.badge.toUpperCase() : actionLabel.toUpperCase()}
        </Button>
        <button
          onClick={() => openModal({ kind: "build", profile: build.name })}
          className="flex h-[52px] w-[52px] items-center justify-center rounded-md border border-border bg-surface-2 text-dim transition-colors hover:bg-surface-3 hover:text-text"
          aria-label="Настройки сборки"
        >
          <Gear size={20} />
        </button>
        {build.hasFeatures && (
          <button
            onClick={() => openModal({ kind: "features", profile: build.name })}
            className="flex h-[52px] items-center gap-2 rounded-md border border-border bg-surface-2 px-4 text-sm font-medium text-dim transition-colors hover:bg-surface-3 hover:text-text"
          >
            <SlidersHorizontal size={18} /> Моды
          </button>
        )}
      </div>
    </section>
  );
}
