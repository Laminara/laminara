import { CaretDown, Gear, SignOut } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { labels } from "@/config/branding";
import { formatCount } from "@/lib/format";
import { BrandMark } from "./BrandMark";
import { OnlineDot } from "@/components/ui/OnlineDot";

export function Header() {
  const players = useLauncher((state) => state.players);
  const account = useLauncher((state) => state.account);
  const menuOpen = useLauncher((state) => state.menuOpen);
  const toggleMenu = useLauncher((state) => state.toggleMenu);
  const closeMenu = useLauncher((state) => state.closeMenu);
  const openModal = useLauncher((state) => state.openModal);
  const logout = useLauncher((state) => state.logout);
  const initial = account?.name?.[0]?.toUpperCase() ?? "?";

  return (
    <header data-tauri-drag-region className="relative z-30 flex items-center justify-between px-9 py-6">
      <BrandMark />
      <div className="flex items-center gap-3">
        {players && players.total.max > 0 && (
          <div className="flex items-center gap-2.5 rounded-full border border-border bg-panel px-4 py-2.5 backdrop-blur-md">
            <OnlineDot />
            <span className="text-sm font-semibold tabular-nums">
              {formatCount(players.total.online)}
              <span className="text-dim"> / {formatCount(players.total.max)}</span>
            </span>
            <span className="text-[11px] uppercase tracking-wider text-dim">{labels.players}</span>
          </div>
        )}

        <button
          onClick={() => openModal({ kind: "general" })}
          className="flex h-10 w-10 items-center justify-center rounded-full border border-border bg-surface text-dim transition-colors hover:bg-surface-2 hover:text-text"
          aria-label="Настройки"
        >
          <Gear size={18} />
        </button>

        <div className="relative">
          <button
            onClick={toggleMenu}
            className="flex items-center gap-2.5 rounded-full border border-border bg-surface py-2 pl-2 pr-3.5 transition-colors hover:bg-surface-2"
          >
            <span className="flex h-8 w-8 items-center justify-center rounded-full bg-primary text-sm font-bold text-primary-ink">{initial}</span>
            <span className="text-sm font-semibold">{account?.name ?? "Гость"}</span>
            <CaretDown size={14} className={`text-dim transition-transform ${menuOpen ? "rotate-180" : ""}`} />
          </button>

          {menuOpen && (
            <>
              <div className="fixed inset-0 z-30" onClick={closeMenu} />
              <div
                className="absolute right-0 top-full z-40 mt-2 w-44 overflow-hidden rounded-lg border border-border bg-bg-tint py-1 shadow-panel"
              >
                <button
                  onClick={() => void logout()}
                  className="flex w-full items-center gap-2.5 px-4 py-2.5 text-sm text-dim transition-colors hover:bg-surface-2 hover:text-text"
                >
                  <SignOut size={16} /> Выход
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </header>
  );
}
