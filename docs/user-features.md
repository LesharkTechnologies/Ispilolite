# User Feature API Additions

All routes use `/api/v1`. Unless marked public, send an access-token header:

```http
Authorization: Bearer <access-token>
```

## Media contract (Cloudinary)

The Go API does **not** receive or proxy image bytes. Flutter uploads directly
to Cloudinary, then sends the resulting HTTPS delivery URL in `*_urls` fields.
The API validates the host, stores the URL in PostgreSQL, and returns it on
reads. Default allowed host: `res.cloudinary.com`; add Cloudinary custom CNAMEs
with `CLOUDINARY_ALLOWED_HOSTS`.

## New endpoints

| Feature | Method and route |
|---|---|
| Job status SSE | `GET /jobs/{job_id}/stream` |
| Job event replay | `GET /jobs/{job_id}/events` |
| Live technician position | `GET /jobs/{job_id}/location` |
| Update live position | `POST /jobs/{job_id}/location` (technician) |
| Job chat | `GET/POST /jobs/{job_id}/messages` |
| Chat receipts | `POST /jobs/{job_id}/messages/read` |
| Technician schedule | `PUT /my/availability` |
| Schedule exception | `POST /my/availability/exceptions` |
| Public bookable slots | `GET /technicians/{id}/availability` |
| Search autocomplete | `GET /search/suggest?q=...&domain=all` |
| Search health | `GET /search/health` |
| Start escrow payment | `POST /jobs/{job_id}/payments` |
| View escrow | `GET /jobs/{job_id}/escrow` |
| Approve milestone | `POST /escrows/{id}/release` |
| Provider collection callback | `POST /payments/webhooks/{mpesa|airtel}` |
| Trusted payout settlement | `POST /payments/settlements` |
| Warranty recurrence | `POST /jobs/{job_id}/warranty-claims` |
| Warranty claim list | `GET /warranty-claims` |
| ISP coverage GeoJSON | `GET /geo/coverage-map` (public) |
| Create coverage polygon | `POST /my/coverage/polygons` (ISP) |
| Referral dashboard | `GET /my/referrals` |
| Open dispute | `POST /jobs/{job_id}/disputes` |
| Dispute comments/evidence | `POST /disputes/{id}/comments` |
| Admin dispute queue | `GET /admin/disputes` |
| Translation catalog | `GET /i18n/{en|sw}` (public) |
| Technician earnings | `GET /my/earnings?period=week|month|year` |

## User follows (customers, independent/company technicians, and ISPs)

Every active user can follow another active user, regardless of role. Following
someone is **social only**: it does not hire a technician, join an ISP, or
grant access to private jobs. Company employment continues to use
`POST /my/technicians/:id/link`; invited company technicians are linked when
created by `POST /my/technicians`.

All follow endpoints require an access token (`customer`, `technician`, or
`isp`; admin accounts do not participate). Use the `users.id` UUID for `:id`, not a separate search-profile
ID. A user cannot follow themself, and repeated follow/unfollow is safe.

| Action | Method and route | `data` |
|---|---|---|
| Follow | `POST /users/:id/follow` | `{ "user_id": "...", "is_following": true }` (201 new; 200 already followed) |
| Unfollow | `DELETE /users/:id/follow` | `{ "user_id": "...", "is_following": false }` (200) |
| Follow stats | `GET /users/:id/follow-stats` or `GET /my/follow-stats` | `{ "user_id": "...", "follower_count": 0, "following_count": 0, "is_following": false }` |
| Followers | `GET /users/:id/followers` or `GET /my/followers` | `{ "items": [...], "total": 0, "limit": 20, "offset": 0 }` |
| Following | `GET /users/:id/following` or `GET /my/following` | Same page shape |

Lists accept `limit` (default 20, maximum 100) and `offset` (default 0).
Each item includes only `id`, `name`, `role`, `avatar_url`, `followed_at`;
phone/email are not exposed. Inactive accounts are excluded from public lists.
In `follow-stats`, `is_following` describes the **requesting** user's
relationship to the account being viewed.

Quotation finalization now creates a PDF immediately. Its document object
contains `/documents/{id}/download` when generation succeeds.

## Migrations

Run migrations `0020` through `0030` before deploying the updated API:

```bash
go run ./cmd/migrate
```

Payment release/refund requests are deliberately asynchronous. They remain
`release_pending`/`refund_pending` until a trusted payment worker sends a
signed settlement callback. Client requests can never mark funds as settled.
**No external payout/refund worker is included yet**; do not present this as
an automated settlement flow until the provider's payout API is integrated.

`POST /my/jobs` and `POST /quotations` support guarded per-user response replay
when clients send a stable `Idempotency-Key` header. A concurrent duplicate
returns 409 with `Retry-After`, a completed duplicate returns the stored 2xx
response with `Idempotency-Replayed: true`, and reusing a key with a different
payload returns 409. Other endpoints do not yet share this replay layer.
