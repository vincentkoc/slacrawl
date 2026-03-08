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

for (const [channelID, byTS] of Object.entries(value.messages || {})) {
  if (!byTS || typeof byTS !== "object") {
    continue;
  }
  for (const [ts, entry] of Object.entries(byTS)) {
    if (!entry || typeof entry !== "object") {
      continue;
    }
    messages.push({
      ...entry,
      channel: entry.channel || channelID,
      ts: entry.ts || ts,
    });
  }
}

process.stdout.write(
  JSON.stringify({
    channels,
    members,
    messages,
  })
);
