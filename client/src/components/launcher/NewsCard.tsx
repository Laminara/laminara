import { ArrowUpRight } from "@phosphor-icons/react";
import { ipc } from "@/lib/ipc";
import { cn, formatNewsDate } from "@/lib/format";
import type { NewsItem } from "@/lib/types";

export function NewsCard({ item, compact = false }: { item: NewsItem; compact?: boolean }) {
  const date = formatNewsDate(item.publishedAtUnixNanos);

  return (
    <article className="flex flex-col overflow-hidden rounded-lg border border-border bg-surface">
      {item.bannerDataUri && (
        <img
          src={item.bannerDataUri}
          alt=""
          className={cn("w-full object-cover", compact ? "h-20" : "h-44")}
          style={{ borderBottom: "1px solid var(--lm-border)" }}
        />
      )}

      <div className={cn("flex flex-col gap-2", compact ? "p-4" : "p-6")}>
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.18em] text-mute">
          {item.tag && <span className="rounded bg-surface-2 px-1.5 py-0.5 text-primary">{item.tag}</span>}
          {date && <span>{date}</span>}
        </div>

        <h3 className={cn("font-bold leading-snug", compact ? "text-sm" : "text-lg")}>{item.title}</h3>

        {item.body && (
          <p className={cn("whitespace-pre-line leading-relaxed text-dim", compact ? "line-clamp-3 text-xs" : "text-sm")}>{item.body}</p>
        )}

        {item.link && (
          <button
            onClick={() => void ipc.openExternal(item.link as string)}
            className="mt-1 flex items-center gap-1 self-start text-[11px] font-semibold text-primary transition-opacity hover:opacity-80"
          >
            Подробнее <ArrowUpRight size={12} weight="bold" />
          </button>
        )}
      </div>
    </article>
  );
}
