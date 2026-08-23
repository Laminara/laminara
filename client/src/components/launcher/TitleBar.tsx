import { WindowControls } from "@/components/ui/WindowControls";

export function TitleBar() {
  return (
    <div data-tauri-drag-region className="relative z-40 flex h-9 shrink-0 items-center justify-end">
      <WindowControls />
    </div>
  );
}
