import { useEffect, useState } from "react";
import { ShieldCheck } from "@phosphor-icons/react";
import type { BuildSettings } from "@/lib/types";
import { ipc } from "@/lib/ipc";
import { useLauncher } from "@/store";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { MemoryField } from "./MemoryField";

export function BuildSettingsModal({ profile }: { profile: string }) {
  const close = useLauncher((state) => state.closeModal);
  const repair = useLauncher((state) => state.repairBuild);
  const [data, setData] = useState<BuildSettings | null>(null);
  const [memory, setMemory] = useState(4096);

  useEffect(() => {
    void ipc.buildSettings(profile).then((settings) => {
      setData(settings);
      setMemory(settings.maxMemoryMb ?? settings.defaultMemoryMb);
    });
  }, [profile]);

  const save = async () => {
    await ipc.setBuildMemory(profile, memory);
    close();
  };

  const check = () => {
    close();
    void repair(profile);
  };

  return (
    <Modal title="Настройки сборки" subtitle={profile} compact onClose={close}>
      {data && (
        <div className="flex flex-col gap-6">
          <MemoryField valueMb={memory} onChange={setMemory} />

          <div className="flex flex-col gap-2 border-t border-border pt-4">
            <div className="flex items-center justify-between gap-4">
              <div>
                <p className="text-sm font-medium">Проверить файлы</p>
                <p className="text-xs text-dim">
                  Сверяет каждый файл сборки с сервером и заново скачивает изменённые. Помогает, когда игра
                  падает после вмешательства антивируса.
                </p>
              </div>
              <Button variant="ghost" size="sm" onClick={check} className="shrink-0">
                <ShieldCheck size={16} />
                Проверить
              </Button>
            </div>
          </div>

          <div className="flex justify-end border-t border-border pt-4">
            <Button onClick={() => void save()} className="px-6">
              Сохранить
            </Button>
          </div>
        </div>
      )}
    </Modal>
  );
}
