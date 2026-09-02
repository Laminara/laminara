const status = document.getElementById("status");
const warning = document.getElementById("warning");

if (location.protocol !== "https:" && location.hostname !== "localhost") {
  warning.textContent = "соединение не шифруется";
}

const terminal = new Terminal({
  fontFamily: 'ui-monospace, "JetBrains Mono", Menlo, Consolas, monospace',
  fontSize: 14,
  cursorBlink: true,
  theme: { background: "#1c1b19", foreground: "#e8e4dd" },
});
const fit = new FitAddon.FitAddon();
terminal.loadAddon(fit);
terminal.open(document.getElementById("screen"));
fit.fit();

const socket = new WebSocket(
  (location.protocol === "https:" ? "wss://" : "ws://") + location.host + "/console/socket",
);
socket.binaryType = "arraybuffer";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function tellSize() {
  if (socket.readyState === WebSocket.OPEN) {
    socket.send(terminal.cols + "x" + terminal.rows);
  }
}

socket.onopen = () => {
  status.textContent = "на связи";
  tellSize();
  terminal.focus();
};

socket.onmessage = (event) => {
  terminal.write(decoder.decode(event.data));
};

socket.onclose = () => {
  status.textContent = "сеанс закрыт — обновите страницу, чтобы начать заново";
};

socket.onerror = () => {
  status.textContent = "связь оборвалась";
};

terminal.onData((data) => {
  if (socket.readyState === WebSocket.OPEN) {
    socket.send(encoder.encode(data));
  }
});

window.addEventListener("resize", () => {
  fit.fit();
  tellSize();
});
