import type { ReactNode } from "react";
import { useEffect, useRef } from "react";
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
  const frame = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const focusable = () =>
      Array.from(
        frame.current?.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
        ) ?? [],
      ).filter((node) => !node.hasAttribute("disabled"));

    focusable()[0]?.focus();

    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation();
        onClose();
        return;
      }
      if (event.key !== "Tab") return;
      const nodes = focusable();
      if (nodes.length === 0) return;
      const first = nodes[0];
      const last = nodes[nodes.length - 1];
      const active = document.activeElement;
      if (event.shiftKey && (active === first || !frame.current?.contains(active))) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && active === last) {
        event.preventDefault();
        first.focus();
      }
    };

    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      opener?.focus?.();
    };
  }, [onClose]);

  return (
    <div
      className="absolute inset-0 z-50 flex items-center justify-center p-8"
      onClick={onClose}
      style={{ background: "rgba(0,0,0,0.55)", backdropFilter: "blur(6px)" }}
    >
      <div
        ref={frame}
        role="dialog"
        aria-modal="true"
        aria-label={title}
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
