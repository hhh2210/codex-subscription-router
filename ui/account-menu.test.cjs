"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

function loadHelpers(overrides = {}) {
  const source = fs.readFileSync(path.join(__dirname, "account-menu.js"), "utf8");
  const context = { ...overrides };
  vm.runInNewContext(
    `(() => {${source}\n;globalThis.__codexMuxTest = {
      codexMuxSubscribeToEvents,
      codexMuxConnectedAccounts,
      codexMuxMenuAccounts,
      codexMuxSelectRemoval,
      codexMuxPluginAccounts,
      codexMuxNextPluginAccountId,
      codexMuxInvalidatePluginQueries,
      CodexMuxAccountMenu,
      CodexMuxProfileAvatarStack,
      loginActive: () => codexMuxLoginActive,
    }; })();`,
    context,
  );
  return { context, helpers: context.__codexMuxTest };
}

test("manage mode exposes disconnected and disabled subscriptions", () => {
  const { helpers } = loadHelpers();
  const accounts = [
    { id: "primary", controller: true, connected: true, enabled: true },
    { id: "connected", connected: true, enabled: true },
    { id: "disconnected", connected: false, enabled: true },
    { id: "disabled", connected: true, enabled: false },
  ];

  assert.deepEqual(
    Array.from(helpers.codexMuxMenuAccounts(accounts, false), (account) => account.id),
    ["primary", "connected"],
  );
  assert.deepEqual(
    Array.from(helpers.codexMuxMenuAccounts(accounts, true), (account) => account.id),
    ["primary", "connected", "disconnected", "disabled"],
  );
});

// Evaluate the actual component callbacks with a small hook runtime. Requests,
// events and state updates remain observable without an installed app bundle.
function componentHarness(initialAccounts) {
  let accounts = initialAccounts;
  const states = [], effects = [], sources = [], requests = [];
  let stateIndex = 0, effectIndex = 0;
  const scheduled = [];
  const jsx = (type, props, key) => ({ type, props, key });
  class EventSource {
    constructor() { this.closed = false; sources.push(this); }
    close() { this.closed = true; }
  }
  const loaded = loadHelpers({
    EventSource,
    e7: { jsx, jsxs: jsx, Fragment: "fragment" },
    _H: "menu-item", S2: "info", CH: { Separator: "separator" },
    Lo: () => null, Q: {},
    kXc: {
      useState(initial) {
        const index = stateIndex++;
        if (!(index in states)) states[index] = initial;
        return [states[index], (next) => { states[index] = typeof next === "function" ? next(states[index]) : next; }];
      },
      useCallback: (fn) => fn,
      useEffect(fn, deps) {
        const index = effectIndex++;
        const old = effects[index];
        if (!old || deps.some((dep, n) => dep !== old.deps[n])) {
          scheduled.push(() => {
            old?.cleanup?.();
            effects[index] = { deps, cleanup: fn() };
          });
        }
      },
    },
    window: { addEventListener() {}, removeEventListener() {} },
    setTimeout: () => 1, clearTimeout() {}, setInterval: () => 1, clearInterval() {},
    fetch: async (url, options) => {
      requests.push({ url, method: options.method || "GET" });
      let body;
      if (options.method === "DELETE") {
        const id = decodeURIComponent(url.split("/").pop());
        accounts = accounts.filter((account) => account.id !== id);
        body = {};
      } else if (url.endsWith("/login")) {
        body = { login: { userCode: "TEST-CODE" } };
      } else if (options.method === "POST" && url.endsWith("/accounts")) {
        const account = { id: "pending", label: "Pending", connected: false, enabled: true };
        accounts = [...accounts, account];
        body = { account };
      } else {
        body = { accounts };
      }
      return { ok: true, json: async () => body };
    },
  });
  return {
    ...loaded, requests,
    render(name, props = {}) {
      stateIndex = 0; effectIndex = 0;
      const tree = loaded.helpers[name](props);
      scheduled.splice(0).forEach((effect) => effect());
      return tree;
    },
    emit(payload) { sources.filter((source) => !source.closed).forEach((source) => source.onmessage({ data: JSON.stringify(payload) })); },
    setAccounts(next) { accounts = next; },
    close() { effects.forEach((effect) => effect.cleanup?.()); },
  };
}

function findNode(tree, predicate) {
  if (!tree || typeof tree !== "object") return null;
  if (Array.isArray(tree)) return tree.map((node) => findNode(node, predicate)).find(Boolean);
  return predicate(tree) ? tree : findNode(tree.props?.children, predicate);
}
const tick = () => new Promise((resolve) => setImmediate(resolve));
const selectEvent = { preventDefault() {} };

test("removing the account with an active login clears the real menu state", async () => {
  const h = componentHarness([{ id: "primary", label: "Primary", controller: true, connected: true, enabled: true }]);
  const render = () => h.render("CodexMuxAccountMenu");
  render(); await tick();
  findNode(render(), (node) => node.key === "codex-mux-add").props.onSelect(selectEvent);
  await findNode(render(), (node) => node.key === "codex-mux-add-device-code").props.onSelect(selectEvent);
  let tree = render();
  assert.ok(findNode(tree, (node) => node.key === "codex-mux-login"));
  assert.equal(h.helpers.loginActive(), true);
  assert.equal(findNode(tree, (node) => node.key === "codex-mux-add").props.disabled, true);
  findNode(tree, (node) => node.props?.children === "Manage subscriptions").props.onSelect(selectEvent);
  tree = render();
  findNode(tree, (node) => node.key === "codex-mux-account-pending").props.onSelect(selectEvent);
  await findNode(render(), (node) => node.key === "codex-mux-remove-confirm").props.onSelect(selectEvent);
  tree = render();
  assert.equal(h.helpers.loginActive(), false);
  assert.ok(!findNode(tree, (node) => node.key === "codex-mux-login"));
  assert.ok(!findNode(tree, (node) => node.key === "codex-mux-account-pending"));
  assert.equal(h.requests.filter((request) => request.method === "DELETE").length, 1);
  h.close();
});

test("profile selection resets before removal triggers a combined-profile refetch", async () => {
  const primary = { id: "primary", label: "Primary", connected: true, enabled: true };
  const work = { id: "work", label: "Work", connected: true, enabled: true };
  const h = componentHarness([primary, work]);
  const selections = [];
  const props = { onSelect: () => selections.push(h.context.__codexMuxSelectedProfileAccountId) };
  const render = () => h.render("CodexMuxProfileAvatarStack", props);
  render(); await tick();
  const initialTree = render();
  const workButton = findNode(initialTree, (node) => node.type === "button" && node.props.title === "Work");
  assert.ok(workButton, JSON.stringify({ initialTree, requests: h.requests }));
  workButton.props.onClick();
  render();
  assert.equal(h.context.__codexMuxSelectedProfileAccountId, "work");
  h.setAccounts([primary]);
  h.emit({ type: "account-removed", accountId: "work" });
  assert.equal(selections.at(-1), null);
  await tick();
  const tree = render();
  assert.ok(!findNode(tree, (node) => node.type === "button" && node.props.title === "Work"));
  assert.ok(findNode(tree, (node) => node.type === "button" && node.props.title === "Primary"));
  assert.equal(h.requests.filter((request) => request.url.endsWith("/accounts")).length, 2);
  h.close();
});

test("removal selection keeps the menu open and rejects Primary", () => {
  const { helpers } = loadHelpers();
  let prevented = 0;
  let pending = null;
  let removalError = "previous failure";
  const event = { preventDefault: () => { prevented += 1; } };
  const setPending = (account) => { pending = account; };
  const setError = (message) => { removalError = message; };

  helpers.codexMuxSelectRemoval(
    event,
    { id: "disconnected", connected: false, enabled: false },
    setPending,
    setError,
  );
  assert.equal(prevented, 1);
  assert.equal(pending.id, "disconnected");
  assert.equal(removalError, "");

  pending = null;
  helpers.codexMuxSelectRemoval(
    event,
    { id: "primary", controller: true },
    setPending,
    setError,
  );
  assert.equal(prevented, 2);
  assert.equal(pending, null);
});

test("plugin scope falls back after its selected account is removed", async () => {
  const { helpers } = loadHelpers();
  const remaining = [
    { id: "primary", connected: true, enabled: true },
    { id: "other", connected: true, enabled: true },
  ];
  assert.equal(
    helpers.codexMuxNextPluginAccountId(remaining, "removed"),
    "primary",
  );
  assert.equal(
    helpers.codexMuxNextPluginAccountId(remaining, "other"),
    "other",
  );

  let predicate;
  await helpers.codexMuxInvalidatePluginQueries({
    invalidateQueries: async (options) => { predicate = options.predicate; },
  });
  assert.equal(predicate({ queryKey: ["apps"] }), true);
  assert.equal(predicate({ queryKey: ["plugins"] }), true);
  assert.equal(predicate({ queryKey: ["mcp"] }), true);
  assert.equal(predicate({ queryKey: ["unrelated"] }), false);
});

test("event subscription delivers removal payloads and closes cleanly", () => {
  let source;
  class FakeEventSource {
    constructor(url) {
      this.url = url;
      this.closed = false;
      source = this;
    }
    close() {
      this.closed = true;
    }
  }
  const { helpers } = loadHelpers({ EventSource: FakeEventSource });
  const payloads = [];
  const unsubscribe = helpers.codexMuxSubscribeToEvents((payload) => payloads.push(payload));
  source.onmessage({ data: JSON.stringify({ type: "account-removed", accountId: "work" }) });
  source.onmessage({ data: "not json" });
  assert.equal(source.url.includes("/events?token="), true);
  assert.equal(payloads.length, 1);
  assert.equal(payloads[0].accountId, "work");
  unsubscribe();
  assert.equal(source.closed, true);
});
