import type { ReactNode } from "react";
import { useEffect } from "react";
import { X } from "@phosphor-icons/react";

interface ModalProps {
  title: string;
  subtitle?: string;
  onClose: () => void;
  children: ReactNode;
}

export function Modal({ title, subtitle, onClose, children }: ModalProps) {
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
        className="flex h-[78%] w-[75%] flex-col overflow-hidden rounded-lg border border-border bg-bg-tint shadow-panel"
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
        <div className="@container flex-1 overflow-y-auto px-7 py-6">{children}</div>
      </div>
    </div>
  );
}
