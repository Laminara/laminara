import { useDeferredValue, useEffect, useMemo, useRef } from "react";
import { useConsole } from "@/consoleStore";
import { ipc } from "@/lib/ipc";
import { visibleLines } from "@/lib/gameLog";
import { ConsoleTitleBar } from "./ConsoleTitleBar";
import { ConsoleToolbar } from "./ConsoleToolbar";
import { LogView } from "./LogView";
import { ConsoleFooter } from "./ConsoleFooter";

export function ConsoleApp() {
  const connect = useConsole((state) => state.connect);
  const chunks = useConsole((state) => state.chunks);
  const filter = useConsole((state) => state.filter);
  const query = useConsole((state) => state.query);
  const needle = useDeferredValue(query.trim().toLowerCase());
  const search = useRef<HTMLInputElement>(null);

  const visible = useMemo(() => chunks.map((chunk) => visibleLines(chunk, filter, needle)), [chunks, filter, needle]);
  const shown = useMemo(() => visible.reduce((sum, lines) => sum + lines.length, 0), [visible]);

  useEffect(() => {
    let off: (() => void) | null = null;
    let cancelled = false;
    void connect().then((unlisten) => {
      if (cancelled) {
        unlisten();
        return;
      }
      off = unlisten;
      requestAnimationFrame(() => void ipc.revealWindow());
    });
    return () => {
      cancelled = true;
      off?.();
    };
  }, [connect]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.code === "KeyF") {
        event.preventDefault();
        search.current?.focus();
        search.current?.select();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="flex h-full flex-col bg-bg">
      <ConsoleTitleBar />
      <ConsoleToolbar searchRef={search} visible={visible} shown={shown} />
      <LogView visible={visible} shown={shown} needle={needle} />
      <ConsoleFooter shown={shown} needle={needle} />
    </div>
  );
}
