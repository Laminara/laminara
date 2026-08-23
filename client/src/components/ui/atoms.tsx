import type { InstallState } from "@/lib/types";

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
