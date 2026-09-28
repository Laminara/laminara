import { useEffect, useState } from "react";
import { FolderOpen, PaperPlaneTilt } from "@phosphor-icons/react";
import type { GeneralSettings } from "@/lib/types";
import { ipc } from "@/lib/ipc";
import { useLauncher } from "@/store";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Switch } from "@/components/ui/Switch";

export function GeneralSettingsModal() {
  const close = useLauncher((state) => state.closeModal);
  const logReport = useLauncher((state) => state.logReport);
  const sendLauncherLog = useLauncher((state) => state.sendLauncherLog);
  const openLauncherLogs = useLauncher((state) => state.openLauncherLogs);
  const [settings, setSettings] = useState<GeneralSettings | null>(null);
  const [installDir, setInstallDir] = useState("");
  const [gameConsole, setGameConsole] = useState(false);
  const [cleaned, setCleaned] = useState<number | null>(null);
  const [trouble, setTrouble] = useState<string | null>(null);

  useEffect(() => {
    ipc
      .generalSettings()
      .then((data) => {
        setSettings(data);
        setInstallDir(data.installDir);
        setGameConsole(data.gameConsole);
        setTrouble(null);
      })
      .catch((err: unknown) => setTrouble(String(err)));
  }, []);

  const chooseFolder = async () => {
    const picked = await ipc.pickFolder(installDir);
    if (picked) setInstallDir(picked);
  };

  const save = async () => {
    try {
      if (settings && installDir && installDir !== settings.installDir) {
        await ipc.setInstallDir(installDir);
      }
      if (settings && gameConsole !== settings.gameConsole) {
        await ipc.setGameConsole(gameConsole);
      }
      close();
    } catch (err) {
      setTrouble(String(err));
    }
  };

  return (
    <Modal
      title="Настройки"
      subtitle="Общие параметры лаунчера"
      compact
      onClose={close}
      footer={
        settings && (
          <>
            <span className="mr-auto text-xs text-mute">Laminara v{settings.version}</span>
            <Button onClick={() => void save()} className="px-6">
              Сохранить
            </Button>
          </>
        )
      }
    >
      {trouble && <div className="mb-4 rounded-lg bg-danger/15 px-3 py-2 text-sm text-danger">{trouble}</div>}
      {!settings && !trouble && <div className="text-sm text-dim">Загружаю настройки…</div>}
      {settings && (
        <div className="flex flex-col gap-6">
          <div>
            <div className="mb-1.5 text-sm text-dim">Папка установки</div>
            <div className="flex gap-2">
              <div className="flex h-9 min-w-0 flex-1 items-center rounded-md border border-border bg-surface-2 px-3 text-sm">
                <span className="truncate">{installDir}</span>
              </div>
              <Button variant="secondary" size="sm" icon={<FolderOpen size={16} />} onClick={() => void chooseFolder()}>
                Выбрать
              </Button>
            </div>
          </div>

          <div>
            <div className="mb-1.5 text-sm text-dim">Кэш загрузок</div>
            <div className="flex items-center gap-3">
              <Button variant="secondary" size="sm" onClick={() => void ipc.collectGarbage().then(setCleaned)}>
                Очистить неиспользуемое
              </Button>
              {cleaned !== null && <span className="text-xs text-mute">Удалено объектов: {cleaned}</span>}
            </div>
          </div>

          <div className="flex items-center justify-between gap-6 border-t border-border pt-4">
            <div>
              <p id="game-console-label" className="text-sm font-medium">
                Консоль игры
              </p>
              <p id="game-console-hint" className="mt-0.5 text-xs text-dim">
                Открывать журнал игры в отдельном окне при каждом запуске. Пригодится, если игра не запускается или вы
                собираете сборку. Кнопка «Консоль» на плашке запущенной игры работает и без этого.
              </p>
            </div>
            <Switch checked={gameConsole} onChange={setGameConsole} labelledBy="game-console-label" describedBy="game-console-hint" />
          </div>

          <div className="border-t border-border pt-4">
            <p className="text-sm font-medium">Журнал лаунчера</p>
            <p className="mt-0.5 text-xs text-dim">
              Если лаунчер не качает сборку или не входит, отправьте журнал разработчикам проекта: так они увидят,
              что случилось.
            </p>
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <Button variant="secondary" size="sm" icon={<FolderOpen size={16} />} onClick={() => void openLauncherLogs()}>
                Открыть папку
              </Button>
              <Button
                variant="secondary"
                size="sm"
                icon={<PaperPlaneTilt size={16} />}
                onClick={() => void sendLauncherLog()}
                disabled={logReport.state === "sending" || logReport.state === "sent"}
              >
                {logReport.state === "sending" ? "Отправляю" : "Отправить разработчикам"}
              </Button>
            </div>
            {logReport.state === "sent" && <p className="mt-2 text-xs text-dim">{logReport.message}</p>}
            {logReport.state === "failed" && <p className="mt-2 text-xs text-danger">Журнал не ушёл: {logReport.message}</p>}
          </div>

        </div>
      )}
    </Modal>
  );
}
