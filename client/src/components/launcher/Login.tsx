import { useState } from "react";
import type { FormEvent } from "react";
import { ArrowRight } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { brand } from "@/config/branding";
import { Button } from "@/components/ui/Button";
import { inputClass } from "@/components/ui/atoms";
import { BrandMark } from "./BrandMark";

export function Login() {
  const login = useLauncher((state) => state.login);
  const error = useLauncher((state) => state.error);
  const twoFactor = useLauncher((state) => state.twoFactor);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    await login(username, password, code);
    setBusy(false);
  };

  return (
    <div data-tauri-drag-region className="relative z-10 flex h-full items-center justify-center px-6">
      <form
        onSubmit={submit}
        className="w-[380px] rounded-lg border border-border bg-surface p-8 shadow-panel"
        style={{ backdropFilter: "blur(24px)" }}
      >
        <div className="mb-7 flex justify-center">
          <BrandMark />
        </div>
        <h1 className="mb-6 text-center text-xl font-bold">Вход в лаунчер</h1>
        {brand().tagline && (
          <p className="-mt-5 mb-6 text-center text-sm text-dim">{brand().tagline}</p>
        )}

        <div className="flex flex-col gap-3">
          <input
            className={inputClass}
            placeholder="Имя пользователя"
            autoFocus
            value={username}
            onChange={(event) => setUsername(event.target.value)}
          />
          <input
            className={inputClass}
            type="password"
            placeholder="Пароль"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
          {twoFactor && (
            <input
              className={inputClass}
              placeholder="Код из приложения"
              inputMode="numeric"
              maxLength={6}
              autoFocus
              value={code}
              onChange={(event) => setCode(event.target.value.replace(/\D/g, ""))}
            />
          )}
        </div>

        {error && <p className="mt-3 text-sm text-danger">{error}</p>}

        <Button
          type="submit"
          disabled={busy || !username || (twoFactor && code.length !== 6)}
          icon={<ArrowRight size={16} weight="bold" />}
          className="mt-6 w-full py-3.5"
        >
          {busy ? "Входим…" : "Войти"}
        </Button>
      </form>
    </div>
  );
}
