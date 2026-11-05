import { useEffect, useState } from "react";
import { Check, Plus, Trash } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { CredentialsForm } from "./CredentialsForm";

export function AccountsModal() {
  const options = useLauncher((state) => state.accountOptions);
  const current = useLauncher((state) => state.account);
  const busy = useLauncher((state) => state.accountBusy);
  const error = useLauncher((state) => state.error);
  const refresh = useLauncher((state) => state.refreshAccounts);
  const choose = useLauncher((state) => state.switchAccount);
  const remove = useLauncher((state) => state.removeAccount);
  const close = useLauncher((state) => state.closeModal);
  const dismissError = useLauncher((state) => state.dismissError);
  const [adding, setAdding] = useState(false);
  const [username, setUsername] = useState("");
  const [confirm, setConfirm] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const load = async () => {
    setLoading(true);
    setLoadError(null);
    try { await refresh(); } catch (error) { setLoadError(String(error)); }
    finally { setLoading(false); }
  };
  useEffect(() => { void load(); }, [refresh]);
  const blocked = busy || loading || !!loadError;
  const back = () => { if (!busy) { setAdding(false); dismissError(); } };

  return (
    <Modal title={adding ? "Добавить аккаунт" : "Аккаунты"} compact
      subtitle={adding ? "Текущий вход сохранится" : "Сохранённые входы на этом компьютере"}
      onClose={() => { if (!busy) { dismissError(); close(); } }}
      footer={adding
        ? <Button variant="ghost" disabled={busy} onClick={back}>Назад</Button>
        : <Button disabled={blocked || !options.multiAccount || options.accounts.length >= options.maxAccounts}
            icon={<Plus size={16} />} onClick={() => { dismissError(); setUsername(""); setAdding(true); }}>Добавить аккаунт</Button>}>
      {adding ? <CredentialsForm initialUsername={username} /> : (
        <div className="flex flex-col gap-4">
          {loading && <p className="text-sm text-dim" role="status">Проверяем настройки сервера…</p>}
          {loadError && (
            <div className="flex flex-col items-start gap-3">
              <p role="alert" className="text-sm text-danger">{loadError}</p>
              <Button variant="ghost" onClick={() => void load()}>Повторить</Button>
            </div>
          )}
          {!loading && !loadError && (
            <p className="text-sm text-dim">
              {options.multiAccount
                ? `Сохранено ${options.accounts.length} из ${options.maxAccounts}.`
                : "Проект отключил несколько аккаунтов. Сохранённые входы можно удалить."}
            </p>
          )}
          {options.accounts.map((account) => (
            <div key={account.uuid} className="flex min-w-0 flex-col gap-3 rounded-lg border border-border bg-surface p-4">
              <div className="flex min-w-0 items-center gap-3">
                <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-surface-2 font-semibold" aria-hidden="true">{account.name[0]?.toUpperCase()}</span>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-semibold" title={account.name}>{account.name}</p>
                  <p className="text-xs text-dim">{current?.uuid === account.uuid ? "Текущий аккаунт" : "Вход сохранён"}</p>
                </div>
                {current?.uuid === account.uuid && <Check size={18} className="shrink-0 text-primary" aria-label="Выбран" />}
              </div>
              <div data-card-actions className="flex flex-wrap items-center gap-2">
                <Button size="sm" variant="ghost" disabled={blocked || !options.multiAccount || current?.uuid === account.uuid}
                  onClick={() => void choose(account.uuid)}>Переключиться</Button>
                {options.multiAccount && <Button size="sm" variant="ghost" disabled={blocked}
                  onClick={() => { dismissError(); setUsername(account.name); setAdding(true); }}>Войти заново</Button>}
                <Button size="sm" variant="ghost" disabled={busy}
                  aria-label={`Удалить аккаунт ${account.name}`} onClick={() => setConfirm(account.uuid)}
                  icon={<Trash size={16} />}>Удалить</Button>
              </div>
              {confirm === account.uuid && (
                <div className="flex flex-col items-start gap-3 border-t border-border pt-3">
                  <p className="text-sm">Удалить сохранённый вход? Для возвращения понадобится пароль.</p>
                  <div className="flex gap-2">
                    <Button size="sm" disabled={busy} onClick={() => void remove(account.uuid)}>Удалить вход</Button>
                    <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirm(null)}>Отмена</Button>
                  </div>
                </div>
              )}
            </div>
          ))}
          {!loading && !loadError && options.accounts.length === 0 && <p className="text-sm">Сохранённых аккаунтов пока нет.</p>}
          {error && <p role="alert" className="text-sm text-danger">{error.message}</p>}
          {!loading && options.multiAccount && options.accounts.length >= options.maxAccounts && (
            <p className="text-sm text-dim">Чтобы добавить новый аккаунт, удалите один из сохранённых.</p>
          )}
        </div>
      )}
    </Modal>
  );
}
