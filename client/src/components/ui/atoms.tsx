import type { InstallState } from "@/lib/types";

export const inputClass =
  "w-full rounded-md border border-border bg-surface-2 px-4 py-3 text-sm text-text outline-none transition-colors placeholder:text-mute focus:border-border-strong";

export function Dot() {
  return <span className="h-1 w-1 rounded-full bg-mute" />;
}

export function Divider() {
  return <span className="h-4 w-px bg-border" />;
}

const dotColors: Record<InstallState, string> = {
  installed: "bg-online",
  outdated: "bg-primary",
  missing: "bg-mute",
  syncing: "bg-primary",
};

export function StatusDot({ state }: { state: InstallState }) {
  return <span className={`h-2 w-2 rounded-full ${dotColors[state]}`} />;
}
