import { useEffect, useState } from "react";
import type { BuildSettings } from "@/lib/types";
import { ipc } from "@/lib/ipc";
import { useLauncher } from "@/store";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { MemoryField } from "./MemoryField";

export function BuildSettingsModal({ profile }: { profile: string }) {
  const close = useLauncher((state) => state.closeModal);
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

  return (
    <Modal title="Настройки сборки" subtitle={profile} compact onClose={close}>
      {data && (
        <div className="flex flex-col gap-6">
          <MemoryField valueMb={memory} onChange={setMemory} />

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
