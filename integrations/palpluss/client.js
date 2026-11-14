const axios = require("axios");
const config = require("./config");

// HTTP Basic auth: username = API key, password empty. Per the docs that is
// `Authorization: Basic base64("<key>:")` (same thing `curl -u "<key>:"` sends).
function basicAuthHeader(apiKey) {
  return "Basic " + Buffer.from(`${apiKey || ""}:`).toString("base64");
}

const client = axios.create({
  baseURL: config.palpluss.baseUrl,
  timeout: config.palpluss.timeoutMs,
  headers: { Authorization: basicAuthHeader(config.palpluss.apiKey) },
});

// Normalise both transports into one shape: a successful payload, or a thrown
// Error whose `.code` is the Palpluss error code (see palplussError below).
client.interceptors.response.use(
  (response) => {
    const body = response && response.data;
    if (!body || body.success === false) throw palplussError(response);
    return response;
  },
  (err) => {
    // Axios rejects on non-2xx. Convert to our error so `.code` is the Palpluss
    // code (e.g. INVALID_API_KEY) instead of the Axios one (ERR_BAD_REQUEST).
    throw palplussError(err);
  }
);

// Palpluss wraps everything in { success, data, requestId } / { success:false, error }.
function unwrap(response) {
  return response.data.data;
}

// Palpluss sometimes returns the real error code top-level (error.code) and
// sometimes nested inside error.details.code while error.code is a generic
// "HTTP_400". Pick the most specific documented-looking code.
function resolveErrorCode(error) {
  if (!error) return null;
  const candidates = [error.code, error.details && error.details.code];
  for (const c of candidates) {
    if (typeof c === "string" && /^[A-Z][A-Z0-9_]+$/.test(c) && !/^HTTP_\d+$/.test(c)) {
      return c;
    }
  }
  return error.code || (error.details && error.details.code) || null;
}

// Build an Error that carries the documented Palpluss error `code` on `err.code`.
//
// IMPORTANT: an Axios rejection also has a `.code` (e.g. "ERR_BAD_REQUEST" /
// "ECONNABORTED"), which used to shadow the real Palpluss code and made every
// failure fall through to the generic message. We now read the code from the
// Palpluss error envelope first.
function palplussError(input) {
  const response = input && input.response ? input.response : input;
  const body = response && response.data;
  const error = (body && body.error) || {};
  const nested = error.details || {};
  const e = new Error(
    error.message || nested.message || (input && input.message) || "Palpluss request failed"
  );
  // Prefer the Palpluss code; fall back to the transport code (network/timeout).
  e.code = resolveErrorCode(error) || (input && input.code) || "UNKNOWN_ERROR";
  e.details = nested.details || error.details;
  e.requestId = body && body.requestId;
  e.httpStatus = (response && response.status) || undefined;
  return e;
}

// Normalizes Kenyan numbers (07xx / 01xx / +254xxx / 254xxx) to 254XXXXXXXXX.
// Palpluss does this server-side too; we keep a local copy for display/storage.
function normalizePhone(phone) {
  let p = String(phone || "").replace(/\D/g, "");
  if (p.startsWith("0")) p = "254" + p.slice(1);
  else if (p.startsWith("7") || p.startsWith("1")) p = "254" + p;
  return p;
}

// Initiates an STK Push via Palpluss. No OAuth step needed.
async function stkPush({ phone, amount, accountReference, description }) {
  const payload = {
    amount,
    phone,
    accountReference: String(accountReference).slice(0, 12),
    transactionDesc: String(description).slice(0, 13),
    callbackUrl: config.palpluss.callbackUrl,
  };
  if (config.palpluss.channelId) payload.channelId = config.palpluss.channelId;

  const data = unwrap(await client.post("/payments/stk", payload));
  return data; // { transactionId, status, phone, providerCheckoutId, resultCode, ... }
}

// Poll a single transaction's status (fallback to webhooks).
async function getTransaction(transactionId) {
  return unwrap(await client.get(`/transactions/${encodeURIComponent(transactionId)}`));
}

// Paginated transaction list with an aggregate `overview` for the filter set.
async function listTransactions({ status, type, limit = 20, cursor } = {}) {
  const params = { limit };
  if (status) params.status = status;
  if (type) params.type = type;
  if (cursor) params.cursor = cursor;
  return unwrap(await client.get("/transactions", { params }));
}

async function getBalance() {
  return unwrap(await client.get("/wallets/service/balance"));
}

async function listChannels() {
  const data = unwrap(await client.get("/payment-wallet/channels"));
  return data.items || [];
}

async function topUp({ amount, phone, accountReference, description, idempotencyKey }) {
  const headers = {};
  if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
  const data = unwrap(
    await client.post(
      "/wallets/service/topups",
      {
        amount,
        phone,
        accountReference,
        transactionDesc: description,
      },
      { headers }
    )
  );
  return data;
}

module.exports = {
  stkPush,
  getTransaction,
  listTransactions,
  getBalance,
  listChannels,
  topUp,
  normalizePhone,
  palplussError,
};
