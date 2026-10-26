// API-Router's managed OpenCode extension. Loomy's application files stay untouched.
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.dirname(fileURLToPath(import.meta.url));
const sessionID = /^ses_[a-zA-Z0-9]+$/;
const defaultTitle = /^New session - \d{4}-\d{2}-\d{2}T[\d:.]+Z$/;

async function readJSON(filename, limit) {
  const stat = await fs.lstat(filename);
  if (!stat.isFile() || stat.size > limit) throw new Error("Invalid title context file");
  return JSON.parse(await fs.readFile(filename, "utf8"));
}

async function sourceSession(id) {
  if (!sessionID.test(id)) return null;
  try {
    const record = await readJSON(path.join(root, "chat-headers", id + ".json"), 16384);
    const chatID = record.headers?.ChatId;
    if (typeof chatID !== "string" || !chatID) return null;
    const mapping = await readJSON(path.join(root, "..", "share", "chat-session-map.json"), 1 << 20);
    const entry = Object.hasOwn(mapping, chatID) ? mapping[chatID] : null;
    const source = typeof entry === "string" ? entry : entry?.sessionId;
    return typeof source === "string" && sessionID.test(source) ? source : null;
  } catch {
    // Missing correlation must not redirect an unrelated provider's request.
    return null;
  }
}

export default async ({ client, directory }) => {
  const get = async (id) => {
    const response = await client.session.get({ path: { id }, query: { directory }, throwOnError: true });
    return response.data;
  };
  return {
    "chat.message": async (input, output) => {
      const source = await sourceSession(input.sessionID);
      if (!source) return;
      if (output.message.model?.providerID === "api_router" && source === input.sessionID) {
        const current = await get(source);
        if (current && !current.parentID && defaultTitle.test(current.title)) {
          // Before OpenCode starts ensureTitle: Loomy's milestone writer now owns
          // persistence. Never reset an existing or manually edited title.
          await client.session.update({ path: { id: source }, query: { directory }, body: { title: "新任务" }, throwOnError: true });
        }
        return;
      }
      if (source === input.sessionID || !["title", "build"].includes(input.agent)) return;
      const current = await get(input.sessionID);
      if (current?.title !== "__title_generation__") return;
      const response = await client.session.messages({ path: { id: source }, query: { directory }, throwOnError: true });
      const users = (response.data || []).filter(item => item.info?.role === "user");
      const model = users.at(-1)?.info?.model;
      if (model?.providerID !== "api_router" || !model.modelID) return;
      const texts = users.slice(-8).map(item => item.parts
        .filter(part => part.type === "text" && !part.ignored && !part.synthetic)
        .map(part => part.text).join("\n\n"));
      const text = JSON.stringify({ api_router_title: 1, user_messages: texts });
      if (Buffer.byteLength(text) > 1 << 20) throw new Error("Title context exceeds limit");
      const first = output.parts.find(part => part.type === "text");
      if (!first) throw new Error("Missing title text part");
      output.message.model = { ...model };
      output.message.agent = "title";
      output.parts.splice(0, output.parts.length, { ...first, text });
    },
    "chat.headers": async (input, output) => {
      if (input.agent === "title" && input.model.providerID === "api_router") {
        output.headers["X-Api-Router-Purpose"] = "chat.title";
      }
    },
  };
};
