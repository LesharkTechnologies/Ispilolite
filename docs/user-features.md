# User Feature API Additions

All routes use `/api/v1`. Unless marked public, send an access-token header:

```http
Authorization: Bearer <access-token>
```

## Media contract (Cloudinary)

The Go API does **not** receive or proxy image bytes. An authenticated client
first calls `POST /media/upload-signature`, then uploads the file directly to
Cloudinary using the returned `upload_params`, and finally sends the resulting
`secure_url` in existing `*_urls` fields. Flutter and browser JavaScript code
is documented in [`docs/media-upload.md`](media-upload.md).

The API validates the delivery host, stores the URL in PostgreSQL, and returns
it on reads. Default allowed host: `res.cloudinary.com`; add Cloudinary custom
CNAMEs with `CLOUDINARY_ALLOWED_HOSTS`. Upload signatures are scoped by media
kind and by the authenticated user's folder, so one account cannot sign writes
into another account's folder.

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
| Signed media upload params | `POST /media/upload-signature` (authenticated) |
| Server media fallback | `POST /media/upload` (authenticated multipart) |
| ISP suggestions for speed tests | `GET /speed-tests/isps?county=&town=` (public) |
| Save speed test | `POST /speed-tests` (optional auth) |
| Private speed-test history | `GET /my/speed-tests` (authenticated) |
| ISP speed-test aggregates | `GET /my/speed-tests/stats` (ISP) |
| Speed-test moderation queue | `GET /admin/speed-tests` (admin) |
| Moderate speed test | `PATCH /admin/speed-tests/:id` (admin) |

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

## Public coverage pages

County, town, and estate pages give every covered area a shareable web page
without requiring the app or a login. They are the public landing surface for
search engines, WhatsApp status, and estate groups.

| Action | Method and route | Notes |
|---|---|---|
| County page | `GET /places/:county` | HTML with OpenGraph metadata |
| Town page | `GET /places/:county/:town` | HTML with OpenGraph metadata |
| Estate page | `GET /places/:county/:town/:estate` | HTML with OpenGraph metadata |
| Coverage JSON | `GET /public/coverage?county=&town=&estate=` | Same public data for clients |

Pages list ISPs that publish serviceability for the area, match the ISP county,
or have an explicit coverage location there. Packages show the effective county
price when one exists and fall back to the base price otherwise. The page
includes published serviceability, ratings, review counts, technician counts,
and profile links.

Area names tolerate case, spaces, and punctuation (`kiambu-road` and
`Kiambu Road` are equivalent). Only public ISP, package, and coverage fields
are returned. Customer names, phone numbers, waitlist entries, and browsing
history are never included. View tracking stores only the page type, area key,
referrer origin, and timestamp; IP addresses and user agents are not stored.

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

## Sponsored promotions

Users can promote a billing-system listing, an ISP profile, or a technician
profile. Each promotion belongs to the creating account, while managers of a
billing system can also view and manage promotions for that system. Admin
accounts can inspect and moderate every promotion.

| Action | Method and route | Notes |
|---|---|---|
| Active placements | `GET /promotions?placement=directory&limit=10` | Public; returns active/scheduled-and-started placements only |
| Record impression/click | `POST /promotions/:id/events` | Public; optional token attributes the viewer to aggregate insights |
| Contact the owner | `POST /promotions/:id/enquiry` | Signed-in user; explicit `consent: true` is required |
| My promotions | `GET /my/promotions?status=active&limit=20&offset=0` | Owner or billing-system manager |
| Promotion detail | `GET /my/promotions/:id` | Owner or manager |
| Create promotion | `POST /my/promotions` | Owner or manager |
| Renew | `POST /my/promotions/:id/renew` | Creates a new pending-approval campaign; old statistics stay intact |
| Cancel | `POST /my/promotions/:id/cancel` | Owner or manager |
| Audience insights | `GET /my/promotions/:id/insights?from=YYYY-MM-DD&to=YYYY-MM-DD&placement=directory` | Aggregates only |
| Payment history | `GET /my/promotions/:id/payments` | Owner or manager |
| Enquiries | `GET /my/promotions/:id/enquiries` | Owner or manager; only visitors who chose to share details appear |
| Admin promotion list | `GET /admin/promotions?status=pending_approval` | All promotions |
| Admin promotion detail/insights/payments/enquiries | `GET /admin/promotions/:id[...]` | Same response shapes |
| Approve submitted promotion | `POST /admin/promotions/:id/approve` | Makes it payable/visible according to payment and start date |
| Reject submitted promotion | `POST /admin/promotions/:id/reject` | `{"reason":"..."}`; the owner sees `rejection_reason` |
| Suspend, restore or cancel | `PATCH /admin/promotions/:id` | `{"status":"suspended|active|cancelled"}` |
| Confirm external payment | `POST /admin/promotions/:id/payments/:payment_id/confirm` | `{"provider":"manual","reference":"..."}` |

`POST /my/promotions` accepts:

```json
{
  "listing_type": "billing_system",
  "listing_id": "<uuid>",
  "placement": "directory",
  "destination_type": "internal",
  "destination_url": "/billing-systems/example",
  "starts_at": "2026-11-15T00:00:00Z",
  "duration_days": 30,
  "amount": 2500,
  "currency": "KES"
}
```

`listing_type` is `billing_system`, `isp`, or `technician`. For ISP and
technician promotions, `listing_id` is the owning `users.id`. If
`destination_type` is `internal`, the destination must be a path beginning with
`/`; omit it to use the listing's public profile path. External destinations
must be `http(s)` URLs. `expires_at` can be sent instead of `duration_days`.

Statuses are `pending_approval`, `pending_payment`, `scheduled`, `active`,
`expired`, `cancelled`, `suspended`, and `rejected`. Expiry is calculated from
server time on every read, so the dashboard changes to `expired` automatically.
`time_remaining` is human-readable (for example `3 days remaining` or
`Expired 2 days ago`).

A submitted promotion is private and **cannot appear on the customer side until
an admin approves it**. The lifecycle is:

1. Owner submits details with `POST /my/promotions`; status is
   `pending_approval` and a pending payment row is created.
2. Admin reviews the content. `approve` records approval; `reject` closes the
   request with a reason. Rejected promotions never become public.
3. If payment was already confirmed, approval makes the promotion `active` or
   `scheduled` (depending on `starts_at`). Otherwise it becomes
   `pending_payment` and stays hidden until an admin confirms the payment.
4. Only `active`, or `scheduled` with `starts_at <= now()`, are returned by the
   public placements endpoint.

Promotion responses include impressions, unique viewers, clicks, unique
clickers, click-through rate, payment status, expiry, and enquiry count.
Insights add audience role, broad county, placement, and daily series. Raw IP
addresses, user agents, names, phone numbers, and browsing histories are never
stored in the event table. Anonymous viewers are represented by a salted hash.
Bot-like user agents are ignored, impressions are deduplicated per
viewer/placement for five minutes, and clicks per viewer/placement for one
minute.

**Sponsorship payment rail:** a new promotion creates a pending payment-history
row immediately. No automatic card or STK capture is invented here. An admin
reconciles an externally verified payment reference with the confirm endpoint.
Approval and payment are separate gates: both are required before a promotion
is public. Wire a verified provider contract before automating collection.

## Migrations

Run migrations `0020` through `0040` before deploying the updated API:

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
