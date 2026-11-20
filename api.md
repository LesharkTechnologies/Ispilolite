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
GET    /serviceability?county=Machakos&town=Machakos%20Town&estate=...
POST   /serviceability/waitlist
GET    /my/serviceability
POST   /my/serviceability
DELETE /my/serviceability/:id
GET    /my/serviceability/waitlist
```

The serviceability engine seamlessly unifies explicit ISP building/estate overrides with active network coverage locations (`/my/coverage`) and registered ISP counties. ISPs no longer need to duplicate data between coverage locations and serviceability records.

When customers check serviceability (`GET /serviceability`), the response returns:
- `items`: All serviceable and likely-serviceable ISPs in that area with estimated connection days and supported technologies.
- `speed_recommendation`: Crowdsourced monthly internet speed analytics for that county and town (e.g. minimum, maximum, and average download/upload speeds, latency, and human-friendly recommendations such as *"Machakos Town internet speeds range from 5.0 Mbps to 50.0 Mbps, averaging 30.0 Mbps based on 28 customer test(s) in November 2026"*).
- `count`: Total serviceable providers found.

Customers can join a waitlist (`POST /serviceability/waitlist`) when coverage has not yet reached their specific estate or building.

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

### Job Handover & Escrow Release via Dynamic QR Pairing

```http
POST /api/v1/jobs/:id/pairing-qr
POST /api/v1/my/jobs/:id/pairing-qr
GET  /api/v1/jobs/:id/pairing-status
GET  /api/v1/my/jobs/:id/pairing-status
POST /api/v1/jobs/:id/confirm-pairing
POST /api/v1/my/jobs/:id/confirm-pairing
```

When work is completed on-site, the technician generates a short-lived cryptographically signed QR code. The customer scans it with the Ispilolite customer app to verify authenticity, sign off on the job, approve the installation proof, and immediately release held escrow funds to the technician without manual intervention.

#### 1. Technician generates Pairing QR Payload
- **Endpoint**: `POST /api/v1/jobs/:id/pairing-qr` (or `/api/v1/my/jobs/:id/pairing-qr`)
- **Authorized Roles**: `technician`, `isp`, `both`, `admin`
- **Optional Request Body**:
  ```json
  {
    "expires_in_seconds": 600,
    "amount": 2500.00
  }
  ```
- **Response** (`201 Created`):
  ```json
  {
    "success": true,
    "data": {
      "job_id": "99b0c034-7ce0-4107-a363-2287c2c9d780",
      "pairing_id": "b3e02b21-...",
      "technician_id": "41da8018-...",
      "customer_id": "78fa1019-...",
      "amount": 2500.00,
      "currency": "KES",
      "verification_code": "489215",
      "expires_at": "2026-11-17T20:30:00Z",
      "expires_at_unix": 1794947400,
      "expires_in_seconds": 600,
      "signature": "sha256=a1b2c3d4e5f6...",
      "deep_link": "ispilolite://jobs/99b0c034-7ce0-4107-a363-2287c2c9d780/pair?pairing_id=...&sig=...",
      "qr_payload": "{\"app\":\"ispilolite\",\"action\":\"job_pairing\",\"job_id\":\"...\"}",
      "status": "pending"
    }
  }
  ```
- **Cryptographic Scheme**: HMAC-SHA256 signature generated over canonical message:
  `job_id=<id>|customer_id=<id>|technician_id=<id>|amount=%.2f|currency=<curr>|nonce=<pairing_id>|expires_at=<unix_timestamp>`

#### 2. Customer scans QR & Confirms Handover
- **Endpoint**: `POST /api/v1/jobs/:id/confirm-pairing` (or `/api/v1/my/jobs/:id/confirm-pairing`)
- **Authorized Roles**: `customer`, `client`, `both`, `admin` (must be the requester of the job)
- **Request Body**:
  ```json
  {
    "pairing_id": "b3e02b21-...",
    "signature": "sha256=a1b2c3d4e5f6...",
    "expires_at": 1794947400,
    "verification_code": "489215",
    "notes": "Work completed and tested",
    "rating": 5
  }
  ```
- **Response** (`200 OK`):
  ```json
  {
    "success": true,
    "message": "Job pairing confirmed and handover signed off successfully",
    "data": {
      "job_id": "99b0c034-7ce0-4107-a363-2287c2c9d780",
      "pairing_id": "b3e02b21-...",
      "status": "confirmed",
      "confirmed_at": "2026-11-17T20:25:30Z",
      "job_status": "completed",
      "escrow_released": true,
      "released_amount": 2500.00,
      "currency": "KES"
    }
  }
  ```

#### 3. Realtime Status Check & Polling
- **Endpoint**: `GET /api/v1/jobs/:id/pairing-status`
- **Response** (`200 OK`):
  ```json
  {
    "success": true,
    "data": {
      "job_id": "99b0c034-7ce0-4107-a363-2287c2c9d780",
      "pairing_id": "b3e02b21-...",
      "status": "confirmed",
      "expires_at": "2026-11-17T20:30:00Z",
      "is_expired": false,
      "confirmed_at": "2026-11-17T20:25:30Z",
      "confirmed_by": "78fa1019-...",
      "job_status": "completed",
      "escrow_released": true,
      "amount": 2500.00,
      "currency": "KES"
    }
  }
  ```


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

## Customer flags (trust and safety)

Any authenticated user can report another account or a phone number. A report is
never visible to the reported user, and the reporter sees only their own reports
plus the admin feedback.

```http
POST   /flags
GET    /my/flags?limit=50
POST   /flags/:id/cancel
GET    /admin/flags?status=&limit=50
PATCH  /admin/flags/:id
GET    /admin/monitoring?active=true&limit=50
GET    /admin/monitoring/:user_id/activity?limit=50
DELETE /admin/monitoring/:user_id
```

Report payload:

```json
{
  "reported_user_id": "<uuid>",
  "reported_phone": "+254700000000",
  "category": "harassment",
  "description": "What happened, when, and where (10-4000 characters).",
  "image_urls": [
    "https://media.ispilo.co.ke/<cloud>/image/upload/evidence-1.jpg",
    "https://media.ispilo.co.ke/<cloud>/image/upload/evidence-2.jpg",
    "https://media.ispilo.co.ke/<cloud>/image/upload/evidence-3.jpg"
  ]
}
```

- `reported_user_id` or `reported_phone` is required. A phone number is stored
  as reported only; it never creates or impersonates an account.
- `category` is one of `fraud`, `harassment`, `impersonation`, `spam`,
  `payment_scam`, `fake_listing`, `abusive_content`, `other`.
- `image_urls` accepts at most 3 Cloudflare R2 delivery URLs, using the same
  allowlist as every other media field.
- Limits: 5 reports per reporter per 24 hours, and one open report per reported
  account (duplicate open reports return `409`).

Statuses are `pending` (new), `in_progress` (being reviewed), `approved`
(upheld) and `cancelled` (withdrawn by the reporter or dismissed by an admin).
Reports are created as `pending`.

Admin review payload:

```json
{
  "status": "approved",
  "admin_feedback": "Evidence confirmed; account suspended.",
  "account_action": "suspend"
}
```

`account_action` is only valid when approving, and is one of:

| Action | Effect |
|---|---|
| `none` | Record the decision and feedback only |
| `monitor` | Add the user to the verification/monitoring list |
| `suspend` | Set the account status to `suspended` **and** add it to the monitoring list |

A suspended account cannot log in with OTP or password, and because phone
numbers are unique it also cannot re-register with the same number. Suspension
is not reversible through this endpoint; reinstating an account is a separate
administrative decision.

Monitored users generate entries in `monitored_activity` for watched actions:
`job_application` (applying to a job), `job_claim` (being assigned/invited to a
job), and `client_view` (ISP staff opening a client's job details). The IP
address is stored for abuse investigation and is returned only in
`GET /admin/monitoring/:user_id/activity`. Other users generate no rows: the
insert is a single indexed `EXISTS` probe.

`DELETE /admin/monitoring/:user_id` stops monitoring but does **not** reinstate
a suspended account.

## Identity Verification & Verified Status

Technicians and platform users upload identity documents (e.g. National ID, Passport, Business Permit) linked to the phone number they verified their account with. The administrator reviews submissions, adds/confirms the verified name (which synchronizes across user, technician, and profile records), and sets the verification badge: `ID verified` (`id_verified`), `business verified` (`business_verified`), or `both` (`both`, when user is an ISP and technician).

```http
POST   /identity/verify
POST   /technicians/identity-verification
GET    /identity/status
GET    /identity/me
GET    /admin/identity-verifications?status=&badge=&limit=20&offset=0
GET    /admin/identity-verifications/:id
POST   /admin/identity-verifications/:id/verify
POST   /admin/identity-verifications/:id/approve
POST   /admin/identity-verifications/:id/reject
```

### Submission (Technician / User)

Prerequisites: The account phone number must be verified (`users.is_verified == true`). The ID submission is automatically bound to this verified phone number.

Payload for `POST /identity/verify` or `POST /technicians/identity-verification`:
```json
{
  "id_photo_url": "https://media.ispilo.co.ke/.../id-front.jpg",
  "id_photo_back_url": "https://media.ispilo.co.ke/.../id-back.jpg",
  "document_type": "national_id",
  "id_number": "12345678",
  "notes": "National ID submission"
}
```

### Self Status Check

`GET /identity/status` or `GET /identity/me` returns current verification details, status, and badge:
```json
{
  "success": true,
  "data": {
    "id": "<verification-id>",
    "user_id": "<user-id>",
    "phone": "+254712345678",
    "status": "approved",
    "verification_badge": "id_verified",
    "badge_label": "ID verified",
    "verified_name": "John Kipkorir Tech",
    "id_photo_url": "https://media.ispilo.co.ke/.../id-front.jpg",
    "document_type": "national_id"
  }
}
```

### Admin Verification & Approval

Payload for `POST /admin/identity-verifications/:id/verify` (or `/approve`):
```json
{
  "verified_name": "John Kipkorir Tech",
  "verification_badge": "id_verified",
  "notes": "ID verified against registry records"
}
```
Supported badge values:
- `"id_verified"` (`"ID verified"`)
- `"business_verified"` (`"business verified"`)
- `"both"` (`"both"` / technician & ISP)

Upon approval:
- Sets `verified_name` and updates `name` across `users` and `technicians`.
- Sets `identity_status` and `verification_badge` to the chosen status across `users`, `technicians`, `technician_profiles`, and `isps`.
- Records audit log entry with IP, actor ID, action `UPDATE`, target `identity_verification`.

### Admin Rejection

Payload for `POST /admin/identity-verifications/:id/reject`:
```json
{
  "rejection_reason": "ID photo is blurry and unreadable",
  "notes": "Please re-upload a clear copy under good lighting"
}
```

## User Display Names & Identity Representation

Users, technicians, and ISPs can choose a custom public display name / moniker (placeholder example: *"I am Collins, but I am known as Leshark"*). The application displays this display name as the primary headline on cards and app views, while keeping their verified legal name visible in profiles under the identity tag: `identity [Collins Muthomi]`.

```http
PATCH  /my/display-name
PUT    /my/display-name
PATCH  /my/profile
PUT    /my/profile
```

### Update Display Name Payload

```json
{
  "display_name": "Leshark"
}
```

Response:
```json
{
  "success": true,
  "message": "display name updated successfully",
  "data": {
    "id": "7fa1...",
    "display_name": "Leshark",
    "name": "Collins Muthomi",
    "verified_name": "Collins Muthomi",
    "identity_label": "identity [Collins Muthomi]",
    "identity_status": "id_verified"
  }
}
```

## E-Commerce Cards & Directory Discovery

Public and mobile marketplace endpoints format ISPs, technicians, customers with jobs, and open jobs as product-like cards with icons, prices/budgets, locations, and badges.

```http
GET /technicians?county=&town=&skill=&limit=20
GET /customers/with-jobs?county=&limit=20
GET /jobs?county=&town=&service_type=&limit=20
GET /cards/isps?county=&limit=20
GET /cards/technicians?county=&town=&skill=&limit=20
GET /cards/customers-with-jobs?county=&limit=20
GET /cards/jobs?county=&town=&service_type=&limit=20
GET /cards/overview?county=&limit=10
GET /ecommerce/directory?county=&limit=10
```

### 1. Technician Cards (`GET /technicians` or `/cards/technicians`)
Returns active technicians with display name as primary title, identity tag (e.g. `identity [Collins Muthomi]`), verification badge, avatar icon, hourly rate, skills, and rating:
```json
[
  {
    "id": "<tech-id>",
    "user_id": "<user-id>",
    "name": "Collins Muthomi",
    "display_name": "Leshark",
    "primary_name": "Leshark",
    "verified_name": "Collins Muthomi",
    "identity_label": "identity [Collins Muthomi]",
    "identity_status": "id_verified",
    "badge_label": "ID verified",
    "icon_url": "https://...",
    "rating": 4.9,
    "jobs_done": 34,
    "price_per_hour": 1500,
    "skills": ["Fiber splicing", "WiFi setup"],
    "county": "Nairobi",
    "town": "Westlands",
    "is_available": true
  }
]
```

### 2. Customers with Jobs Cards (`GET /customers/with-jobs` or `/cards/customers-with-jobs`)
Returns customers who currently have active or open job requests:
```json
[
  {
    "customer_id": "<customer-id>",
    "name": "Alice Wanjiku",
    "display_name": "AliceTech",
    "primary_name": "AliceTech",
    "identity_label": "identity [Alice Wanjiku]",
    "identity_status": "id_verified",
    "icon_url": "https://...",
    "county": "Nairobi",
    "town": "Kilimani",
    "active_jobs_count": 2,
    "latest_job_title": "Fiber Installation",
    "latest_job_service": "Fiber Installation",
    "latest_job_budget": 5000
  }
]
```

### 3. Service Job Cards (`GET /jobs` or `/cards/jobs`)
Returns open jobs formatted as product cards with category icons (`fiber`, `wifi`, `cctv`, `network`, `maintenance`), budget, location, and customer identity:
```json
[
  {
    "id": "<job-id>",
    "title": "Fiber Splicing",
    "service_type": "Fiber Splicing",
    "icon_type": "fiber",
    "description": "Restore broken fiber link to home office",
    "budget": 4500,
    "currency": "KES",
    "county": "Nairobi",
    "town": "Parklands",
    "status": "open",
    "customer_id": "<cust-id>",
    "customer_name": "Alice Wanjiku",
    "customer_display_name": "AliceTech",
    "customer_primary_name": "AliceTech",
    "customer_identity": "identity [Alice Wanjiku]",
    "customer_badge": "ID verified",
    "applications_count": 3
  }
]
```

### 4. Marketplace Directory Overview (`GET /cards/overview` or `/ecommerce/directory`)
Aggregates all 4 product-card collections into a single payload for instant rendering of the app's e-commerce marketplace view:
```json
{
  "isps": [...],
  "technicians": [...],
  "customers_with_jobs": [...],
  "jobs": [...],
  "total_isps": 14,
  "total_technicians": 56,
  "total_customers": 28,
  "total_jobs": 42
}
```

## User Display Names & Identity Representation

Users, technicians, and ISPs can choose a custom public display name / moniker (placeholder example: *"I am Collins, but I am known as Leshark"*). The application displays this display name as the primary headline on cards and app views, while keeping their verified legal name visible in profiles under the identity tag: `identity [Collins Muthomi]`.

```http
PATCH  /my/display-name
PUT    /my/display-name
PATCH  /my/profile
PUT    /my/profile
```

### Update Display Name Payload

```json
{
  "display_name": "Leshark"
}
```

Response:
```json
{
  "success": true,
  "message": "display name updated successfully",
  "data": {
    "id": "7fa1...",
    "display_name": "Leshark",
    "name": "Collins Muthomi",
    "verified_name": "Collins Muthomi",
    "identity_label": "identity [Collins Muthomi]",
    "identity_status": "id_verified"
  }
}
```

## E-Commerce Cards & Directory Discovery

Public and mobile marketplace endpoints format ISPs, technicians, customers with jobs, and open jobs as product-like cards with icons, prices/budgets, locations, and badges.

```http
GET /technicians?county=&town=&skill=&limit=20
GET /customers/with-jobs?county=&limit=20
GET /jobs?county=&town=&service_type=&limit=20
GET /cards/isps?county=&limit=20
GET /cards/technicians?county=&town=&skill=&limit=20
GET /cards/customers-with-jobs?county=&limit=20
GET /cards/jobs?county=&town=&service_type=&limit=20
GET /cards/overview?county=&limit=10
GET /ecommerce/directory?county=&limit=10
```

### 1. Technician Cards (`GET /technicians` or `/cards/technicians`)
Returns active technicians with display name as primary title, identity tag (e.g. `identity [Collins Muthomi]`), verification badge, avatar icon, hourly rate, skills, and rating:
```json
[
  {
    "id": "<tech-id>",
    "user_id": "<user-id>",
    "name": "Collins Muthomi",
    "display_name": "Leshark",
    "primary_name": "Leshark",
    "verified_name": "Collins Muthomi",
    "identity_label": "identity [Collins Muthomi]",
    "identity_status": "id_verified",
    "badge_label": "ID verified",
    "icon_url": "https://...",
    "rating": 4.9,
    "jobs_done": 34,
    "price_per_hour": 1500,
    "skills": ["Fiber splicing", "WiFi setup"],
    "county": "Nairobi",
    "town": "Westlands",
    "is_available": true
  }
]
```

### 2. Customers with Jobs Cards (`GET /customers/with-jobs` or `/cards/customers-with-jobs`)
Returns customers who currently have active or open job requests:
```json
[
  {
    "customer_id": "<customer-id>",
    "name": "Alice Wanjiku",
    "display_name": "AliceTech",
    "primary_name": "AliceTech",
    "identity_label": "identity [Alice Wanjiku]",
    "identity_status": "id_verified",
    "icon_url": "https://...",
    "county": "Nairobi",
    "town": "Kilimani",
    "active_jobs_count": 2,
    "latest_job_title": "Fiber Installation",
    "latest_job_service": "Fiber Installation",
    "latest_job_budget": 5000
  }
]
```

### 3. Service Job Cards (`GET /jobs` or `/cards/jobs`)
Returns open jobs formatted as product cards with category icons (`fiber`, `wifi`, `cctv`, `network`, `maintenance`), budget, location, and customer identity:
```json
[
  {
    "id": "<job-id>",
    "title": "Fiber Splicing",
    "service_type": "Fiber Splicing",
    "icon_type": "fiber",
    "description": "Restore broken fiber link to home office",
    "budget": 4500,
    "currency": "KES",
    "county": "Nairobi",
    "town": "Parklands",
    "status": "open",
    "customer_id": "<cust-id>",
    "customer_name": "Alice Wanjiku",
    "customer_display_name": "AliceTech",
    "customer_primary_name": "AliceTech",
    "customer_identity": "identity [Alice Wanjiku]",
    "customer_badge": "ID verified",
    "applications_count": 3
  }
]
```

### 4. Marketplace Directory Overview (`GET /cards/overview` or `/ecommerce/directory`)
Aggregates all 4 product-card collections into a single payload for instant rendering of the app's e-commerce marketplace view:
```json
{
  "isps": [...],
  "technicians": [...],
  "customers_with_jobs": [...],
  "jobs": [...],
  "total_isps": 14,
  "total_technicians": 56,
  "total_customers": 28,
  "total_jobs": 42
}
```

## Account Type Switching & ISP Controls

Users can transition between account types: Customer (`customer` / `C`), Technician (`technician` / `T`), ISP (`isp` / `I`), and Dual Role (`both` / `T & I`).

### 1. User Self-Service Account Type Switch
```http
PATCH /my/account-type
POST  /my/account-type
PUT   /my/account-type
PATCH /users/account-type
```

#### Cooldown Constraint
- **Strict 24-hour rate limit**: Once an account type change occurs, the timestamp is recorded (`role_changed_at`).
- If an account type change is requested again within 24 hours, the request is rejected with `429 Too Many Requests` indicating the remaining cooldown time.
- If immediate modification is needed before the 24 hours expire, users must contact an administrator for review.

#### Request Payload
```json
{
  "account_type": "technician"
}
```
*Aliases accepted*: `"customer"` / `"c"`, `"technician"` / `"t"`, `"isp"` / `"i"`, `"both"` / `"t&i"`.

#### Successful Response (`200 OK`)
Returns the updated user profile and a freshly minted JWT token with the new role so the client can immediately continue making authenticated requests:
```json
{
  "success": true,
  "message": "account type successfully changed to technician",
  "data": {
    "user": {
      "id": "user-uuid",
      "name": "Collins Muthomi",
      "role": "technician",
      "status": "active"
    },
    "token": "eyJhbGciOi...",
    "token_type": "Bearer",
    "role": "technician",
    "role_changed_at": "2026-11-17T19:30:00Z",
    "cooldown_hours": 24
  }
}
```

#### Cooldown Rejection Response (`429 Too Many Requests`)
```json
{
  "success": false,
  "error": "Account type can only be changed once every 24 hours. Please wait 21h 45m or contact admin for review."
}
```

---

### 2. ISP Technician Access Controls & Status Updates
ISPs can manage technicians belonging to their company, including changing their user type or suspending/deactivating them to prevent system access:

```http
PATCH /my/technicians/:id/status
POST  /my/technicians/:id/suspend
POST  /my/technicians/:id/activate
PATCH /my/technicians/:id/account-type
PUT   /my/technicians/:id/account-type
```

- When status is set to `"suspended"` or `"inactive"`:
  - All active authentication/refresh sessions for that technician are immediately revoked.
  - Technician's availability in marketplace/directory searches is disabled.
  - Subsequent requests with existing JWTs are immediately rejected by authentication middleware (`401 Unauthorized: account is not active`).
- When status is set to `"active"`:
  - Account is reactivated, allowing login and technician marketplace visibility.
- ISPs can also change technician roles directly via `PATCH /my/technicians/:id/account-type`, which bypasses the standard 24h user cooldown.

---

### 3. Admin Account Overrides
Administrators have full access to switch any user's role or status at any time, bypassing cooldown restrictions:

```http
PATCH /admin/users/:id/account-type
PUT   /admin/users/:id/account-type
PATCH /admin/users/:id/role
PATCH /admin/users/:id/status
```

## Profile Completion & Uploading Missing Information

When users register or switch account types (e.g. from Customer to Technician or ISP), they often have unfulfilled fields (such as ID photos, skills, hourly rate, bio, or ISP company logo). Ispilolite provides dedicated inspection and upload endpoints so users know exactly what is missing and can submit everything in one place.

### 1. Check Missing Fields & Profile Completion Checklist
```http
GET /my/profile/completion
GET /my/missing-fields
```

Returns the completion percentage, lists what has been completed, and provides an actionable checklist of missing fields with exact target endpoints:

```json
{
  "success": true,
  "data": {
    "user_id": "user-uuid",
    "role": "technician",
    "completion_percentage": 55,
    "is_complete": false,
    "filled_count": 5,
    "total_required": 9,
    "filled_fields": [
      "phone",
      "name",
      "location"
    ],
    "missing_fields": [
      {
        "field": "display_name",
        "label": "Display Name / Moniker",
        "category": "profile",
        "description": "Public moniker shown on discovery cards (e.g. 'Leshark')",
        "endpoint": "/api/v1/my/display-name",
        "method": "PATCH"
      },
      {
        "field": "id_photo_url",
        "label": "Identity Verification Document",
        "category": "identity",
        "description": "Upload ID photo/passport or business permit to receive verified badge",
        "endpoint": "/api/v1/identity/verify",
        "method": "POST"
      },
      {
        "field": "bio",
        "label": "Technician Bio / Experience",
        "category": "services",
        "description": "Describe your experience, past installations, and specialties",
        "endpoint": "/api/v1/my/portfolio/profile",
        "method": "PUT"
      },
      {
        "field": "price_per_hour",
        "label": "Hourly Service Rate (KES)",
        "category": "services",
        "description": "Set your hourly rate for marketplace card display",
        "endpoint": "/api/v1/my/portfolio/profile",
        "method": "PUT"
      },
      {
        "field": "skills",
        "label": "Technical Skills",
        "category": "services",
        "description": "Add skills (fiber, wifi, cctv, splicing, networking)",
        "endpoint": "/api/v1/my/skills",
        "method": "POST"
      }
    ],
    "identity_status": "unverified",
    "upload_instructions": {
      "display_name": "PATCH /api/v1/my/display-name (set public moniker e.g. 'Leshark')",
      "fill_missing": "PATCH /api/v1/my/profile/fill-missing (submit all unfulfilled fields in one payload)",
      "identity_verify": "POST /api/v1/identity/verify (id_photo_url, id_photo_back_url, id_number, document_type)",
      "media_signature": "POST /api/v1/media/upload-signature (for direct Cloudflare R2 client-side upload)",
      "media_upload": "POST /api/v1/media/upload (multipart form: kind=profile, portfolio, identity_document, isp_logo)",
      "packages": "POST /api/v1/my/packages (create ISP packages)",
      "portfolio_profile": "PUT /api/v1/my/portfolio/profile (technician bio, hourly rate, skills, availability)",
      "skills": "POST /api/v1/my/skills (add specific technician skills)"
    }
  }
}
```

---

### 2. Single Unified Endpoint to Submit Missing Information
Users do not need to call 5 different endpoints; they can submit any or all unfulfilled fields in a single request:

```http
PATCH /my/profile/fill-missing
POST  /my/profile/fill-missing
PATCH /my/profile/complete
```

#### Request Payload
```json
{
  "display_name": "Leshark",
  "bio": "Experienced fiber splicing & CCTV technician with 5+ years experience across Nairobi and Kiambu.",
  "price_per_hour": 1500,
  "skills": ["Fiber splicing", "CCTV installation", "WiFi setup", "Structured cabling"],
  "county": "Nairobi",
  "town": "Westlands",
  "village": "Parklands",
  "location": {
    "lat": -1.264,
    "lng": 36.804
  },
  "id_photo_url": "https://media.ispilo.co.ke/demo/image/upload/id_front.jpg",
  "id_photo_back_url": "https://media.ispilo.co.ke/demo/image/upload/id_back.jpg",
  "id_number": "12345678",
  "document_type": "national_id"
}
```

#### Response (`200 OK`)
Returns the updated user profile and fresh completion report:
```json
{
  "success": true,
  "message": "profile information updated successfully",
  "data": {
    "user": { ... },
    "completion_report": {
      "user_id": "user-uuid",
      "role": "technician",
      "completion_percentage": 100,
      "is_complete": true,
      "filled_count": 9,
      "total_required": 9,
      "filled_fields": [ ... ],
      "missing_fields": []
    }
  }
}
```

---

### 3. File & Document Upload Endpoints

| Upload Kind | Target Media Endpoint | Allowed Types | Purpose |
|---|---|---|---|
| ID Photo / Permit | `POST /media/upload?kind=identity_document` or `id_photo` | JPEG, PNG, WEBP, HEIC | Front/back national ID, passport, or business permit |
| Profile Avatar | `POST /media/upload?kind=profile` | JPEG, PNG, WEBP | User avatar |
| ISP Logo | `POST /media/upload?kind=isp_logo` | JPEG, PNG, WEBP | Company logo for marketplace cards |
| Work Portfolio | `POST /media/upload?kind=portfolio` | JPEG, PNG, WEBP | Past installation photos |
| Cloudflare R2 Signature | `POST /media/upload-signature` | JSON | Direct browser-to-cloud signed upload |

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

## Media uploads (Cloudflare R2 signed uploads)

The API does not receive image bytes. It issues a signed upload request scoped
to the authenticated user's folder; the client uploads the file directly to
Cloudflare R2 from Flutter or browser JavaScript, then sends the returned
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
Cloudflare R2 endpoint and the exact multipart fields to submit with the file:

```json
{
  "success": true,
  "data": {
    "kind": "installation_proof",
    "cloud_name": "your-cloud-name",
    "upload_url": "https://<account-id>.r2.cloudflarestorage.com/v1_1/your-cloud-name/image/upload",
    "folder": "ispilolite/installation-proofs/550e8400-e29b-41d4-a716-446655440000",
    "timestamp": 1770000000,
    "expires_at": 1770000600,
    "max_file_size": 15728640,
    "allowed_formats": ["jpg", "jpeg", "png", "webp", "heic", "heif"],
    "upload_params": {
      "api_key": "your-public-api-key",
      "allowed_formats": "jpg,jpeg,png,webp,heic,heif",
      "folder": "ispilolite/installation-proofs/550e8400-e29b-41d4-a716-446655440000",
      "signature": "r2-signature",
      "timestamp": "1770000000"
    }
  }
}
```

Submit every `upload_params` entry as a multipart field, add the file as
`file`, and POST to `upload_url`. The response is a standard Cloudflare R2 upload
object; store its `secure_url`, never a temporary local path.

| `kind` | Allowed roles | Max file size | Cloudflare R2 folder |
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
upload endpoint returns `503` until `R2_ENDPOINT`,
`R2_ACCESS_KEY_ID`, and `R2_SECRET_ACCESS_KEY` are configured.

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
| View single test | `GET /api/v1/my/speed-tests/{id}` | Authenticated user |
| Delete single test | `DELETE /api/v1/my/speed-tests/{id}` | Authenticated user |
| ISP dashboard stats | `GET /api/v1/my/speed-tests/stats?county=&town=` | ISP only |
| Regional analytics & recommendations | `GET /api/v1/geo/speed-stats?county=Machakos&town=Machakos%20Town&year=2026&month=11` | Public |
| Town-by-town speed breakdown | `GET /api/v1/geo/speed-stats/towns?county=Machakos` | Public |
| County benchmarks (all Kenya) | `GET /api/v1/geo/county-speed-benchmarks?year=2026&month=11` | Public |
| Moderation queue | `GET /api/v1/admin/speed-tests?status=pending` | Admin only |
| Moderate result | `PATCH /api/v1/admin/speed-tests/{id}` | Admin only |
| Aggregate monthly statistics | `POST /api/v1/admin/speed-tests/aggregate` | Admin only |

Results are private to the submitting user by default, stored individually with the exact test execution timestamp and user ID. New submissions start
as `pending`; only moderated `accepted` tests should be used for official public
statistics. `include_in_public_stats` defaults to `false`, so a user can save a
private result without affecting public rankings.

Every submission captures the trusted client IP resolved using
`TRUSTED_PROXIES`. The raw IP is retained in the restricted speed-test table
for abuse investigation and moderation; it is never returned in user history,
ISP dashboard results, public statistics, or rankings. ISP dashboards receive
aggregate counts and accepted-test averages for tests attributed to their
registered ISP, without customer identity or IP addresses.

### Regional Speed Benchmarks & Customer Recommendations
The platform aggregates speed tests monthly across counties and towns to power customer ISP recommendations.
For example, a check in Machakos Town returns:
```json
{
  "success": true,
  "data": {
    "stats": {
      "county": "Machakos",
      "town": "Machakos Town",
      "year": 2026,
      "month": 11,
      "sample_count": 42,
      "min_download_mbps": 5.0,
      "max_download_mbps": 50.0,
      "avg_download_mbps": 30.0,
      "avg_upload_mbps": 14.5,
      "avg_latency_ms": 18.2,
      "recommendation_summary": "Machakos Town, Machakos internet speeds range from 5.0 Mbps to 50.0 Mbps, averaging 30.0 Mbps based on 42 customer speed test(s) in November 2026."
    }
  }
}
```

County benchmarks compare overall county performance across Kenya:
- Mombasa: avg 80 Mbps
- Kisumu: avg 56 Mbps
- Nairobi: avg 50 Mbps
- Machakos: avg 30 Mbps

Migrations `0041_speed_test_history.sql` and `0042_monthly_regional_speed_stats.sql` must be applied before deploying the speed-test and regional analytics APIs.

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

## Audit logs and Audit Officer Portal

```http
GET /admin/audit?event=user.login&limit=100
GET /admin/audit?category=client&status=success
GET /admin/audit?format=html
GET /admin/database/overview
```

Audit records capture all main system activities including:
- **IP Address**: Source client IP (`ip_address`)
- **User ID**: Identified actor (`user_id` / `actor_id`)
- **Actor Identity**: Who did this (`actor_name`, `actor_username`, `actor_email`, `actor_phone`)
- **User Category**: Role classification (`user_category`: `admin`, `client`, `isp`, `technicians`)
- **Event**: Specific activity (`event`, e.g. `user.login`, `job.create`, `payment.initiate`, `installation.requested`, `dispute.create`)
- **Action**: High-level verb (`action`: `CREATE`, `UPDATE`, `DELETE`, `GET`, `LOGIN`, `REGISTER`, `REQUEST`, `EXPORT`, `SYSTEM`)
- **Status**: Execution outcome (`status`: `SUCCESS` or `FAILED`, boolean `success`)
- **Timestamp**: Time of occurrence (`time` and `occurred_at`)
- **Description**: Detailed human-readable description of what transpired

Supported query parameters on `/admin/audit`:
- `category` or `role`: `admin`, `client` (customer), `isp`, `technicians`
- `status`: `success` or `failed`
- `event`: filter by event name (substring match)
- `user_id`: filter by actor UUID
- `ip`: filter by IP address
- `q`: full-text search across descriptions, actor names, usernames, and IP addresses
- `start` / `end`: RFC3339 timestamps for time range
- `limit` (default 50) and `offset`
- `format=html`: renders the responsive Audit Officer Portal HTML Dashboard for direct browser inspection

Super Administrator Credentials (.env):
Administrators can log in via `/api/v1/auth/login` using `ADMIN_USERNAME` and `ADMIN_PASSWORD` defined in `.env`.
The admin user is automatically provisioned and synchronized with full super-privileges to access all system data, database overviews (`/admin/database/overview`), and any customer/ISP/technician endpoints.

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
POST   /admin/sms
GET    /admin/sms/balance
GET    /admin/sms/profile
GET    /admin/sms/contacts
POST   /admin/sms/contacts/:group_id
POST   /admin/critical-alerts
```

`GET /admin/sms/balance` queries the TalkSasa API and returns current remaining SMS balance (e.g. `{"remaining_balance": "Ksh67"}`).
`GET /admin/sms/profile` queries the TalkSasa account profile (`/me`).
`GET /admin/sms/contacts` lists TalkSasa contact groups.
`POST /admin/sms/contacts/:group_id` stores a contact into a TalkSasa group (`{"phone": "2547...", "first_name": "...", "last_name": "..."}`).

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

The OTP in the SMS is the same server-generated code stored in Redis for that
user. Codes are persisted only as keyed HMAC-SHA256 digests (`OTP_HASH_KEY`,
falling back to `JWT_SECRET`); the plaintext code exists only in the SMS
message and the verification request. Verification uses constant-time
comparison. A new code replaces the previous code only after the resend cooldown.
OTP requests are protected by a 30-second per-phone cooldown, a maximum of 3
requests per phone in 15 minutes, and a maximum of 10 requests per source IP
in 15 minutes. Verification guesses are limited to 5 attempts per active code.
The server returns `429 Too Many Requests` with `Retry-After` when these limits
are reached. Clients should disable the resend button during the cooldown and
must not generate or compare OTP values locally.

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
- **OTP resend cooldown**: 30 seconds per phone.
- **Per-phone OTP requests**: Max 3 requests per 15-minute window.
- **Per-source-IP OTP requests**: Max 10 requests per 15-minute window.
- **OTP verification guesses**: Max 5 attempts per active code.

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
