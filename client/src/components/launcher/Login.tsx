import { useState } from "react";
import type { FormEvent } from "react";
import { ArrowRight } from "@phosphor-icons/react";
import { useLauncher } from "@/store";
import { brand } from "@/config/branding";
import { Button } from "@/components/ui/Button";
import { BrandMark } from "./BrandMark";

const fieldClass =
  "w-full rounded-md border border-border bg-surface-2 px-4 py-3 text-sm text-text outline-none transition-colors placeholder:text-mute focus:border-border-strong";

export function Login() {
  const login = useLauncher((state) => state.login);
  const error = useLauncher((state) => state.error);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    await login(username, password);
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
        <h1 className="text-center text-xl font-bold">Вход в лаунчер</h1>
        <p className="mb-6 mt-1 text-center text-sm text-dim">{brand().tagline}</p>

        <div className="flex flex-col gap-3">
          <input
            className={fieldClass}
            placeholder="Имя пользователя"
            autoFocus
            value={username}
            onChange={(event) => setUsername(event.target.value)}
          />
          <input
            className={fieldClass}
            type="password"
            placeholder="Пароль"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </div>

        {error && <p className="mt-3 text-sm text-danger">{error}</p>}

        <Button type="submit" disabled={busy || !username} icon={<ArrowRight size={16} weight="bold" />} className="mt-6 w-full py-3.5">
          {busy ? "Входим…" : "Войти"}
        </Button>
      </form>
    </div>
  );
}
