# Palpluss payments client

Standalone Node.js client for the Palpluss mobile-money API (STK push,
transactions, wallet balance, channels, and top-ups). The Go API's own payment
providers live in `internal/services/payment/`; this module is for a separate
Node worker or script that talks to Palpluss and is not imported by the Go
services.

## Setup

```bash
cd integrations/palpluss
npm install
cp .env.example .env      # then set PALPLUSS_BASE_URL and PALPLUSS_API_KEY
```

Node 18+ is required. Load the environment with `node --env-file=.env` (built in
from Node 20.6, no `dotenv` dependency).

## Usage

```js
const palpluss = require("./client");

const tx = await palpluss.stkPush({
  phone: palpluss.normalizePhone("0712 345 678"),
  amount: 1500,
  accountReference: "ILO-12345",
  description: "Installation",
});
// tx.transactionId, tx.providerCheckoutId, tx.status

const status = await palpluss.getTransaction(tx.transactionId);
const page = await palpluss.listTransactions({ status: "success", limit: 20 });
const balance = await palpluss.getBalance();
const channels = await palpluss.listChannels();

await palpluss.topUp({
  amount: 5000,
  phone: "254712345678",
  accountReference: "TOPUP-1",
  description: "Float top-up",
  idempotencyKey: "topup-1-2026-01-01",
});
```

## Errors

All failures reject with an `Error` whose `code` is the Palpluss code when one
is present, otherwise the transport code:

```js
try {
  await palpluss.stkPush({ ... });
} catch (err) {
  console.error(err.code, err.message, err.requestId, err.httpStatus);
}
```

`err.details` carries the nested provider details, `err.requestId` the Palpluss
request ID for support tickets, and `err.httpStatus` the HTTP status.

## Operational notes

- The Go API currently registers only `mpesa` and `airtel` providers
  (`payment.ProvidersFromEnv`). Before pointing
  `/api/v1/payments/webhooks/palpluss` at this backend, add a Palpluss `Provider`
  implementation in `internal/services/payment/`.
- Keep `PALPLUSS_API_KEY` server-side only. Never ship it in the Flutter app.
- `topUp` accepts an `Idempotency-Key`; generate a stable key per logical
  operation so retries do not double-charge.
- Prefer webhooks for final status and use `getTransaction` only for polling
  fallback. Verify the webhook request against your configured callback secret
  before mutating orders.
- `accountReference` is truncated to 12 characters and `description` to 13,
  matching the provider limits in the client.
- Amounts are sent as-is; keep them in the smallest currency unit expected by
  Palpluss and validate server-side before calling the client.
