import { useLauncher } from "@/store";
import { plural } from "@/lib/format";
import { Modal } from "@/components/ui/Modal";
import { NewsCard } from "./NewsCard";

export function NewsModal() {
  const news = useLauncher((state) => state.news);
  const close = useLauncher((state) => state.closeModal);

  return (
    <Modal title="Новости" subtitle={`${news.length} ${plural(news.length, "запись", "записи", "записей")}`} onClose={close}>
      {news.length === 0 ? (
        <div className="flex h-full items-center justify-center text-sm text-mute">Пока новостей нет</div>
      ) : (
        <div className="grid grid-cols-1 gap-5 @2xl:grid-cols-2">
          {news.map((item) => (
            <NewsCard key={item.id} item={item} />
          ))}
        </div>
      )}
    </Modal>
  );
}
