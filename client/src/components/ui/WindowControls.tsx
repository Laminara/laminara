import { Minus, X } from "@phosphor-icons/react";

const isTauri = typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;

async function appWindow() {
  const { getCurrentWindow } = await import("@tauri-apps/api/window");
  return getCurrentWindow();
}

const button = "flex h-9 w-12 items-center justify-center text-dim transition-colors hover:bg-surface-2 hover:text-text";

export function WindowControls() {
  if (!isTauri) return null;
  return (
    <div className="flex items-center">
      <button className={button} onClick={() => appWindow().then((w) => w.minimize())} aria-label="Свернуть">
        <Minus size={15} weight="bold" />
      </button>
      <button className={`${button} hover:bg-danger hover:text-white`} onClick={() => appWindow().then((w) => w.close())} aria-label="Закрыть">
        <X size={16} weight="bold" />
      </button>
    </div>
  );
}
