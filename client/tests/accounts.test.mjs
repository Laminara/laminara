import assert from "node:assert/strict";
import { after, beforeEach, test } from "node:test";
import { createServer } from "vite";

const server = await createServer({ server: { middlewareMode: true, watch: null }, appType: "custom" });
const { useLauncher } = await server.ssrLoadModule("/src/store.ts");
const { ipc } = await server.ssrLoadModule("/src/lib/ipc.ts");
const { mockBuilds } = await server.ssrLoadModule("/src/lib/mock.ts");
const alice = { uuid: "alice", name: "Alice", endpointId: "main" };
const bob = { uuid: "bob", name: "Bob", endpointId: "main" };
globalThis.localStorage = { getItem: () => null, setItem: () => {} };

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((ok, fail) => { resolve = ok; reject = fail; });
  return { promise, resolve, reject };
}

beforeEach(() => {
  useLauncher.setState(useLauncher.getInitialState(), true);
  useLauncher.setState({ phase: "home", account: alice, builds: mockBuilds, selected: mockBuilds[0].name });
  Object.assign(ipc, {
    accountOptions: async () => ({ multiAccount: true, maxAccounts: 3, accounts: [alice, bob] }),
    restoreSession: async () => ({ signedIn: true, username: bob.name, uuid: bob.uuid }),
    switchAccount: async () => ({ signedIn: true, username: bob.name, uuid: bob.uuid }),
    listBuilds: async () => [],
    playerCounts: async () => null,
    playerFace: async () => null,
    news: async () => [],
  });
});
after(async () => { await server.close(); delete globalThis.localStorage; });

test("disabled account management cannot be opened with saved accounts", () => {
  useLauncher.setState({ accountOptions: { multiAccount: false, maxAccounts: 1, accounts: [alice, bob] } });
  useLauncher.getState().openModal({ kind: "accounts" });
  assert.equal(useLauncher.getState().modal, null);
});

test("disabling account management closes its window without deleting accounts", async () => {
  useLauncher.setState({ accountOptions: { multiAccount: true, maxAccounts: 3, accounts: [alice, bob] } });
  useLauncher.getState().openModal({ kind: "accounts" });
  ipc.accountOptions = async () => ({ multiAccount: false, maxAccounts: 1, accounts: [alice, bob] });
  await useLauncher.getState().refreshAccounts();
  assert.equal(useLauncher.getState().modal, null);
  assert.deepEqual(useLauncher.getState().accountOptions.accounts, [alice, bob]);
  assert.equal(useLauncher.getState().account.uuid, "alice");
});

test("switch clears the previous player's private view and reloads allowed builds", async () => {
  useLauncher.setState({ face: "alice-face", crash: { session: 1 }, staleBuilds: [mockBuilds[0].name] });
  await useLauncher.getState().switchAccount("bob");
  const state = useLauncher.getState();
  assert.equal(state.account.uuid, "bob");
  assert.deepEqual(state.builds, []);
  assert.equal(state.selected, null);
  assert.equal(state.face, null);
  assert.equal(state.crash, null);
  assert.deepEqual(state.staleBuilds, []);
  assert.equal(state.accountBusy, false);
});

test("a failed switch keeps the current account and its view", async () => {
  ipc.switchAccount = async () => { throw "Сессия истекла"; };
  await useLauncher.getState().switchAccount("bob");
  assert.equal(useLauncher.getState().account.uuid, "alice");
  assert.deepEqual(useLauncher.getState().builds, mockBuilds);
  assert.match(useLauncher.getState().error.message, /Сессия истекла/);
  assert.equal(useLauncher.getState().accountBusy, false);
});

test("an old response cannot restore the previous player's build list", async () => {
  const pending = deferred();
  ipc.listBuilds = () => pending.promise;
  const refresh = useLauncher.getState().refreshBuilds();
  ipc.listBuilds = async () => [];
  await useLauncher.getState().switchAccount("bob");
  pending.resolve(mockBuilds);
  await refresh;
  assert.deepEqual(useLauncher.getState().builds, []);
  assert.equal(useLauncher.getState().account.uuid, "bob");
});

test("switching is serialized and starting a game is blocked during the change", async () => {
  const pending = deferred();
  let switches = 0;
  let launches = 0;
  ipc.switchAccount = () => { switches++; return pending.promise; };
  ipc.launch = async () => { launches++; };
  const change = useLauncher.getState().switchAccount("bob");
  await useLauncher.getState().switchAccount("alice");
  await useLauncher.getState().play();
  assert.equal(switches, 1);
  assert.equal(launches, 0);
  pending.resolve({ signedIn: true });
  await change;
});

test("removing an inactive account preserves the current session", async () => {
  ipc.removeAccount = async () => ({ signedIn: true, username: alice.name, uuid: alice.uuid });
  await useLauncher.getState().removeAccount("bob");
  assert.equal(useLauncher.getState().account.uuid, "alice");
  assert.equal(useLauncher.getState().phase, "home");
});

test("removing the current account returns to login and clears its builds", async () => {
  ipc.removeAccount = async () => ({ signedIn: false });
  await useLauncher.getState().removeAccount("alice");
  assert.equal(useLauncher.getState().account, null);
  assert.equal(useLauncher.getState().phase, "login");
  assert.deepEqual(useLauncher.getState().builds, []);
});

test("failed two-factor login preserves the existing account", async () => {
  ipc.login = async () => { throw { kind: "secondFactor", message: "Введите код" }; };
  await useLauncher.getState().login("bob", "password");
  assert.equal(useLauncher.getState().account.uuid, "alice");
  assert.equal(useLauncher.getState().twoFactor, true);
  assert.equal(useLauncher.getState().accountBusy, false);
});
