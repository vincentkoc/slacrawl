const fs = require("fs");
const v8 = require("v8");

const inputPath = process.argv[1];
if (!inputPath) {
  throw new Error("decoded V8 payload path is required");
}

const buffer = fs.readFileSync(inputPath);
const value = v8.deserialize(buffer);

const channels = Object.values(value.channels || {}).filter(
  (entry) => entry && typeof entry === "object" && entry.id
);
const members = Object.values(value.members || {}).filter(
  (entry) => entry && typeof entry === "object" && entry.id
);
const messages = [];
const seenMessages = new Set();
const workspaceId =
  value.selfTeamIds?.teamId ||
  value.selfTeamIds?.defaultWorkspaceId ||
  value.bootData?.team_id ||
  "";
const userId = value.bootData?.user_id || "";

function looksLikeMessage(entry) {
  if (!entry || typeof entry !== "object") {
    return false;
  }
  if (entry.__proto__ && entry.__proto__ !== Object.prototype && !Array.isArray(entry)) {
    return false;
  }
  const hasTimestamp =
    typeof entry.ts === "string" ||
    typeof entry.ts === "number" ||
    typeof entry.thread_ts === "string" ||
    typeof entry.thread_ts === "number";
  if (!hasTimestamp) {
    return false;
  }
  return (
    entry.type === "message" ||
    typeof entry.text === "string" ||
    typeof entry.subtype === "string" ||
    typeof entry.reply_count === "number" ||
    typeof entry.user === "string" ||
    typeof entry.parent_user_id === "string"
  );
}

function pushMessage(entry, fallbackChannel, fallbackTS) {
  if (!looksLikeMessage(entry)) {
    return;
  }
  const channel =
    entry.channel || entry.channel_id || entry.conversation || entry.conversation_id || fallbackChannel;
  const ts =
    entry.ts !== undefined && entry.ts !== null && entry.ts !== ""
      ? String(entry.ts)
      : fallbackTS !== undefined && fallbackTS !== null && fallbackTS !== ""
        ? String(fallbackTS)
        : "";
  if (!channel || !ts) {
    return;
  }
  const key = `${channel}|${ts}`;
  if (seenMessages.has(key)) {
    return;
  }
  seenMessages.add(key);
  messages.push({
    ...entry,
    channel,
    ts,
    thread_ts:
      entry.thread_ts !== undefined && entry.thread_ts !== null && entry.thread_ts !== ""
        ? String(entry.thread_ts)
        : "",
    latest_reply:
      entry.latest_reply !== undefined && entry.latest_reply !== null && entry.latest_reply !== ""
        ? String(entry.latest_reply)
        : "",
  });
}

function walkMessages(node, fallbackChannel, seenNodes) {
  if (!node || typeof node !== "object") {
    return;
  }
  if (seenNodes.has(node)) {
    return;
  }
  seenNodes.add(node);
  if (Array.isArray(node)) {
    for (const entry of node) {
      walkMessages(entry, fallbackChannel, seenNodes);
    }
    return;
  }

  pushMessage(node, fallbackChannel);

  for (const [key, child] of Object.entries(node)) {
    const nextChannel =
      key.startsWith("C") || key.startsWith("G") ? key : fallbackChannel;
    const nextFallbackTS =
      child && typeof child === "object" && !Array.isArray(child) && child.ts === undefined
        ? key
        : undefined;
    if (looksLikeMessage(child)) {
      pushMessage(child, nextChannel, nextFallbackTS);
    }
    walkMessages(child, nextChannel, seenNodes);
  }
}

for (const [channelID, byTS] of Object.entries(value.messages || {})) {
  walkMessages(byTS, channelID, new WeakSet());
}

for (const [channelID, threadState] of Object.entries(value.threads || {})) {
  walkMessages(threadState, channelID, new WeakSet());
}

process.stdout.write(
  JSON.stringify({
    workspace_id: workspaceId,
    user_id: userId,
    channels,
    members,
    messages,
  })
);
