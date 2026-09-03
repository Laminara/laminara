import type { ReactNode } from "react";
import { useEffect } from "react";
import { X } from "@phosphor-icons/react";

import { cn } from "@/lib/format";

interface ModalProps {
  title: string;
  subtitle?: string;
  compact?: boolean;
  onClose: () => void;
  children: ReactNode;
}

export function Modal({ title, subtitle, compact = false, onClose, children }: ModalProps) {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="absolute inset-0 z-50 flex items-center justify-center p-8"
      onClick={onClose}
      style={{ background: "rgba(0,0,0,0.55)", backdropFilter: "blur(6px)" }}
    >
      <div
        className={cn(
          "flex flex-col overflow-hidden rounded-lg border border-border bg-bg-tint shadow-panel",
          compact ? "max-h-[85%] w-[560px]" : "h-[88%] w-[84%]",
        )}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-start justify-between border-b border-border px-6 py-5">
          <div>
            <h2 className="text-lg font-bold">{title}</h2>
            {subtitle && <p className="mt-0.5 text-sm text-dim">{subtitle}</p>}
          </div>
          <button onClick={onClose} className="-mr-1.5 rounded-md p-1.5 text-dim transition-colors hover:bg-surface-2 hover:text-text" aria-label="Закрыть">
            <X size={18} />
          </button>
        </div>
        <div className={cn("@container overflow-y-auto px-7 py-6", compact ? "" : "flex-1")}>{children}</div>
      </div>
    </div>
  );
}
