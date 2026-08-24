import { useState } from "react";
import { useLauncher } from "@/store";
import { loaderLabels } from "@/config/branding";
import { buildBlock } from "@/lib/buildState";
import { cn, formatBytes, formatCount, plural } from "@/lib/format";
import { Modal } from "@/components/ui/Modal";

export function LibraryModal() {
  const builds = useLauncher((state) => state.builds);
  const selected = useLauncher((state) => state.selected);
  const players = useLauncher((state) => state.players);
  const select = useLauncher((state) => state.select);
  const close = useLauncher((state) => state.closeModal);
  const [query, setQuery] = useState("");

  const needle = query.trim().toLowerCase();
  const filtered = builds.filter(
    (build) => build.name.toLowerCase().includes(needle) || (build.loader ?? "").toLowerCase().includes(needle),
  );

  return (
    <Modal title="Все сборки" subtitle={`${builds.length} ${plural(builds.length, "сборка", "сборки", "сборок")}`} onClose={close}>
      <input
        autoFocus
        placeholder="Поиск сборки…"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        className="mb-4 w-full rounded-md border border-border bg-surface-2 px-4 py-2.5 text-sm outline-none transition-colors placeholder:text-mute focus:border-border-strong"
      />

      <div className="grid grid-cols-3 gap-3">
        {filtered.map((build) => {
          const online = players?.perBuild[build.name];
          const block = buildBlock(build);
          return (
            <button
              key={build.name}
              onClick={() => {
                select(build.name);
                close();
              }}
              className={cn(
                "flex flex-col gap-2 rounded-lg border p-4 text-left transition-colors",
                build.name === selected ? "border-primary/50 bg-primary-soft" : "border-border bg-surface hover:bg-surface-2",
                block && "opacity-50",
              )}
            >
              <div className="flex items-center justify-between">
                <span className="text-[10px] font-semibold uppercase tracking-[0.18em] text-mute">
                  {build.loader ? loaderLabels[build.loader] : "Сборка"}
                </span>
                {block ? (
                  <span className="text-[10px] font-bold uppercase tracking-wider text-mute">{block.badge}</span>
                ) : (
                  online && (
                    <span className="flex items-center gap-1 text-[11px] text-dim">
                      <span className="h-1.5 w-1.5 rounded-full bg-online" />
                      {formatCount(online.online)} / {formatCount(online.max)}
                    </span>
                  )
                )}
              </div>
              <span className="text-[15px] font-bold leading-tight">{build.name}</span>
              <span className="text-xs tabular-nums text-dim">{block ? block.reason : formatBytes(build.sizeBytes)}</span>
            </button>
          );
        })}
        {filtered.length === 0 && <div className="col-span-3 py-10 text-center text-sm text-mute">Ничего не найдено</div>}
      </div>
    </Modal>
  );
}
