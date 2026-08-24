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

  useEffect(() => {
    void ipc.generalSettings().then((data) => {
      setSettings(data);
      setInstallDir(data.installDir);
    });
  }, []);

  const chooseFolder = async () => {
    const picked = await ipc.pickFolder(installDir);
    if (picked) setInstallDir(picked);
  };

  const save = async () => {
    if (settings && installDir && installDir !== settings.installDir) {
      await ipc.setInstallDir(installDir);
    }
    close();
  };

  return (
    <Modal title="Настройки" subtitle="Общие параметры лаунчера" compact onClose={close}>
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
