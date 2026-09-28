import { useEffect, useState } from "react";
import { CopySimple, Minus, Square, X } from "@phosphor-icons/react";

import { isTauri } from "@/lib/ipc";

async function appWindow() {
  const { getCurrentWindow } = await import("@tauri-apps/api/window");
  return getCurrentWindow();
}

const button = "flex h-9 w-12 items-center justify-center text-dim transition-colors hover:bg-surface-2 hover:text-text";

function useMaximized(enabled: boolean): boolean {
  const [maximized, setMaximized] = useState(false);
  useEffect(() => {
    if (!enabled || !isTauri) return;
    let off: (() => void) | null = null;
    let cancelled = false;
    void appWindow().then(async (win) => {
      const sync = () => void win.isMaximized().then(setMaximized);
      sync();
      const unlisten = await win.onResized(sync);
      if (cancelled) unlisten();
      else off = unlisten;
    });
    return () => {
      cancelled = true;
      off?.();
    };
  }, [enabled]);
  return maximized;
}

export function WindowControls({ maximizable = false }: { maximizable?: boolean }) {
  const maximized = useMaximized(maximizable);
  if (!isTauri) return null;
  return (
    <div className="flex items-center">
      <button className={button} onClick={() => appWindow().then((w) => w.minimize())} aria-label="Свернуть">
        <Minus size={15} weight="bold" />
      </button>
      {maximizable && (
        <button
          className={button}
          onClick={() => appWindow().then((w) => w.toggleMaximize())}
          aria-label={maximized ? "Вернуть размер" : "Развернуть"}
        >
          {maximized ? <CopySimple size={14} weight="bold" className="-scale-x-100" /> : <Square size={13} weight="bold" />}
        </button>
      )}
      <button className={`${button} hover:bg-danger hover:text-white`} onClick={() => appWindow().then((w) => w.close())} aria-label="Закрыть">
        <X size={16} weight="bold" />
      </button>
    </div>
  );
}
