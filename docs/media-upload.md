# Cloudflare R2 media uploads

Ispilo uses Cloudflare R2 only. R2 access credentials stay in the Go API.
Flutter requests a short-lived SigV4 `PUT` URL, uploads bytes directly to R2,
then stores the returned `public_url` through the relevant business endpoint.

## Environment

```dotenv
R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
R2_ACCESS_KEY_ID=<server-only-key-id>
R2_SECRET_ACCESS_KEY=<server-only-secret>
R2_BUCKET=ispilo-media
R2_PUBLIC_BASE_URL=https://media.ispilo.co.ke
R2_REGION=auto
R2_FOLDER_PREFIX=ispilolite
R2_ALLOWED_HOSTS=media.ispilo.co.ke
```

Configure R2 CORS for the Flutter web origins and permit `PUT`, `GET`, and
`HEAD` with `Content-Type`. Mobile apps are not browser-CORS constrained.

## Direct upload

`POST /api/v1/media/upload-signature` with an authenticated API token:

```json
{
  "kind": "profile",
  "file_name": "avatar.jpg",
  "file_size": 248302,
  "content_type": "image/jpeg"
}
```

Response:

```json
{
  "success": true,
  "data": {
    "method": "PUT",
    "upload_url": "https://<account>.r2.cloudflarestorage.com/<bucket>/<key>?X-Amz-...",
    "public_url": "https://media.ispilo.co.ke/<key>",
    "object_key": "ispilolite/profiles/<user>/<year>/<month>/<random>.jpg",
    "headers": {"Content-Type": "image/jpeg"},
    "expires_at": 1770000600,
    "max_file_size": 5242880
  }
}
```

Flutter sends a bare `PUT` to `upload_url` with the returned headers. It must
not attach the Ispilo bearer token or R2 credentials. If direct upload is
blocked, `POST /api/v1/media/upload` accepts authenticated multipart fields
`kind` and `file`, and returns the same `public_url` contract.

## Media kinds

| Kind | Cardinality/use | Maximum |
|---|---|---:|
| `profile` | One customer/technician avatar | 5 MB |
| `isp_logo` | One ISP logo | 5 MB |
| `billing_logo` | One managed billing-system logo | 5 MB |
| `portfolio` | Up to 10 images per technician portfolio post | 15 MB each |
| `job_request` | Up to 6 customer request photos | 15 MB each |
| `installation_proof` | Up to 10 completion-proof images | 15 MB each |
| `identity_document`, `id_photo`, `document` | Single document side/file | 15 MB |
| `review` | Up to 6 review images | 5 MB each |
| `chat` | Up to 5 message images | 10 MB each |
| `promotion` | ISP promotion media | 10 MB |

Business endpoints store only R2 delivery URLs. They reject untrusted hosts,
presigned query strings, duplicate URLs, and over-limit arrays. Upload URLs are
temporary credentials and must never be stored in PostgreSQL or logs.

## Persistence endpoints

- Profile avatar: `PATCH /my/profile/fill-missing` with `avatar_url`.
- ISP logo: same endpoint with `logo_url` and `avatar_url`.
- Technician portfolio: `POST/PUT /my/portfolio/posts` with `media_urls`.
- Job/request photos: `POST /my/jobs` or direct technician request with
  `photo_urls`.
- Installation proof: `POST /jobs/:id/installation-proof` with `photo_urls`.
- Billing logo: `POST /billing-systems` or `PUT /billing-systems/:id` with
  `logo_url`.
- Identity documents: verification endpoints with the documented single URL
  fields.
