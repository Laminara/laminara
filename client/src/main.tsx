import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@fontsource-variable/inter";
import "@/styles/app.css";
import App from "@/App";
import { applyBranding } from "@/config/branding";
import { ipc } from "@/lib/ipc";

async function start() {
  applyBranding(await ipc.branding().catch(() => null));
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}

void start();
