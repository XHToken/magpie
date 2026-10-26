import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

for (const providerID of ["api_router", "wefto"]) test(`Loomy title bridge scopes ${providerID} requests to the source session and preserves manual titles`, async t => {
  const home = await fs.mkdtemp(path.join(os.tmpdir(), "router-title-test-"));
  t.after(() => fs.rm(home, { recursive: true, force: true }));
  const root = path.join(home, "opencode");
  await fs.mkdir(path.join(root, "chat-headers"), { recursive: true });
  await fs.mkdir(path.join(home, "share"));
  const plugin = path.join(root, "api-router-title.mjs");
  await fs.copyFile(new URL("../loomy_title.mjs", import.meta.url), plugin);
  const { default: create } = await import(pathToFileURL(plugin));
  const source = "ses_source123";
  const temp = "ses_temp123";
  for (const id of [source, temp]) {
    await fs.writeFile(path.join(root, "chat-headers", id + ".json"), JSON.stringify({ headers: { ChatId: "chat-shared" } }));
  }
  await fs.writeFile(path.join(home, "share", "chat-session-map.json"), JSON.stringify({ "chat-shared": { sessionId: source } }));
  let title = "New session - 2026-10-23T01:14:39.730Z";
  let selected = { providerID, modelID: "group/2222" };
  const updates = [];
  const client = { session: {
    get: async ({ path: p }) => ({ data: { title: p.id === temp ? "__title_generation__" : title } }),
    update: async ({ body }) => { updates.push(body.title); title = body.title; },
    messages: async () => ({ data: [{ info: { role: "user", model: selected }, parts: [
      { type: "text", text: "USER_VISIBLE_TEXT" },
      { type: "text", text: "SYNTHETIC_NOTE", synthetic: true },
    ] }] }),
  } };
  const hooks = await create({ client, directory: home });
  const main = () => ({ message: { model: { providerID, modelID: "group/2222" } }, parts: [] });
  await hooks["chat.message"]({ sessionID: source, agent: "loomy" }, main());
  assert.deepEqual(updates, ["新任务"]);
  title = "用户手动标题";
  await hooks["chat.message"]({ sessionID: source, agent: "loomy" }, main());
  assert.equal(title, "用户手动标题");
  const auxiliary = () => ({ message: { model: { providerID: "imodel", modelID: "external" }, agent: "title" }, parts: [{ id: "part123", type: "text", text: "Loomy original title prompt" }] });
  const output = auxiliary();
  await hooks["chat.message"]({ sessionID: temp, agent: "title" }, output);
  assert.deepEqual(output.message.model, selected);
  assert.deepEqual(JSON.parse(output.parts[0].text).user_messages, ["USER_VISIBLE_TEXT"]);
  const headers = { headers: {} };
  await hooks["chat.headers"]({ agent: "title", model: selected }, headers);
  assert.equal(headers.headers["X-Api-Router-Purpose"], "chat.title");
  selected = { providerID: "imodel", modelID: "external" };
  const other = auxiliary();
  await hooks["chat.message"]({ sessionID: temp, agent: "title" }, other);
  assert.equal(other.parts[0].text, "Loomy original title prompt");
  assert.deepEqual(other.message.model, selected);
  await fs.writeFile(path.join(home, "share", "chat-session-map.json"), "broken json");
  const unknown = auxiliary();
  await hooks["chat.message"]({ sessionID: temp, agent: "title" }, unknown);
  assert.equal(unknown.parts[0].text, "Loomy original title prompt");
});
