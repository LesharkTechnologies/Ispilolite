# ISPILO API Guide

This document is the frontend integration reference for the current ISPILO API.

## Base Rules

The API prefix is:

```text
/api/v1
```

Examples below assume:

```text
https://api.example.com/api/v1
```

## Authentication

Protected endpoints require:

```http
Authorization: Bearer <access_token>
Content-Type: application/json
Accept: application/json
```

JWT role values are:

```text
customer
isp
technician
admin
```

The backend uses one `users` identity table. “Client” in older names means a user with role `customer`; it is not a separate identity type.

### Phone Number Format (E.164)
Phone numbers must strictly match E.164 international format (`^\+[1-9]\d{1,14}$`), e.g. `+254712345678`.

### Token Refresh & Replay Protection
`POST /api/v1/auth/refresh` issues a new access token and invalidates the supplied refresh token. Single-use JTI replay protection is enforced in Redis (`jti:<claims.ID>`); replaying a previously used refresh token causes all active refresh sessions for that user to be revoked.

## Response Envelopes

Most endpoints return:

```json
{
  "success": true,
  "message": "optional message",
  "data": {}
}
```

Errors normally return:

```json
{
  "success": false,
  "message": "human-readable error"
}
```

Common status codes:

| Status | Meaning |
|---|---|
| `200` | Successful read/update/action |
| `201` | Resource created |
| `202` | Accepted for asynchronous processing |
| `400` | Invalid payload or query |
| `401` | Missing/invalid JWT |
| `403` | Valid JWT but insufficient role/permission |
| `404` | Resource not found |
| `409` | Conflict, unavailable resource, duplicate, or invalid state transition |
| `500` | Server error |

Use RFC3339 UTC timestamps, for example `2026-09-12T23:59:59Z`.

Frontend should treat IDs as opaque UUID/string values and should not derive identity from phone numbers. Phone numbers are contact fields only.

## Production workflow endpoints

### ISP work queue

```http
GET /my/isp-jobs?status=open
GET /my/isp-jobs/:id
```

Both require an ISP token. The queue includes requests targeting the ISP,
requests assigned to it, its own technician-work requests, and requests to
which it applied. ISP installation views and updates now use the same
`service_requests` records as customer creation routes.

The installation pipeline exposes SLA timestamps on job records:
`response_due_at`, `quote_due_at`, `installation_due_at`, `accepted_at`,
`scheduled_at`, and `completed_at`. Quotation creation moves an eligible request
to `quoted`; customer acceptance moves it to `accepted`; technician assignment
moves it to `scheduled`.

### Serviceability

```http
GET    /serviceability?county=Nairobi&town=Westlands&estate=...
POST   /serviceability/waitlist
GET    /my/serviceability
POST   /my/serviceability
DELETE /my/serviceability/:id
GET    /my/serviceability/waitlist
```

ISPs maintain manual serviceability records with `serviceable`,
`likely_serviceable`, `survey_required`, or `not_serviceable` status, technology,
estimated installation days, and a reason. Customers can check an area before
creating a request and join a waitlist when service is unavailable.

### Installation proof

```http
POST /jobs/:id/installation-proof
GET  /jobs/:id/installation-proof
POST /jobs/:id/installation-confirm
```

Technicians submit check-in/out data, photos, a checklist, device serial, and
test results. The customer confirms the proof. A job cannot transition to
`completed` until at least one photo, a device serial, checklist data, and
customer confirmation are present.

### Provider matches

```http
GET /my/matches?status=notified&limit=20
POST /matches/:id/respond
DELETE /matches/:id/decline
```

ISP and technician providers receive stable match IDs in this inbox and in
match notifications. Only the customer who owns a request can call
`GET /matches/request/:id` to generate/re-run matching.

### Notifications

```http
GET /notifications/unread-count
PUT /notifications/read-all
PUT /notifications/:id/read
```

All three operate only on the authenticated user's notifications.

### Favorites and reactions

```http
POST   /favorites/{isp|technician}/:id
DELETE /favorites/{isp|technician}/:id
GET    /my/favorites?type=&limit=20&offset=0

POST   /reactions/{user_profile|technician_post|review}/:id
DELETE /reactions/:type/:id?reaction=like
GET    /reactions/:type/:id
```

Reaction POST bodies use `{ "reaction": "like" }` or
`{ "reaction": "helpful" }`. Counters use unique per-user reactions, so
repeating the same request does not inflate totals.

### Push devices

```http
POST /my/devices
DELETE /my/devices
```

Registration uses `{ "platform": "android", "token": "..." }`; supported
platforms are `android`, `ios`, and `web`. Push events are forwarded to the
authenticated HTTPS notification gateway configured in the environment.

### Vacancy applicants

```http
GET   /my/vacancies/:id/applications
PATCH /my/vacancies/:id/applications/:application_id
DELETE /vacancies/:id/application
```

The PATCH body accepts `accepted` or `rejected`. Accepting enough applicants
to fill all slots closes the vacancy. Linking an accepted technician as an
employee remains an explicit company action.

### Admin account provisioning

```http
POST /admin/users
GET /admin/users?q=&role=&status=&limit=20&offset=0
PATCH /admin/users/:id
GET /admin/settlements/outbox?status=pending
POST /admin/settlements/outbox/:id/retry
```

Admins can provision ISP, technician, and admin accounts. Provisioning creates
the ISP/technician catalog rows in the same transaction. The PATCH endpoint
accepts `status` (`active` or `suspended`) and/or `is_verified`; suspending an
account revokes refresh sessions, and access middleware rejects its existing
access tokens immediately.

### Chat messages and Job communication

```http
GET    /jobs/:id/messages?before=&limit=50
POST   /jobs/:id/messages
POST   /jobs/:id/messages/read
DELETE /jobs/messages/:id
DELETE /chat/messages/:id
```

Participants (customer, ISP, assigned technician) can send messages and attachments. Calling `DELETE /jobs/messages/:id` marks the message soft-deleted (`deleted_at = NOW()`), hiding it from the conversation while retaining the record for KDPA and legal compliance. Admins may delete any message.

### Dispute resolution and Escrow

```http
POST   /jobs/:id/disputes
GET    /disputes/:id
POST   /disputes/:id/comments
DELETE /disputes/:id
POST   /disputes/:id/withdraw
POST   /disputes/:id/cancel
GET    /admin/disputes?status=&limit=50
PATCH  /admin/disputes/:id
DELETE /admin/disputes/:id
```

When an escrow is disputed, its status transitions to `disputed`. Users who opened the dispute can withdraw or cancel it with `DELETE /disputes/:id` or `POST /disputes/:id/withdraw`, which restores escrow status to `held` and updates dispute status to `cancelled`. Admins can resolve disputes (`PATCH /admin/disputes/:id` with resolution `release` or `refund`) or cancel them with `DELETE /admin/disputes/:id`.

## User follows (customer, technician, ISP)

Following is social only: it does not hire a technician, add them to an ISP,
or grant access to a job. Use the **users.id** UUID, which is `user_id` on
technician search results, for `:id`.

| Action | Method and path | Result in `data` |
|---|---|---|
| Follow | `POST /users/:id/follow` | Summary object below (201 new, 200 already followed) |
| Unfollow | `DELETE /users/:id/follow` | `{ "user_id": "...", "is_following": false }` |
| Counts/status | `GET /users/:id/follow-stats` or `/my/follow-stats` | `{ "follower_count": 0, "following_count": 0, "is_following": false, "user_id": "...", "profile_path": "/u/...", "profile_url": "https://..." }` |
| Followers | `GET /users/:id/followers` or `/my/followers` | `{ "items": [], "total": 0, "limit": 20, "offset": 0 }` |
| Following | `GET /users/:id/following` or `/my/following` | Same paginated response |
| My share link | `GET /my/public-profile` | `{ "profile_slug": "...", "profile_path": "/u/...", "profile_url": "https://..." }` |

Public profile pages are available without a token at `GET /u/:slug` (HTML)
and `GET /profiles/:slug` (JSON). The stored path is domain-free; the API
combines it with `PROFILE_PUBLIC_BASE_URL` at response time, so changing the
domain keeps old links valid.

All require a customer, technician, or ISP JWT. `limit` is capped at 100;
follower lists expose only public user fields. Follow is idempotent (201 new,
200 already following), unfollow is idempotent (200), self-follow returns 400,
and inactive/missing targets return 404. Company invitations through
`POST /my/technicians` now create the company link so invited technicians
appear in the ISP's `GET /my/technicians` list; independent technicians do not.

## Public coverage pages

Area pages are public, indexable, and safe to share on WhatsApp or social
media. They use the same origin as `PROFILE_PUBLIC_BASE_URL` for canonical and
share links.

| Action | Method and route | Notes |
|---|---|---|
| County page | `GET /places/:county` | Server-rendered HTML, no auth |
| Town page | `GET /places/:county/:town` | Server-rendered HTML, no auth |
| Estate page | `GET /places/:county/:town/:estate` | Server-rendered HTML, no auth |
| Coverage JSON | `GET /api/v1/public/coverage?county=nairobi&town=westlands` | Flutter/API clients, no auth |

`county` is required. `town` and `estate` are optional and narrow the result.
Area names are matched without regard to case, spaces, or punctuation, so
`kiambu-road` and `Kiambu Road` resolve to the same page. The response contains
only public ISP details: name, logo, description, rating, review count,
published serviceability, technician count, public profile link, and active
packages with their effective county price. It never includes customer,
technician, or waitlist identities.

```json
{
  "success": true,
  "data": {
    "page_type": "town",
    "county": "Nairobi",
    "town": "Westlands",
    "title": "Internet providers in Westlands, Nairobi | Ispilo Lite",
    "description": "Compare 2 internet providers and 8 packages in Westlands, Nairobi. See prices, speeds, ratings and coverage on Ispilo Lite.",
    "canonical_url": "https://lite.ispilo.co.ke/places/nairobi/westlands",
    "isp_count": 2,
    "package_count": 8,
    "from_price": 2500,
    "isps": []
  }
}
```

The HTML page adds OpenGraph and Twitter metadata, a canonical URL, WhatsApp,
Facebook, and X share links, package tables, and per-ISP profile links. Each
view stores only the page type, area key, referrer origin, and timestamp in
`coverage_page_views`; IP addresses and user agents are not stored.

## Media uploads (Cloudinary signed uploads)

The API does not receive image bytes. It issues a signed upload request scoped
to the authenticated user's folder; the client uploads the file directly to
Cloudinary from Flutter or browser JavaScript, then sends the returned
`secure_url` in the existing `photo_urls`, `media_urls`, `attachments`, or
similar fields.

```http
POST /api/v1/media/upload-signature
Authorization: Bearer <access-token>
Content-Type: application/json
```

```json
{
  "kind": "installation_proof",
  "file_size": 1843200,
  "content_type": "image/jpeg"
}
```

`file_size` and `content_type` are optional checks. The response includes the
Cloudinary endpoint and the exact multipart fields to submit with the file:

```json
{
  "success": true,
  "data": {
    "kind": "installation_proof",
    "cloud_name": "your-cloud-name",
    "upload_url": "https://api.cloudinary.com/v1_1/your-cloud-name/image/upload",
    "folder": "ispilolite/installation-proofs/550e8400-e29b-41d4-a716-446655440000",
    "timestamp": 1770000000,
    "expires_at": 1770000600,
    "max_file_size": 15728640,
    "allowed_formats": ["jpg", "jpeg", "png", "webp", "heic", "heif"],
    "upload_params": {
      "api_key": "your-public-api-key",
      "allowed_formats": "jpg,jpeg,png,webp,heic,heif",
      "folder": "ispilolite/installation-proofs/550e8400-e29b-41d4-a716-446655440000",
      "signature": "cloudinary-signature",
      "timestamp": "1770000000"
    }
  }
}
```

Submit every `upload_params` entry as a multipart field, add the file as
`file`, and POST to `upload_url`. The response is a standard Cloudinary upload
object; store its `secure_url`, never a temporary local path.

| `kind` | Allowed roles | Max file size | Cloudinary folder |
|---|---|---|---|
| `profile` | customer, technician, ISP, admin | 5 MB | `profiles/<user-id>` |
| `installation_proof` | technician, ISP, admin | 15 MB | `installation-proofs/<user-id>` |
| `portfolio` | technician, admin | 15 MB | `technician-portfolio/<user-id>` |
| `promotion` | ISP, admin | 10 MB | `promotions/<user-id>` |
| `isp_logo` | ISP, admin | 5 MB | `isp-logos/<user-id>` |
| `review` | customer, admin | 5 MB | `reviews/<user-id>` |
| `chat` | customer, technician, ISP, admin | 10 MB | `chat/<user-id>` |

Only JPEG, PNG, WebP, HEIC, and HEIF images are accepted. The folder is chosen
by the server from `kind` and the authenticated user ID; clients cannot pick
an arbitrary folder. `expires_at` is ten minutes after signing; it is advisory,
so the client should upload promptly. The API secret is never returned. The
upload endpoint returns `503` until `CLOUDINARY_CLOUD_NAME`,
`CLOUDINARY_API_KEY`, and `CLOUDINARY_API_SECRET` are configured.

## Speed-test history and ISP attribution

The speed-test flow lets a user select a registered ISP suggested for their
county/town or type an ISP name that is not registered. A typed name is stored
as user-reported text and never automatically creates or impersonates a
registered ISP.

| Action | Method and route | Access |
|---|---|---|
| ISP suggestions | `GET /api/v1/speed-tests/isps?county=Machakos&town=Machakos%20Town` | Public |
| Save result | `POST /api/v1/speed-tests` | Optional login |
| My history | `GET /api/v1/my/speed-tests?limit=50` | Authenticated user |
| ISP dashboard stats | `GET /api/v1/my/speed-tests/stats?county=&town=` | ISP only |
| Moderation queue | `GET /api/v1/admin/speed-tests?status=pending` | Admin only |
| Moderate result | `PATCH /api/v1/admin/speed-tests/{id}` | Admin only |

Results are private to the submitting user by default. New submissions start
as `pending`; only moderated `accepted` tests should be used for official public
statistics. `include_in_public_stats` defaults to `false`, so a user can save a
private result without affecting public rankings.

Every submission captures the trusted client IP resolved using
`TRUSTED_PROXIES`. The raw IP is retained in the restricted speed-test table
for abuse investigation and moderation; it is never returned in user history,
ISP dashboard results, public statistics, or rankings. ISP dashboards receive
aggregate counts and accepted-test averages for tests attributed to their
registered ISP, without customer identity or IP addresses.

Example submission:

```json
{
  "test_session_id": "8f01d22b-5c7f-4f8d-a9f1-9124d6e1c821",
  "download_mbps": 87.42,
  "upload_mbps": 42.18,
  "latency_ms": 12.5,
  "jitter_ms": 3.2,
  "isp_id": null,
  "isp_name": "HomeNet",
  "isp_plan_speed_mbps": 100,
  "county": "Machakos",
  "town": "Machakos Town",
  "include_in_public_stats": false
}
```

Migration `0041_speed_test_history.sql` must be applied before deploying the
speed-test API.

Admin moderation uses:

```json
{ "status": "accepted", "reason": "measurement is plausible" }
```

Allowed decisions are `accepted`, `rejected`, and `suspicious`. The moderation
queue is the only API response that includes the captured IP address, and it is
restricted to administrators.

# Part 1: Admin API

All endpoints in this section require an `admin` JWT.

## Admin tax rates

```http
GET /admin/tax-rates
```

Returns active system VAT/tax rates stored in PostgreSQL.

```json
{
  "success": true,
  "data": [
    {
      "id": "tax-rate-uuid",
      "name": "VAT",
      "rate": 16,
      "is_default": true
    }
  ]
}
```

```http
POST /admin/tax-rates
```

Payload:

```json
{
  "name": "VAT",
  "rate": 16,
  "is_default": true
}
```

Rates must be between `0` and `100`. Creating a default rate clears the previous default.

Delete tax rate:

```http
DELETE /tax-rates/{id}
DELETE /admin/tax-rates/{id}
```

Deactivates the tax rate so it is excluded from future quotations while preserving historic audit integrity.

## Admin system units

```http
POST /admin/system-units
```

Payload:

```json
{
  "name": "Hour",
  "singular_name": "hour",
  "plural_name": "hours",
  "symbol": "hr"
}
```

Response:

```json
{
  "success": true,
  "data": {
    "id": "unit-uuid",
    "name": "Hour",
    "singular_name": "hour",
    "plural_name": "hours",
    "symbol": "hr",
    "is_system": true
  }
}
```

## Business profile moderation

```http
PUT /admin/business-profiles/{user_id}
```

Payload:

```json
{
  "legal_name": "Example ISP Ltd",
  "registration_number": "REG-123",
  "tax_number": "PIN-123",
  "address": "Nairobi",
  "phone": "254700000000",
  "email": "admin@example.com"
}
```

This saves an approved company profile for the selected user.

## Package moderation

```http
PUT /admin/packages/{package_id}/moderate
```

Payload:

```json
{
  "status": "approved",
  "note": "Package details verified"
}
```

Allowed statuses:

```text
approved
rejected
suspended
```

## Deletion requests and KDPA 2-year statutory retention

According to the Kenya Data Protection Act, 2019 (KDPA) and telecommunications/financial compliance regulations, user data must be retained for 2 years following account closure for statutory audit purposes, while becoming immediately invisible across the consumer application (blocking login, profile access, and public searches).

```http
GET    /admin/deletion-requests
POST   /admin/approve-deletion
DELETE /admin/deletion-requests/{user_id}
DELETE /admin/users/{user_id}
POST   /admin/retention/purge-expired
DELETE /admin/retention/expired
```

Approve payload (when using `POST /admin/approve-deletion`):

```json
{
  "user_id": "user-uuid"
}
```

Alternatively, sending `DELETE /admin/users/{user_id}` or `DELETE /admin/deletion-requests/{user_id}` directly triggers the KDPA-compliant retention lifecycle:
- Status is set to `'deleted'`.
- `deleted_at` is stamped with the current time.
- `retention_until` is set to `NOW() + INTERVAL '2 years'`.
- All active JWT refresh sessions are immediately revoked.
- Technician availability and ISP active status are set to false, preventing any consumer discovery or bookings.
- User skills, names, phones, emails, and transaction histories are preserved in the DB for statutory audit, completely invisible on consumer endpoints.
- After the 2-year window, `POST /admin/retention/purge-expired` or `DELETE /admin/retention/expired` executes the final automated scrubbing of expired PII.

## Audit logs

```http
GET /admin/audit?event=user.login&limit=100
```

Audit records include event, action, actor, resource, description, success/failure, request ID, IP, user agent, metadata, and timestamp.

Export:

```http
POST /admin/audit/export?period=DAILY&start=2026-08-09T00:00:00Z&end=2026-08-10T00:00:00Z
```

The export is an Excel-compatible CSV file. It returns the archive path and event count.

Delete logs only after an archive exists:

```http
DELETE /admin/audit?start=2026-08-09T00:00:00Z&end=2026-08-10T00:00:00Z&event=user.login
```

The backend refuses deletion when no completed archive covers the entire range.

## Admin SMS and critical alerts

```http
POST /admin/sms
POST /admin/critical-alerts
```

Admin SMS payload:

```json
{
  "all": true,
  "message": "Scheduled maintenance tonight."
}
```

Other targeting options:

```json
{
  "role": "technician",
  "message": "New training schedule available."
}
```

```json
{
  "user_ids": ["user-uuid-1", "user-uuid-2"],
  "message": "Your job assignment has changed."
}
```

Critical alert payload:

```json
{
  "component": "database",
  "message": "Database health check failed",
  "data": {
    "error_code": "DB_UNAVAILABLE"
  }
}
```

# Part 2: ISP API

ISP endpoints require an `isp` JWT unless noted otherwise.

## ISP profile and technicians

```http
GET /isps/{isp_id}
GET /isps/{isp_id}/packages
GET /isps/{isp_id}/reviews
GET /my/profile
PUT /my/profile
DELETE /my/profile
```

Link an existing technician:

```http
POST /my/technicians/{technician_id}/link
```

Response includes the stable company number:

```json
{
  "success": true,
  "data": {
    "company_technician_id": "ISP1234-TECH-0001"
  }
}
```

List company technicians and assigned technicians:

```http
GET /my/technicians
GET /my/technicians/direct
GET /my/technicians/all
```

End a company relationship or remove technician:

```http
POST   /my/technicians/{technician_id}/end
DELETE /my/technicians/{technician_id}/link
DELETE /my/technicians/{technician_id}
DELETE /technicians/{technician_id}
```

The technician remains a user and can work independently after the company link ends.

## ISP technician work

```http
POST /my/technician-work-requests
```

Payload:

```json
{
  "target_technician_id": "technician-user-id",
  "service_type": "server_configuration",
  "description": "Configure the ISP customer server",
  "budget": 15000,
  "deadline_at": "2026-09-12T23:59:59Z"
}
```

This creates an ISP-owned direct technician request with `requester_role=isp`.

Assign a company technician to a customer request:

```http
PUT /my/requests/{request_id}/assign-technician
```

Payload:

```json
{
  "technician_id": "technician-user-id"
}
```

The technician must be active and linked to the requesting ISP.

## ISP vacancies

```http
POST   /my/vacancies
GET    /my/vacancies
DELETE /my/vacancies/{vacancy_id}
```

Create payload:

```json
{
  "title": "Fiber Installation Technician",
  "description": "Need technicians for residential fiber work.",
  "service_type": "fiber_installation",
  "slots": 3,
  "deadline_at": "2026-09-12T23:59:59Z"
}
```

Vacancies automatically become `expired` after the deadline. The ISP can close them manually before the deadline.

## ISP packages

```http
POST   /my/packages
PUT    /packages/{package_id}
PUT    /packages/{package_id}/prices
POST   /packages/{package_id}/archive
POST   /my/packages/{package_id}/archive
DELETE /packages/{package_id}
DELETE /my/packages/{package_id}
```

Package payload:

```json
{
  "name": "Home Fiber 20",
  "category": "pppoe",
  "speed_value": 20,
  "speed_unit_id": "bandwidth-unit-uuid",
  "base_price": 2500,
  "billing_cycle": "monthly",
  "capacity_type": "capped",
  "capacity_value": 100,
  "capacity_unit_id": "data-unit-uuid",
  "max_subscriptions": 50,
  "description": "Home internet package"
}
```

## ISP subscriptions and coverage

```http
GET    /my/subscriptions
PUT    /subscriptions/{subscription_id}
GET    /my/coverage
POST   /my/coverage
DELETE /my/coverage/areas/{location_id}
POST   /my/coverage/polygons
DELETE /my/coverage/polygons/{polygon_id}
DELETE /coverage/polygons/{polygon_id}
DELETE /admin/coverage/polygons/{polygon_id}
GET    /my/coverage/recommendations
```

Subscription status payload:

```json
{
  "status": "active",
  "ends_at": "2026-12-31T23:59:59Z"
}
```

Allowed status transitions are enforced by the backend.

# Part 3: Technician API

Technician endpoints require a `technician` JWT unless marked public.

## Technician profile and portfolio

```http
GET    /technicians/{technician_id}/profile
GET    /technicians/{technician_id}/reviews
GET    /technicians/{technician_id}/availability
GET    /my/profile
PUT    /my/profile
DELETE /my/profile
GET    /my/skills
POST   /my/skills
DELETE /my/skills
DELETE /my/skills/{skill}
PUT    /my/availability
POST   /my/availability/exceptions
DELETE /my/availability/exceptions
DELETE /my/availability/exceptions/{date}
PUT    /my/portfolio/profile
GET    /my/portfolio/posts
POST   /my/portfolio/posts
PUT    /my/portfolio/posts/{post_id}
DELETE /my/portfolio/posts/{post_id}
DELETE /reviews/{review_id}
DELETE /admin/reviews/{review_id}
```

Portfolio profile payload:

```json
{
  "bio": "Independent appliance and network technician",
  "experience_years": 5,
  "price_per_hour": 1500,
  "is_available": true,
  "county": "Nairobi",
  "town": "Westlands",
  "village": "Parklands",
  "skills": ["washing_machine_repair", "wifi_installation"]
}
```

Public technician profiles include `works_with`, showing company name, company technician ID, phone snapshot, and employment start/end dates.

## Independent technician requests

```http
POST /technicians/{technician_id}/requests
```

Payload:

```json
{
  "service_type": "washing_machine_repair",
  "description": "Washing machine does not drain.",
  "county": "Nairobi",
  "town": "Kasarani",
  "budget": 3000,
  "preferred_date": "2026-09-12T10:00:00Z"
}
```

This is a direct request to the individual technician and does not require an ISP.

## Matching and job applications

```http
GET    /available-jobs
POST   /jobs/{request_id}/apply
PUT    /jobs/{request_id}
POST   /matches/{match_id}/respond
DELETE /matches/{match_id}/decline
```

Application payload:

```json
{
  "message": "I can complete this installation tomorrow.",
  "proposed_price": 4500
}
```

Job status payload:

```json
{
  "status": "accepted"
}
```

## Technician vacancies

```http
POST /vacancies/{vacancy_id}/apply
```

Payload:

```json
{
  "message": "I have five years of fiber installation experience."
}
```

## Technician reviews

Customers can review independent work or company work:

```http
POST /technicians/{technician_id}/reviews
```

Independent review:

```json
{
  "rating": 5,
  "comment": "Excellent independent repair service."
}
```

Company-scoped review:

```json
{
  "rating": 5,
  "comment": "The ISP technician completed the installation professionally.",
  "company_id": "isp-user-id",
  "engagement_id": "engagement-uuid"
}
```

# Part 4: Customer API

Customer endpoints require a `customer` JWT unless marked public.

## Customer requests

```http
POST /my/jobs
GET  /my/jobs
GET  /my/jobs/{request_id}
PUT  /my/jobs/{request_id}
PATCH /my/jobs/{request_id}
DELETE /my/jobs/{request_id}
POST /my/jobs/{request_id}/terminate
```

Customer request payload:

```json
{
  "request_type": "internet",
  "mode": "direct",
  "target_isp_id": "isp-user-id",
  "service_type": "hotspot_installation",
  "description": "Install a hotspot for a small business.",
  "county": "Nairobi",
  "town": "Embakasi",
  "budget": 12000,
  "deadline_at": "2026-09-12T23:59:59Z"
}
```

For broadcast matching, use:

```json
{
  "request_type": "technician_service",
  "mode": "broadcast",
  "service_type": "wifi_installation",
  "description": "Need Wi-Fi installation.",
  "county": "Nairobi",
  "town": "Kasarani"
}
```

Terminate a request:

```json
{
  "ends_at": "2026-09-12T14:00:00Z",
  "reason": "Service no longer required"
}
```

## Package reservation and subscriptions

```http
POST /package-reservations
POST /package-reservations/{reservation_id}/subscribe
GET  /my/subscriptions
PUT  /subscriptions/{subscription_id}
```

Reservation payload:

```json
{
  "package_id": "package-uuid",
  "county": "Nairobi"
}
```

The reservation response includes:

```json
{
  "success": true,
  "data": {
    "reservation_id": "reservation-uuid",
    "expires_in": 900
  }
}
```

## Quotations

```http
GET  /quotations
GET  /quotations/{quotation_id}
POST /quotations/{quotation_id}/respond
GET  /public/quotations/{public_code}
GET  /documents/{document_id}/download
```

Only ISP/technician issuers can finalize quotations. Customers can respond to quotations.

Quotation responses may include:

```json
{
  "document": {
    "id": "document-uuid",
    "file_name": "quotation-QT-2026-00001.pdf",
    "content_type": "application/pdf",
    "visibility": "PRIVATE",
    "download_url": "/api/v1/documents/document-uuid/download"
  }
}
```

The download URL still requires a JWT. The URL itself is not a permission token.

## Customer notifications

```http
GET    /notifications
GET    /notifications/unread-count
PUT    /notifications/{notification_id}/read
PUT    /notifications/read-all
DELETE /notifications/{notification_id}
DELETE /notifications
GET    /notifications/stream
```

SSE request:

```http
Authorization: Bearer <access_token>
Accept: text/event-stream
```

Initial response:

```text
: connected

```

Notification event:

```text
event: notification
data: {"id":"...","type":"service_request","title":"New match","message":"..."}

```

The stream only emits notifications for the authenticated user.

# Part 5: Common API

## Authentication

```http
POST /auth/register
POST /auth/login
POST /auth/verify-otp
POST /auth/refresh
POST /auth/logout
```

Customer registration payload:

```json
{
  "phone": "254700000000",
  "name": "Jane Doe",
  "email": "jane@example.com"
}
```

Public registration always creates a `customer`. The client must not send or rely on a `role`, `username`, or `password` value during public registration. Attempts to register as `isp`, `technician`, or `admin` are ignored and normalized to `customer`.

Registration does not issue a JWT immediately. It sends an OTP through the configured SMS provider. The JWT is issued only after the OTP is verified:

```text
POST /auth/register
  -> customer record created
  -> SMS OTP sent
POST /auth/verify-otp
  -> OTP verified
  -> access JWT + refresh JWT issued
```

Customer login is phone-only and OTP-based:

```json
{
  "phone": "254700000000"
}
```

The response sends an OTP. Submit that OTP to `/auth/verify-otp`; successful verification returns the JWT pair.

ISP, technician, and admin login is username/password-only:

```json
{
  "username": "staff.username",
  "password": "secret"
}
```

Staff login must not include a phone number. Staff accounts are provisioned by an authorized admin/internal workflow and cannot be created through public registration.

OTP verification payload:

```json
{
  "user_id": "user-uuid",
  "otp": "482910"
}
```

Successful authentication returns:

```json
{
  "success": true,
  "data": {
    "access_token": "jwt-access-token",
    "refresh_token": "jwt-refresh-token",
    "token_type": "Bearer",
    "expires_in": 900,
    "user": {
      "id": "user-uuid",
      "role": "customer",
      "name": "Jane Doe"
    }
  }
}
```

The access token is the token used for API requests:

```http
Authorization: Bearer <access_token>
```

The refresh token is only accepted by `POST /auth/refresh`; sending it to a protected API returns `401`. The JWT contains the authenticated `user_id`, role, token use, issuer, expiry, not-before time, issued-at time, and token ID.

The frontend should store the role returned in `data.user.role` and route the user to the correct application area:

| Role | Login | Initial frontend area |
|---|---|---|
| `customer` | Phone + OTP | Customer dashboard |
| `isp` | Username + password | ISP dashboard |
| `technician` | Username + password | Technician dashboard |
| `admin` | Username + password | Admin dashboard |

Role is never selected by an untrusted public registration request. The backend role claim controls route authorization.

Refresh payload:

```json
{
  "refresh_token": "jwt-refresh-token"
}
```

Logout requires the access JWT. Logout revokes the presented token and all refresh sessions for the user.

## Public ISP and location browsing

```http
GET /isps
GET /isps/{isp_id}
GET /isps/{isp_id}/packages
GET /isps/{isp_id}/reviews
GET /isp-packages
GET /package-units
GET /geo/locations/search
GET /geo/locations/by-county
GET /geo/locations/{location_id}
GET /geo/search?q=Nairobi&type=town&limit=20
GET /geo/nearby/isps?lat=-1.28&lon=36.82&radius_km=10
GET /geo/nearby/technicians?lat=-1.28&lon=36.82&radius_km=10
GET /geo/coverage?lat=-1.28&lon=36.82
```

## Reviews

```http
POST /reviews/{review_id}/report
```

Payload:

```json
{
  "reason": "This review is abusive or fraudulent."
}
```

## Health and readiness

```http
GET /health
GET /readyz
GET /metrics
```

`/health` is a liveness endpoint. `/readyz` checks database and Redis dependencies. `/metrics` is Prometheus output and should be network-restricted in production.

## Idempotency

Mutation clients should send:

```http
Idempotency-Key: <unique-key-per-user-operation>
```

The key must not be reused for a different request body:
- **Redis HTTP Replay Middleware**: Automatic idempotency caching is provided via `pkg/idempotency.Store.Middleware`. Successful (2xx) responses are stored in Redis with a 24-hour TTL and replayed verbatim with header `Idempotency-Replayed: true` when matching keys are received.
- **PostgreSQL Idempotency Store**: Core state-changing transactions can use `pkg/idempotency.PostgresStore` (`Claim` and `Complete`) against the `idempotency_keys` table with SHA-256 request payload hashing (`HashRequest`).

## Rate limiting and request IDs

The backend may return:

```http
X-Request-ID: <request-id>
```

Include this ID in bug reports and support requests. API traffic is rate-limited per client/IP through Redis fixed-window limiters:
- **Auth endpoints (`/api/v1/auth/*`)**: 5 requests per minute.
- **Standard API endpoints (`/api/v1/*`)**: 300 requests per minute.
- **Per-phone OTP attempts**: Max 3 requests per 15-minute window (`otp_attempts:<phone>`).

When rate limits are exceeded, the API responds with `429 Too Many Requests` and a `Retry-After: <seconds>` header.

## Security Headers and CORS

All responses include defense-in-depth HTTP headers:
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: no-referrer`
- `Content-Security-Policy: default-src 'self'`
- `Strict-Transport-Security: max-age=86400; includeSubDomains; preload` (HSTS)
- Dynamic CORS origins configured via `CORS_ALLOWED_ORIGINS` with preflight `OPTIONS` handling.

## Frontend implementation notes

- Store access and refresh tokens securely; never log them.
- Refresh the access token after `401` only when the refresh token is still valid.
- Treat `403` as a permissions error, not a missing resource.
- Use SSE for live notifications and refetch `GET /notifications` after reconnecting.
- Display `deadline_at`, `started_at`, `ended_at`, `terminated_at`, and `ends_at` in the user’s local timezone.
- A null `ended_at` means the company relationship or work engagement is still active.
- A document `download_url` is protected by JWT and should be requested with the Authorization header.
- Use the server-provided `company_technician_id` when displaying a technician’s company identity; use `users.id` for API references.
