import { useLauncher } from "@/store";
import { brand } from "@/config/branding";
import { Button } from "@/components/ui/Button";
import { BrandMark } from "./BrandMark";
import { CredentialsForm } from "./CredentialsForm";

export function Login() {
  const options = useLauncher((state) => state.accountOptions);
  const openModal = useLauncher((state) => state.openModal);
  return (
    <div data-tauri-drag-region className="relative z-10 flex h-full items-center justify-center px-6">
      <div className="w-[380px] rounded-lg border border-border bg-surface p-8 shadow-panel"
        style={{ backdropFilter: "blur(24px)" }}>
        <div className="mb-7 flex justify-center"><BrandMark /></div>
        <h1 className="mb-6 text-center text-xl font-bold">Вход в лаунчер</h1>
        {brand().tagline && <p className="-mt-5 mb-6 text-center text-sm text-dim">{brand().tagline}</p>}
        <CredentialsForm />
        {options.multiAccount && options.accounts.length > 0 && (
          <Button variant="ghost" className="mt-3 w-full" onClick={() => openModal({ kind: "accounts" })}>
            Сохранённые аккаунты
          </Button>
        )}
      </div>
    </div>
  );
}
