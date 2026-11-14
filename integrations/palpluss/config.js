// Palpluss client configuration. All values come from the environment so the
// API key is never committed. Load a local .env with `node --env-file=.env`.

function required(name) {
  const value = process.env[name];
  if (!value) {
    throw new Error(`${name} is required (see integrations/palpluss/.env.example)`);
  }
  return value;
}

function optionalInt(name, fallback) {
  const raw = process.env[name];
  if (!raw) return fallback;
  const value = Number.parseInt(raw, 10);
  if (!Number.isFinite(value) || value <= 0) {
    throw new Error(`${name} must be a positive integer`);
  }
  return value;
}

module.exports = {
  palpluss: {
    baseUrl: required("PALPLUSS_BASE_URL").replace(/\/+$/, ""),
    apiKey: required("PALPLUSS_API_KEY"),
    callbackUrl: process.env.PALPLUSS_CALLBACK_URL || "",
    channelId: process.env.PALPLUSS_CHANNEL_ID || "",
    timeoutMs: optionalInt("PALPLUSS_TIMEOUT_MS", 15000),
  },
};
