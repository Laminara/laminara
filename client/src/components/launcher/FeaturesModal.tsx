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

  useEffect(() => {
    void ipc.buildFeatures(profile).then((data) => {
      setModel(data.model);
      setSelected(data.selection.selected ?? {});
    });
  }, [profile]);

  const onChange = (addr: string, ids: string[]) => setSelected((prev) => ({ ...prev, [addr]: ids }));

  const save = async () => {
    await ipc.setBuildFeatures(profile, { selected });
    markOutdated(profile);
    close();
  };

  return (
    <Modal title="Моды" subtitle={profile} onClose={close}>
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
