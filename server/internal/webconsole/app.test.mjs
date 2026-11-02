import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

const script = readFileSync(new URL("assets/app.js", import.meta.url), "utf8");

function consolePage() {
  let output = "";
  let socket;
  const context = {
    document: { getElementById: () => ({ textContent: "" }) },
    location: { protocol: "http:", hostname: "localhost", host: "localhost" },
    window: { addEventListener() {} },
    TextDecoder,
    TextEncoder,
    Terminal: class {
      cols = 80;
      rows = 24;
      loadAddon() {}
      open() {}
      focus() {}
      onData() {}
      write(text) { output += text; }
    },
    FitAddon: { FitAddon: class { fit() {} } },
    WebSocket: class { constructor() { socket = this; } },
  };
  vm.runInNewContext(script, context);
  return { send: (data) => socket.onmessage({ data }), output: () => output };
}

test("terminal preserves Cyrillic and borders split across WebSocket messages", () => {
  const text = "╭─ Приёмка сборки ─╮\r\n│ Готово ✓ │";
  const bytes = new TextEncoder().encode(text);
  for (let split = 1; split < bytes.length; split++) {
    const page = consolePage();
    page.send(bytes.slice(0, split).buffer);
    page.send(bytes.slice(split).buffer);
    assert.equal(page.output(), text, `split at byte ${split}`);
  }
});
