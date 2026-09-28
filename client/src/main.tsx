import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@fontsource-variable/inter";
import "@fontsource-variable/jetbrains-mono";
import "@/styles/app.css";
import App from "@/App";
import { ConsoleApp } from "@/components/console/ConsoleApp";
import { applyBranding } from "@/config/branding";
import { ipc } from "@/lib/ipc";

async function start() {
  applyBranding(await ipc.branding().catch(() => null));
  const view = await ipc.view();
  createRoot(document.getElementById("root")!).render(<StrictMode>{view === "console" ? <ConsoleApp /> : <App />}</StrictMode>);
}

void start();
