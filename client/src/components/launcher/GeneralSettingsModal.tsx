import { useEffect, useState } from "react";
import { FolderOpen } from "@phosphor-icons/react";
import type { GeneralSettings } from "@/lib/types";
import { ipc } from "@/lib/ipc";
import { useLauncher } from "@/store";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";

export function GeneralSettingsModal() {
  const close = useLauncher((state) => state.closeModal);
  const [settings, setSettings] = useState<GeneralSettings | null>(null);
  const [installDir, setInstallDir] = useState("");
  const [cleaned, setCleaned] = useState<number | null>(null);
  const [trouble, setTrouble] = useState<string | null>(null);

  useEffect(() => {
    ipc
      .generalSettings()
      .then((data) => {
        setSettings(data);
        setInstallDir(data.installDir);
        setTrouble(null);
      })
      .catch((err: unknown) => setTrouble(String(err)));
  }, []);

  const chooseFolder = async () => {
    const picked = await ipc.pickFolder(installDir);
    if (picked) setInstallDir(picked);
  };

  const save = async () => {
    try {
      if (settings && installDir && installDir !== settings.installDir) {
        await ipc.setInstallDir(installDir);
      }
      close();
    } catch (err) {
      setTrouble(String(err));
    }
  };

  return (
    <Modal title="Настройки" subtitle="Общие параметры лаунчера" compact onClose={close}>
      {trouble && <div className="mb-4 rounded-lg bg-danger/15 px-3 py-2 text-sm text-danger">{trouble}</div>}
      {!settings && !trouble && <div className="text-sm text-dim">Загружаю настройки…</div>}
      {settings && (
        <div className="flex flex-col gap-6">
          <div>
            <div className="mb-1.5 text-sm text-dim">Папка установки</div>
            <div className="flex gap-2">
              <div className="flex-1 truncate rounded-md border border-border bg-surface-2 px-3 py-2 text-sm">{installDir}</div>
              <button
                onClick={() => void chooseFolder()}
                className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 text-sm text-dim transition-colors hover:bg-surface-2 hover:text-text"
              >
                <FolderOpen size={16} /> Выбрать
              </button>
            </div>
          </div>

          <div>
            <div className="mb-1.5 text-sm text-dim">Кэш загрузок</div>
            <div className="flex items-center gap-3">
              <button
                onClick={() => void ipc.collectGarbage().then(setCleaned)}
                className="rounded-md border border-border bg-surface px-3 py-2 text-sm text-dim transition-colors hover:bg-surface-2 hover:text-text"
              >
                Очистить неиспользуемое
              </button>
              {cleaned !== null && <span className="text-xs text-mute">Удалено объектов: {cleaned}</span>}
            </div>
          </div>

          <div className="flex items-center justify-between border-t border-border pt-4">
            <span className="text-xs text-mute">Laminara v{settings.version}</span>
            <Button onClick={() => void save()} className="px-6">
              Сохранить
            </Button>
          </div>
        </div>
      )}
    </Modal>
  );
}
