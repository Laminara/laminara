import { useLauncher } from "@/store";
import { labels } from "@/config/branding";
import { BuildCard } from "./BuildCard";

export function BuildStrip() {
  const builds = useLauncher((state) => state.builds);
  const selected = useLauncher((state) => state.selected);
  const select = useLauncher((state) => state.select);
  const openModal = useLauncher((state) => state.openModal);

  return (
    <div className="px-10 pb-8">
      <div className="mb-3 flex items-center justify-between">
        <span className="text-[11px] font-semibold uppercase tracking-[0.24em] text-dim">{labels.changeBuild}</span>
        <button
          onClick={() => openModal({ kind: "library" })}
          className="text-[11px] uppercase tracking-wider text-mute transition-colors hover:text-text"
        >
          Все сборки ({builds.length})
        </button>
      </div>
      <div className="flex gap-3 overflow-x-auto pb-1" style={{ scrollbarWidth: "thin" }}>
        {builds.map((build) => (
          <div key={build.name} className="w-[210px] shrink-0">
            <BuildCard build={build} selected={build.name === selected} onClick={() => select(build.name)} />
          </div>
        ))}
      </div>
    </div>
  );
}
