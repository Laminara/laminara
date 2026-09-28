import { useEffect } from "react";
import { useLauncher } from "@/store";
import { cn } from "@/lib/format";
import { HeroMedia } from "@/components/launcher/HeroMedia";
import { TitleBar } from "@/components/launcher/TitleBar";
import { Header } from "@/components/launcher/Header";
import { Hero } from "@/components/launcher/Hero";
import { BuildStrip } from "@/components/launcher/BuildStrip";
import { Login } from "@/components/launcher/Login";
import { SyncOverlay } from "@/components/launcher/SyncOverlay";
import { RunningBar } from "@/components/launcher/RunningBar";
import { GeneralSettingsModal } from "@/components/launcher/GeneralSettingsModal";
import { BuildSettingsModal } from "@/components/launcher/BuildSettingsModal";
import { FeaturesModal } from "@/components/launcher/FeaturesModal";
import { LibraryModal } from "@/components/launcher/LibraryModal";
import { NewsPanel } from "@/components/launcher/NewsPanel";
import { NewsModal } from "@/components/launcher/NewsModal";
import { CrashModal } from "@/components/launcher/CrashModal";
import { UpdateBanner } from "@/components/launcher/UpdateBanner";
import { UpdateOverlay } from "@/components/launcher/UpdateOverlay";
import { ErrorToast } from "@/components/launcher/ErrorToast";
import { ModalPresence } from "@/components/ui/Modal";

export default function App() {
  const init = useLauncher((state) => state.init);
  const unbindListeners = useLauncher((state) => state.unbindListeners);
  const phase = useLauncher((state) => state.phase);
  const modal = useLauncher((state) => state.modal);
  const crash = useLauncher((state) => state.crash);
  const refreshPlayers = useLauncher((state) => state.refreshPlayers);
  const refreshBuilds = useLauncher((state) => state.refreshBuilds);
  const refreshFace = useLauncher((state) => state.refreshFace);

  useEffect(() => {
    void init();
    return () => unbindListeners();
  }, [init, unbindListeners]);

  useEffect(() => {
    const id = setInterval(() => void refreshPlayers(), 30000);
    return () => clearInterval(id);
  }, [refreshPlayers]);

  useEffect(() => {
    const id = setInterval(() => void refreshBuilds(), 20000);
    const onFocus = () => {
      void refreshBuilds();
      void refreshFace();
    };
    window.addEventListener("focus", onFocus);
    return () => {
      clearInterval(id);
      window.removeEventListener("focus", onFocus);
    };
  }, [refreshBuilds, refreshFace]);

  const isHome = phase !== "connecting" && phase !== "login" && phase !== "updating";

  return (
    <div className="relative flex h-full w-full flex-col overflow-hidden">
      <HeroMedia />
      <TitleBar />

      {phase === "connecting" && (
        <div data-tauri-drag-region className="relative z-10 flex flex-1 items-center justify-center text-dim">
          Подключение…
        </div>
      )}

      {phase === "updating" && <UpdateOverlay />}

      {phase === "login" && (
        <div className="relative z-10 flex-1">
          <Login />
        </div>
      )}

      {isHome && <UpdateBanner />}

      {isHome && (
        <div inert={phase === "syncing"} className={cn("relative z-10 flex flex-1 flex-col", phase === "syncing" && "pointer-events-none")}>
          <Header />
          <main className="flex min-h-0 flex-1 justify-between gap-8 overflow-hidden">
            <div className="flex min-h-0 flex-1 flex-col justify-end overflow-hidden">
              <Hero />
            </div>
            <NewsPanel />
          </main>
          {phase === "running" && <RunningBar />}
          <BuildStrip />
        </div>
      )}

      {phase === "syncing" && <SyncOverlay />}

      <ModalPresence value={modal}>
        {(active) => (
          <>
            {active.kind === "general" && <GeneralSettingsModal />}
            {active.kind === "build" && <BuildSettingsModal profile={active.profile} />}
            {active.kind === "features" && <FeaturesModal profile={active.profile} />}
            {active.kind === "library" && <LibraryModal />}
            {active.kind === "news" && <NewsModal />}
          </>
        )}
      </ModalPresence>
      <ModalPresence value={crash}>{(shown) => <CrashModal crash={shown} />}</ModalPresence>
      <ErrorToast />
    </div>
  );
}
