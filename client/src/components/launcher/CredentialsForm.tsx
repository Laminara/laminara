import { useState } from "react";
import type { FormEvent } from "react";
import { ArrowRight } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { Button } from "@/components/ui/Button";
import { inputClass } from "@/components/ui/atoms";

export function CredentialsForm({ initialUsername = "" }: { initialUsername?: string }) {
  const login = useLauncher((state) => state.login);
  const dismissError = useLauncher((state) => state.dismissError);
  const error = useLauncher((state) => state.error);
  const twoFactor = useLauncher((state) => state.twoFactor);
  const busy = useLauncher((state) => state.accountBusy);
  const [username, setUsername] = useState(initialUsername);
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    await login(username, password, code);
  };

  return (
    <form onSubmit={submit}>
      <div className="flex flex-col gap-3">
        <label className="flex flex-col gap-2 text-sm">
          Имя пользователя
          <input className={inputClass} autoComplete="username" autoFocus
            value={username} onChange={(event) => { setUsername(event.target.value); if (twoFactor) dismissError(); }} disabled={busy} />
        </label>
        <label className="flex flex-col gap-2 text-sm">
          Пароль
          <input className={inputClass} type="password" autoComplete="current-password"
            value={password} onChange={(event) => setPassword(event.target.value)} disabled={busy} />
        </label>
        {twoFactor && (
          <label className="flex flex-col gap-2 text-sm">
            Код из приложения
            <input className={inputClass} inputMode="numeric" autoComplete="one-time-code" maxLength={6}
              autoFocus value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ""))}
              disabled={busy} />
          </label>
        )}
      </div>
      {error && <p role="alert" className="mt-3 text-sm text-danger">{error.message}</p>}
      <Button type="submit" disabled={busy || !username || !password || (twoFactor && code.length !== 6)}
        icon={<ArrowRight size={16} weight="bold" />} className="mt-6 w-full">
        {busy ? "Входим…" : "Войти"}
      </Button>
    </form>
  );
}
