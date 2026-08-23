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

## Deletion requests

```http
GET  /admin/deletion-requests
POST /admin/approve-deletion
```

Approve payload:

```json
{
  "user_id": "user-uuid"
}
```

Approval sanitizes account data and marks the account deleted.

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

List company technicians:

```http
GET /my/technicians
```

End a company relationship:

```http
POST /my/technicians/{technician_id}/end
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
DELETE /packages/{package_id}
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
GET  /my/subscriptions
PUT  /subscriptions/{subscription_id}
GET  /my/coverage
POST /my/coverage
GET  /my/coverage/recommendations
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
GET /technicians/{technician_id}/profile
GET /technicians/{technician_id}/reviews
GET /technicians/{technician_id}/requests
PUT /my/portfolio/profile
GET /my/portfolio/posts
POST /my/portfolio/posts
PUT /my/portfolio/posts/{post_id}
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
GET /notifications
PUT /notifications/{notification_id}/read
GET /notifications/stream
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

The key must not be reused for a different request body. The backend has PostgreSQL idempotency storage; endpoint-by-endpoint replay wiring is still being completed.

## Rate limiting and request IDs

The backend may return:

```http
X-Request-ID: <request-id>
```

Include this ID in bug reports and support requests. API traffic is rate-limited per client/IP through the configured middleware.

## Frontend implementation notes

- Store access and refresh tokens securely; never log them.
- Refresh the access token after `401` only when the refresh token is still valid.
- Treat `403` as a permissions error, not a missing resource.
- Use SSE for live notifications and refetch `GET /notifications` after reconnecting.
- Display `deadline_at`, `started_at`, `ended_at`, `terminated_at`, and `ends_at` in the user’s local timezone.
- A null `ended_at` means the company relationship or work engagement is still active.
- A document `download_url` is protected by JWT and should be requested with the Authorization header.
- Use the server-provided `company_technician_id` when displaying a technician’s company identity; use `users.id` for API references.
