# Media uploads for Flutter and JavaScript

The API signs Cloudinary uploads. Image bytes go directly from the client to
Cloudinary, so the Go API does not buffer files and the Cloudinary API secret
never reaches the app.

If direct client upload is blocked or fails, use the server fallback endpoint:

```http
POST /api/v1/media/upload
Authorization: Bearer <access-token>
Content-Type: multipart/form-data
```

Send `kind` as a form field and the image as `file`. The server enforces the
same role and size rules, forwards the file to Cloudinary, and returns the
Cloudinary `secure_url`. This is a backup path, not the primary path; clients
should use `/media/upload-signature` first to avoid sending large files through
the API server.

## Flow

```text
Flutter / JS                Ispilo Lite API                 Cloudinary
    |                            |                              |
    | POST /media/upload-signature|                              |
    |--------------------------->|                              |
    |<-- upload_url + params ----|                              |
    |                                                           |
    | POST multipart file + signed params --------------------->|
    |<------------------------------ secure_url ----------------|
    |                                                           |
    | Send secure_url in photo_urls / media_urls / attachments  |
    |--------------------------->|                              |
```

## Server configuration

```dotenv
CLOUDINARY_CLOUD_NAME=your-cloud-name
CLOUDINARY_API_KEY=your-public-api-key
CLOUDINARY_API_SECRET=your-server-only-api-secret
CLOUDINARY_FOLDER_PREFIX=ispilolite
CLOUDINARY_ALLOWED_HOSTS=res.cloudinary.com
```

`CLOUDINARY_API_SECRET` must stay on the server. If any of the cloud name, API
key, or API secret is missing, the signature endpoint returns `503`.

## Request a signature

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

`file_size` and `content_type` are optional. Use one of these kinds:

| Kind | Who can sign | Limit |
|---|---|---|
| `profile` | customer, technician, ISP, admin | 5 MB |
| `installation_proof` | technician, ISP, admin | 15 MB |
| `portfolio` | technician, admin | 15 MB |
| `promotion` | ISP, admin | 10 MB |
| `isp_logo` | ISP, admin | 5 MB |
| `review` | customer, admin | 5 MB |
| `chat` | customer, technician, ISP, admin | 10 MB |

The response contains `upload_url`, `upload_params`, and `max_file_size`. Add
every `upload_params` entry as a multipart field, then add the file as `file`.

## Flutter

Add `http` to `pubspec.yaml` if it is not already present:

```yaml
dependencies:
  http: ^1.2.0
```

```dart
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;

class MediaUploader {
  MediaUploader({required this.apiBase, required this.accessToken});

  final String apiBase;
  final String accessToken;

  Future<String> uploadImage(File file, {required String kind}) async {
    final signatureResponse = await http.post(
      Uri.parse('$apiBase/api/v1/media/upload-signature'),
      headers: {
        'Authorization': 'Bearer $accessToken',
        'Content-Type': 'application/json',
      },
      body: jsonEncode({
        'kind': kind,
        'file_size': await file.length(),
      }),
    );

    final signatureJson = _decode(signatureResponse.body);
    if (signatureResponse.statusCode != 200) {
      throw Exception(
        signatureJson['message'] ?? 'Could not create upload signature',
      );
    }

    final data = signatureJson['data'] as Map<String, dynamic>;
    final upload = http.MultipartRequest(
      'POST',
      Uri.parse(data['upload_url'] as String),
    );
    (data['upload_params'] as Map<String, dynamic>).forEach((key, value) {
      upload.fields[key] = value.toString();
    });
    upload.files.add(await http.MultipartFile.fromPath('file', file.path));

    final streamed = await upload.send();
    final uploadBody = await streamed.stream.bytesToString();
    final uploaded = _decode(uploadBody);
    if (streamed.statusCode != 200) {
      throw Exception(
        uploaded['error']?['message'] ?? 'Cloudinary upload failed',
      );
    }
    return uploaded['secure_url'] as String;
  }

  Map<String, dynamic> _decode(String body) =>
      jsonDecode(body) as Map<String, dynamic>;
}
```

For Flutter web, use bytes instead of a path:

```dart
upload.files.add(http.MultipartFile.fromBytes(
  'file',
  await file.readAsBytes(),
  filename: file.name,
));
```

## Browser JavaScript

```js
async function uploadImage(file, apiBase, accessToken, kind = 'profile') {
  const signatureResponse = await fetch(
    `${apiBase}/api/v1/media/upload-signature`,
    {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${accessToken}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        kind,
        file_size: file.size,
        content_type: file.type,
      }),
    },
  );

  const signatureBody = await signatureResponse.json();
  if (!signatureResponse.ok) {
    throw new Error(signatureBody.message || 'Could not create upload signature');
  }

  const data = signatureBody.data;
  const form = new FormData();
  Object.entries(data.upload_params).forEach(([key, value]) => {
    form.append(key, String(value));
  });
  form.append('file', file);

  // Do not set Content-Type here; the browser adds the multipart boundary.
  const uploadResponse = await fetch(data.upload_url, {
    method: 'POST',
    body: form,
  });
  const uploaded = await uploadResponse.json();
  if (!uploadResponse.ok) {
    throw new Error(uploaded.error?.message || 'Cloudinary upload failed');
  }
  return uploaded.secure_url;
}
```

### JavaScript server-fallback upload

```js
async function uploadViaServer(file, apiBase, accessToken, kind = 'profile') {
  const form = new FormData();
  form.append('kind', kind);
  form.append('file', file);

  const response = await fetch(`${apiBase}/api/v1/media/upload`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${accessToken}` },
    body: form,
  });
  const body = await response.json();
  if (!response.ok) throw new Error(body.message || 'Server upload failed');
  return body.data.secure_url;
}
```

If the site uses a Content Security Policy, allow Cloudinary in both places:

```text
connect-src https://api.cloudinary.com
img-src     https://res.cloudinary.com
```

## After the upload

Pass the returned `secure_url` to the existing API field. For example, an
installation proof is saved with:

```http
POST /api/v1/jobs/{job_id}/installation-proof
Authorization: Bearer <technician-token>
Content-Type: application/json
```

```json
{
  "check_in_at": "2026-11-16T09:00:00Z",
  "photo_urls": [
    "https://res.cloudinary.com/your-cloud/image/upload/v1770000000/ispilolite/installation-proofs/user/photo.jpg"
  ],
  "checklist": {
    "cable_secured": true,
    "router_configured": true,
    "speed_tested": true
  },
  "device_serial": "ISP-ROUTER-001",
  "download_mbps": 24.5,
  "upload_mbps": 12.1
}
```

Never send a local file path such as `/data/user/0/...` to the API. Store only
the Cloudinary `secure_url`. The API validates that stored media URLs use an
allowed HTTPS host.
