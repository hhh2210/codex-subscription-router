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
    `${source}\n;globalThis.__codexMuxTest = {
      codexMuxSubscribeToEvents,
      codexMuxConnectedAccounts,
      codexMuxMenuAccounts,
      codexMuxSelectRemoval,
      codexMuxPluginAccounts,
      codexMuxNextPluginAccountId,
      codexMuxInvalidatePluginQueries,
    };`,
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
