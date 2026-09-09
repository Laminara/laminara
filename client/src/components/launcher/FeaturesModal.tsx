import { useEffect, useState } from "react";
import type { FeatureGroup } from "@/lib/types";
import { ipc } from "@/lib/ipc";
import { useLauncher } from "@/store";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { FeatureGroupView } from "./FeatureGroupView";
import { activeAddresses, type Selected } from "@/lib/features";

export function FeaturesModal({ profile }: { profile: string }) {
  const close = useLauncher((state) => state.closeModal);
  const markOutdated = useLauncher((state) => state.markOutdated);
  const [model, setModel] = useState<FeatureGroup[] | null>(null);
  const [selected, setSelected] = useState<Selected>({});
  const [trouble, setTrouble] = useState<string | null>(null);

  useEffect(() => {
    ipc
      .buildFeatures(profile)
      .then((data) => {
        setModel(data.model);
        setSelected(data.selection.selected ?? {});
        setTrouble(null);
      })
      .catch((err: unknown) => setTrouble(String(err)));
  }, [profile]);

  const onChange = (addr: string, ids: string[]) => setSelected((prev) => ({ ...prev, [addr]: ids }));

  const save = async () => {
    try {
      await ipc.setBuildFeatures(profile, { selected });
      markOutdated(profile);
      close();
    } catch (err) {
      setTrouble(String(err));
    }
  };

  return (
    <Modal title="Моды" subtitle={profile} onClose={close}>
      {trouble && <div className="mb-4 rounded-lg bg-danger/15 px-3 py-2 text-sm text-danger">{trouble}</div>}
      {!model && !trouble && <div className="text-sm text-dim">Загружаю список модов…</div>}
      {model && (
        <div className="flex flex-col gap-6">
          {model.length === 0 && <p className="text-sm text-mute">У этой сборки нет опциональных модов.</p>}
          {model.map((group) => (
            <FeatureGroupView
              key={group.id}
              group={group}
              addr={group.id}
              selected={selected}
              active={activeAddresses(model, selected)}
              onChange={onChange}
            />
          ))}

          <div className="flex items-center justify-end gap-2 border-t border-border pt-4">
            <Button variant="ghost" size="sm" onClick={() => setSelected({})}>
              Сбросить
            </Button>
            <Button onClick={() => void save()} className="px-6">
              Сохранить
            </Button>
          </div>
        </div>
      )}
    </Modal>
  );
}
