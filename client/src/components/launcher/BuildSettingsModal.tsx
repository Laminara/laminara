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
  const [override, setOverride] = useState(false);
  const [memory, setMemory] = useState(4096);

  useEffect(() => {
    void ipc.buildSettings(profile).then((settings) => {
      setData(settings);
      setOverride(settings.maxMemoryMb != null);
      setMemory(settings.maxMemoryMb ?? settings.defaultMemoryMb);
    });
  }, [profile]);

  const save = async () => {
    await ipc.setBuildMemory(profile, override ? memory : null);
    close();
  };

  return (
    <Modal title="Настройки сборки" subtitle={profile} onClose={close}>
      {data && (
        <div className="flex flex-col gap-6">
          <label className="flex cursor-pointer items-center gap-3 text-sm">
            <input
              type="checkbox"
              checked={override}
              onChange={(event) => setOverride(event.target.checked)}
              className="h-4 w-4 accent-[var(--lm-primary)]"
            />
            Своя память для этой сборки
          </label>

          <div className={override ? "" : "pointer-events-none opacity-40"}>
            <MemoryField valueMb={memory} onChange={setMemory} />
          </div>

          {!override && <p className="text-xs text-mute">Сейчас используется общая память — {Math.round(data.defaultMemoryMb / 1024)} ГБ.</p>}

          <div className="flex justify-end">
            <Button onClick={() => void save()} className="px-6">
              Сохранить
            </Button>
          </div>
        </div>
      )}
    </Modal>
  );
}
