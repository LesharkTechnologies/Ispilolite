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
| Follow | `POST /users/:id/follow` | Full summary (201 new; 200 already followed) |
| Unfollow | `DELETE /users/:id/follow` | `{ "user_id": "...", "is_following": false }` (200) |
| Follow stats | `GET /users/:id/follow-stats` or `GET /my/follow-stats` | `{ "user_id": "...", "follower_count": 0, "following_count": 0, "is_following": false, "profile_path": "/u/...", "profile_url": "https://..." }` |
| Followers | `GET /users/:id/followers` or `GET /my/followers` | `{ "items": [...], "total": 0, "limit": 20, "offset": 0 }` |
| Following | `GET /users/:id/following` or `GET /my/following` | Same page shape |

The follow response returns the same summary object as `follow-stats`, so the
client can update counts and the share link without a second request.

Lists accept `limit` (default 20, maximum 100) and `offset` (default 0).
Each item includes only `id`, `name`, `role`, `avatar_url`, `followed_at`,
`profile_path`, and `profile_url`; phone/email are not exposed. Inactive
accounts are excluded from public lists. In `follow-stats`, `is_following`
describes the **requesting** user's relationship to the account being viewed.

## Stable public profile links

Every account gets an immutable path such as
`/u/amina-2026-00112233445566778899aabbccddeeff`. It is stored in PostgreSQL
(`users.profile_slug`, `users.profile_path`) and never includes a hostname.
`PROFILE_PUBLIC_BASE_URL` supplies the origin at response time, so changing the
domain does not invalidate previously shared links.

| Action | Method and route | Notes |
|---|---|---|
| Public profile page | `GET /u/:slug` | Browser-viewable HTML, no auth |
| Public profile JSON | `GET /profiles/:slug` | Flutter/API clients, no auth |
| My link | `GET /my/public-profile` | Requires customer, technician, or ISP token |

Profile responses expose only `id`, `name`, `role`, `avatar_url`,
`profile_slug`, `profile_path`, `profile_url`, and follower/following counts.
Phone, email, employment, and job data are excluded. Slugs are derived once
from the name, creation year, and UUID; later renames do not change the link.

## Favorites and reactions

Customers, technicians, and ISPs can maintain private favorite lists and react
to public content. Repeated add/remove calls are idempotent.

| Action | Method and route |
|---|---|
| Favorite ISP/technician | `POST /favorites/{isp|technician}/:user_id` |
| Remove favorite | `DELETE /favorites/{isp|technician}/:user_id` |
| My favorites | `GET /my/favorites?type=isp&limit=20&offset=0` |
| React | `POST /reactions/{user_profile|technician_post|review}/:id` |
| Remove reaction | `DELETE /reactions/:type/:id?reaction=like` |
| Reaction counters | `GET /reactions/:type/:id` |

Supported reactions are `like` and `helpful`. Public profiles expose
`favorite_count` and `like_count`; technician posts expose `like_count`, and
reviews expose `like_count` and `helpful_count`.

## Push devices

Register or unregister a delivery token with `POST /my/devices` and
`DELETE /my/devices`. Registration accepts `platform` (`android`, `ios`, or
`web`) and `token`. Persisted notifications publish `notification.push` events
containing active tokens to RabbitMQ. `cmd/notification` forwards these events
to `NOTIFICATION_WEBHOOK_URL` using `NOTIFICATION_WEBHOOK_TOKEN`; that HTTPS
gateway performs the final FCM/APNs/web-push protocol call.

## Vacancy applications

ISPs list applicants with `GET /my/vacancies/:id/applications` and accept or
reject with `PATCH /my/vacancies/:id/applications/:application_id`.
Technicians withdraw with `DELETE /vacancies/:id/application`. Filling the
final slot closes the vacancy. Employment remains the explicit
`POST /my/technicians/:id/link` action.

Quotation finalization now creates a PDF immediately. Its document object
contains `/documents/{id}/download` when generation succeeds.

## Migrations

Run migrations `0020` through `0035` before deploying the updated API:

```bash
go run ./cmd/migrate
```

Payment release/refund requests are deliberately asynchronous. They remain
`release_pending`/`refund_pending` until a trusted payment worker sends a
signed settlement callback. Client requests can never mark funds as settled.
**No external payout/refund worker is included yet**; do not present this as
an automated settlement flow until the provider's payout API is integrated.

Migration `0035` adds a durable `payment_settlement_outbox`. Release/refund
requests are recorded there transactionally and signed settlement callbacks
mark them completed. Admins can inspect it at
`GET /admin/settlements/outbox`. It deliberately does not call an undocumented
money-moving API.

`POST /my/jobs` and `POST /quotations` support guarded per-user response replay
when clients send a stable `Idempotency-Key` header. A concurrent duplicate
returns 409 with `Retry-After`, a completed duplicate returns the stored 2xx
response with `Idempotency-Replayed: true`, and reusing a key with a different
payload returns 409. Other endpoints do not yet share this replay layer.
