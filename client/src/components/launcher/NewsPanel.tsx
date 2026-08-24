import { CaretRight, Newspaper } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { formatNewsDate } from "@/lib/format";
import { NewsCard } from "./NewsCard";

export function NewsPanel() {
  const news = useLauncher((state) => state.news);
  const unread = useLauncher((state) => state.unreadNews);
  const openModal = useLauncher((state) => state.openModal);
  if (news.length === 0) return null;

  const [featured, ...rest] = news;

  return (
    <aside className="hidden min-h-0 w-[320px] shrink-0 flex-col gap-3 overflow-hidden pr-10 pt-6 lg:flex">
      <button
        onClick={() => openModal({ kind: "news" })}
        className="flex items-center justify-between text-[11px] font-semibold uppercase tracking-[0.24em] text-dim transition-colors hover:text-text"
      >
        <span className="flex items-center gap-2">
          <Newspaper size={14} weight="bold" /> Новости
          {unread > 0 && <span className="h-1.5 w-1.5 rounded-full bg-primary" />}
        </span>
        <span className="tracking-normal text-mute">все ({news.length})</span>
      </button>

      <div className="flex min-h-0 flex-col gap-2 overflow-hidden">
        <NewsCard item={featured} compact />
        {rest.slice(0, 2).map((item) => (
          <button
            key={item.id}
            onClick={() => openModal({ kind: "news" })}
            className="flex items-center gap-2 rounded-lg border border-border bg-panel px-3.5 py-2.5 text-left backdrop-blur-md transition-colors hover:bg-panel-2"
          >
            <div className="min-w-0 flex-1">
              <div className="truncate text-xs font-semibold">{item.title}</div>
              <div className="text-[10px] uppercase tracking-[0.14em] text-mute">
                {item.tag ?? formatNewsDate(item.publishedAtUnixNanos)}
              </div>
            </div>
            <CaretRight size={12} weight="bold" className="shrink-0 text-mute" />
          </button>
        ))}
      </div>
    </aside>
  );
}
